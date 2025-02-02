// Copyright 2015 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package dscache

import (
	"bytes"

	ds "github.com/luci/gae/service/datastore"
	mc "github.com/luci/gae/service/memcache"
	"github.com/luci/luci-go/common/errors"
	"github.com/luci/luci-go/common/logging"
	"golang.org/x/net/context"
)

type facts struct {
	getKeys   []*ds.Key
	getMeta   ds.MultiMetaGetter
	lockItems []mc.Item
	nonce     []byte
}

type plan struct {
	keepMeta bool

	// idxMap maps from the original list of keys to the reduced set of keys in
	// toGet. E.g. given the index `j` while looping over toGet, idxMap[j] will
	// equal the index in the original facts.getKeys list.
	idxMap []int

	// toGet is a list of datstore keys to retrieve in the call down to the
	// underlying datastore. It will have length >= facts.getKeys. All the keys
	// in this will be valid (not nil).
	toGet []*ds.Key

	// toGetMeta is a MultiMetaGetter to be passed in the call down to the
	// underlying datastore.GetMulti. It has the same length as toGet.
	toGetMeta ds.MultiMetaGetter

	// toSave is the list of memcache items to save the results from the
	// underlying datastore.GetMulti. It MAY contain nils, which is an indicator
	// that this entry SHOULD NOT be saved to memcache.
	toSave []mc.Item

	// decoded is a list of all the decoded property maps. Its length always ==
	// len(facts.getKeys). After the plan is formed, it may contain nils. These
	// nils will be filled in from the underlying datastore.GetMulti call in
	// ds.go.
	decoded []ds.PropertyMap

	// lme is a LazyMultiError whose target Size == len(facts.getKeys). The errors
	// will eventually bubble back to the layer above this filter in callbacks.
	lme errors.LazyMultiError
}

// add adds a new entry to be retrieved from the actual underlying datastore
// (via GetMulti).
//
//   - idx is the index into the original facts.getKeys
//   - get and m are the pair of values that will be passed to datastore.GetMulti
//   - save is the memcache item to save the result back to. If it's nil, then
//     it will not be saved back to memcache.
func (p *plan) add(idx int, get *ds.Key, m ds.MetaGetter, save mc.Item) {
	p.idxMap = append(p.idxMap, idx)
	p.toGet = append(p.toGet, get)

	p.toSave = append(p.toSave, save)

	if p.keepMeta {
		p.toGetMeta = append(p.toGetMeta, m)
	}
}

func (p *plan) empty() bool {
	return len(p.idxMap) == 0
}

// makeFetchPlan takes the input facts and makes a plan about what to do with them.
//
// Possible scenarios:
//   * all entries we got from memcache are valid data, and so we don't need
//     to call through to the underlying datastore at all.
//   * some entries are 'lock' entries, owned by us, and so we should get them
//     from datastore and then attempt to save them back to memcache.
//   * some entries are 'lock' entries, owned by something else, so we should
//     get them from datastore and then NOT save them to memcache.
//
// Or some combination thereof. This also handles memcache enries with invalid
// data in them, cases where items have caching disabled entirely, etc.
func makeFetchPlan(c context.Context, aid, ns string, f *facts) *plan {
	p := plan{
		keepMeta: f.getMeta != nil,
		decoded:  make([]ds.PropertyMap, len(f.lockItems)),
		lme:      errors.NewLazyMultiError(len(f.lockItems)),
	}
	for i, lockItm := range f.lockItems {
		m := f.getMeta.GetSingle(i)
		getKey := f.getKeys[i]

		if lockItm == nil {
			// this item wasn't cacheable (e.g. the model had caching disabled,
			// shardsForKey returned 0, etc.)
			p.add(i, getKey, m, nil)
			continue
		}

		switch FlagValue(lockItm.Flags()) {
		case ItemHasLock:
			if bytes.Equal(f.nonce, lockItm.Value()) {
				// we have the lock
				p.add(i, getKey, m, lockItm)
			} else {
				// someone else has the lock, don't save
				p.add(i, getKey, m, nil)
			}

		case ItemHasData:
			pmap, err := decodeItemValue(lockItm.Value(), aid, ns)
			switch err {
			case nil:
				p.decoded[i] = pmap
			case ds.ErrNoSuchEntity:
				p.lme.Assign(i, ds.ErrNoSuchEntity)
			default:
				(logging.Fields{"error": err}).Warningf(c,
					"dscache: error decoding %s, %s", lockItm.Key(), getKey)
				p.add(i, getKey, m, nil)
			}

		default:
			// have some other sort of object, or our AddMulti failed to add this item.
			p.add(i, getKey, m, nil)
		}
	}
	return &p
}

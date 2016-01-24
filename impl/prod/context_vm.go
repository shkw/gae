// Copyright 2015 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// +build !appengine

package prod

import (
	"golang.org/x/net/context"
	"google.golang.org/appengine"
)

// UseBackground adds production implementations for all the gae services
// to the context not associated with a request. This only works on Managed VMs.
//
// The services added are:
//   - github.com/luci-go/common/logging
//   - github.com/tetrafolium/gae/service/datastore
//   - github.com/tetrafolium/gae/service/info
//   - github.com/tetrafolium/gae/service/mail
//   - github.com/tetrafolium/gae/service/memcache
//   - github.com/tetrafolium/gae/service/taskqueue
//   - github.com/tetrafolium/gae/service/urlfetch
//   - github.com/tetrafolium/gae/service/user
//
// These can be retrieved with the <service>.Get functions.
//
// The implementations are all backed by the real appengine SDK functionality,
func UseBackground(c context.Context) context.Context {
	return setupAECtx(c, appengine.BackgroundContext())
}

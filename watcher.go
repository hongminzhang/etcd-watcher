// Copyright 2017 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcdwatcher

import (
	"context"
	"log"
	"runtime"
	"strconv"
	"time"

	"github.com/casbin/casbin/persist"
	"github.com/etcd-io/etcd/client"
	"github.com/etcd-io/etcd/clientv3"
)

type Watcher struct {
	endpoint string
	client   *clientv3.Client
	running  bool
	callback func(string)
}

// finalizer is the destructor for Watcher.
func finalizer(w *Watcher) {
	w.running = false
}

// NewWatcher is the constructor for Watcher.
// endpoint is the endpoint for etcd clusters.
func NewWatcher(endpoint string) (persist.Watcher, error) {
	w := &Watcher{}
	w.endpoint = endpoint
	w.running = true
	w.callback = nil

	// Create the client.
	err := w.createClient()
	if err != nil {
		return nil, err
	}

	// Call the destructor when the object is released.
	runtime.SetFinalizer(w, finalizer)

	go w.startWatch()

	return w, nil
}

// Close closes the Watcher.
func (w *Watcher) Close() {
	finalizer(w)
}

func (w *Watcher) createClient() error {
	cfg := clientv3.Config{
		Endpoints: []string{w.endpoint},
		// set timeout per request to fail fast when the target endpoint is unavailable
		DialKeepAliveTimeout: time.Second * 10,
		DialTimeout:          time.Second * 30,
	}

	c, err := clientv3.New(cfg)
	if err != nil {
		return err
	}
	w.client = c
	return nil
}

// SetUpdateCallback sets the callback function that the watcher will call
// when the policy in DB has been changed by other instances.
// A classic callback is Enforcer.LoadPolicy().
func (w *Watcher) SetUpdateCallback(callback func(string)) error {
	w.callback = callback
	return nil
}

// Update calls the update callback of other instances to synchronize their policy.
// It is usually called after changing the policy in DB, like Enforcer.SavePolicy(),
// Enforcer.AddPolicy(), Enforcer.RemovePolicy(), etc.
func (w *Watcher) Update() error {
	rev := 0
	// Get "/casbin" key's value.
	resp, err := w.client.Get(context.Background(), "/casbin")
	if err != nil {
		log.Println(err)
		switch err := err.(type) {
		case client.Error:
			if err.Code != client.ErrorCodeKeyNotFound {
				return err
			}
		case *client.ClusterError:
			return err
		}
	} else {
		if resp.Count != 0 {
			rev, err = strconv.Atoi(string(resp.Kvs[0].Value))
			if err != nil {
				return err
			}
			log.Println("Get revision: ", rev)
			rev += 1
		}
	}

	newRev := strconv.Itoa(rev)

	// Set "/casbin" key with new revision value.
	log.Println("Set revision: ", newRev)
	_, err = w.client.Put(context.TODO(), "/casbin", newRev)
	return err
}

// startWatch is a goroutine that watches the policy change.
func (w *Watcher) startWatch() error {
	watcher := w.client.Watch(context.Background(), "/casbin")
	for res := range watcher {
		t := res.Events[0]
		if t.IsCreate() || t.IsModify() {
			if w.callback != nil {
				w.callback(string(t.Kv.Value))
			}
		}

	}
	return nil
}

/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package persistentstorage

import (

	"testing"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
)


type FakeWatcher struct {
	w *watch.FakeWatcher
	*client.Fake
}

func TestWatchControllers(t *testing.T) {
	fakeWatch := watch.NewFake()
	client := &client.Fake{Watch: fakeWatch}
	controller := NewPersistentVolumeBindingController(client)
	var testPersistentVolume api.PersistentVolume
	received := make(chan struct{})
	controller.watchHandler = func(pv api.PersistentVolume) error {
			if !api.Semantic.DeepEqual(pv, testPersistentVolume) {
				t.Errorf("Expected %#v, but got %#v", testPersistentVolume, pv)
			}
			close(received)
			return nil
		}

	resourceVersion := ""
	go controller.watchPersistentVolumes(&resourceVersion)

	// Test normal case
	testPersistentVolume.Name = "foo"
	fakeWatch.Add(&testPersistentVolume)

	select {
	case <-received:
	case <-time.After(10 * time.Millisecond):
	t.Errorf("Expected 1 call but got 0")
	}
}

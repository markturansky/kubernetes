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

package client

import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

// FakePods implements PodsInterface. Meant to be embedded into a struct to get a default
// implementation. This makes faking out just the methods you want to test easier.
type FakePersistentStorageDevices struct {
	Fake      *Fake
	Namespace string
}

func (c *FakePersistentStorageDevices) List(selector labels.Selector) (*api.PersistentStorageDeviceList, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "list-persistentStorageDevices"})
	return api.Scheme.CopyOrDie(&c.Fake.PersistentStorageDeviceList).(*api.PersistentStorageDeviceList), nil
}

func (c *FakePersistentStorageDevices) Get(name string) (*api.PersistentStorageDevice, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "get-persistentStorageDevice", Value: name})
	return &api.PersistentStorageDevice{ObjectMeta: api.ObjectMeta{Name: name, Namespace: c.Namespace}}, nil
}

func (c *FakePersistentStorageDevices) Delete(name string) error {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "delete-persistentStorageDevice", Value: name})
	return nil
}

func (c *FakePersistentStorageDevices) Create(persistentstoragedevice *api.PersistentStorageDevice) (*api.PersistentStorageDevice, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "create-persistentStorageDevice"})
	return &api.PersistentStorageDevice{}, nil
}

func (c *FakePersistentStorageDevices) Update(persistentstoragedevice *api.PersistentStorageDevice) (*api.PersistentStorageDevice, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "update-persistentStorageDevice", Value: persistentstoragedevice.Name})
	return &api.PersistentStorageDevice{}, nil
}

func (c *FakePersistentStorageDevices) Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return nil, nil
}

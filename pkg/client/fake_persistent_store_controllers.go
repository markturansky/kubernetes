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
type FakePersistentVolumeControllers struct {
	Fake      *Fake
	Namespace string
}

func (c *FakePersistentVolumeControllers) List(selector labels.Selector) (*api.PersistentVolumeControllerList, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "list-persistentVolumeControllers"})
	return api.Scheme.CopyOrDie(&c.Fake.PersistentVolumeControllerList).(*api.PersistentVolumeControllerList), nil
}

func (c *FakePersistentVolumeControllers) Get(name string) (*api.PersistentVolumeController, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "get-persistentVolumeController", Value: name})
	return &api.PersistentVolumeController{ObjectMeta: api.ObjectMeta{Name: name, Namespace: c.Namespace}}, nil
}

func (c *FakePersistentVolumeControllers) Delete(name string) error {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "delete-persistentVolumeController", Value: name})
	return nil
}

func (c *FakePersistentVolumeControllers) Create(persistentvolumecontroller *api.PersistentVolumeController) (*api.PersistentVolumeController, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "create-persistentVolumeController"})
	return &api.PersistentVolumeController{}, nil
}

func (c *FakePersistentVolumeControllers) Update(persistentvolumecontroller *api.PersistentVolumeController) (*api.PersistentVolumeController, error) {
	c.Fake.Actions = append(c.Fake.Actions, FakeAction{Action: "update-persistentVolumeController", Value: persistentvolumecontroller.Name})
	return &api.PersistentVolumeController{}, nil
}

func (c *FakePersistentVolumeControllers) Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return nil, nil
}

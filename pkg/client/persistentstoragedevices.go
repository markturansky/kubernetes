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
	"errors"
	"fmt"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

// PersistentStorageDevicesNamespacer has methods to work with PersistentStorageDevice resources in a namespace
type PersistentStorageDevicesNamespacer interface {
	PersistentStorageDevices(namespace string) PersistentStorageDeviceInterface
}

// PersistentStorageDeviceInterface has methods to work with PersistentStorageDevice resources.
type PersistentStorageDeviceInterface interface {
	List(selector labels.Selector) (*api.PersistentStorageDeviceList, error)
	Get(name string) (*api.PersistentStorageDevice, error)
	Create(ctrl *api.PersistentStorageDevice) (*api.PersistentStorageDevice, error)
	Update(ctrl *api.PersistentStorageDevice) (*api.PersistentStorageDevice, error)
	Delete(name string) error
	Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error)
}

// persistentStorageDevices implements PersistentStorageDevicesNamespacer interface
type persistentStorageDevices struct {
	r  *Client
	ns string
}

// newPersistentStorageDevices returns a PodsClient
func newPersistentStorageDevices(c *Client, namespace string) *persistentStorageDevices {
	return &persistentStorageDevices{c, namespace}
}

// List takes a selector, and returns the list of replication controllers that match that selector.
func (c *persistentStorageDevices) List(selector labels.Selector) (result *api.PersistentStorageDeviceList, err error) {
	result = &api.PersistentStorageDeviceList{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentStorageDevices").SelectorParam("labels", selector).Do().Into(result)
	return
}

// Get returns information about a particular replication controller.
func (c *persistentStorageDevices) Get(name string) (result *api.PersistentStorageDevice, err error) {
	if len(name) == 0 {
		return nil, errors.New("name is required parameter to Get")
	}

	result = &api.PersistentStorageDevice{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentStorageDevices").Name(name).Do().Into(result)
	return
}

// Create creates a new replication controller.
func (c *persistentStorageDevices) Create(controller *api.PersistentStorageDevice) (result *api.PersistentStorageDevice, err error) {
	result = &api.PersistentStorageDevice{}
	err = c.r.Post().Namespace(c.ns).Resource("persistentStorageDevices").Body(controller).Do().Into(result)
	return
}

// Update updates an existing replication controller.
func (c *persistentStorageDevices) Update(controller *api.PersistentStorageDevice) (result *api.PersistentStorageDevice, err error) {
	result = &api.PersistentStorageDevice{}
	if len(controller.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", controller)
		return
	}
	err = c.r.Put().Namespace(c.ns).Resource("persistentStorageDevices").Name(controller.Name).Body(controller).Do().Into(result)
	return
}

// Delete deletes an existing replication controller.
func (c *persistentStorageDevices) Delete(name string) error {
	return c.r.Delete().Namespace(c.ns).Resource("persistentStorageDevices").Name(name).Do().Error()
}

// Watch returns a watch.Interface that watches the requested controllers.
func (c *persistentStorageDevices) Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return c.r.Get().
		Prefix("watch").
		Namespace(c.ns).
		Resource("persistentStorageDevices").
		Param("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

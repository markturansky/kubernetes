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

// PersistentVolumesNamespacer has methods to work with PersistentVolume resources in a namespace
type PersistentVolumesNamespacer interface {
	PersistentVolumes(namespace string) PersistentVolumeInterface
}

// PersistentVolumeInterface has methods to work with PersistentVolume resources.
type PersistentVolumeInterface interface {
	List(selector labels.Selector) (*api.PersistentVolumeList, error)
	Get(name string) (*api.PersistentVolume, error)
	Create(ctrl *api.PersistentVolume) (*api.PersistentVolume, error)
	Update(ctrl *api.PersistentVolume) (*api.PersistentVolume, error)
	Delete(name string) error
	Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error)
}

// persistentVolumes implements PersistentVolumesNamespacer interface
type persistentVolumes struct {
	r  *Client
	ns string
}

// newPersistentVolumes returns a PodsClient
func newPersistentVolumes(c *Client, namespace string) *persistentVolumes {
	return &persistentVolumes{c, namespace}
}

// List takes a selector, and returns the list of replication controllers that match that selector.
func (c *persistentVolumes) List(selector labels.Selector) (result *api.PersistentVolumeList, err error) {
	result = &api.PersistentVolumeList{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentVolumes").SelectorParam("labels", selector).Do().Into(result)
	return
}

// Get returns information about a particular replication controller.
func (c *persistentVolumes) Get(name string) (result *api.PersistentVolume, err error) {
	if len(name) == 0 {
		return nil, errors.New("name is required parameter to Get")
	}

	result = &api.PersistentVolume{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentVolumes").Name(name).Do().Into(result)
	return
}

// Create creates a new replication controller.
func (c *persistentVolumes) Create(controller *api.PersistentVolume) (result *api.PersistentVolume, err error) {
	result = &api.PersistentVolume{}
	err = c.r.Post().Namespace(c.ns).Resource("persistentVolumes").Body(controller).Do().Into(result)
	return
}

// Update updates an existing replication controller.
func (c *persistentVolumes) Update(controller *api.PersistentVolume) (result *api.PersistentVolume, err error) {
	result = &api.PersistentVolume{}
	if len(controller.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", controller)
		return
	}
	err = c.r.Put().Namespace(c.ns).Resource("persistentVolumes").Name(controller.Name).Body(controller).Do().Into(result)
	return
}

// Delete deletes an existing replication controller.
func (c *persistentVolumes) Delete(name string) error {
	return c.r.Delete().Namespace(c.ns).Resource("persistentVolumes").Name(name).Do().Error()
}

// Watch returns a watch.Interface that watches the requested controllers.
func (c *persistentVolumes) Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return c.r.Get().
		Prefix("watch").
		Namespace(c.ns).
		Resource("persistentVolumes").
		Param("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

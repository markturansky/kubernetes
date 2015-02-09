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

// PersistentVolumeClaimsNamespacer has methods to work with PersistentVolumeClaim resources in a namespace
type PersistentVolumeClaimsNamespacer interface {
	PersistentVolumeClaims(namespace string) PersistentVolumeClaimInterface
}

// PersistentVolumeClaimInterface has methods to work with PersistentVolumeClaim resources.
type PersistentVolumeClaimInterface interface {
	List(selector labels.Selector) (*api.PersistentVolumeClaimList, error)
	Get(name string) (*api.PersistentVolumeClaim, error)
	Create(ctrl *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
	Update(ctrl *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
	Delete(name string) error
	Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error)
}

// persistentVolumeClaims implements PersistentVolumeClaimsNamespacer interface
type persistentVolumeClaims struct {
	r  *Client
	ns string
}

// newPersistentVolumeClaims returns a PodsClient
func newPersistentVolumeClaims(c *Client, namespace string) *persistentVolumeClaims {
	return &persistentVolumeClaims{c, namespace}
}

// List takes a selector, and returns the list of replication controllers that match that selector.
func (c *persistentVolumeClaims) List(selector labels.Selector) (result *api.PersistentVolumeClaimList, err error) {
	result = &api.PersistentVolumeClaimList{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentVolumeClaims").SelectorParam("labels", selector).Do().Into(result)
	return
}

// Get returns information about a particular replication controller.
func (c *persistentVolumeClaims) Get(name string) (result *api.PersistentVolumeClaim, err error) {
	if len(name) == 0 {
		return nil, errors.New("name is required parameter to Get")
	}

	result = &api.PersistentVolumeClaim{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentVolumeClaims").Name(name).Do().Into(result)
	return
}

// Create creates a new replication controller.
func (c *persistentVolumeClaims) Create(controller *api.PersistentVolumeClaim) (result *api.PersistentVolumeClaim, err error) {
	result = &api.PersistentVolumeClaim{}
	err = c.r.Post().Namespace(c.ns).Resource("persistentVolumeClaims").Body(controller).Do().Into(result)
	return
}

// Update updates an existing replication controller.
func (c *persistentVolumeClaims) Update(controller *api.PersistentVolumeClaim) (result *api.PersistentVolumeClaim, err error) {
	result = &api.PersistentVolumeClaim{}
	if len(controller.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", controller)
		return
	}
	err = c.r.Put().Namespace(c.ns).Resource("persistentVolumeClaims").Name(controller.Name).Body(controller).Do().Into(result)
	return
}

// Delete deletes an existing replication controller.
func (c *persistentVolumeClaims) Delete(name string) error {
	return c.r.Delete().Namespace(c.ns).Resource("persistentVolumeClaims").Name(name).Do().Error()
}

// Watch returns a watch.Interface that watches the requested controllers.
func (c *persistentVolumeClaims) Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return c.r.Get().
		Prefix("watch").
		Namespace(c.ns).
		Resource("persistentVolumeClaims").
		Param("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

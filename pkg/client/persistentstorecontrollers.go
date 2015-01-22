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

// PersistentVolumeControllersNamespacer has methods to work with PersistentVolumeController resources in a namespace
type PersistentVolumeControllersNamespacer interface {
	PersistentVolumeControllers(namespace string) PersistentVolumeControllerInterface
}

// PersistentVolumeControllerInterface has methods to work with PersistentVolumeController resources.
type PersistentVolumeControllerInterface interface {
	List(selector labels.Selector) (*api.PersistentVolumeControllerList, error)
	Get(name string) (*api.PersistentVolumeController, error)
	Create(ctrl *api.PersistentVolumeController) (*api.PersistentVolumeController, error)
	Update(ctrl *api.PersistentVolumeController) (*api.PersistentVolumeController, error)
	Delete(name string) error
	Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error)
}

// persistentVolumeControllers implements PersistentVolumeControllersNamespacer interface
type persistentVolumeControllers struct {
	r  *Client
	ns string
}

// newPersistentVolumeControllers returns a PodsClient
func newPersistentVolumeControllers(c *Client, namespace string) *persistentVolumeControllers {
	return &persistentVolumeControllers{c, namespace}
}

// List takes a selector, and returns the list of replication controllers that match that selector.
func (c *persistentVolumeControllers) List(selector labels.Selector) (result *api.PersistentVolumeControllerList, err error) {
	result = &api.PersistentVolumeControllerList{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentVolumeControllers").SelectorParam("labels", selector).Do().Into(result)
	return
}

// Get returns information about a particular replication controller.
func (c *persistentVolumeControllers) Get(name string) (result *api.PersistentVolumeController, err error) {
	if len(name) == 0 {
		return nil, errors.New("name is required parameter to Get")
	}

	result = &api.PersistentVolumeController{}
	err = c.r.Get().Namespace(c.ns).Resource("persistentVolumeControllers").Name(name).Do().Into(result)
	return
}

// Create creates a new replication controller.
func (c *persistentVolumeControllers) Create(controller *api.PersistentVolumeController) (result *api.PersistentVolumeController, err error) {
	result = &api.PersistentVolumeController{}
	err = c.r.Post().Namespace(c.ns).Resource("persistentVolumeControllers").Body(controller).Do().Into(result)
	return
}

// Update updates an existing replication controller.
func (c *persistentVolumeControllers) Update(controller *api.PersistentVolumeController) (result *api.PersistentVolumeController, err error) {
	result = &api.PersistentVolumeController{}
	if len(controller.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", controller)
		return
	}
	err = c.r.Put().Namespace(c.ns).Resource("persistentVolumeControllers").Name(controller.Name).Body(controller).Do().Into(result)
	return
}

// Delete deletes an existing replication controller.
func (c *persistentVolumeControllers) Delete(name string) error {
	return c.r.Delete().Namespace(c.ns).Resource("persistentVolumeControllers").Name(name).Do().Error()
}

// Watch returns a watch.Interface that watches the requested controllers.
func (c *persistentVolumeControllers) Watch(label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return c.r.Get().
		Prefix("watch").
		Namespace(c.ns).
		Resource("persistentVolumeControllers").
		Param("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

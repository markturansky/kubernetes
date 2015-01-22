/*
Copyright 2015 Google Inc. All rights reserved.

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

package persistentstoragedevice

import (
	"fmt"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/validation"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/generic"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

// REST implements the RESTStorage interface in terms of a PodRegistry.
type REST struct {
	registry generic.Registry
}

type RESTConfig struct {
	Registry generic.Registry
}

// NewREST returns a new REST.
func NewREST(registry generic.Registry) *REST {
	return &REST{
		registry: registry,
	}
}

func (*REST) New() runtime.Object {
	return &api.PersistentStorageDevice{}
}

func (*REST) NewList() runtime.Object {
	return &api.PersistentStorageDeviceList{}
}

func (rs *REST) Create(ctx api.Context, obj runtime.Object) (<-chan apiserver.RESTResult, error) {
	persistentstoragedevice := obj.(*api.PersistentStorageDevice)
	if !api.ValidNamespace(ctx, &persistentstoragedevice.ObjectMeta) {
		return nil, errors.NewConflict("persistentstoragedevice", persistentstoragedevice.Namespace, fmt.Errorf("PersistentStorageDevice.Namespace does not match the provided context"))
	}

	api.FillObjectMetaSystemFields(ctx, &persistentstoragedevice.ObjectMeta)
	if errs := validation.ValidatePersistentStorageDevice(persistentstoragedevice); len(errs) > 0 {
		return nil, errors.NewInvalid("persistentstoragedevice", persistentstoragedevice.Name, errs)
	}
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		if err := rs.registry.Create(ctx, persistentstoragedevice.Name, persistentstoragedevice); err != nil {
			return nil, err
		}
		return rs.registry.Get(ctx, persistentstoragedevice.Name)
	}), nil
}

func (rs *REST) Delete(ctx api.Context, id string) (<-chan apiserver.RESTResult, error) {
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		return &api.Status{Status: api.StatusSuccess}, rs.registry.Delete(ctx, id)
	}), nil
}

func (rs *REST) Get(ctx api.Context, id string) (runtime.Object, error) {
	persistentstoragedevice, err := rs.registry.Get(ctx, id)
	if err != nil {
		return persistentstoragedevice, err
	}
	if persistentstoragedevice == nil {
		return persistentstoragedevice, nil
	}
	return persistentstoragedevice, err
}

func (rs *REST) getAttrs(obj runtime.Object) (objLabels, objFields labels.Set, err error) {
	return labels.Set{}, labels.Set{}, nil
}

func (rs *REST) List(ctx api.Context, label, field labels.Selector) (runtime.Object, error) {
	return rs.registry.List(ctx, &generic.SelectionPredicate{label, field, rs.getAttrs})
}

func (rs *REST) Watch(ctx api.Context, label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return rs.registry.Watch(ctx, &generic.SelectionPredicate{label, field, rs.getAttrs}, resourceVersion)
}

func (rs *REST) Update(ctx api.Context, obj runtime.Object) (<-chan apiserver.RESTResult, error) {
	persistentstoragedevice := obj.(*api.PersistentStorageDevice)
	if !api.ValidNamespace(ctx, &persistentstoragedevice.ObjectMeta) {
		return nil, errors.NewConflict("persistentstoragedevice", persistentstoragedevice.Namespace, fmt.Errorf("PersistentStorageDevice.Namespace does not match the provided context"))
	}
	if errs := validation.ValidatePersistentStorageDevice(persistentstoragedevice); len(errs) > 0 {
		return nil, errors.NewInvalid("persistentstoragedevice", persistentstoragedevice.Name, errs)
	}
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		if err := rs.registry.Update(ctx, persistentstoragedevice.Name, persistentstoragedevice); err != nil {
			return nil, err
		}
		return rs.registry.Get(ctx, persistentstoragedevice.Name)
	}), nil
}

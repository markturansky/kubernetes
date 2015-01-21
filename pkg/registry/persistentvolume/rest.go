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

package persistentvolume

import (
	"fmt"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/validation"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

// REST implements the RESTStorage interface in terms of a PodRegistry.
type REST struct {
	registry Registry
}

type RESTConfig struct {
	Registry Registry
}

// NewREST returns a new REST.
func NewREST(registry Registry) *REST {
	return &REST{
		registry: registry,
	}
}

func (rs *REST) Create(ctx api.Context, obj runtime.Object) (<-chan apiserver.RESTResult, error) {
	persistentvolume := obj.(*api.PersistentVolume)
	if !api.ValidNamespace(ctx, &persistentvolume.ObjectMeta) {
		return nil, errors.NewConflict("persistentvolume", persistentvolume.Namespace, fmt.Errorf("PersistentVolume.Namespace does not match the provided context"))
	}

	api.FillObjectMetaSystemFields(ctx, &persistentvolume.ObjectMeta)
	if errs := validation.ValidatePersistentVolume(persistentvolume); len(errs) > 0 {
		return nil, errors.NewInvalid("persistentvolume", persistentvolume.Name, errs)
	}
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		if err := rs.registry.CreatePersistentVolume(ctx, persistentvolume); err != nil {
			return nil, err
		}
		return rs.registry.GetPersistentVolume(ctx, persistentvolume.Name)
	}), nil
}

func (rs *REST) Delete(ctx api.Context, id string) (<-chan apiserver.RESTResult, error) {
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		return &api.Status{Status: api.StatusSuccess}, rs.registry.DeletePersistentVolume(ctx, id)
	}), nil
}

func (rs *REST) Get(ctx api.Context, id string) (runtime.Object, error) {
	persistentvolume, err := rs.registry.GetPersistentVolume(ctx, id)
	if err != nil {
		return persistentvolume, err
	}
	if persistentvolume == nil {
		return persistentvolume, nil
	}
	return persistentvolume, err
}

func (rs *REST) List(ctx api.Context) (runtime.Object, error) {
	persistentvolumes, err := rs.registry.ListPersistentVolumes(ctx, labels.Everything())
//	if err == nil {
//		for i := range persistentvolumes.Items {
//			pod := &persistentvolumes.Items[i]
//			if status, err := rs.podCache.GetPodStatus(pod.Namespace, pod.Name); err != nil {
//				pod.Status = api.PodStatus{
//					Phase: api.PodUnknown,
//				}
//			} else {
//				pod.Status = *status
//			}
//		}
//	}
	return persistentvolumes, err
}

func (rs *REST) Watch(ctx api.Context, label, field labels.Selector, resourceVersion string) (watch.Interface, error) {
	return rs.registry.WatchPersistentVolumes(ctx, label, field, resourceVersion)
}

func (*REST) New() runtime.Object {
	return &api.PersistentVolume{}
}

func (*REST) NewList() runtime.Object {
	return &api.PersistentVolumeList{}
}

func (rs *REST) Update(ctx api.Context, obj runtime.Object) (<-chan apiserver.RESTResult, error) {
	persistentvolume := obj.(*api.PersistentVolume)
	if !api.ValidNamespace(ctx, &persistentvolume.ObjectMeta) {
		return nil, errors.NewConflict("persistentvolume", persistentvolume.Namespace, fmt.Errorf("PersistentVolume.Namespace does not match the provided context"))
	}
	if errs := validation.ValidatePersistentVolume(persistentvolume); len(errs) > 0 {
		return nil, errors.NewInvalid("persistentvolume", persistentvolume.Name, errs)
	}
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		if err := rs.registry.UpdatePersistentVolume(ctx, persistentvolume); err != nil {
			return nil, err
		}
		return rs.registry.GetPersistentVolume(ctx, persistentvolume.Name)
	}), nil
}

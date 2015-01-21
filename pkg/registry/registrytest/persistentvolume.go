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

package registrytest

import (
	"sync"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

func NewPersistentVolumeRegistry(volumes *api.PersistentVolumeList) *PersistentVolumeRegistry {
	return &PersistentVolumeRegistry{
		List:	volumes,
		broadcaster: watch.NewBroadcaster(0, watch.WaitIfChannelFull),
	}
}

type PersistentVolumeRegistry struct {
	mu            		sync.Mutex
	List          		*api.PersistentVolumeList
	PersistentVolume    *api.PersistentVolume
	Err           		error
	broadcaster 		*watch.Broadcaster

	DeletedID string
	GottenID  string
	UpdatedID string
}

func (r *PersistentVolumeRegistry) ListPersistentVolumes(ctx api.Context, field labels.Selector) (*api.PersistentVolumeList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ns, _ := api.NamespaceFrom(ctx)

	// Copy metadata from internal list into result
	persistentVolumesList := new(api.PersistentVolumeList)
	persistentVolumesList.TypeMeta = r.List.TypeMeta
	persistentVolumesList.ListMeta = r.List.ListMeta

	if ns != api.NamespaceAll {
		for _, persistentvolume := range r.List.Items {
			if ns == persistentvolume.Namespace {
				persistentVolumesList.Items = append(persistentVolumesList.Items, persistentvolume)
			}
		}
	} else {
		persistentVolumesList.Items = append([]api.PersistentVolume{}, r.List.Items...)
	}

	return persistentVolumesList, r.Err
}

func (r *PersistentVolumeRegistry) WatchPersistentVolumes(ctx api.Context, label, field labels.Selector, resourceVersion string) (watch.Interface, error){
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.broadcaster.Watch(), r.Err
}

func (r *PersistentVolumeRegistry) GetPersistentVolume(ctx api.Context, persistentvolumeID string) (*api.PersistentVolume, error){
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.PersistentVolume, r.Err
}

func (r *PersistentVolumeRegistry) CreatePersistentVolume(ctx api.Context, persistentvolume *api.PersistentVolume) error{
	r.mu.Lock()
	defer r.mu.Unlock()

	r.PersistentVolume = persistentvolume
	r.broadcaster.Action(watch.Added, persistentvolume)
	return r.Err
}

func (r *PersistentVolumeRegistry) UpdatePersistentVolume(ctx api.Context, persistentvolume *api.PersistentVolume) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PersistentVolume = persistentvolume
	r.broadcaster.Action(watch.Modified, persistentvolume)
	return r.Err
}

func (r *PersistentVolumeRegistry) DeletePersistentVolume(ctx api.Context, persistentvolumeName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.PersistentVolume.Name == persistentvolumeName {
		r.broadcaster.Action(watch.Deleted, r.PersistentVolume)
	}
	return r.Err
}

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

func NewPersistentStorageDeviceRegistry(volumes *api.PersistentStorageDeviceList) *PersistentStorageDeviceRegistry {
	return &PersistentStorageDeviceRegistry{
		List:	volumes,
		broadcaster: watch.NewBroadcaster(0, watch.WaitIfChannelFull),
	}
}

type PersistentStorageDeviceRegistry struct {
	mu            		sync.Mutex
	List          		*api.PersistentStorageDeviceList
	PersistentStorageDevice    *api.PersistentStorageDevice
	Err           		error
	broadcaster 		*watch.Broadcaster

	DeletedID string
	GottenID  string
	UpdatedID string
}

func (r *PersistentStorageDeviceRegistry) ListPersistentStorageDevices(ctx api.Context, field labels.Selector) (*api.PersistentStorageDeviceList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ns, _ := api.NamespaceFrom(ctx)

	// Copy metadata from internal list into result
	persistentStorageDevicesList := new(api.PersistentStorageDeviceList)
	persistentStorageDevicesList.TypeMeta = r.List.TypeMeta
	persistentStorageDevicesList.ListMeta = r.List.ListMeta

	if ns != api.NamespaceAll {
		for _, persistentstoragedevice := range r.List.Items {
			if ns == persistentstoragedevice.Namespace {
				persistentStorageDevicesList.Items = append(persistentStorageDevicesList.Items, persistentstoragedevice)
			}
		}
	} else {
		persistentStorageDevicesList.Items = append([]api.PersistentStorageDevice{}, r.List.Items...)
	}

	return persistentStorageDevicesList, r.Err
}

func (r *PersistentStorageDeviceRegistry) WatchPersistentStorageDevices(ctx api.Context, label, field labels.Selector, resourceVersion string) (watch.Interface, error){
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.broadcaster.Watch(), r.Err
}

func (r *PersistentStorageDeviceRegistry) GetPersistentStorageDevice(ctx api.Context, persistentstoragedeviceID string) (*api.PersistentStorageDevice, error){
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.PersistentStorageDevice, r.Err
}

func (r *PersistentStorageDeviceRegistry) CreatePersistentStorageDevice(ctx api.Context, persistentstoragedevice *api.PersistentStorageDevice) error{
	r.mu.Lock()
	defer r.mu.Unlock()

	r.PersistentStorageDevice = persistentstoragedevice
	r.broadcaster.Action(watch.Added, persistentstoragedevice)
	return r.Err
}

func (r *PersistentStorageDeviceRegistry) UpdatePersistentStorageDevice(ctx api.Context, persistentstoragedevice *api.PersistentStorageDevice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PersistentStorageDevice = persistentstoragedevice
	r.broadcaster.Action(watch.Modified, persistentstoragedevice)
	return r.Err
}

func (r *PersistentStorageDeviceRegistry) DeletePersistentStorageDevice(ctx api.Context, persistentstoragedeviceName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.PersistentStorageDevice.Name == persistentstoragedeviceName {
		r.broadcaster.Action(watch.Deleted, r.PersistentStorageDevice)
	}
	return r.Err
}

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
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

type Registry interface {
	ListPersistentStorageDevices(ctx api.Context, field labels.Selector) (*api.PersistentStorageDeviceList, error)
	WatchPersistentStorageDevices(ctx api.Context, label, field labels.Selector, resourceVersion string) (watch.Interface, error)
	GetPersistentStorageDevice(ctx api.Context, persistentStorageDeviceID string) (*api.PersistentStorageDevice, error)
	CreatePersistentStorageDevice(ctx api.Context, persistentStorageDevice *api.PersistentStorageDevice) error
	UpdatePersistentStorageDevice(ctx api.Context, persistentStorageDevice *api.PersistentStorageDevice) error
	DeletePersistentStorageDevice(ctx api.Context, persistentStorageDeviceID string) error
}

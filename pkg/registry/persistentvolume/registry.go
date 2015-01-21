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
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

type Registry interface {
	ListPersistentVolumes(ctx api.Context, field labels.Selector) (*api.PersistentVolumeList, error)
	WatchPersistentVolumes(ctx api.Context, label, field labels.Selector, resourceVersion string) (watch.Interface, error)
	GetPersistentVolume(ctx api.Context, persistentvolumeID string) (*api.PersistentVolume, error)
	CreatePersistentVolume(ctx api.Context, persistentvolume *api.PersistentVolume) error
	UpdatePersistentVolume(ctx api.Context, persistentvolume *api.PersistentVolume) error
	DeletePersistentVolume(ctx api.Context, persistentvolumeID string) error
}

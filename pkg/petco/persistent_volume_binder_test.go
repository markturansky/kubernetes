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

package petco

import (
	"sort"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/resource"
)

func TestAccessModes(t *testing.T) {

	tests := []struct {
		expected		string
		volumeSource	api.VolumeSource
	}{
		{
			expected: "RWO",
			volumeSource: api.VolumeSource{
				AWSElasticBlockStore: &api.AWSElasticBlockStore{},
			},
		}, {
			expected: "RWOROX",
			volumeSource: api.VolumeSource{
				GCEPersistentDisk: &api.GCEPersistentDisk{},
			},
		}, {
			expected: "RWOROXRWX",
			volumeSource: api.VolumeSource{
				NFSMount: &api.NFSMount{},
			},
		}, {
			expected: "",
			volumeSource: api.VolumeSource{
				EmptyDir: &api.EmptyDir{},
			},
		},
	}

	for _, item := range tests {
		modes := GetAccessModeType(item.volumeSource)
		modesStr := GetAccessModesAsString(modes)
		if modesStr != item.expected {
			t.Errorf("Unexpected access modes string for %+v", item.volumeSource)
		}
	}
}

func TestMatchVolume(t *testing.T){
	binder := NewPersistentVolumeIndex()
	for _, pv := range createTestVolumes() {
		binder.Add(pv)
		if !binder.Exists(pv) {
			t.Errorf("Expected to find persistent volume in binder")
		}
	}

	claim := &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Name: "claim01",
			Namespace: "myns",
		},
		Spec: api.PersistentVolumeClaimSpec{
			AccessModes: api.AccessModeType {
				ReadWriteOnce: &api.ReadWriteOnce{},
				ReadOnlyMany:  &api.ReadOnlyMany{},
			},
			Resources: api.ResourceList{
				api.ResourceName(api.ResourceSize): resource.MustParse("10G"),
			},
		},
	}

	volume := binder.Match(claim)

	if volume == nil || volume.UID != "ghi" {
		t.Errorf("Expected GCE disk of size 10G, received: %+v", volume)
	}


	claim = &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Name: "claim01",
			Namespace: "myns",
		},
		Spec: api.PersistentVolumeClaimSpec{
			AccessModes: api.AccessModeType {
				ReadWriteOnce: &api.ReadWriteOnce{},
			},
			Resources: api.ResourceList{
				api.ResourceName(api.ResourceSize): resource.MustParse("10G"),
			},
		},
	}

	volume = binder.Match(claim)

	if volume == nil || volume.UID != "yz" {
		t.Errorf("Expected AWS block store of size 10G, received: %+v", volume)
	}

}

func TestSort(t *testing.T){
	volumes := createTestVolumes()
	volumes = volumes[0:3]

	sort.Sort(PersistentVolumeComparator(volumes))

	if volumes[0].UID != "def" {
		t.Error("Incorrect ordering of persistent volumes.  Expected 'def' first.")
	}

	if volumes[1].UID != "abc" {
		t.Error("Incorrect ordering of persistent volumes.  Expected 'abc' second.")
	}

	if volumes[2].UID != "ghi" {
		t.Error("Incorrect ordering of persistent volumes.  Expected 'ghi' last.")
	}
}


func createTestVolumes() []*api.PersistentVolume {
	return []*api.PersistentVolume{
		{
			ObjectMeta: api.ObjectMeta{
				UID: "abc",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("5G"),
				},
				Source: api.VolumeSource{
					GCEPersistentDisk: &api.GCEPersistentDisk{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "def",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("1G"),
				},
				Source: api.VolumeSource{
					GCEPersistentDisk: &api.GCEPersistentDisk{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "ghi",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("10G"),
				},
				Source: api.VolumeSource{
					GCEPersistentDisk: &api.GCEPersistentDisk{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "jkl",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("5G"),
				},
				Source: api.VolumeSource{
					NFSMount: &api.NFSMount{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "mno",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("1G"),
				},
				Source: api.VolumeSource{
					NFSMount: &api.NFSMount{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "pqr",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("10G"),
				},
				Source: api.VolumeSource{
					NFSMount: &api.NFSMount{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "stu",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("5G"),
				},
				Source: api.VolumeSource{
					AWSElasticBlockStore: &api.AWSElasticBlockStore{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "vwx",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("1G"),
				},
				Source: api.VolumeSource{
					AWSElasticBlockStore: &api.AWSElasticBlockStore{},
				},
			},
		},
		{
			ObjectMeta: api.ObjectMeta{
				UID: "yz",
			},
			Spec: api.PersistentVolumeSpec{
				Capacity: api.ResourceList{
					api.ResourceName(api.ResourceSize): resource.MustParse("10G"),
				},
				Source: api.VolumeSource{
					AWSElasticBlockStore: &api.AWSElasticBlockStore{},
				},
			},
		},
	}
}



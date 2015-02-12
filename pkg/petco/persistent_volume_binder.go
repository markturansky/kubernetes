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

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/golang/glog"
)

type PersistentVolumeIndex interface {
	Add(volume *api.PersistentVolume) error
	Match(claim *api.PersistentVolumeClaim) *api.PersistentVolume
	Exists(volume *api.PersistentVolume) bool
}

// generic implementation creates an index of volumes like so:
//
//		RWO:		[]api.PersistentVolume		-- sorted by Size, smallest to largest
//		RWOROXRWO:	[]api.PersistentVolume		-- sorted by Size, smallest to largest
//		RWOROXRWX:	[]api.PersistentVolume		-- sorted by Size, smallest to largest
//
// This allow fast identification of a volume by its capabilities (accessModeType) and then
// to find the closet-without-going-under size request
type genericPersistentVolumeIndex struct {
	cache map[string][]*api.PersistentVolume
}

func NewPersistentVolumeIndex() PersistentVolumeIndex {
	cache := make(map[string][]*api.PersistentVolume)
	return &genericPersistentVolumeIndex{
		cache: cache,
	}
}

type PersistentVolumeComparator []*api.PersistentVolume

func (comp PersistentVolumeComparator) Len() int      { return len(comp) }
func (comp PersistentVolumeComparator) Swap(i, j int) { comp[i], comp[j] = comp[j], comp[i] }
func (comp PersistentVolumeComparator) Less(i, j int) bool {
	aQty := comp[i].Spec.Capacity[api.ResourceSize]
	bQty := comp[j].Spec.Capacity[api.ResourceSize]
	aSize := aQty.Value()
	bSize := bQty.Value()
	return aSize < bSize
}

// given a set of volumes, match the one that closest fits the claim
func (binder *genericPersistentVolumeIndex) Match(claim *api.PersistentVolumeClaim) *api.PersistentVolume {

	desiredModes := GetAccessModesAsString(claim.Spec.AccessModes)
	quantity := claim.Spec.Resources[api.ResourceSize]
	desiredSize := quantity.Value()

	glog.V(5).Infof("Attempting to match %s and %s\n", desiredModes, desiredSize)

	volumes := binder.cache[desiredModes]

	for _, v := range volumes {
		qty := v.Spec.Capacity[api.ResourceSize]
		glog.V(5).Infof("found size %s\n", qty.Value())
		if qty.Value() >= desiredSize {
			return v
		}
	}

	return nil

}

func (binder *genericPersistentVolumeIndex) Add(volume *api.PersistentVolume) error {

	modes := GetAccessModeType(volume.Spec.Source)
	modesStr := GetAccessModesAsString(modes)

	if _, ok := binder.cache[modesStr]; !ok {
		binder.cache[modesStr] = []*api.PersistentVolume{}
	}

	if !binder.Exists(volume) {
		binder.cache[string(volume.ObjectMeta.UID)] = append([]*api.PersistentVolume{}, volume)
		binder.cache[modesStr] = append(binder.cache[modesStr], volume)
	}

	sort.Sort(PersistentVolumeComparator(binder.cache[modesStr]))

	return nil
}

func (binder *genericPersistentVolumeIndex) Exists(volume *api.PersistentVolume) bool {
	if _, ok := binder.cache[string(volume.UID)]; !ok {
		return false
	}
	return true
}

func GetAccessModesAsString(modes api.AccessModeType) string {

	modesAsString := ""

	if modes.ReadWriteOnce != nil {
		modesAsString += "RWO"
	}

	if modes.ReadOnlyMany != nil {
		modesAsString += "ROX"
	}

	if modes.ReadWriteMany != nil {
		modesAsString += "RWX"
	}

	return modesAsString
}

// would this be better on api.VolumeSource?
func GetAccessModeType(source api.VolumeSource) api.AccessModeType {

	if source.AWSElasticBlockStore != nil {
		return api.AccessModeType{
			ReadWriteOnce: &api.ReadWriteOnce{},
		}
	}

	if source.GCEPersistentDisk != nil {
		return api.AccessModeType{
			ReadWriteOnce: &api.ReadWriteOnce{},
			ReadOnlyMany:  &api.ReadOnlyMany{},
		}
	}

	if source.NFSMount != nil {
		return api.AccessModeType{
			ReadWriteOnce: &api.ReadWriteOnce{},
			ReadOnlyMany:  &api.ReadOnlyMany{},
			ReadWriteMany: &api.ReadWriteMany{},
		}
	}

	return api.AccessModeType{}
}

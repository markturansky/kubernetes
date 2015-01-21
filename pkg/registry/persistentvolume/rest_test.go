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

package persistentvolume

import (
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/registrytest"
	"reflect"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

type fakeCache struct {
	requestedNamespace string
	requestedName      string

	statusToReturn *api.PodStatus
	errorToReturn  error
}

func (f *fakeCache) GetPodStatus(namespace, name string) (*api.PodStatus, error) {
	f.requestedNamespace = namespace
	f.requestedName = name
	return f.statusToReturn, f.errorToReturn
}

func TestCreatePersistentVolume(t *testing.T) {
	pvRegistry := registrytest.NewPersistentVolumeRegistry(&api.PersistentVolumeList{})
	storage := REST{
		registry: pvRegistry,
	}
	persistentVolume := &api.PersistentVolume{
		ObjectMeta: api.ObjectMeta{ Name: "foo" },
	}
	ctx := api.NewDefaultContext()
	observer, err := storage.Watch(ctx, labels.Everything(), labels.Everything(), "")
	channel, err := storage.Create(ctx, persistentVolume)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	select {
	case <-channel:
		// Do nothing, this is expected.
	case <-time.After(time.Millisecond * 100):
		t.Error("Unexpected timeout on async channel")
	}
	if !api.HasObjectMetaSystemFieldValues(&pvRegistry.PersistentVolume.ObjectMeta) {
		t.Errorf("Expected ObjectMeta field values were not populated")
	}

	myEvent := <-observer.ResultChan()
	if myEvent.Type != watch.Added {
		t.Errorf("Expected %v event for new persistent volume, received %v", watch.Added, myEvent.Type)
	}
	if !reflect.DeepEqual(myEvent.Object, persistentVolume) {
		t.Errorf("Unexpected PersistentVolume returned from watch")
	}
}

func TestGetPersistentVolume(t *testing.T) {

	pvRegistry := registrytest.NewPersistentVolumeRegistry(&api.PersistentVolumeList{})
	pvRegistry.PersistentVolume = &api.PersistentVolume{
		ObjectMeta: api.ObjectMeta{Name: "foo"},
	}
	storage := REST{
		registry: pvRegistry,
	}
	ctx := api.NewContext()
	obj, err := storage.Get(ctx, "foo")
	persistentvolume := obj.(*api.PersistentVolume)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	expect := *pvRegistry.PersistentVolume
	if e, a := &expect, persistentvolume; !reflect.DeepEqual(e,a) {
		t.Errorf("Unexpected persistentvolume.  Expected %#v, Got %#v", e, a)
	}

}

func TestListPersistentVolumes(t *testing.T) {

	pvRegistry := registrytest.NewPersistentVolumeRegistry(nil)
	pvRegistry.List = &api.PersistentVolumeList{
		Items: []api.PersistentVolume{
			{ObjectMeta: api.ObjectMeta{Name: "foo", Namespace: "ns1"},},
			{ObjectMeta: api.ObjectMeta{Name: "bar", Namespace: "ns1"},},
			{ObjectMeta: api.ObjectMeta{Name: "foo", Namespace: "ns2"},},
		},
	}

	storage := REST{ registry: pvRegistry }
	ctx := api.WithNamespace(api.NewContext(), "ns1")
	obj, err := storage.List(ctx)
	persistentvolumes := obj.(*api.PersistentVolumeList)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if len(persistentvolumes.Items) != 2 {
		t.Errorf("Expected 2 persistentvolumes in this namespace, got %v", len(persistentvolumes.Items))
	}

	ctx = api.WithNamespace(api.NewContext(), "ns2")
	obj, err = storage.List(ctx)
	persistentvolumes = obj.(*api.PersistentVolumeList)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if len(persistentvolumes.Items) != 1 {
		t.Errorf("Expected 1 persistentvolumes in this namespace, got %v", len(persistentvolumes.Items))
	}

}


func TestUpdatePersistentVolume(t *testing.T) {
	pvRegistry := registrytest.NewPersistentVolumeRegistry(&api.PersistentVolumeList{})
	persistentvolume := &api.PersistentVolume{ ObjectMeta: api.ObjectMeta{Name: "foo", Namespace: "bar"}}
	pvRegistry.PersistentVolume = persistentvolume
	storage := REST{ registry: pvRegistry }
	ctx := api.WithNamespace(api.NewContext(), "bar")

	persistentvolume.Status.Phase = api.Attached

	_, err := storage.Update(ctx, pvRegistry.PersistentVolume)

	observer, err := storage.Watch(ctx, labels.Everything(), labels.Everything(), "")
	channel, err := storage.Update(ctx, persistentvolume)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	select {
	case <-channel:
		// Do nothing, this is expected.
	case <-time.After(time.Millisecond * 100):
		t.Error("Unexpected timeout on async channel")
	}

	myEvent := <-observer.ResultChan()
	if myEvent.Type != watch.Modified {
		t.Errorf("Expected %v event for existing persistent volume, received %v", watch.Modified, myEvent.Type)
	}
	if !reflect.DeepEqual(myEvent.Object, persistentvolume) {
		t.Errorf("Unexpected PersistentVolume returned from watch")
	}
}

func TestDeletePersistentVolume(t *testing.T){
	pvRegistry := registrytest.NewPersistentVolumeRegistry(&api.PersistentVolumeList{})
	storage := REST{
		registry: pvRegistry,
	}
	persistentVolume := &api.PersistentVolume{
		ObjectMeta: api.ObjectMeta{ Name: "foo" },
	}
	ctx := api.NewDefaultContext()

	storage.Create(ctx, persistentVolume)
	storage.Delete(ctx, persistentVolume.Name)
	observer, err := storage.Watch(ctx, labels.Everything(), labels.Everything(), "")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	addEvent := <- observer.ResultChan()
	if addEvent.Type != watch.Added {
		t.Errorf("Expected %v event for new persistent volume, received %v", watch.Added, addEvent.Type)
	}

	deleteEvent := <-observer.ResultChan()
	if deleteEvent.Type != watch.Deleted {
		t.Errorf("Expected %v event for existing persistent volume, received %v", watch.Deleted, deleteEvent.Type)
	}
	if !reflect.DeepEqual(deleteEvent.Object, persistentVolume) {
		t.Errorf("Unexpected PersistentVolume returned from watch")
	}
}

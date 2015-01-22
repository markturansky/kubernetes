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

package persistentstoragedevice

import (
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/registrytest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

type testRegistry struct {
	*registrytest.GenericRegistry
}

func NewTestREST() (testRegistry, *REST) {
	reg := testRegistry{registrytest.NewGeneric(nil)}
	return reg, NewREST(reg)
}

func testDevice(name string, ns string) *api.PersistentStorageDevice {
	return &api.PersistentStorageDevice{
		ObjectMeta: api.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}

func TestRESTCreate(t *testing.T) {
	table := []struct {
		ctx    api.Context
		device *api.PersistentStorageDevice
		valid  bool
	}{
		{
			ctx:    api.WithNamespace(api.NewContext(), "foo"),
			device: testDevice("foo", "foo"),
			valid:  true,
		}, {
			ctx:    api.WithNamespace(api.NewContext(), "bar"),
			device: testDevice("bar", "bar"),
			valid:  true,
		}, {
			ctx:    api.WithNamespace(api.NewContext(), "not-baz"),
			device: testDevice("baz", "baz"),
			valid:  false,
		},
	}

	for _, item := range table {
		_, rest := NewTestREST()
		c, err := rest.Create(item.ctx, item.device)
		if !item.valid {
			if err == nil {
				ctxNS := api.Namespace(item.ctx)
				t.Errorf("unexpected non-error for %v (%v, %v)", item.device.Name, ctxNS, item.device.Namespace)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: Unexpected error %v", item.device.Name, err)
			continue
		}
		if !api.HasObjectMetaSystemFieldValues(&item.device.ObjectMeta) {
			t.Errorf("storage did not populate object meta field values")
		}
		if e, a := item.device, (<-c).Object; !reflect.DeepEqual(e, a) {
			t.Errorf("diff: %s", util.ObjectDiff(e, a))
		}
		// Ensure we implement the interface
		_ = apiserver.ResourceWatcher(rest)
	}
}

func TestRESTDelete(t *testing.T) {
	_, rest := NewTestREST()
	device := testDevice("foo", api.NamespaceDefault)
	c, err := rest.Create(api.NewDefaultContext(), device)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	<-c
	c, err = rest.Delete(api.NewDefaultContext(), device.Name)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	if stat := (<-c).Object.(*api.Status); stat.Status != api.StatusSuccess {
		t.Errorf("unexpected status: %v", stat)
	}
}

func TestRESTGet(t *testing.T) {
	_, rest := NewTestREST()
	device := testDevice("foo", api.NamespaceDefault)
	c, err := rest.Create(api.NewDefaultContext(), device)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	<-c
	got, err := rest.Get(api.NewDefaultContext(), device.Name)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	if e, a := device, got; !reflect.DeepEqual(e, a) {
		t.Errorf("diff: %s", util.ObjectDiff(e, a))
	}
}

func TestRESTList(t *testing.T) {
	reg, rest := NewTestREST()
	deviceA := testDevice("foo", api.NamespaceDefault)
	deviceB := testDevice("bar", api.NamespaceDefault)
	deviceC := testDevice("baz", api.NamespaceDefault)

	deviceA.Labels = map[string]string{
		"a-label-key": "some value",
	}

	reg.ObjectList = &api.PersistentStorageDeviceList{
		Items: []api.PersistentStorageDevice{*deviceA, *deviceB, *deviceC},
	}
	got, err := rest.List(api.NewContext(), labels.Everything(), labels.Everything())
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	expect := &api.PersistentStorageDeviceList{
		Items: []api.PersistentStorageDevice{*deviceA, *deviceB, *deviceC},
	}
	if e, a := expect, got; !reflect.DeepEqual(e, a) {
		t.Errorf("diff: %s", util.ObjectDiff(e, a))
	}
}

func TestRESTWatch(t *testing.T) {
	deviceA := testDevice("foo", api.NamespaceDefault)

	reg, rest := NewTestREST()
	wi, err := rest.Watch(api.NewContext(), labels.Everything(), labels.Everything(), "0")
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	go func() {
		reg.Broadcaster.Action(watch.Added, deviceA)
	}()
	got := <-wi.ResultChan()
	if e, a := deviceA, got.Object; !reflect.DeepEqual(e, a) {
		t.Errorf("diff: %s", util.ObjectDiff(e, a))
	}
}

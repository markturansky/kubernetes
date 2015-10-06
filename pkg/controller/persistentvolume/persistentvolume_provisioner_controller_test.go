/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

package volumeclaimbinder

import (
	"fmt"
	//	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/resource"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/testclient"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/fake"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/host_path"
	"k8s.io/kubernetes/pkg/watch"
)

func TestProvisionerRunStop(t *testing.T) {
	client := &testclient.Fake{}
	cloud := &fake_cloud.FakeCloud{}
	plugins := host_path.ProbeVolumePlugins(volume.VolumeConfig{})

	provisioners := map[string]volume.ProvisionableVolumePlugin{
		"foo": host_path.ProbeVolumePlugins(volume.VolumeConfig{})[0].(volume.ProvisionableVolumePlugin),
	}

	provisioner, _ := NewPersistentVolumeController(NewControllerClient(client), 1*time.Second, plugins, provisioners, cloud)

	if len(provisioner.stopChannels) != 0 {
		t.Errorf("Non-running provisioner should not have any stopChannels.  Got %v", len(provisioner.stopChannels))
	}

	provisioner.Run()

	if len(provisioner.stopChannels) != 2 {
		t.Errorf("Running provisioner should have exactly 2 stopChannels.  Got %v", len(provisioner.stopChannels))
	}

	provisioner.Stop()

	if len(provisioner.stopChannels) != 0 {
		t.Errorf("Non-running provisioner should not have any stopChannels.  Got %v", len(provisioner.stopChannels))
	}
}

func TestReconcileClaim(t *testing.T) {
	client := &mockControllerClient{}

	pv := &api.PersistentVolume{
		ObjectMeta: api.ObjectMeta{
			Annotations: map[string]string{},
			Name:        "ebs1",
		},
		Spec: api.PersistentVolumeSpec{
			AccessModes: []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
			Capacity: api.ResourceList{
				api.ResourceName(api.ResourceStorage): resource.MustParse("10Gi"),
			},
			PersistentVolumeSource: api.PersistentVolumeSource{
				HostPath: &api.HostPathVolumeSource{
					Path: "/tmp/data01",
				},
			},
		},
	}

	pvc := &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Annotations: map[string]string{},
			Name:        "claim01",
			Namespace:   "ns",
		},
		Spec: api.PersistentVolumeClaimSpec{
			AccessModes: []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
			Resources: api.ResourceRequirements{
				Requests: api.ResourceList{
					api.ResourceName(api.ResourceStorage): resource.MustParse("8G"),
				},
			},
		},
	}

	controller, _ := NewPersistentVolumeController(client, 1*time.Second, nil, nil, nil)

	// watch would have added the claim to the store
	controller.claimStore.Add(pvc)
	pvc, status, err := controller.reconcileClaim(pvc)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// plain PVC and no volumes in the controller.
	if status.Phase != api.ClaimPending {
		t.Errorf("Expected %s but got %s", api.ClaimPending, status.Phase)
	}

	// watch event handler would have added a new volume to the store, the controller adds it to the index
	controller.volumeStore.Add(pv)
	controller.volumeIndex.Add(pv)

	pvc, status, _ = controller.reconcileClaim(pvc)

	// still pending because the claim needs to update its status to reflect new volume bind
	if status.Phase != api.ClaimPending {
		t.Errorf("Expected %s but got %s", api.ClaimPending, status.Phase)
	}
	if pvc.Spec.VolumeName != pv.Name {
		t.Errorf("Expected %s but got %s", pv.Name, pvc.Spec.VolumeName)
	}

	// a ClaimRef is needed on PV and this var is required for testing.
	api.ForTesting_ReferencesAllowBlankSelfLinks = true

	// updating the VolumeName triggers another watch event
	pvc, status, err = controller.reconcileClaim(pvc)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if status.Phase != api.ClaimBound {
		t.Errorf("Expected %s but got %s", api.ClaimBound, status.Phase)
	}
}

func TestProvisionClaim(t *testing.T) {
	client := &mockControllerClient{}

	pvc := &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Annotations: map[string]string{
				qosProvisioningKey: "not-foo",
			},
			Name:      "claim01",
			Namespace: "ns",
		},
		Spec: api.PersistentVolumeClaimSpec{
			AccessModes: []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
			Resources: api.ResourceRequirements{
				Requests: api.ResourceList{
					api.ResourceName(api.ResourceStorage): resource.MustParse("8G"),
				},
			},
		},
	}

	provisioners := map[string]volume.ProvisionableVolumePlugin{
		"foo": &volume.FakeVolumePlugin{},
	}

	controller, _ := NewPersistentVolumeController(client, 1*time.Second, nil, provisioners, nil)

	pvc, status, err := controller.reconcileClaim(pvc)
	if err == nil || !strings.HasPrefix(err.Error(), "No provisioner found") {
		t.Errorf("Unexpected nil error or correct error prefix not found: %#v", err)
	}

	// still pending and no new PV was yet provisioned.  wrong annotation name on claim.
	if status.Phase != api.ClaimPending {
		t.Errorf("Expected %s but got %s", api.ClaimPending, status.Phase)
	}

	// no volume yet created.
	retVal, _ := controller.client.GetPersistentVolume("ebs1")
	if retVal != nil {
		t.Errorf("Unexpected non-nil volume: %#v", retVal)
	}

	// use the correct annotation that matches a provisioner
	pvc.Annotations[qosProvisioningKey] = "foo"
	pvc, status, err = controller.reconcileClaim(pvc)
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}

	// volume should no longer be nil
	retVal, _ = controller.client.GetPersistentVolume("ebs1")
	if retVal == nil {
		t.Errorf("Unexpected nil volume: %#v", retVal)
	}

	// still pending but with a new PV created in response to the correct annotation
	if status.Phase != api.ClaimPending {
		t.Errorf("Expected %s but got %s", api.ClaimPending, status.Phase)
	}

	// claim should still be missing the bind (volumeName) even though the PV was created for it.
	// will get the new volume when it binds on the next pass
	if pvc.Spec.VolumeName != "" {
		t.Errorf("Expected claim to be unbound, but got pvc.Spec.VolumeName=%s", pvc.Spec.VolumeName)
	}
	if retVal.Spec.ClaimRef == nil {
		t.Errorf("Unexpected nil ClaimRef on volume.  should have been bound to claim %s", pvc.Name)
	}
	if retVal.Spec.ClaimRef.Name != pvc.Name {
		t.Errorf("Expected %s but got %s", pvc.Name, retVal.Spec.ClaimRef.Name)
	}
	if retVal.Annotations[provisionedForKey] != ClaimToProvisionableKey(pvc) {
		t.Errorf("Expected %s but got %s", ClaimToProvisionableKey(pvc), retVal.Annotations[provisionedForKey])
	}

	// fully bound
	pvc, status, err = controller.reconcileClaim(pvc)
	if pvc.Spec.VolumeName == retVal.Name {
		t.Errorf("Expected %s but got %s", retVal.Name, pvc.Spec.VolumeName)
	}
}

func TestReconcileVolume(t *testing.T) {
	mockClient := &mockControllerClient{}
	provisioners := map[string]volume.ProvisionableVolumePlugin{
		"foo": host_path.ProbeVolumePlugins(volume.VolumeConfig{})[0].(volume.ProvisionableVolumePlugin),
	}
	controller, _ := NewPersistentVolumeController(mockClient, 1*time.Second, nil, provisioners, nil)

	pv := &api.PersistentVolume{
		ObjectMeta: api.ObjectMeta{
			Annotations: map[string]string{},
			Name:        "pv01",
		},
		Spec: api.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: api.PersistentVolumeReclaimDelete,
			AccessModes:                   []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
			Capacity: api.ResourceList{
				api.ResourceName(api.ResourceStorage): resource.MustParse("10Gi"),
			},
			PersistentVolumeSource: api.PersistentVolumeSource{
				HostPath: &api.HostPathVolumeSource{
					Path: "/tmp/data01",
				},
			},
		},
	}

	pvc := &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Annotations: map[string]string{
				qosProvisioningKey: "not-foo",
			},
			Name:      "claim01",
			Namespace: "ns",
		},
		Spec: api.PersistentVolumeClaimSpec{
			AccessModes: []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
			Resources: api.ResourceRequirements{
				Requests: api.ResourceList{
					api.ResourceName(api.ResourceStorage): resource.MustParse("8G"),
				},
			},
		},
	}
	pv, status, _ := controller.reconcileVolume(pv)

	// no claims yet, so PV must be available
	if status.Phase != api.VolumeAvailable {
		t.Errorf("Expected %s but got %s", api.VolumeAvailable, status.Phase)
	}

	// watch adds claim to the store.
	// we need to add it to our mock client to mimic normal Get call
	controller.claimStore.Add(pvc)
	mockClient.claim = pvc

	// pretend the claim and volume are bound, no provisioning required
	claimRef, _ := api.GetReference(pvc)
	pv.Spec.ClaimRef = claimRef
	pvc.Spec.VolumeName = pv.Name
	pv, status, err := controller.reconcileVolume(pv)
	if status.Phase != api.VolumeBound {
		t.Errorf("Expected %s but got %s - %v", api.VolumeBound, status.Phase, err)
	}

	// pretend the claim and volume are bound and PV is fully provisioned
	pv.Annotations[pvProvisioningRequired] = pvProvisioningCompleted
	pv.Annotations[provisionedForKey] = ClaimToProvisionableKey(pvc)
	pv, status, err = controller.reconcileVolume(pv)
	if status.Phase != api.VolumeBound {
		t.Errorf("Expected %s but got %s - %v", api.VolumeBound, status.Phase, err)
	}

	// pretend the claim and volume are bound but PV still requires provisioning
	pv.Annotations[pvProvisioningRequired] = "yes, please!"
	pv.Annotations[provisionedForKey] = ClaimToProvisionableKey(pvc)
	pv, status, err = controller.reconcileVolume(pv)
	if status.Phase != api.VolumePending {
		t.Errorf("Expected %s but got %s - %v", api.VolumePending, status.Phase, err)
	}

	// the controller launches a Go routine that provisions the volume.
	// the controller locks on the pv's name and the provision func then deletes the lock on exit
	for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(1) {
		if _, exists := controller.locks[pv.Name]; !exists {
			continue
		}
	}
	if _, exists := controller.locks[pv.Name]; exists {
		t.Error("Unexpected lock.  The provisioner was expected to delete the lock on the pv")
	}

	// the claim has been deleted by the user. the volume is ready for recycling
	mockClient.claim = nil
	pv, status, err = controller.reconcileVolume(pv)
	if status.Phase != api.VolumeReleased {
		t.Errorf("Expected %s but got %s - %v", api.VolumeReleased, status.Phase, err)
	}
	if !keyExists(pvProvisioningRequired, pv.Annotations) {
		t.Errorf("volume expected to have 'recycling required' annotation: %#v", pv.Annotations)
	}

	// saving the recycling annotation triggers another watch update
	pv, status, err = controller.reconcileVolume(pv)
	if status.Phase != api.VolumeReleased {
		t.Errorf("Expected %s but got %s - %v", api.VolumeReleased, status.Phase, err)
	}

	// the controller launches a Go routine that reyclces the volume.
	// the controller locks on the pv's name and the recycling func then deletes the lock on exit
	for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(1) {
		if _, exists := controller.locks[pv.Name]; !exists {
			continue
		}
	}
	if _, exists := controller.locks[pv.Name]; exists {
		t.Error("Unexpected lock.  The recycler was expected to delete the lock on the pv")
	}
}

var _ controllerClient = &mockControllerClient{}

type mockControllerClient struct {
	volume *api.PersistentVolume
	claim  *api.PersistentVolumeClaim
}

func (c *mockControllerClient) GetPersistentVolume(name string) (*api.PersistentVolume, error) {
	return c.volume, nil
}

func (c *mockControllerClient) CreatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error) {
	if pv.GenerateName != "" && pv.Name == "" {
		pv.Name = fmt.Sprintf(pv.GenerateName, util.NewUUID())
	}
	c.volume = pv
	return c.volume, nil
}

func (c *mockControllerClient) ListPersistentVolumes(labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeList, error) {
	return &api.PersistentVolumeList{
		Items: []api.PersistentVolume{*c.volume},
	}, nil
}

func (c *mockControllerClient) WatchPersistentVolumes(labels labels.Selector, fields fields.Selector, resourceVersion string) (watch.Interface, error) {
	return watch.NewFake(), nil
}

func (c *mockControllerClient) UpdatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.CreatePersistentVolume(pv)
}

func (c *mockControllerClient) DeletePersistentVolume(volume *api.PersistentVolume) error {
	c.volume = nil
	return nil
}

func (c *mockControllerClient) UpdatePersistentVolumeStatus(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
	return volume, nil
}

func (c *mockControllerClient) GetPersistentVolumeClaim(namespace, name string) (*api.PersistentVolumeClaim, error) {
	if c.claim != nil {
		return c.claim, nil
	} else {
		return nil, errors.NewNotFound("persistentVolume", name)
	}
}

func (c *mockControllerClient) ListPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeClaimList, error) {
	return &api.PersistentVolumeClaimList{
		Items: []api.PersistentVolumeClaim{*c.claim},
	}, nil
}

func (c *mockControllerClient) WatchPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector, resourceVersion string) (watch.Interface, error) {
	return watch.NewFake(), nil
}

func (c *mockControllerClient) UpdatePersistentVolumeClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	c.claim = claim
	return c.claim, nil
}

func (c *mockControllerClient) UpdatePersistentVolumeClaimStatus(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	return claim, nil
}

func (c *mockControllerClient) GetKubeClient() client.Interface {
	return nil
}

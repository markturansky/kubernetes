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
	"os"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/resource"
	"k8s.io/kubernetes/pkg/client/unversioned/testclient"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/fake"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/host_path"
)

func TestProvisionerRunStop(t *testing.T) {
	client := &testclient.Fake{}
	cloud := &fake_cloud.FakeCloud{}
	plugins := map[string]volume.CreatableVolumePlugin{
		"foo": host_path.ProbeVolumePlugins(volume.VolumeConfig{})[0].(volume.CreatableVolumePlugin),
	}

	provisioner, _ := NewPersistentVolumeProvisioner(client, 1*time.Second, plugins, cloud)

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

func TestProvisionerWithClaim(t *testing.T) {
	//	client := &testclient.Fake{}
	//	cloud := &fake_cloud.FakeCloud{}
	plugin := host_path.ProbeVolumePlugins(volume.VolumeConfig{})[0]
	creater := plugin.(volume.CreatableVolumePlugin)
	client := &mockProvisionerClient{}
	provisionedVolumes := map[string]bool{}

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

	err := provisionClaim(pvc, creater, client, provisionedVolumes)
	if err != nil {
		t.Error("Expected nil error. claim non-provisionable due to missing provisionable annotation")
	}

	pvc.Annotations[provisionableKey] = "true"
	err = provisionClaim(pvc, creater, client, provisionedVolumes)
	if err == nil {
		t.Error("Expected error for missing QoS tier annotation key.")
	}

	pvc.Annotations[qosProvisioningKey] = "foo"

	err = provisionClaim(pvc, creater, client, provisionedVolumes)

	if client.volume == nil {
		t.Error("Unexpected nil volume.  Expected a PV to have been provisioned and created.")
	}

	if client.volume.Spec.HostPath == nil {
		t.Error("Unexpected nil HostPath.  Expected a PV to have been provisioned and created.")
	}

	value, exists := client.volume.Annotations[provisionedForKey]
	if !exists {
		t.Error("PV expected to have 'provisioned for' annotation:  %+v", client.volume)
	}

	if value != ClaimToProvisionableKey(pvc) {
		t.Error("Expected %s but got %s", ClaimToProvisionableKey(pvc), value)
	}

	os.RemoveAll(client.volume.Spec.HostPath.Path)
}

type mockProvisionerClient struct {
	volume *api.PersistentVolume
	claim  *api.PersistentVolumeClaim
}

func (c *mockProvisionerClient) CreatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error) {
	c.volume = pv
	return c.volume, nil
}

func (c *mockProvisionerClient) UpdatePersistentVolumeClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	c.claim = claim
	return c.claim, nil
}

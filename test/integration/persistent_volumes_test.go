// +build integration,!no-etcd

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

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/resource"
	"k8s.io/kubernetes/pkg/api/testapi"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	cloud "k8s.io/kubernetes/pkg/cloudprovider/providers/fake"
	"k8s.io/kubernetes/pkg/controller/persistentvolume"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/host_path"
	"k8s.io/kubernetes/pkg/watch"
)

func init() {
	requireEtcd()
}

func TestPersistentVolumeRecycler(t *testing.T) {
	_, s := runAMaster(t)
	defer s.Close()

	deleteAllEtcdKeys()
	testKubeClient := client.NewOrDie(&client.Config{Host: s.URL, Version: testapi.Default.Version()})
	binderKubeClient := client.NewOrDie(&client.Config{Host: s.URL, Version: testapi.Default.Version()})
	recyclerKubeClient := client.NewOrDie(&client.Config{Host: s.URL, Version: testapi.Default.Version()})
	provisionerKubeClient := client.NewOrDie(&client.Config{Host: s.URL, Version: testapi.Default.Version()})

	// Test all components of the PersistentVolume framework: Binder, Recycler, and Provisioner.
	// Each component is tested serially inside 1 test to avoid race conditions inherent in concurrent integration tests (multiple masters, concurrent deleteAllEtcdKeys, etc)

	binder := volumeclaimbinder.NewPersistentVolumeClaimBinder(binderKubeClient, 1*time.Second)
	binder.Run()
	defer binder.Stop()

	plugins := []volume.VolumePlugin{&volume.FakeVolumePlugin{"plugin-name", volume.NewFakeVolumeHost("/tmp/fake", nil, nil), volume.VolumeConfig{}}}
	recycler, _ := volumeclaimbinder.NewPersistentVolumeRecycler(recyclerKubeClient, 1*time.Second, plugins, nil)
	recycler.Run()
	defer recycler.Stop()

	pvcreater, _ := getPVCreater()
	provisioner, _ := volumeclaimbinder.NewPersistentVolumeProvisioner(provisionerKubeClient, 1*time.Second, pvcreater, &cloud.FakeCloud{})
	provisioner.Run()
	defer provisioner.Stop()

	// This PV will be claimed, released, recycled, and deleted.
	pv := &api.PersistentVolume{
		ObjectMeta: api.ObjectMeta{
			Name:        "fake-pv",
			Annotations: map[string]string{},
		},
		Spec: api.PersistentVolumeSpec{
			PersistentVolumeSource:        api.PersistentVolumeSource{HostPath: &api.HostPathVolumeSource{Path: "/tmp/foo"}},
			Capacity:                      api.ResourceList{api.ResourceName(api.ResourceStorage): resource.MustParse("10G")},
			AccessModes:                   []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
			PersistentVolumeReclaimPolicy: api.PersistentVolumeReclaimRecycle,
		},
	}

	pvc := &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Name:        "fake-pvc",
			Annotations: map[string]string{},
		},
		Spec: api.PersistentVolumeClaimSpec{
			Resources:   api.ResourceRequirements{Requests: api.ResourceList{api.ResourceName(api.ResourceStorage): resource.MustParse("5G")}},
			AccessModes: []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
		},
	}

	// -- begin Recycler test,

	w, _ := testKubeClient.PersistentVolumes().Watch(labels.Everything(), fields.Everything(), "0")
	defer w.Stop()

	_, _ = testKubeClient.PersistentVolumes().Create(pv)
	_, _ = testKubeClient.PersistentVolumeClaims(api.NamespaceDefault).Create(pvc)

	// wait until the binder pairs the volume and claim
	waitForPersistentVolumePhase(w, api.VolumeBound)

	// deleting a claim releases the volume, after which it can be recycled
	if err := testKubeClient.PersistentVolumeClaims(api.NamespaceDefault).Delete(pvc.Name); err != nil {
		t.Errorf("error deleting claim %s", pvc.Name)
	}

	waitForPersistentVolumePhase(w, api.VolumeReleased)
	waitForPersistentVolumePhase(w, api.VolumeAvailable)

	// end of Recycler test.  Begin Deleter test.
	deleteAllEtcdKeys()

	// change the reclamation policy of the PV for the next test
	pv.Spec.PersistentVolumeReclaimPolicy = api.PersistentVolumeReclaimDelete

	_, _ = testKubeClient.PersistentVolumes().Create(pv)
	_, _ = testKubeClient.PersistentVolumeClaims(api.NamespaceDefault).Create(pvc)

	waitForPersistentVolumePhase(w, api.VolumeBound)

	// deleting a claim releases the volume, after which it can be deleted
	if err := testKubeClient.PersistentVolumeClaims(api.NamespaceDefault).Delete(pvc.Name); err != nil {
		t.Errorf("error deleting claim %s", pvc.Name)
	}

	waitForPersistentVolumePhase(w, api.VolumeReleased)
	waitForDelete(w)

	// end of Deleter test.  Begin Provisioner test.
	deleteAllEtcdKeys()

	pvc.Annotations["volume.experimental.kubernetes.io/quality-of-service"] = "foo"

	claim, err := testKubeClient.PersistentVolumeClaims(api.NamespaceDefault).Create(pvc)
	if err != nil {
		t.Errorf("Could not update PVClaim: %#v", claim)
	}
	glog.Infof("Waiting for new volume to be provisioned for %s", volumeclaimbinder.ClaimToProvisionableKey(claim))

	waitForPersistentVolumePhase(w, api.VolumeBound)

	pvlist, _ := testKubeClient.PersistentVolumes().List(labels.Everything(), fields.Everything())
	pv = &pvlist.Items[0]
	value, exists := pv.Annotations["volume.experimental.kubernetes.io/provisioned-for"]
	if !exists {
		t.Errorf("PV missing expected provisioned-for annotation: %#v: ", pv)
	}
	if value != volumeclaimbinder.ClaimToProvisionableKey(claim) {
		t.Errorf("PV expected to match claim.  Expected %s but got %s :", volumeclaimbinder.ClaimToProvisionableKey(claim), value)
	}

	// deleting a claim releases the volume, after which it can be deleted
	if err := testKubeClient.PersistentVolumeClaims(claim.Namespace).Delete(claim.Name); err != nil {
		t.Errorf("error deleting claim %s", claim.Name)
	}

	// this is a reprise of the Deleter test
	waitForPersistentVolumePhase(w, api.VolumeReleased)
	waitForDelete(w)
}

func waitForPersistentVolumePhase(w watch.Interface, phase api.PersistentVolumePhase) {
	for {
		event := <-w.ResultChan()
		volume := event.Object.(*api.PersistentVolume)
		if volume.Status.Phase == phase {
			break
		}
	}
}

func waitForDelete(w watch.Interface) {
	for {
		event := <-w.ResultChan()
		if event.Type == watch.Deleted {
			break
		}
	}
}

func getPVCreater() (volume.CreatableVolumePlugin, error) {
	plugin := host_path.ProbeVolumePlugins(volume.VolumeConfig{})[0]
	if creatableVolumePlugin, ok := plugin.(volume.CreatableVolumePlugin); ok {
		return creatableVolumePlugin, nil
	}
	return nil, fmt.Errorf("Error making HostPath Creater plugin")
}

/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

//
//import (
//	"fmt"
//
//	"k8s.io/kubernetes/pkg/api"
//	"k8s.io/kubernetes/pkg/api/errors"
//	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
//	"k8s.io/kubernetes/pkg/util"
//	"k8s.io/kubernetes/pkg/watch"
//)

//
//func TestProvisionerRunStop(t *testing.T) {
//	controller, _, _ := makeTestController()
//
//	if len(controller.stopChannels) != 0 {
//		t.Errorf("Non-running provisioner should not have any stopChannels.  Got %v", len(controller.stopChannels))
//	}
//
//	controller.Run()
//
//	if len(controller.stopChannels) != 2 {
//		t.Errorf("Running provisioner should have exactly 2 stopChannels.  Got %v", len(controller.stopChannels))
//	}
//
//	controller.Stop()
//
//	if len(controller.stopChannels) != 0 {
//		t.Errorf("Non-running provisioner should not have any stopChannels.  Got %v", len(controller.stopChannels))
//	}
//}
//
//func makeTestVolume() *api.PersistentVolume {
//	return &api.PersistentVolume{
//		ObjectMeta: api.ObjectMeta{
//			Annotations: map[string]string{},
//			Name:        "pv01",
//		},
//		Spec: api.PersistentVolumeSpec{
//			PersistentVolumeReclaimPolicy: api.PersistentVolumeReclaimDelete,
//			AccessModes:                   []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
//			Capacity: api.ResourceList{
//				api.ResourceName(api.ResourceStorage): resource.MustParse("10Gi"),
//			},
//			PersistentVolumeSource: api.PersistentVolumeSource{
//				HostPath: &api.HostPathVolumeSource{
//					Path: "/somepath/data01",
//				},
//			},
//		},
//	}
//}
//
//func makeTestClaim() *api.PersistentVolumeClaim {
//	return &api.PersistentVolumeClaim{
//		ObjectMeta: api.ObjectMeta{
//			Annotations: map[string]string{},
//			Name:        "claim01",
//			Namespace:   "ns",
//			SelfLink:    testapi.Default.SelfLink("pvc", ""),
//		},
//		Spec: api.PersistentVolumeClaimSpec{
//			AccessModes: []api.PersistentVolumeAccessMode{api.ReadWriteOnce},
//			Resources: api.ResourceRequirements{
//				Requests: api.ResourceList{
//					api.ResourceName(api.ResourceStorage): resource.MustParse("8G"),
//				},
//			},
//		},
//	}
//}
//
//func makeTestController() (*PersistentVolumeProvisionerController, *mockControllerClient, *volume.FakeVolumePlugin) {
//	mockClient := &mockControllerClient{}
//	mockVolumePlugin := &volume.FakeVolumePlugin{}
//	controller, _ := NewPersistentVolumeProvisionerController(mockClient, 1*time.Second, nil, mockVolumePlugin, &fake_cloud.FakeCloud{})
//	return controller, mockClient, mockVolumePlugin
//}
//
//func TestReconcileClaim(t *testing.T) {
//	controller, mockClient, _ := makeTestController()
//	pvc := makeTestClaim()
//
//	// watch would have added the claim to the store
//	controller.claimStore.Add(pvc)
//	err := controller.reconcileClaim(pvc)
//	if err != nil {
//		t.Errorf("Unexpected error: %v", err)
//	}
//
//	// non-provisionable PVC should not have created a volume on reconciliation
//	if mockClient.volume != nil {
//		t.Error("Unexpected volume found in mock client.  Expected nil")
//	}
//
//	pvc.Annotations[qosProvisioningKey] = "foo"
//
//	err = controller.reconcileClaim(pvc)
//	if err != nil {
//		t.Errorf("Unexpected error: %v", err)
//	}
//
//	// PVC requesting provisioning should have a PV created for it
//	if mockClient.volume == nil {
//		t.Error("Expected to find bound volume but got nil")
//	}
//
//	if mockClient.volume.Spec.ClaimRef.Name != pvc.Name {
//		t.Errorf("Expected PV to be bound to %s but got %s", mockClient.volume.Spec.ClaimRef.Name, pvc.Name)
//	}
//}
//
//func checkTagValue(t *testing.T, tags map[string]string, tag string, expectedValue string) {
//	value, found := tags[tag]
//	if !found || value != expectedValue {
//		t.Errorf("Expected tag value %s = %s but value %s found", tag, expectedValue, value)
//	}
//}
//
//func TestReconcileVolume(t *testing.T) {
//
//	controller, mockClient, mockVolumePlugin := makeTestController()
//	pv := makeTestVolume()
//	pvc := makeTestClaim()
//
//	err := controller.reconcileVolume(pv)
//	if err != nil {
//		t.Errorf("Unexpected error %v", err)
//	}
//
//	// watch adds claim to the store.
//	// we need to add it to our mock client to mimic normal Get call
//	controller.claimStore.Add(pvc)
//	mockClient.claim = pvc
//
//	// pretend the claim and volume are bound, no provisioning required
//	claimRef, _ := api.GetReference(pvc)
//	pv.Spec.ClaimRef = claimRef
//	err = controller.reconcileVolume(pv)
//	if err != nil {
//		t.Errorf("Unexpected error %v", err)
//	}
//
//	pv.Annotations[pvProvisioningRequiredAnnotationKey] = "!pvProvisioningCompleted"
//	pv.Annotations[qosProvisioningKey] = "foo"
//	err = controller.reconcileVolume(pv)
//
//	if !isAnnotationMatch(pvProvisioningRequiredAnnotationKey, pvProvisioningCompletedAnnotationValue, mockClient.volume.Annotations) {
//		t.Errorf("Expected %s but got %s", pvProvisioningRequiredAnnotationKey, mockClient.volume.Annotations[pvProvisioningRequiredAnnotationKey])
//	}
//
//	// Check that the volume plugin was called with correct tags
//	tags := *mockVolumePlugin.LastProvisionerOptions.CloudTags
//	checkTagValue(t, tags, cloudVolumeCreatedForClaimNamespaceTag, pvc.Namespace)
//	checkTagValue(t, tags, cloudVolumeCreatedForClaimNameTag, pvc.Name)
//	checkTagValue(t, tags, cloudVolumeCreatedForVolumeNameTag, pv.Name)
//
//}

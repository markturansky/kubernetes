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

package volumeclaimbinder

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/types"
	"k8s.io/kubernetes/pkg/util/io"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/watch"

	"github.com/golang/glog"
)

// PersistentVolumeController reconciles the state of all PersistentVolumes and PersistentVolumeClaims.
// This controller inspects a PV/C and adds the appropriate annotations to denote its condition.

//
type PersistentVolumeController struct {
	volumeIndex        *persistentVolumeOrderedIndex
	volumeController   *framework.Controller
	volumeStore        cache.Store
	claimController    *framework.Controller
	claimStore         cache.Store
	client             controllerClient
	cloud              cloudprovider.Interface
	provisionerPlugins map[string]volume.ProvisionableVolumePlugin
	pluginMgr          volume.VolumePluginMgr
	stopChannels       map[string]chan struct{}
	locks              map[string]*sync.RWMutex
}

// NewPersistentVolumeController creates a new PersistentVolumeController
func NewPersistentVolumeController(client controllerClient, syncPeriod time.Duration, plugins []volume.VolumePlugin, provisionerPlugins map[string]volume.ProvisionableVolumePlugin, cloud cloudprovider.Interface) (*PersistentVolumeController, error) {
	volumeIndex := NewPersistentVolumeOrderedIndex()
	controller := &PersistentVolumeController{
		volumeIndex:        volumeIndex,
		client:             client,
		cloud:              cloud,
		provisionerPlugins: provisionerPlugins,
		locks:              map[string]*sync.RWMutex{"_main": {}},
	}

	if err := controller.pluginMgr.InitPlugins(plugins, controller); err != nil {
		return nil, fmt.Errorf("Could not initialize volume plugins for PersistentVolumeController: %+v", err)
	}

	controller.volumeStore, controller.volumeController = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return client.ListPersistentVolumes(labels.Everything(), fields.Everything())
			},
			WatchFunc: func(resourceVersion string) (watch.Interface, error) {
				return client.WatchPersistentVolumes(labels.Everything(), fields.Everything(), resourceVersion)
			},
		},
		&api.PersistentVolume{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc:    controller.handleAddVolume,
			UpdateFunc: controller.handleUpdateVolume,
			DeleteFunc: controller.handleDeleteVolume,
		},
	)
	controller.claimStore, controller.claimController = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return client.ListPersistentVolumeClaims(api.NamespaceAll, labels.Everything(), fields.Everything())
			},
			WatchFunc: func(resourceVersion string) (watch.Interface, error) {
				return client.WatchPersistentVolumeClaims(api.NamespaceAll, labels.Everything(), fields.Everything(), resourceVersion)
			},
		},
		&api.PersistentVolumeClaim{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc:    controller.handleAddClaim,
			UpdateFunc: controller.handleUpdateClaim,
			DeleteFunc: controller.handleDeleteClaim,
		},
	)

	return controller, nil
}

func (controller *PersistentVolumeController) handleAddVolume(obj interface{}) {
	controller.locks["_main"].Lock()
	defer controller.locks["_main"].Unlock()
	cachedPv, _, _ := controller.volumeStore.Get(obj)
	if pv, ok := cachedPv.(*api.PersistentVolume); ok {
		controller.volumeIndex.Add(pv)
		pv, reconciledStatus, err := controller.reconcileVolume(pv)
		if err != nil {
			glog.Errorf("Error encoutered reconciling volume %s: %#v", pv.Name, err)
		}
		if pv.Status.Phase != reconciledStatus.Phase {
			pv.Status = reconciledStatus
			controller.client.UpdatePersistentVolumeStatus(pv)
		}
	}
}

func (controller *PersistentVolumeController) handleUpdateVolume(oldObj, newObj interface{}) {
	controller.handleAddVolume(newObj)
}

func (controller *PersistentVolumeController) handleDeleteVolume(obj interface{}) {
	controller.locks["_main"].Lock()
	defer controller.locks["_main"].Unlock()
	if del, deleted := obj.(cache.DeletedFinalStateUnknown); deleted {
		controller.volumeIndex.Delete(del.Obj)
		return
	}
	if _, ok := obj.(*api.PersistentVolume); ok {
		controller.volumeIndex.Delete(obj)
	}
}

func (controller *PersistentVolumeController) handleAddClaim(obj interface{}) {
	controller.locks["_main"].Lock()
	defer controller.locks["_main"].Unlock()
	cachedPvc, exists, _ := controller.claimStore.Get(obj)
	if !exists {
		glog.Errorf("PersistentVolumeCache does not exist in the local cache: %#v", obj)
		return
	}
	if pvc, ok := cachedPvc.(*api.PersistentVolumeClaim); ok {
		_, reconciledStatus, err := controller.reconcileClaim(pvc)
		if err != nil {
			glog.Errorf("Error encoutered reconciling claim %s: %#v", pvc.Name, err)
		}
		if pvc.Status.Phase != reconciledStatus.Phase {
			pvc.Status = reconciledStatus
			controller.client.UpdatePersistentVolumeClaimStatus(pvc)
		}
		glog.V(5).Infof("PersistentVolumeClaim[%s] is reconciled", pvc.Name)
	}
}

func (controller *PersistentVolumeController) handleUpdateClaim(oldObj, newObj interface{}) {
	controller.handleAddClaim(newObj)
}

func (controller *PersistentVolumeController) handleDeleteClaim(obj interface{}) {
	controller.locks["_main"].Lock()
	defer controller.locks["_main"].Unlock()
	if del, deleted := obj.(cache.DeletedFinalStateUnknown); deleted {
		controller.claimStore.Delete(del.Obj)
		return
	}
	if _, ok := obj.(*api.PersistentVolumeClaim); ok {
		controller.claimStore.Delete(obj)
	}
}

func (controller *PersistentVolumeController) reconcileClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, api.PersistentVolumeClaimStatus, error) {
	glog.V(5).Infof("Reconciling PersistentVolumeClaim[%s]", claim.Name)
	// claims can be in one of the following phases
	//
	//    ClaimPending -- default value -- not bound to a volume. A volume matching the claim may not exist.
	//    ClaimBound -- claim is bound to a volume and the volume exists.
	//    TODO: needs ClaimError phase/message like pv.status

	// Bound
	if claim.Spec.VolumeName != "" {
		glog.V(5).Infof("PVC[%s] is bound to %s", claim.Name, claim.Spec.VolumeName)
		obj, exists, _ := controller.volumeStore.GetByKey(claim.Spec.VolumeName)
		if !exists {
			glog.V(5).Infof("PersistentVolumeClaim[%s] could not find bound volume", claim.Name)
			return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("PersistentVolumeClaim[%s] could not find bound volume: %s", claim.Spec.VolumeName)
		}
		pv, _ := obj.(*api.PersistentVolume)
		if pv.Spec.ClaimRef == nil {
			glog.V(5).Infof("PVC[%s] - found volume to match claim's bind - %s", claim.Name, pv.Name)
			claimRef, err := api.GetReference(claim)
			if err != nil {
				return claim, api.PersistentVolumeClaimStatus{
					Phase:       api.ClaimPending,
					Capacity:    pv.Spec.Capacity,
					AccessModes: pv.Spec.AccessModes,
				}, fmt.Errorf("Error getting claim reference for bind")
			}
			pv.Spec.ClaimRef = claimRef
			_, err = controller.client.UpdatePersistentVolume(pv)
			if err != nil {
				glog.V(5).Infof("PVC[%s] - error updating PV %s with claimRef", claim.Name, claim.Spec.VolumeName)
			}
		}
		return claim, api.PersistentVolumeClaimStatus{
			Phase:       api.ClaimBound,
			Capacity:    pv.Spec.Capacity,
			AccessModes: pv.Spec.AccessModes,
		}, nil
	}

	// Claim is unbound.  Attempt to bind.  Volumes provisioned for this claim will match exclusively.
	pv, err := controller.volumeIndex.FindBestMatchForClaim(claim)
	if err != nil {
		glog.V(5).Infof("Error searching for volume! %v", err)
		return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("encountered error: %v", err)
	}

	// matching volume found!  bind to claim.  will trigger status change and bound phase is set above.
	if pv != nil {
		glog.V(5).Infof("PersistentVolumeClaim[%s] found matching volume: %s", claim.Name, pv.Name)
		claim.Spec.VolumeName = pv.Name
		claim, err = controller.client.UpdatePersistentVolumeClaim(claim)
		if err != nil {
			// rollback
			claim.Spec.VolumeName = ""
			return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("Error updating claim: %v", err)
		}
		claimRef, err := api.GetReference(claim)
		if err != nil {
			return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("Error getting reference to claim: %v", err)
		}
		// change the pointer on the volume so that future claims don't accidentally bind to it
		pv.Spec.ClaimRef = claimRef
		glog.V(5).Infof("PVC is now bound to %s", pv.Name)
		return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, nil
	}

	// no match found.  attempt provisioning.

	// no provisioning requested, return Pending. Claim may be pending indefinitely.
	if !keyExists(qosProvisioningKey, claim.Annotations) {
		glog.V(5).Infof("No provisioning required for PVC[%s]", claim.Name)
		return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, nil
	}

	// a quality-of-service annotation represents a specific kind of resource request by a user.
	// the qos value is opaque to the system and will be configurable per plugin.
	qos, _ := claim.Annotations[qosProvisioningKey]

	if plugin, exists := controller.provisionerPlugins[qos]; exists {
		glog.V(5).Infof("Provisioning PVC[%s]", claim.Name)
		volumeOptions := volume.VolumeOptions{
			Capacity:                      claim.Spec.Resources.Requests[api.ResourceName(api.ResourceStorage)],
			AccessModes:                   claim.Spec.AccessModes,
			PersistentVolumeReclaimPolicy: api.PersistentVolumeReclaimDelete,
		}

		provisioner, err := plugin.NewCreater(volumeOptions)
		newVolume, err := provisioner.NewPersistentVolumeTemplate()

		claimRef, err := api.GetReference(claim)
		if err != nil {
			return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("Unexpected error getting claim reference: %v\n", err)
		}

		// the creation of this volume is the bind to the claim.
		// The claim will match the volume during the next sync period when the volume is in the local cache
		newVolume.Spec.ClaimRef = claimRef
		newVolume.Annotations[provisionedForKey] = ClaimToProvisionableKey(claim)
		newVolume.Annotations[pvProvisioningRequired] = "true"
		newVolume.Annotations[qosProvisioningKey] = qos
		_, err = controller.client.CreatePersistentVolume(newVolume)
		glog.V(5).Infof("PersistentVolume[%s] created for PVC[%s]", newVolume.Name, claim.Name)
		if err != nil {
			return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("PersistentVolumeClaim[%s] failed provisioning: %+v", claim.Name, err)
		}
		return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, nil
	}

	return claim, api.PersistentVolumeClaimStatus{Phase: api.ClaimPending}, fmt.Errorf("No provisioner found for qos request: %s", qos)
}

func (controller *PersistentVolumeController) reconcileVolume(pv *api.PersistentVolume) (*api.PersistentVolume, api.PersistentVolumeStatus, error) {
	glog.V(5).Infof("PersistentVolume[%s] - Reconciling", pv.Name)

	// volumes can be in one of the following phases:
	//
	//  VolumeAvailable -- not bound to a claim
	//  VolumePending -- bound to a claim, but not yet provisioned in the provider.
	//  VolumeBound -- bound to a claim and verified with the provider.
	//  VolumeReleased -- unbound from a claim but requires reclamation
	//  VolumeFailed -- if a volume fails reconcilation for any reason

	// Available
	if pv.Spec.ClaimRef == nil {
		glog.V(5).Infof("PersistentVolume[%s] is available", pv.Name)
		return pv, api.PersistentVolumeStatus{Phase: api.VolumeAvailable}, nil
	}

	// Released and needs Recycling.  Add annotation to PV.
	if pvRequiresRecycleOrDelete(pv) {
		glog.V(5).Infof("PersistentVolume[%s] is released and requires recycling", pv.Name)
		// this may be the 1st annotation on the volume
		if pv.Annotations == nil {
			pv.Annotations = map[string]string{}
		}

		switch {
		case pv.Spec.PersistentVolumeReclaimPolicy == api.PersistentVolumeReclaimRecycle:
			pv.Annotations[pvRecycleRequired] = "true"
		case pv.Spec.PersistentVolumeReclaimPolicy == api.PersistentVolumeReclaimDelete:
			pv.Annotations[pvDeleteRequired] = "true"
		}
		pv, err := controller.client.UpdatePersistentVolume(pv)
		if err != nil {
			return pv, api.PersistentVolumeStatus{Phase: api.VolumeFailed, Message: err.Error()}, err
		}
		return pv, api.PersistentVolumeStatus{Phase: api.VolumeReleased}, nil
	}

	if awaitingRecycleToComplete(pv) {
		glog.V(5).Infof("PersistentVolume[%s] - requires recycling", pv.Name)
		// one lock per volume to allow parallel processing but limit activity per volume
		if _, exists := controller.locks[pv.Name]; !exists {
			controller.locks[pv.Name] = &sync.RWMutex{}
			glog.V(5).Infof("PersistentVolume[%s] - attempting recycling operation", pv.Name)
			// re-entrant via locked named with pv name
			go recycleVolume(pv, controller)
		}
		return pv, api.PersistentVolumeStatus{Phase: api.VolumeReleased}, nil
	}

	if awaitingDeleteToComplete(pv) {
		glog.V(5).Infof("PersistentVolume[%s] - requires deletion", pv.Name)
		// one lock per volume to allow parallel processing but limit activity per volume
		if _, exists := controller.locks[pv.Name]; !exists {
			controller.locks[pv.Name] = &sync.RWMutex{}
			glog.V(5).Infof("PersistentVolume[%s] - attempting deletion operation ", pv.Name)
			// re-entrant via locked named with pv name
			go deleteVolume(pv, controller)
		}
		return pv, api.PersistentVolumeStatus{Phase: api.VolumeReleased}, nil
	}

	if recycleIsComplete(pv) {
		oldClaimRef := pv.Spec.ClaimRef

		// making the ClaimRef nil is sufficient to make the volume available again via this controller
		pv.Spec.ClaimRef = nil
		_, err := controller.client.UpdatePersistentVolume(pv)
		if err != nil {
			//rollback values on pointer
			pv.Spec.ClaimRef = oldClaimRef
			return pv, api.PersistentVolumeStatus{Phase: api.VolumeFailed, Message: err.Error()}, err
		}
	}

	if deleteIsComplete(pv) {
		err := controller.client.DeletePersistentVolume(pv)
		if err != nil {
			return pv, api.PersistentVolumeStatus{Phase: api.VolumeFailed, Message: err.Error()}, err
		}
		return nil, api.PersistentVolumeStatus{Phase: api.VolumeReleased}, nil
	}

	// Newly Released -- claim recently deleted
	// making our own key (a leaky abstraction of the store) was required because attempting to get the object by ClaimRef fails (no Meta on object).
	obj, exists, _ := controller.claimStore.GetByKey(fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name))
	if !exists {
		glog.V(5).Infof("PersistentVolume[%s] - cannot find claim %s.  Checking with API server to confirm deletion.", pv.Name, pv.Spec.ClaimRef.Name)
		// store may be out-of-date. confirm with API server that PVC is deleted.
		claim, err := controller.client.GetPersistentVolumeClaim(pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				glog.V(5).Infof("PersistentVolume[%s] - is released", pv.Spec.ClaimRef.Name)
				return pv, api.PersistentVolumeStatus{Phase: api.VolumeReleased}, nil
			}
			glog.V(5).Infof("PersistentVolume[%s] - could not find claim %s", pv.Name, pv.Spec.ClaimRef.Name)
			return pv, api.PersistentVolumeStatus{
				Phase:   api.VolumeFailed,
				Message: "Error getting claim",
				Reason:  fmt.Sprintf("%v", err),
			}, err
		}

		// This condition seems unlikely, but the claim fetched from the API was not in the local store.
		// The rest of this reconcileVolume func expects a claim to have been found in the local store.
		obj = claim
	}

	claim := obj.(*api.PersistentVolumeClaim)

	// security check -- claim might not yet have been matched to the volume.
	if pv.Name != claim.Spec.VolumeName {
		glog.V(5).Infof("PersistentVolume[%s] - security mismatch.  Expecting %s but found %s", pv.Name, pv.Name, pv.Spec.ClaimRef.Name)
		glog.V(5).Infof("Security mismatch.  Expecting %s but found %s", pv.Name, claim.Spec.VolumeName)
		return pv, api.PersistentVolumeStatus{
			Phase:   api.VolumeFailed,
			Message: "Mismatched claim/volume names",
		}, fmt.Errorf("Mismatched claim/volume names - expected %s but found %s", claim.Spec.VolumeName, pv.Name)
	}

	// volume and claim match.  check provisioning.

	// no provisioning required, return Bound
	if !keyExists(pvProvisioningRequired, pv.Annotations) {
		glog.V(5).Infof("PersistentVolumeClaim[%s] - no provisioning required", claim.Spec.VolumeName)
		return pv, api.PersistentVolumeStatus{Phase: api.VolumeBound}, nil
	}

	// provisioning is completed, return Bound
	if isAnnotationMatch(pvProvisioningRequired, pvProvisioningCompleted, pv.Annotations) {
		glog.V(5).Infof("PersistentVolume[%s] - provisioning complete", pv.Name)
		// should be a provisioned match
		provisionedFor, _ := pv.Annotations[provisionedForKey]
		if provisionedFor != ClaimToProvisionableKey(claim) {
			return pv, api.PersistentVolumeStatus{
				Phase:   api.VolumeFailed,
				Message: "Mismatched claim/volume annotations",
			}, fmt.Errorf("pre-bind mismatch - expected %s but found %s", ClaimToProvisionableKey(claim), provisionedFor)
		}
		return pv, api.PersistentVolumeStatus{Phase: api.VolumeBound}, nil
	}

	// provisioning is not complete, return Pending
	if !isAnnotationMatch(pvProvisioningRequired, pvProvisioningCompleted, pv.Annotations) {
		glog.V(5).Infof("PersistentVolume[%s] - provisioning in progress", pv.Name)
		// one lock per volume to allow parallel processing but limit activity per volume
		if _, exists := controller.locks[pv.Name]; !exists {
			controller.locks[pv.Name] = &sync.RWMutex{}
			go provisionVolume(pv, controller)
		}
		return pv, api.PersistentVolumeStatus{Phase: api.VolumePending, Message: "Awaiting provisioning"}, nil
	}

	return pv, api.PersistentVolumeStatus{Phase: api.VolumeFailed, Message: "Unknown state"}, nil
}

//
func provisionVolume(pv *api.PersistentVolume, controller *PersistentVolumeController) {
	controller.locks[pv.Name].Lock()
	defer func(pv *api.PersistentVolume, controller *PersistentVolumeController) {
		controller.locks[pv.Name].Unlock()
		delete(controller.locks, pv.Name)
	}(pv, controller)

	qos, exists := pv.Annotations[qosProvisioningKey]
	if !exists {
		glog.V(5).Infof("No QoS tier on volume %s.  Provisioning not required.", pv.Name)
		return
	}

	provisioner, found := controller.provisionerPlugins[qos]
	if !found {
		glog.V(5).Infof("No provisioner found for tier %s", qos)
		return
	}

	creater, _ := provisioner.NewCreater(volume.VolumeOptions{})
	err := creater.Provision(pv)
	if err != nil {
		glog.Errorf("Could not provision %s", pv.Name)
		return
	}

	pv.Annotations[pvProvisioningRequired] = pvProvisioningCompleted
	pv, err = controller.client.UpdatePersistentVolume(pv)
	if err != nil {
		// TODO:  https://github.com/kubernetes/kubernetes/issues/14443
		// the volume was created in the infrastructure and likely has a PV name on it, but we failed to mark the provisioning completed.
		return
	}

	// only releasing the lock if successful to prevent dupes from being created in the infrastructure
	controller.locks[pv.Name].Unlock()
	delete(controller.locks, pv.Name)
}

func recycleVolume(pv *api.PersistentVolume, controller *PersistentVolumeController) {
	controller.locks[pv.Name].Lock()
	defer func(pv *api.PersistentVolume, controller *PersistentVolumeController) {
		controller.locks[pv.Name].Unlock()
		delete(controller.locks, pv.Name)
	}(pv, controller)

	spec := volume.NewSpecFromPersistentVolume(pv, false)
	plugin, err := controller.pluginMgr.FindRecyclablePluginBySpec(spec)
	if err != nil {
		glog.Errorf("Could not find recyclable volume plugin for spec: %+v", err)
		return
	}
	recycler, err := plugin.NewRecycler(spec)
	if err != nil {
		glog.Errorf("could not obtain Recycler for spec: %+v", err)
		return
	}
	// blocks until completion
	err = recycler.Recycle()
	if err != nil {
		glog.Errorf("PersistentVolume[%s] failed recycling: %+v", pv.Name, err)
		pv.Status.Message = fmt.Sprintf("Recycling error: %s", err)
		return
	}

	pv.Annotations[pvRecycleRequired] = pvRecycleCompleted
	_, err = controller.client.UpdatePersistentVolume(pv)
	if err != nil {
		glog.Errorf("PersistentVolume[%s] failed update: %#v", err)
		// rollback on the pv pointer
		pv.Annotations[pvRecycleRequired] = "true"
		return
	}
	glog.V(5).Infof("PersistentVolume[%s] successfully recycled through plugin\n", pv.Name)

}

func deleteVolume(pv *api.PersistentVolume, controller *PersistentVolumeController) {
	controller.locks[pv.Name].Lock()
	defer func(pv *api.PersistentVolume, controller *PersistentVolumeController) {
		controller.locks[pv.Name].Unlock()
		delete(controller.locks, pv.Name)
	}(pv, controller)

	spec := volume.NewSpecFromPersistentVolume(pv, false)
	plugin, err := controller.pluginMgr.FindDeletablePluginBySpec(spec)
	if err != nil {
		glog.Errorf("Could not find deletable volume plugin for spec: %+v", err)
		return
	}
	deleter, err := plugin.NewDeleter(spec)
	if err != nil {
		glog.Errorf("could not obtain Deleter for spec: %+v", err)
		return
	}
	// blocks until completion
	err = deleter.Delete()
	if err != nil {
		glog.Errorf("PersistentVolume[%s] failed deletion: %+v", pv.Name, err)
		pv.Status.Message = fmt.Sprintf("Deletion error: %s", err)
		return
	}

	pv.Annotations[pvDeleteRequired] = pvDeleteCompleted
	_, err = controller.client.UpdatePersistentVolume(pv)
	if err != nil {
		glog.Errorf("PersistentVolume[%s] failed update: %#v", err)
		// rollback on the pv pointer
		pv.Annotations[pvDeleteRequired] = "true"
		return
	}
	glog.V(5).Infof("PersistentVolume[%s] successfully deleted through plugin\n", pv.Name)
}

// Run starts all of this controller's control loops
func (controller *PersistentVolumeController) Run() {
	glog.V(5).Infof("Starting PersistentVolumeController\n")
	if controller.stopChannels == nil {
		controller.stopChannels = make(map[string]chan struct{})
	}

	if _, exists := controller.stopChannels["volumes"]; !exists {
		controller.stopChannels["volumes"] = make(chan struct{})
		go controller.volumeController.Run(controller.stopChannels["volumes"])
	}

	if _, exists := controller.stopChannels["claims"]; !exists {
		controller.stopChannels["claims"] = make(chan struct{})
		go controller.claimController.Run(controller.stopChannels["claims"])
	}
}

// Stop gracefully shuts down this controller
func (controller *PersistentVolumeController) Stop() {
	glog.V(5).Infof("Stopping PersistentVolumeController\n")
	for name, stopChan := range controller.stopChannels {
		close(stopChan)
		delete(controller.stopChannels, name)
	}
}

// helper funcs to aid code clarity
func pvRequiresRecycleOrDelete(pv *api.PersistentVolume) bool {
	return pv.Status.Phase == api.VolumeReleased &&
		isRecyclable(pv.Spec.PersistentVolumeReclaimPolicy) &&
		!keyExists(pvRecycleRequired, pv.Annotations) &&
		!keyExists(pvDeleteRequired, pv.Annotations)
}

func awaitingRecycleToComplete(pv *api.PersistentVolume) bool {
	return keyExists(pvRecycleRequired, pv.Annotations) && !isAnnotationMatch(pvRecycleRequired, pvRecycleCompleted, pv.Annotations)
}

func recycleIsComplete(pv *api.PersistentVolume) bool {
	return isAnnotationMatch(pvRecycleRequired, pvRecycleCompleted, pv.Annotations)
}

func awaitingDeleteToComplete(pv *api.PersistentVolume) bool {
	return keyExists(pvDeleteRequired, pv.Annotations) && !isAnnotationMatch(pvDeleteRequired, pvDeleteCompleted, pv.Annotations)
}

func deleteIsComplete(pv *api.PersistentVolume) bool {
	return isAnnotationMatch(pvDeleteRequired, pvDeleteCompleted, pv.Annotations)
}

// controllerClient abstracts access to PVs and PVCs.  Easy to mock for testing and wrap for real client.
type controllerClient interface {
	CreatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error)
	ListPersistentVolumes(labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeList, error)
	WatchPersistentVolumes(labels labels.Selector, fields fields.Selector, resourceVersion string) (watch.Interface, error)
	GetPersistentVolume(name string) (*api.PersistentVolume, error)
	UpdatePersistentVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	DeletePersistentVolume(volume *api.PersistentVolume) error
	UpdatePersistentVolumeStatus(volume *api.PersistentVolume) (*api.PersistentVolume, error)

	GetPersistentVolumeClaim(namespace, name string) (*api.PersistentVolumeClaim, error)
	ListPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeClaimList, error)
	WatchPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector, resourceVersion string) (watch.Interface, error)
	UpdatePersistentVolumeClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
	UpdatePersistentVolumeClaimStatus(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)

	// provided to give VolumeHost and plugins access to the kube client
	GetKubeClient() client.Interface
}

func NewControllerClient(c client.Interface) controllerClient {
	return &realControllerClient{c}
}

type realControllerClient struct {
	client client.Interface
}

func (c *realControllerClient) GetPersistentVolume(name string) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().Get(name)
}

func (c *realControllerClient) ListPersistentVolumes(labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeList, error) {
	return c.client.PersistentVolumes().List(labels, fields)
}

func (c *realControllerClient) WatchPersistentVolumes(labels labels.Selector, fields fields.Selector, resourceVersion string) (watch.Interface, error) {
	return c.client.PersistentVolumes().Watch(labels, fields, resourceVersion)
}

func (c *realControllerClient) CreatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().Create(pv)
}

func (c *realControllerClient) UpdatePersistentVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().Update(volume)
}

func (c *realControllerClient) DeletePersistentVolume(volume *api.PersistentVolume) error {
	return c.client.PersistentVolumes().Delete(volume.Name)
}

func (c *realControllerClient) UpdatePersistentVolumeStatus(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().UpdateStatus(volume)
}

func (c *realControllerClient) GetPersistentVolumeClaim(namespace, name string) (*api.PersistentVolumeClaim, error) {
	return c.client.PersistentVolumeClaims(namespace).Get(name)
}

func (c *realControllerClient) ListPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeClaimList, error) {
	return c.client.PersistentVolumeClaims(namespace).List(labels, fields)
}

func (c *realControllerClient) WatchPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector, resourceVersion string) (watch.Interface, error) {
	return c.client.PersistentVolumeClaims(namespace).Watch(labels, fields, resourceVersion)
}

func (c *realControllerClient) UpdatePersistentVolumeClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	return c.client.PersistentVolumeClaims(claim.Namespace).Update(claim)
}

func (c *realControllerClient) UpdatePersistentVolumeClaimStatus(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	return c.client.PersistentVolumeClaims(claim.Namespace).UpdateStatus(claim)
}

func (c *realControllerClient) GetKubeClient() client.Interface {
	return c.client
}

func keyExists(key string, haystack map[string]string) bool {
	_, exists := haystack[key]
	return exists
}

func isAnnotationMatch(key, needle string, haystack map[string]string) bool {
	value, exists := haystack[key]
	if !exists {
		return false
	}
	return value == needle
}

func isRecyclable(policy api.PersistentVolumeReclaimPolicy) bool {
	return policy == api.PersistentVolumeReclaimDelete || policy == api.PersistentVolumeReclaimRecycle
}

// VolumeHost implementation
// PersistentVolumeRecycler is host to the volume plugins, but does not actually mount any volumes.
// Because no mounting is performed, most of the VolumeHost methods are not implemented.
func (c *PersistentVolumeController) GetPluginDir(podUID string) string {
	return ""
}

func (c *PersistentVolumeController) GetPodVolumeDir(podUID types.UID, pluginName, volumeName string) string {
	return ""
}

func (c *PersistentVolumeController) GetPodPluginDir(podUID types.UID, pluginName string) string {
	return ""
}

func (c *PersistentVolumeController) GetKubeClient() client.Interface {
	return c.client.GetKubeClient()
}

func (c *PersistentVolumeController) NewWrapperBuilder(spec *volume.Spec, pod *api.Pod, opts volume.VolumeOptions) (volume.Builder, error) {
	return nil, fmt.Errorf("NewWrapperBuilder not supported by PVClaimBinder's VolumeHost implementation")
}

func (c *PersistentVolumeController) NewWrapperCleaner(spec *volume.Spec, podUID types.UID) (volume.Cleaner, error) {
	return nil, fmt.Errorf("NewWrapperCleaner not supported by PVClaimBinder's VolumeHost implementation")
}

func (c *PersistentVolumeController) GetCloudProvider() cloudprovider.Interface {
	return c.cloud
}

func (c *PersistentVolumeController) GetMounter() mount.Interface {
	return nil
}

func (c *PersistentVolumeController) GetWriter() io.Writer {
	return nil
}

const (
	pvRecycleRequired  = "volume.experimental.kubernetes.io/recycle-required"
	pvRecycleCompleted = "volume.experimental.kubernetes.io/recycle-completed"

	pvDeleteRequired  = "volume.experimental.kubernetes.io/delete-required"
	pvDeleteCompleted = "volume.experimental.kubernetes.io/delete-completed"

	pvProvisioningRequired  = "volume.experimental.kubernetes.io/provisioning-required"
	pvProvisioningCompleted = "volume.experimental.kubernetes.io/provisioning-completed"
)

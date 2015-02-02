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
	"sync"
	"time"

	"fmt"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/cache"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/golang/glog"
)

// PersistentVolumeController is responsible for tracking volumes in the system
type PersistentVolumeController struct {
	volumeStore cache.Store
	claimStore  cache.Store
	client      persistentVolumeControllerClient
	volumeIndex PersistentVolumeIndex
}

// NewPersistentVolumeController creates a new PersistentVolumeController
func NewPersistentVolumeController(kubeClient client.Interface) *PersistentVolumeController {

	pvListWatcher := &ListWatcherImpl{
		ListFunc: func() (runtime.Object, error) {
			return kubeClient.PersistentVolumes(api.NamespaceAll).List(labels.Everything())
		},
		WatchFunc: func(resourceVersion string) (watch.Interface, error) {
			return kubeClient.PersistentVolumes(api.NamespaceAll).Watch(labels.Everything(), labels.Everything(), resourceVersion)
		},
	}

	pvcListWatcher := &ListWatcherImpl{
		ListFunc: func() (runtime.Object, error) {
			return kubeClient.PersistentVolumeClaims(api.NamespaceAll).List(labels.Everything())
		},
		WatchFunc: func(resourceVersion string) (watch.Interface, error) {
			return kubeClient.PersistentVolumeClaims(api.NamespaceAll).Watch(labels.Everything(), labels.Everything(), resourceVersion)
		},
	}
	volumeStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	cache.NewReflector(pvListWatcher, &api.PersistentVolume{}, volumeStore).Run()

	claimStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	cache.NewReflector(pvcListWatcher, &api.PersistentVolumeClaim{}, claimStore).Run()

	client := &persistentVolumeControllerClientImpl{
		UpdateVolumeFunc: func(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
			return kubeClient.PersistentVolumes(api.NamespaceAll).Update(volume)
		},
		UpdateClaimFunc: func(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
			return kubeClient.PersistentVolumeClaims(claim.Namespace).Update(claim)
		},
		GetClaimFunc: func(name, namespace string)  (*api.PersistentVolumeClaim, error) {
			return kubeClient.PersistentVolumeClaims(namespace).Get(name)
		},
	}

	controller := &PersistentVolumeController{
		volumeStore: volumeStore,
		claimStore:  claimStore,
		client:      client,
		volumeIndex: NewPersistentVolumeIndex(),
	}

	return controller
}

func (controller *PersistentVolumeController) Run(period time.Duration) {
	glog.V(5).Infof("Starting PersistentVolumeController\n")
	go util.Forever(func() { controller.synchronize() }, period)
}

func (controller *PersistentVolumeController) synchronize() {
	volumeReconciler := Reconciler{
		ListFunc:      controller.volumeStore.List,
		ReconcileFunc: controller.syncPersistentVolume,
	}

	claimsReconciler := Reconciler{
		ListFunc:      controller.claimStore.List,
		ReconcileFunc: controller.syncPersistentVolumeClaim,
	}

	controller.reconcile(volumeReconciler, claimsReconciler)
}

func (controller *PersistentVolumeController) syncPersistentVolume(obj interface{}) (interface{}, error) {
	volume := obj.(*api.PersistentVolume)
	glog.V(5).Infof("Synchronizing persistent volume: %s\n", volume.Name)

	// bring all newly found volumes under management
	if !controller.volumeIndex.Exists(volume) {
		controller.volumeIndex.Add(volume)
		glog.V(2).Infof("Managing PersistentVolume[UID=%s]\n", volume.UID)
	}

	// TODO index needs Remove methods to keep available storage in sync.

	// verify the volume is still claimed by a user
	if volume.Status.PersistentVolumeClaimReference != nil {
		if _, err := controller.client.GetClaim(volume.Status.PersistentVolumeClaimReference.Name, volume.Status.PersistentVolumeClaimReference.Namespace); err == nil {
			glog.V(5).Infof("%s has a bound claim: %s", volume.Name, volume.Status.PersistentVolumeClaimReference.Name)
		} else {
			//claim was deleted by user.
			glog.V(2).Infof("PersistentVolumeClaim[UID=%s] unbound from PersistentVolume[UID=%s]\n", volume.Status.PersistentVolumeClaimReference.UID, volume.UID)
			volume.Status.PersistentVolumeClaimReference = nil
			controller.client.UpdateVolume(volume)
		}
	}

	return volume, nil
}

func (controller *PersistentVolumeController) syncPersistentVolumeClaim(obj interface{}) (interface{}, error) {
	claim := obj.(*api.PersistentVolumeClaim)
	glog.V(5).Infof("Synchronizing persistent volume claim: %v\n", obj)

	if claim.Status.PersistentVolumeReference != nil {
		glog.V(5).Infof("Claim bound to : %s\n", claim.Status.PersistentVolumeReference.Name)
		return obj, nil
	}


	glog.V(5).Infof("Attempting match for claim: %s\n", claim.Name)

	volume := controller.volumeIndex.Match(claim)

	if volume != nil {
		claimRef, err := api.GetReference(claim)
		if err != nil {
			return nil, fmt.Errorf("Unexpected error making claim reference: %v\n", err)
		}

		volumeRef, err := api.GetReference(volume)
		if err != nil {
			return nil, fmt.Errorf("Unexpected error making volume reference: %v\n", err)
		}

		volume.Status.PersistentVolumeClaimReference = claimRef
		claim.Status.PersistentVolumeReference = volumeRef

		controller.client.UpdateVolume(volume)
		controller.client.UpdateClaim(claim)

		glog.V(2).Infof("PersistentVolumeClaim[UID=%s] bound to PersistentVolume[UID=%s]\n", claim.UID, volume.UID)

	} else {
		glog.V(5).Infof("No volume match found for %s\n", claim.UID)
	}

	return obj, nil
}

//
// generic Reconciler & reconciliation loop, because we're reconciling two Kinds in this controller
//
type Reconciler struct {
	ListFunc      func() []interface{}
	ReconcileFunc func(interface{}) (interface{}, error)
}

func (controller *PersistentVolumeController) reconcile(reconcilers ...Reconciler) {

	for _, reconciler := range reconcilers {

		items := reconciler.ListFunc()

		if len(items) == 0 {
			return
		}

		wg := sync.WaitGroup{}
		wg.Add(len(items))
		for ix := range items {
			go func(ix int) {
				defer wg.Done()
				obj := items[ix]
				glog.V(5).Infof("Reconciliation of %v", obj)
				obj, err := reconciler.ReconcileFunc(obj)
				if err != nil {
					glog.Errorf("Error reconciling: %v", err)
				}
			}(ix)
		}

		wg.Wait()

	}
}

//
// decouple kubeClient from the controller by wrapping it in a narrow, private interface
//
type persistentVolumeControllerClient interface {
	UpdateVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	UpdateClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
	GetClaim(name, namespace string) (*api.PersistentVolumeClaim, error)
}

type persistentVolumeControllerClientImpl struct {
	UpdateVolumeFunc func(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	UpdateClaimFunc  func(volume *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
	GetClaimFunc  func(name, namespace string) (*api.PersistentVolumeClaim, error)
}

func (i *persistentVolumeControllerClientImpl) UpdateVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
	return i.UpdateVolumeFunc(volume)
}

func (i *persistentVolumeControllerClientImpl) UpdateClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	return i.UpdateClaimFunc(claim)
}

func (i *persistentVolumeControllerClientImpl) GetClaim(name, namespace string) (*api.PersistentVolumeClaim, error) {
	return i.GetClaimFunc(name, namespace)
}

//
// generic pattern for ListWatcher rather than creating a new ListWatcher impl for each Kind I want to watch
//
type ListWatcherImpl struct {
	ListFunc  func() (runtime.Object, error)
	WatchFunc func(resourceVersion string) (watch.Interface, error)
}

func (lw *ListWatcherImpl) List() (runtime.Object, error) {
	return lw.ListFunc()
}

func (lw *ListWatcherImpl) Watch(resourceVersion string) (watch.Interface, error) {
	return lw.WatchFunc(resourceVersion)
}

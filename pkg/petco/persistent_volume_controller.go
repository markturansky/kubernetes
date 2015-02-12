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

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/cache"
	"fmt"
)

// PersistentVolumeController is responsible for tracking volumes in the system
type PersistentVolumeController struct {
	volumeStore cache.Store
	claimStore 	cache.Store
	client 		persistentVolumeControllerClient
	volumeIndex PersistentVolumeIndex
}

// NewPersistentVolumeController creates a new PersistentVolumeController
func NewPersistentVolumeController(kubeClient client.Interface) *PersistentVolumeController {

	pvListWatcher := &ListWatcherImpl{
		ListFunc: func()(runtime.Object, error){
			return kubeClient.PersistentVolumes(api.NamespaceAll).List(labels.Everything())
		},
		WatchFunc: func(resourceVersion string)(watch.Interface, error){
			return kubeClient.PersistentVolumes(api.NamespaceAll).Watch(labels.Everything(), labels.Everything(), resourceVersion)
		},
	}

	pvcListWatcher := &ListWatcherImpl{
		ListFunc: func()(runtime.Object, error){
			return kubeClient.PersistentVolumeClaims(api.NamespaceAll).List(labels.Everything())
		},
		WatchFunc: func(resourceVersion string)(watch.Interface, error){
			return kubeClient.PersistentVolumeClaims(api.NamespaceAll).Watch(labels.Everything(), labels.Everything(), resourceVersion)
		},
	}
	volumeStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	cache.NewReflector(pvListWatcher, &api.PersistentVolume{}, volumeStore).Run()

	claimStore := cache.NewStore(cache.MetaNamespaceKeyFunc)
	cache.NewReflector(pvcListWatcher, &api.PersistentVolumeClaim{}, claimStore).Run()

	client := &persistentVolumeControllerClientImpl{
		UpdateVolumeFunc: func(volume *api.PersistentVolume)(*api.PersistentVolume, error){
			return kubeClient.PersistentVolumes(api.NamespaceDefault).Update(volume)
		},
		UpdateClaimFunc: func(claim *api.PersistentVolumeClaim)(*api.PersistentVolumeClaim, error){
			return kubeClient.PersistentVolumeClaims(claim.Namespace).Update(claim)
		},
	}

	controller := &PersistentVolumeController{
		volumeStore: volumeStore,
		claimStore: claimStore,
		client: client,
		volumeIndex: NewPersistentVolumeIndex(),
	}

	return controller
}


func (controller *PersistentVolumeController) Run(period time.Duration) {
	glog.V(5).Infof("Starting PersistentVolumeController\n")
	go util.Forever(func() { controller.synchronize() }, period)
}

func (controller *PersistentVolumeController) synchronize() {
	glog.V(5).Infof("Beginning persistent volume controller sync\n")

	volumeReconciler := Reconciler {
		ListFunc: controller.volumeStore.List,
		ReconcileFunc: controller.syncPersistentVolume,
	}

	claimsReconciler := Reconciler {
		ListFunc: controller.claimStore.List,
		ReconcileFunc: controller.syncPersistentVolumeClaim,
	}

	controller.reconcile(volumeReconciler, claimsReconciler)
	glog.V(5).Infof("Exiting persistent volume controller sync\n")
}






func (controller *PersistentVolumeController) syncPersistentVolume(obj interface{}) (interface{}, error) {
	volume := obj.(*api.PersistentVolume)
	glog.V(5).Infof("Synchronizing persistent volume: %+v\n", obj)


	// bring all newly found volumes under management
	if !controller.volumeIndex.Exists(volume){
		controller.volumeIndex.Add(volume)
	}

	// verify the volume is still claimed by a user
	if volume.Status.PersistentVolumeClaimReference != nil {
		if claim, exists, _ := controller.claimStore.Get(volume.Status.PersistentVolumeClaimReference); exists {
			glog.V(5).Infof("has a bound claim!! %+v\n", claim)
		} else {
			//claim was deleted by user.
			volume.Status.PersistentVolumeClaimReference = nil
			controller.client.UpdateVolume(volume)
		}
	}

	return obj, nil
}


func (controller *PersistentVolumeController) syncPersistentVolumeClaim(obj interface{}) (interface{}, error) {
	claim := obj.(*api.PersistentVolumeClaim)
	glog.V(5).Infof("Synchronizing persistent volume claim: %v\n", obj)


	if claim.Status.PersistentVolumeReference == nil {

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

			controller.client.UpdateClaim(claim)
			controller.client.UpdateVolume(volume)
		}
	}

	return obj, nil
}


//
// generic Reconciler & reconciliation loop, because we're reconciling two Kinds in this controller
//
type Reconciler struct {
	ListFunc	func() []interface{}
	ReconcileFunc func(interface{}) (interface{}, error)
}

func (controller *PersistentVolumeController) reconcile(reconcilers ...Reconciler ){

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
}

type persistentVolumeControllerClientImpl struct {
	UpdateVolumeFunc func(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	UpdateClaimFunc func(volume *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
}

func (i *persistentVolumeControllerClientImpl) UpdateVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error){
	return i.UpdateVolumeFunc(volume)
}

func (i *persistentVolumeControllerClientImpl) UpdateClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error){
	return i.UpdateClaimFunc(claim)
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

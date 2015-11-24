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

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/conversion"
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
type PersistentVolumeController struct {
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
	controller := &PersistentVolumeController{
		client:             client,
		cloud:              cloud,
		provisionerPlugins: provisionerPlugins,
		locks:              map[string]*sync.RWMutex{"_main": {}},
	}

	if err := controller.pluginMgr.InitPlugins(plugins, controller); err != nil {
		return nil, fmt.Errorf("Could not initialize volume plugins for PersistentVolumeController: %+v", err)
	}

	for qosClass, p := range provisionerPlugins {
		glog.V(5).Infof("For quality-of-service tier %s use provisioner %s", qosClass, p.Name())
		p.Init(controller)
	}

	controller.volumeStore, controller.volumeController = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return client.ListPersistentVolumes(labels.Everything(), fields.Everything())
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.WatchPersistentVolumes(labels.Everything(), fields.Everything(), options)
			},
		},
		&api.PersistentVolume{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc:    controller.handleAddVolume,
			UpdateFunc: controller.handleUpdateVolume,
			//			DeleteFunc: controller.handleDeleteVolume,
		},
	)
	controller.claimStore, controller.claimController = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return client.ListPersistentVolumeClaims(api.NamespaceAll, labels.Everything(), fields.Everything())
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.WatchPersistentVolumeClaims(api.NamespaceAll, labels.Everything(), fields.Everything(), options)
			},
		},
		&api.PersistentVolumeClaim{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc:    controller.handleAddClaim,
			UpdateFunc: controller.handleUpdateClaim,
			//			DeleteFunc: controller.handleDeleteClaim,
		},
	)

	return controller, nil
}

func (controller *PersistentVolumeController) handleAddVolume(obj interface{}) {
	controller.locks["_main"].Lock()
	defer controller.locks["_main"].Unlock()
	cachedPv, _, _ := controller.volumeStore.Get(obj)
	if pv, ok := cachedPv.(*api.PersistentVolume); ok {
		err := controller.reconcileVolume(pv)
		if err != nil {
			glog.Errorf("Error encoutered reconciling volume %s: %+v", pv.Name, err)
		}
	}
}

func (controller *PersistentVolumeController) handleUpdateVolume(oldObj, newObj interface{}) {
	controller.handleAddVolume(newObj)
}

func (controller *PersistentVolumeController) handleAddClaim(obj interface{}) {
	controller.locks["_main"].Lock()
	defer controller.locks["_main"].Unlock()
	cachedPvc, exists, _ := controller.claimStore.Get(obj)
	if !exists {
		glog.Errorf("PersistentVolumeCache does not exist in the local cache: %+v", obj)
		return
	}
	if pvc, ok := cachedPvc.(*api.PersistentVolumeClaim); ok {
		err := controller.reconcileClaim(pvc)
		if err != nil {
			glog.Errorf("Error encoutered reconciling claim %s: %+v", pvc.Name, err)
		}
	}
}

func (controller *PersistentVolumeController) handleUpdateClaim(oldObj, newObj interface{}) {
	controller.handleAddClaim(newObj)
}

func (controller *PersistentVolumeController) reconcileClaim(claim *api.PersistentVolumeClaim) error {
	// no provisioning requested, return Pending. Claim may be pending indefinitely without a match.
	if !keyExists(qosProvisioningKey, claim.Annotations) {
		glog.V(5).Infof("PersistentVolumeClaim[%s] no provisioning required", claim.Name)
		return nil
	}
	if len(claim.Spec.VolumeName) != 0 {
		glog.V(5).Infof("PersistentVolumeClaim[%s] already bound. No provisioning required", claim.Name)
		return nil
	}
	if isAnnotationMatch(pvProvisioningRequired, pvProvisioningCompleted, claim.Annotations) {
		glog.V(5).Infof("PersistentVolumeClaim[%s] is already provisioned.", claim.Name)
		return nil
	}

	// a quality-of-service annotation represents a specific kind of resource request by a user.
	// the qos value is opaque to the system and will be configurable per plugin.
	qos, _ := claim.Annotations[qosProvisioningKey]

	if plugin, exists := controller.provisionerPlugins[qos]; exists {
		glog.V(5).Infof("PersistentVolumeClaim[%] provisioning", claim.Name)
		provisioner, err := newProvisioner(plugin, claim)
		if err != nil {
			return fmt.Errorf("Unexpected error getting new provisioner for QoS Class %s: %v\n", qos, err)
		}
		newVolume, err := provisioner.NewPersistentVolumeTemplate()
		if err != nil {
			return fmt.Errorf("Unexpected error getting new volume template for QoS Class %s: %v\n", qos, err)
		}

		claimRef, err := api.GetReference(claim)
		if err != nil {
			return fmt.Errorf("Unexpected error getting claim reference for %s: %v\n", claim.Name, err)
		}

		// the creation of this volume is the bind to the claim.
		// The claim will match the volume during the next sync period when the volume is in the local cache
		newVolume.Spec.ClaimRef = claimRef
		newVolume.Annotations[provisionedForKey] = ClaimToProvisionableKey(claim)
		newVolume.Annotations[pvProvisioningRequired] = "true"
		newVolume.Annotations[qosProvisioningKey] = qos
		newVolume, err = controller.client.CreatePersistentVolume(newVolume)
		glog.V(5).Infof("PersistentVolume[%s] created for PVC[%s]", newVolume.Name, claim.Name)
		if err != nil {
			return fmt.Errorf("PersistentVolumeClaim[%s] failed provisioning: %+v", claim.Name, err)
		}

		claim.Annotations[pvProvisioningRequired] = pvProvisioningCompleted
		_, err = controller.client.UpdatePersistentVolumeClaim(claim)
		if err != nil {
			glog.Error("error updating persistent volume claim: %v", err)
		}

		return nil
	}

	return fmt.Errorf("No provisioner found for qos request: %s", qos)
}

func (controller *PersistentVolumeController) reconcileVolume(pv *api.PersistentVolume) error {
	glog.V(5).Infof("PersistentVolume[%s] reconciling", pv.Name)

	if pv.Spec.ClaimRef == nil {
		glog.V(5).Infof("PersistentVolume[%s] is not bound to a claim.  No provisioning required", pv.Name)
		return nil
	}

	// TODO:  fix this leaky abstraction.  Had to make our own store key because ClaimRef fails the default keyfunc (no Meta on object).
	obj, exists, _ := controller.claimStore.GetByKey(fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name))
	if !exists {
		return fmt.Errorf("PersistentVolumeClaim[%s/%s] not found in local cache", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
	}

	claim := obj.(*api.PersistentVolumeClaim)

	// no provisioning required, volume is ready and Bound
	if !keyExists(pvProvisioningRequired, pv.Annotations) {
		glog.V(5).Infof("PersistentVolume[%s] does not require provisioning", pv.Name)
		return nil
	}

	// provisioning is completed, volume is ready and return Bound
	if isAnnotationMatch(pvProvisioningRequired, pvProvisioningCompleted, pv.Annotations) {
		glog.V(5).Infof("PersistentVolume[%s] is bound and provisioning is complete", pv.Name)
		provisionedFor, _ := pv.Annotations[provisionedForKey]
		if provisionedFor != ClaimToProvisionableKey(claim) {
			return fmt.Errorf("pre-bind mismatch - expected %s but found %s", ClaimToProvisionableKey(claim), provisionedFor)
		}
		return nil
	}

	// provisioning is not complete, return Pending
	if !isAnnotationMatch(pvProvisioningRequired, pvProvisioningCompleted, pv.Annotations) {
		glog.V(5).Infof("PersistentVolume[%s] provisioning in progress", pv.Name)
		// one lock per volume to allow parallel processing but limit activity per volume
		if _, exists := controller.locks[pv.Name]; !exists {
			controller.locks[pv.Name] = &sync.RWMutex{}
			provisionVolume(pv, controller)
		}

		return nil
	}

	return nil
}

// provisionVolume provisions a volume that has been created in the cluster but not yet fulfilled by
// the storage provider.  This func (and Recycle/Delete) locks on the PV.Name to limit 1 operation per volume at a time.
func provisionVolume(pv *api.PersistentVolume, controller *PersistentVolumeController) {
	controller.locks[pv.Name].Lock()
	defer func(pv *api.PersistentVolume, controller *PersistentVolumeController) {
		controller.locks[pv.Name].Unlock()
		delete(controller.locks, pv.Name)
	}(pv, controller)

	if isAnnotationMatch(pvProvisioningRequired, pvProvisioningCompleted, pv.Annotations) {
		glog.V(5).Infof("PersistentVolume[%s] is already provisioned", pv.Name)
		return
	}

	qos, exists := pv.Annotations[qosProvisioningKey]
	if !exists {

		fmt.Printf("shit fuc")
		glog.V(5).Infof("No QoS tier on volume %s.  Provisioning not required.", pv.Name)
		return
	}

	provisioner, found := controller.provisionerPlugins[qos]
	if !found {
		glog.V(5).Infof("No provisioner found for tier %s", qos)
		return
	}

	// Find the claim in local cache
	obj, exists, _ := controller.claimStore.GetByKey(fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name))
	if !exists {
		glog.V(5).Infof("No claim[%s] found for PV[%s]", pv.Spec.ClaimRef.Name, pv.Name)
		return
	}
	claim := obj.(*api.PersistentVolumeClaim)

	creater, _ := newProvisioner(provisioner, claim)
	err := creater.Provision(pv)
	if err != nil {
		glog.Errorf("Could not provision %s", pv.Name)
		pv.Status.Phase = api.VolumeFailed
		pv.Status.Message = err.Error()
		pv, err = controller.client.UpdatePersistentVolumeStatus(pv)
		if err != nil {
			glog.Errorf("Could not update %s", pv.Name)
		}
		return
	}

	clone, err := conversion.NewCloner().DeepCopy(pv)
	volumeClone, ok := clone.(*api.PersistentVolume)
	if !ok {
		glog.Errorf("Unexpected pv cast error : %v\n", volumeClone)
		return
	}
	volumeClone.Annotations[pvProvisioningRequired] = pvProvisioningCompleted

	pv, err = controller.client.UpdatePersistentVolume(volumeClone)
	if err != nil {
		// TODO:  https://github.com/kubernetes/kubernetes/issues/14443
		// the volume was created in the infrastructure and likely has a PV name on it,
		// but we failed to save the annotation that marks the volume as provisioned.
		return
	}
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

func isRecyclingRequired(pv *api.PersistentVolume) bool {
	return pv.Status.Phase == api.VolumeReleased &&
		isRecyclable(pv.Spec.PersistentVolumeReclaimPolicy) &&
		!keyExists(pvRecycleRequired, pv.Annotations) &&
		!keyExists(pvDeleteRequired, pv.Annotations)
}

func newProvisioner(plugin volume.ProvisionableVolumePlugin, claim *api.PersistentVolumeClaim) (volume.Creater, error) {
	volumeOptions := volume.VolumeOptions{
		Capacity:                      claim.Spec.Resources.Requests[api.ResourceName(api.ResourceStorage)],
		AccessModes:                   claim.Spec.AccessModes,
		PersistentVolumeReclaimPolicy: api.PersistentVolumeReclaimDelete,
	}

	provisioner, err := plugin.NewCreater(volumeOptions)
	return provisioner, err
}

func awaitingRecycleCompletion(pv *api.PersistentVolume) bool {
	return keyExists(pvRecycleRequired, pv.Annotations) && !isAnnotationMatch(pvRecycleRequired, pvRecycleCompleted, pv.Annotations)
}

func recycleIsComplete(pv *api.PersistentVolume) bool {
	return isAnnotationMatch(pvRecycleRequired, pvRecycleCompleted, pv.Annotations)
}

func awaitingDeleteCompletion(pv *api.PersistentVolume) bool {
	return keyExists(pvDeleteRequired, pv.Annotations) && !isAnnotationMatch(pvDeleteRequired, pvDeleteCompleted, pv.Annotations)
}

func deleteIsComplete(pv *api.PersistentVolume) bool {
	return keyExists(pvDeleteRequired, pv.Annotations) && isAnnotationMatch(pvDeleteRequired, pvDeleteCompleted, pv.Annotations)
}

// controllerClient abstracts access to PVs and PVCs.  Easy to mock for testing and wrap for real client.
type controllerClient interface {
	CreatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error)
	ListPersistentVolumes(labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeList, error)
	WatchPersistentVolumes(labels labels.Selector, fields fields.Selector, options api.ListOptions) (watch.Interface, error)
	GetPersistentVolume(name string) (*api.PersistentVolume, error)
	UpdatePersistentVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	DeletePersistentVolume(volume *api.PersistentVolume) error
	UpdatePersistentVolumeStatus(volume *api.PersistentVolume) (*api.PersistentVolume, error)

	GetPersistentVolumeClaim(namespace, name string) (*api.PersistentVolumeClaim, error)
	ListPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector) (*api.PersistentVolumeClaimList, error)
	WatchPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector, options api.ListOptions) (watch.Interface, error)
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

func (c *realControllerClient) WatchPersistentVolumes(labels labels.Selector, fields fields.Selector, options api.ListOptions) (watch.Interface, error) {
	return c.client.PersistentVolumes().Watch(labels, fields, options)
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

func (c *realControllerClient) WatchPersistentVolumeClaims(namespace string, labels labels.Selector, fields fields.Selector, options api.ListOptions) (watch.Interface, error) {
	return c.client.PersistentVolumeClaims(namespace).Watch(labels, fields, options)
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

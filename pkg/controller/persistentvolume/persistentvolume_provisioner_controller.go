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
	"sync"
	"time"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/types"
	ioutil "k8s.io/kubernetes/pkg/util/io"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/watch"
)

var _ volume.VolumeHost = &PersistentVolumeProvisioner{}

// PersistentVolumeProvisioner is a controller that watches for PersistentVolumes that are released from their claims.
// This controller will Recycle those volumes whose reclaim policy is set to PersistentVolumeReclaimRecycle and make them
// available again for a new claim.
type PersistentVolumeProvisioner struct {
	// the controller loop watching for PVClaims
	pvclaimController *framework.Controller
	// the controller loop watching for PVs
	volumeController *framework.Controller
	// the stop channels used to start/stop the control loops
	stopChannels map[string]chan struct{}
	// the cloud provider under which this provisioner is running
	cloudProvider cloudprovider.Interface
	// volume Creaters mapped to strings allows admins to configure volume creation
	creaters map[string]volume.CreatableVolumePlugin
	// a narrow interface to PVs
	client            provisionerClient
	kubeClient        client.Interface
	lock              sync.RWMutex
	provisionedClaims map[string]bool
}

// NewPersistentVolumeProvisioner creates a new PersistentVolumeProvisioner
func NewPersistentVolumeProvisioner(kubeClient client.Interface, syncPeriod time.Duration, creaters map[string]volume.CreatableVolumePlugin, cp cloudprovider.Interface) (*PersistentVolumeProvisioner, error) {
	provisionerClient := NewProvisionerClient(kubeClient)
	provisioner := &PersistentVolumeProvisioner{
		client:            provisionerClient,
		kubeClient:        kubeClient,
		creaters:          creaters,
		cloudProvider:     cp,
		provisionedClaims: map[string]bool{},
	}

	for _, creater := range provisioner.creaters {
		creater.Init(provisioner)
	}

	_, volumeController := framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return kubeClient.PersistentVolumes().List(labels.Everything(), fields.Everything())
			},
			WatchFunc: func(resourceVersion string) (watch.Interface, error) {
				return kubeClient.PersistentVolumes().Watch(labels.Everything(), fields.Everything(), resourceVersion)
			},
		},
		&api.PersistentVolume{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc:    provisioner.addVolume,
			UpdateFunc: provisioner.updateVolume,
			DeleteFunc: provisioner.deleteVolume,
		},
	)

	_, claimController := framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return kubeClient.PersistentVolumeClaims(api.NamespaceAll).List(labels.Everything(), fields.Everything())
			},
			WatchFunc: func(resourceVersion string) (watch.Interface, error) {

				return kubeClient.PersistentVolumeClaims(api.NamespaceAll).Watch(labels.Everything(), fields.Everything(), resourceVersion)
			},
		},
		&api.PersistentVolumeClaim{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc:    provisioner.addClaim,
			UpdateFunc: provisioner.updateClaim,
			// no DeleteFunc needed.  deleted claims require no provisioning
		},
	)

	provisioner.pvclaimController = claimController
	provisioner.volumeController = volumeController
	return provisioner, nil
}

func (provisioner *PersistentVolumeProvisioner) addVolume(obj interface{}) {
	provisioner.lock.Lock()
	defer provisioner.lock.Unlock()
	volume := obj.(*api.PersistentVolume)
	if claimKey, exists := volume.Annotations[provisionedForKey]; exists {
		provisioner.provisionedClaims[claimKey] = true
	}
}

func (provisioner *PersistentVolumeProvisioner) updateVolume(oldObj, newObj interface{}) {
	provisioner.lock.Lock()
	defer provisioner.lock.Unlock()
	volume := newObj.(*api.PersistentVolume)
	if claimKey, exists := volume.Annotations[provisionedForKey]; exists {
		provisioner.provisionedClaims[claimKey] = true
	}
}

func (provisioner *PersistentVolumeProvisioner) deleteVolume(obj interface{}) {
	provisioner.lock.Lock()
	defer provisioner.lock.Unlock()
	if _, deleted := obj.(cache.DeletedFinalStateUnknown); deleted {
		glog.V(5).Info("Missed delete event for a persistent volume")
		return
	}
	volume := obj.(*api.PersistentVolume)
	if claimKey, exists := volume.Annotations[provisionedForKey]; exists {
		delete(provisioner.provisionedClaims, claimKey)
	}
}

func (provisioner *PersistentVolumeProvisioner) addClaim(obj interface{}) {
	provisioner.lock.Lock()
	defer provisioner.lock.Unlock()
	claim := obj.(*api.PersistentVolumeClaim)
	if _, exists := claim.Annotations[provisionedKey]; exists {
		provisioner.provisionedClaims[ClaimToProvisionableKey(claim)] = true
	}

	qos, exists := claim.Annotations[qosProvisioningKey]
	if !exists {
		glog.V(5).Infof("No QoS tier on claim %s.  Provisioning not required.", claim.Name)
		return
	}

	creater, found := provisioner.creaters[qos]
	if !found {
		glog.V(5).Infof("No QoS tier on claim %s.  Provisioning not required.", claim.Name)
		return
	}

	err := provisionClaim(claim, creater, provisioner.client, provisioner.provisionedClaims)
	if err != nil {
		glog.Errorf("PersistentVolumeProvisioner could not handle claim %s: %+v", claim.Name, err)
	}
}

func (provisioner *PersistentVolumeProvisioner) updateClaim(oldObj, newObj interface{}) {
	provisioner.lock.Lock()
	defer provisioner.lock.Unlock()
	if claim, ok := newObj.(*api.PersistentVolumeClaim); ok {
		provisioner.addClaim(claim)
	}
}

func provisionClaim(pvc *api.PersistentVolumeClaim, plugin volume.CreatableVolumePlugin, client provisionerClient, provisionedClaims map[string]bool) error {
	// TODO: revisit this when field selectors can look for specific annotations
	if _, exists := pvc.Annotations[provisionableKey]; !exists {
		glog.V(5).Infof("PersistentVolumeClaim[%s/%s] missing provisionable annotation from binder.  No volume will be created.", pvc.Namespace, pvc.Name)
		return nil
	}

	if _, exists := pvc.Annotations[provisionedKey]; exists {
		glog.V(5).Infof("PersistentVolumeClaim[%s/%s] has already been provisioned.  No volume will be creatd.", pvc.Namespace, pvc.Name)
		return nil
	}

	if _, exists := provisionedClaims[ClaimToProvisionableKey(pvc)]; exists {
		glog.V(5).Infof("PersistentVolumeClaim[%s/%s] has already been provisioned.  No volume will be creatd.", pvc.Namespace, pvc.Name)
		return nil
	}

	qos, exists := pvc.Annotations[qosProvisioningKey]
	if !exists {
		return fmt.Errorf("PersistentVolumeClaim[%s/%s] missing quality of service annotation", pvc.Namespace, pvc.Name)
	}
	glog.V(5).Infof("Provisioning PersistentVolumeClaim[%s/%s]\n", pvc.Namespace, pvc.Name)

	volumeOptions := volume.VolumeOptions{
		Capacity:                      pvc.Spec.Resources.Requests[api.ResourceName(api.ResourceStorage)],
		AccessModes:                   pvc.Spec.AccessModes,
		PersistentVolumeReclaimPolicy: api.PersistentVolumeReclaimDelete,
		QOSTier: qos,
	}

	creater, err := plugin.NewCreater(volumeOptions)
	if err != nil {
		return fmt.Errorf("Could not obtain Creater from plugin %s", plugin.Name())
	}
	// blocks until completion
	pv, err := creater.Create()
	if err != nil {
		return fmt.Errorf("PersistentVolumeClaim[%s] failed provisioning: %+v", pvc.Name, err)
	}
	glog.V(5).Infof("PersistentVolume[%s] successfully created for %s/%s\n", pv.Name, pvc.Namespace, pvc.Name)

	// this prevents duplicates from being provisioned in case updating the PVC fails it's API update
	provisionedClaims[ClaimToProvisionableKey(pvc)] = true

	// this is a persistent record that the PVC has been provisioned that is also used to prevent dupes
	pvc.Annotations[provisionedKey] = "true"

	_, err = client.UpdatePersistentVolumeClaim(pvc)
	if err != nil {
		// this error is bad but not fatal because we will attempt storing the claim name in the PV's annotations
		glog.Errorf("PersistentVolumeClaim[%s] failed update.  Its provisioned annotation was not persisteed.")
	}

	pv.Annotations[provisionedForKey] = ClaimToProvisionableKey(pvc)
	pv, err = client.CreatePersistentVolume(pv)
	if err != nil {
		// the volume was created in the infrastructure but we failed to create a PV pointing to it.
		// the volume is now orphaned and there is no easy means of finding/recovering it.
		// TODO:  https://github.com/kubernetes/kubernetes/issues/14443
		return fmt.Errorf("Error creating PersistentVolume for claim %s: %+v", pvc.Name, err)
	}

	return nil
}

// Run starts this provisioner's control loops
func (provisioner *PersistentVolumeProvisioner) Run() {
	glog.V(5).Infof("Starting PersistentVolumeProvisioner\n")
	if provisioner.stopChannels == nil {
		provisioner.stopChannels = make(map[string]chan struct{})
	}
	if _, exists := provisioner.stopChannels["volumes"]; !exists {
		provisioner.stopChannels["volumes"] = make(chan struct{})
		go provisioner.volumeController.Run(provisioner.stopChannels["volumes"])
	}

	if _, exists := provisioner.stopChannels["claims"]; !exists {
		provisioner.stopChannels["claims"] = make(chan struct{})
		go provisioner.pvclaimController.Run(provisioner.stopChannels["claims"])
	}
}

// Stop gracefully shuts down this controller
func (provisioner *PersistentVolumeProvisioner) Stop() {
	glog.V(5).Infof("Stopping PersistentVolumeProvisioner\n")
	for name, stopChan := range provisioner.stopChannels {
		close(stopChan)
		delete(provisioner.stopChannels, name)
	}
}

// provisionerClient abstracts access to PVs and allows for easy mocks and testing
type provisionerClient interface {
	CreatePersistentVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	UpdatePersistentVolumeClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error)
}

func NewProvisionerClient(c client.Interface) provisionerClient {
	return &realProvisionerClient{c}
}

type realProvisionerClient struct {
	client client.Interface
}

func (c *realProvisionerClient) CreatePersistentVolume(pv *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().Create(pv)
}

func (c *realProvisionerClient) UpdatePersistentVolumeClaim(claim *api.PersistentVolumeClaim) (*api.PersistentVolumeClaim, error) {
	return c.client.PersistentVolumeClaims(claim.Namespace).Update(claim)
}

// TODO: refactor this volumeHost for re-use with persistentvolume_claim_binder_controller

// PersistentVolumeProvisioner is host to the volume plugins, but does not actually mount any volumes.
// Because no mounting is performed, most of the VolumeHost methods are not implemented.
func (p *PersistentVolumeProvisioner) GetPluginDir(podUID string) string {
	return ""
}

func (p *PersistentVolumeProvisioner) GetPodVolumeDir(podUID types.UID, pluginName, volumeName string) string {
	return ""
}

func (p *PersistentVolumeProvisioner) GetPodPluginDir(podUID types.UID, pluginName string) string {
	return ""
}

func (p *PersistentVolumeProvisioner) GetKubeClient() client.Interface {
	return p.kubeClient
}

func (p *PersistentVolumeProvisioner) NewWrapperBuilder(spec *volume.Spec, pod *api.Pod, opts volume.VolumeOptions) (volume.Builder, error) {
	return nil, fmt.Errorf("NewWrapperBuilder not supported by PersistentVolumeProvisioner's VolumeHost implementation")
}

func (p *PersistentVolumeProvisioner) NewWrapperCleaner(spec *volume.Spec, podUID types.UID) (volume.Cleaner, error) {
	return nil, fmt.Errorf("NewWrapperCleaner not supported by PersistentVolumeProvisioner's VolumeHost implementation")
}

func (p *PersistentVolumeProvisioner) GetCloudProvider() cloudprovider.Interface {
	return p.cloudProvider
}

func (p *PersistentVolumeProvisioner) GetMounter() mount.Interface {
	return nil
}

func (p *PersistentVolumeProvisioner) GetWriter() ioutil.Writer {
	return nil
}

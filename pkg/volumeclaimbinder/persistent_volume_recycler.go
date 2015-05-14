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
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/cache"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/controller/framework"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/volume"

	"github.com/golang/glog"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/mount"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/types"
)

// PersistentVolumeRecycler is a controller that synchronizes recycles PersistentVolume that have been released from their claims.
type PersistentVolumeRecycler struct {
	volumeController *framework.Controller
	stopChannel		chan struct{}
	client           recyclerClient
	kubeClient		 client.Interface
	pluginMgr		volume.VolumePluginMgr
}

// PersistentVolumeRecycler creates a new PersistentVolumeRecycler
func NewPersistentVolumeRecycler(kubeClient client.Interface, syncPeriod time.Duration, plugins []volume.VolumePlugin) (*PersistentVolumeRecycler, error) {
	recyclerClient := NewRecyclerClient(kubeClient)
	recycler := &PersistentVolumeRecycler{
		client:      recyclerClient,
		kubeClient:	 kubeClient,
	}

	if err := recycler.pluginMgr.InitPlugins(plugins, recycler); err != nil {
		return nil, fmt.Errorf("Could not initialize volume plugins for PVClaimBinder: %+v", err)
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
			AddFunc: func(obj interface{}) {
				pv := obj.(*api.PersistentVolume)
				recycler.recycleVolume(pv)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				pv := newObj.(*api.PersistentVolume)
				recycler.recycleVolume(pv)
			},
		},
	)

	recycler.volumeController = volumeController
	return recycler, nil
}

func (recycler *PersistentVolumeRecycler) recycleVolume(pv *api.PersistentVolume) error {

	if pv.Spec.RecyclePolicy == api.RecycleOnRelease && pv.Spec.ClaimRef != nil {
		spec := volume.NewSpecFromPersistentVolume(pv)
		plugin, err := recycler.pluginMgr.FindPluginBySpec(spec)
		if err != nil {
			errMsg := fmt.Sprintf("Could not find volume plugin for spec: %+v", pv)
			glog.Errorf(errMsg)
			return fmt.Errorf(errMsg)
		}
		pvPlugin, err := recycler.pluginMgr.FindPersistentPluginByName(plugin.Name())
		if err != nil {
			errMsg := fmt.Sprintf("Could not find persistent volume plugin for spec: %+v", pv)
			glog.Errorf(errMsg)
			return fmt.Errorf(errMsg)
		}
		volRecycler, err := pvPlugin.NewRecycler(spec)
		if err != nil {
			errMsg := fmt.Sprintf("Could not obtain Recycler for persistent volume plugin for spec: %+v", pv)
			glog.Errorf(errMsg)
			return fmt.Errorf(errMsg)
		} else {
			// blocks until completion
			// TODO: allow parallel recycling operations to increase throughput
			err := volRecycler.Recycle()

			if err != nil {
				glog.Errorf("Error recycling persistent volume %+v", pv)
			} else {
				pv.Status.Phase = api.VolumeAvailable
				pv, err := recycler.client.UpdatePersistentVolumeStatus(pv)
				if err != nil {
					glog.Errorf("Error updateing pv.Status: %+v", pv)
				}
			}
		}
	}
	return nil
}

// Run starts this recycler's control loops
func (recycler *PersistentVolumeRecycler) Run() {
	glog.V(5).Infof("Starting PersistentVolumeRecycler\n")
	if recycler.stopChannel == nil {
		recycler.stopChannel = make(chan struct{})
		go recycler.volumeController.Run(recycler.stopChannel)
	}
}

// Stop gracefully shuts down this binder
func (recycler *PersistentVolumeRecycler) Stop() {
	glog.V(5).Infof("Stopping PersistentVolumeRecycler\n")
	if recycler.stopChannel != nil {
		close(recycler.stopChannel)
		recycler.stopChannel = nil
	}
}

// recyclerClient abstracts access to PVs
type recyclerClient interface {
	GetPersistentVolume(name string) (*api.PersistentVolume, error)
	UpdatePersistentVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error)
	UpdatePersistentVolumeStatus(volume *api.PersistentVolume) (*api.PersistentVolume, error)
}

func NewRecyclerClient(c client.Interface) recyclerClient {
	return &realRecyclerClient{c}
}

type realRecyclerClient struct {
	client client.Interface
}

func (c *realRecyclerClient) GetPersistentVolume(name string) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().Get(name)
}

func (c *realRecyclerClient) UpdatePersistentVolume(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().Update(volume)
}

func (c *realRecyclerClient) UpdatePersistentVolumeStatus(volume *api.PersistentVolume) (*api.PersistentVolume, error) {
	return c.client.PersistentVolumes().UpdateStatus(volume)
}


// PersistentVolumeRecycler is host to the volume plugins, but does not actually mount any volumes.
// Because no mounting is performed, most of the VolumeHost methods are not implemented.
func (f *PersistentVolumeRecycler) GetPluginDir(podUID string) string {
	return ""
}

func (f *PersistentVolumeRecycler) GetPodVolumeDir(podUID types.UID, pluginName, volumeName string) string {
	return ""
}

func (f *PersistentVolumeRecycler) GetPodPluginDir(podUID types.UID, pluginName string) string {
	return ""
}

func (f *PersistentVolumeRecycler) GetKubeClient() client.Interface {
	return f.kubeClient
}

func (f *PersistentVolumeRecycler) NewWrapperBuilder(spec *volume.Spec, pod *api.Pod, opts volume.VolumeOptions, mounter mount.Interface) (volume.Builder, error) {
	return nil, fmt.Errorf("NewWrapperBuilder not supported by PVClaimBinder's VolumeHost implementation")
}

func (f *PersistentVolumeRecycler) NewWrapperCleaner(spec *volume.Spec, podUID types.UID, mounter mount.Interface) (volume.Cleaner, error) {
	return nil, fmt.Errorf("NewWrapperCleaner not supported by PVClaimBinder's VolumeHost implementation")
}

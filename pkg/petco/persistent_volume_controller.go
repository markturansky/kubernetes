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
)

// PersistentVolumeController is responsible for tracking volumes in the system
type PersistentVolumeController struct {
	kubeClient client.Interface
	syncTime   <-chan time.Time

	// To allow injection of syncUsage for testing.
	syncHandler func(volume api.PersistentVolume) error
}

// NewPersistentVolumeController creates a new PersistentVolumeController
func NewPersistentVolumeController(kubeClient client.Interface) *PersistentVolumeController {

	rm := &PersistentVolumeController{
		kubeClient: kubeClient,
	}

	// set the synchronization handler
	rm.syncHandler = rm.syncPersistentVolume
	return rm
}

// Run begins watching and syncing.
func (rm *PersistentVolumeController) Run(period time.Duration) {
	glog.V(5).Infof("Starting PersistentVolumeController\n")
	rm.syncTime = time.Tick(period)
	go util.Forever(func() { rm.synchronize() }, period)
}

func (rm *PersistentVolumeController) synchronize() {
	var volumes []api.PersistentVolume
	list, err := rm.kubeClient.PersistentVolumes(api.NamespaceAll).List(labels.Everything())
	if err != nil {
		glog.Errorf("Synchronization error: %v (%#v)", err, err)
	}
	volumes = list.Items
	wg := sync.WaitGroup{}
	wg.Add(len(volumes))
	for ix := range volumes {
		go func(ix int) {
			defer wg.Done()
			glog.V(4).Infof("periodic sync of %v/%v", volumes[ix].Namespace, volumes[ix].Name)
			err := rm.syncHandler(volumes[ix])
			if err != nil {
				glog.Errorf("Error synchronizing: %v", err)
			}
		}(ix)
	}
	wg.Wait()
}

// syncPersistentVolume runs a complete sync of current status
func (rm *PersistentVolumeController) syncPersistentVolume(volume api.PersistentVolume) (err error) {
	glog.V(5).Infof("Synchronizing persistent volumes: %v\n", volume)
	return nil
}

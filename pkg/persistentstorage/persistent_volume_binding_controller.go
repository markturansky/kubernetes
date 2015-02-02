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

package persistentstorage

import (
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
)

type PersistentVolumeBindingController struct {
	kubeClient      client.Interface
	syncTime		<-chan time.Time

	// to allow injection of handleWatch for testing
	watchHandler	func(persistentvolume api.PersistentVolume) error
}

// NewPersistentVolumeBindingController returns a new node controller to sync instances from cloudprovider.
func NewPersistentVolumeBindingController(kubeClient client.Interface) *PersistentVolumeBindingController {
	c := &PersistentVolumeBindingController{
		kubeClient:      kubeClient,
	}
	c.watchHandler = c.handleWatch
	return c

}

func (c *PersistentVolumeBindingController) Run(period time.Duration) {
	c.syncTime = time.Tick(period)
	resourceVersion := ""
	go util.Forever(func() { c.watchPersistentVolumes(&resourceVersion)}, period)
}


func (c *PersistentVolumeBindingController) watchPersistentVolumes(resourceVersion *string) {

	watching, err := c.kubeClient.PersistentVolumes(api.NamespaceAll).Watch(
		labels.Everything(),
		labels.Everything(),
		*resourceVersion,
	)

	if err != nil {
		util.HandleError((fmt.Errorf("unable to watch: %v", err)))
		time.Sleep(5 * time.Second)
		return
	}

	for {
		select {
		case <-c.syncTime:
			glog.V(4).Infof("Not sure if this is needed ... ")
		case event, open := <-watching.ResultChan():
			if !open {
				// watchChannel has been closed or something else went wrong with our etcd watch call.
				// Let the util.Forever that called us call us again.
				return
			}
			if event.Type == watch.Error {
				util.HandleError(fmt.Errorf("error from watch during sync: %v", errors.FromObject(event.Object)))
				continue
			}
			glog.V(4).Infof("Got watch: %#v", event)
			pv, ok := event.Object.(*api.PersistentVolume)
			if !ok {
				util.HandleError(fmt.Errorf("Unexpected object: %#v", event.Object))
				continue
			}

			*resourceVersion = pv.ResourceVersion
			c.watchHandler(*pv)
		}
	}
}

func (c *PersistentVolumeBindingController) handleWatch(pv api.PersistentVolume) error {
	glog.V(4).Infof("Observed watch of PV:  %+v", pv)
	return nil
}

/*
Copyright 2016 The Kubernetes Authors All rights reserved.

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
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/watch"

	"github.com/golang/glog"
)

// ThirdPartyAnalyticsController is a controller that synchronizes PersistentVolumeClaims.
type ThirdPartyAnalyticsController struct {
	podController *framework.Controller
}

// NewThirdPartyAnalyticsController creates a new ThirdPartyAnalyticsController
func NewThirdPartyAnalyticsController(kubeClient clientset.Interface) *ThirdPartyAnalyticsController {
	ctrl := &ThirdPartyAnalyticsController{}

	_, ctrl.podController = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return kubeClient.Core().Pods(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Core().Pods(api.NamespaceAll).Watch(options)
			},
		},
		&api.Pod{},
		0, // 0 means no re-sync
		framework.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, ok := obj.(*api.Pod)
				if !ok {
					glog.Errorf("Expected Pod but handler received %+v", obj)
					return
				}
				ctrl.track("Pod", "Add", pod.Namespace)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				pod, ok := newObj.(*api.Pod)
				if !ok {
					glog.Errorf("Expected Pod but handler received %+v", newObj)
					return
				}
				ctrl.track("Pod", "Update", pod.Namespace)
			},
			DeleteFunc: func(obj interface{}) {
				pod, ok := obj.(*api.Pod)
				if !ok {
					unk, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						glog.Errorf("Expected Pod but handler received %+v", obj)
						return
					}
					pod, ok = unk.Obj.(*api.Pod)
					if !ok {
						glog.Errorf("Expected Pod but handler received %+v", obj)
						return
					}
				}
				ctrl.track("Pod", "Delete", pod.Namespace)
			},
		},
	)

	return ctrl
}

func (c *ThirdPartyAnalyticsController) track(objName, action, namespace string) {
	// TODO: All of these values/keys need to come from config
	tracker := NewAnalyticsTracker()
	params := map[string]string{
		"host":                 "dev.openshift.redhat.com",
		"event":                fmt.Sprintf("%s_%s", strings.ToLower(objName), strings.ToLower(action)),
		"cv_email":             namespace,
		"cv_project_namespace": namespace,
	}

	if err := tracker.TrackEvent(namespace, params, "GET", "http://www.woopra.com/track/ce?%s"); err != nil {
		glog.Errorf("Error posting tracking event: %v", err)
	}
}

// Run starts all of this binder's control loops
func (controller *ThirdPartyAnalyticsController) Run(stopCh <-chan struct{}) {
	glog.V(5).Infof("Starting ThirdPartyAnalyticsController\n")
	go controller.podController.Run(stopCh)
}

type AnalyticsTracker interface {
	SaveEvent(objName, action, namespace string) error
}

func NewAnalyticsTracker() *realAnalyticsTracker {
	return &realAnalyticsTracker{}
}

type realAnalyticsTracker struct {
}

func (c *realAnalyticsTracker) TrackEvent(eventType string, params map[string]string, method, endpoint string) error {
	urlParams := url.Values{}
	for key, value := range params {
		urlParams[key] = []string{value}
	}
	if method == "GET" {
		resp, err := http.Get(fmt.Sprintf(endpoint, urlParams.Encode()))
		//	req.SetBasicAuth(AppID, SecretKey)
		if err != nil {
			return err
		}

		bodyText, err := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("json: %v", string(bodyText))
	}
	return nil
}

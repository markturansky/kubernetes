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
	"k8s.io/kubernetes/pkg/api/meta"
)

// ThirdPartyAnalyticsController is a controller that synchronizes PersistentVolumeClaims.
type ThirdPartyAnalyticsController struct {
	podController *framework.Controller
	controllers   map[string]*framework.Controller
	queue         *workqueue.Type
}

// NewThirdPartyAnalyticsController creates a new ThirdPartyAnalyticsController
func NewThirdPartyAnalyticsController(kubeClient clientset.Interface) *ThirdPartyAnalyticsController {
	ctrl := &ThirdPartyAnalyticsController{
		controllers: make(map[string]*framework.Controller),
		queue:       workqueue.New(),
	}

	watches := map[string]struct {
		objType   runtime.Object
		listFunc  func(options api.ListOptions) (runtime.Object, error)
		watchFunc func(options api.ListOptions) (watch.Interface, error)
	}{
		"pod": {
			objType: &api.Pod{},
			listFunc: func(options api.ListOptions) (runtime.Object, error) {
				return kubeClient.Core().Pods(api.NamespaceAll).List(options)
			},
			watchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Core().Pods(api.NamespaceAll).Watch(options)
			},
		},
		"replication_controller": {
			objType: &api.ReplicationController{},
			listFunc: func(options api.ListOptions) (runtime.Object, error) {
				return kubeClient.Core().ReplicationControllers(api.NamespaceAll).List(options)
			},
			watchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Core().ReplicationControllers(api.NamespaceAll).Watch(options)
			},
		},
		"pvclaim": {
			objType: &api.ReplicationController{},
			listFunc: func(options api.ListOptions) (runtime.Object, error) {
				return kubeClient.Core().PersistentVolumeClaims(api.NamespaceAll).List(options)
			},
			watchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Core().PersistentVolumeClaims(api.NamespaceAll).Watch(options)
			},
		},
		"secret": {
			objType: &api.ReplicationController{},
			listFunc: func(options api.ListOptions) (runtime.Object, error) {
				return kubeClient.Core().Secrets(api.NamespaceAll).List(options)
			},
			watchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Core().Secrets(api.NamespaceAll).Watch(options)
			},
		},
		"service": {
			objType: &api.ReplicationController{},
			listFunc: func(options api.ListOptions) (runtime.Object, error) {
				return kubeClient.Core().Services(api.NamespaceAll).List(options)
			},
			watchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Core().Services(api.NamespaceAll).Watch(options)
			},
		},
	}

	for name, watch := range watches {
		_, c := framework.NewInformer(
			&cache.ListWatch{
				ListFunc:  watch.listFunc,
				WatchFunc: watch.watchFunc,
			},
			watch.objType,
			0, // 0 is no re-sync
			framework.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					meta, err := meta.Accessor(obj)
					if err != nil {
						glog.Errorf("object has no meta: %v", err)
					}
					ctrl.queue(newEvent(name, "add", meta.GetNamespace()))
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					meta, err := meta.Accessor(newObj)
					if err != nil {
						glog.Errorf("object has no meta: %v", err)
					}
					ctrl.queue(newEvent(name, "update", meta.GetNamespace()))
				},
				DeleteFunc: func(obj interface{}) {
					unk, ok := obj.(cache.DeletedFinalStateUnknown)
					if ok {
						obj = unk.Obj
					}
					meta, err := meta.Accessor(obj)
					if err != nil {
						glog.Errorf("object has no meta: %v", err)
					}
					ctrl.queue(newEvent(name, "delete", meta.GetNamespace()))
				},
			},
		)

		ctrl.controllers[name] = c
	}

	return ctrl
}

// Run starts all of this binder's control loops
func (controller *ThirdPartyAnalyticsController) Run(stopCh <-chan struct{}) {
	glog.V(5).Infof("Starting ThirdPartyAnalyticsController\n")
	for name, c := range controller.controllers {
		glog.V(5).Infof("Starting watch for %s", name)
		go c.Run(stopCh)
	}
}

type AnalyticsTracker interface {
	SaveEvent(objName, action, namespace string) error
}

func NewAnalyticsTracker() *realAnalyticsTracker {
	return &realAnalyticsTracker{}
}

type realAnalyticsTracker struct {
}

func (c *realAnalyticsTracker) TrackEvent(params map[string]string, method, endpoint string) error {
	urlParams := url.Values{}
	for key, value := range params {
		urlParams.Add(key, value)
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

type analyticsEvent struct {
	objectName string
	action     string
	namespace  string
}

func newEvent(objName, action, namespace string) *analyticsEvent {
	return &analyticsEvent{objName, action, namespace}
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

	if err := tracker.TrackEvent(params, "GET", "http://www.woopra.com/track/ce?%s"); err != nil {
		glog.Errorf("Error posting tracking event: %v", err)
	}
}

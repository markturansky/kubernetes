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
	"time"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/client/cache"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/conversion"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/types"
	"k8s.io/kubernetes/pkg/util/io"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/util/wait"
	"k8s.io/kubernetes/pkg/util/workqueue"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/watch"
)

var _ volume.VolumeHost = &PersistentVolumeController{}

// PersistentVolumeController is a controller that watches for PersistentVolumes that are released from their claims.
// This controller will Recycle those volumes whose reclaim policy is set to PersistentVolumeReclaimRecycle and make them
// available again for a new claim.
type PersistentVolumeController struct {
	volumeController *framework.Controller
	volumeStore      cache.Store
	stopChannel      chan struct{}
	client           controllerClient
	kubeClient       clientset.Interface
	pluginMgr        volume.VolumePluginMgr
	cloud            cloudprovider.Interface
	syncPeriod       time.Duration
	// PersistentVolumes that need to be synced
	volumeQueue          *workqueue.Type
	workQueue            *workqueue.Type
	conditionQueues      map[api.PersistentVolumeConditionType]*workqueue.Type
	conditionHandlers    map[api.PersistentVolumeConditionType]func(obj interface{}) error
	conditionForgiveness map[api.PersistentVolumeConditionType]time.Duration
}

// NewPersistentVolumeController creates a new PersistentVolumeController
func NewPersistentVolumeController(controllerClient controllerClient, syncPeriod time.Duration, plugins []volume.VolumePlugin, cloud cloudprovider.Interface) (*PersistentVolumeController, error) {
	ctrl := &PersistentVolumeController{
		client:      controllerClient,
		cloud:       cloud,
		syncPeriod:  syncPeriod,
		volumeQueue: workqueue.New(),
		workQueue:   workqueue.New(),
	}

	if err := ctrl.pluginMgr.InitPlugins(plugins, ctrl); err != nil {
		return nil, fmt.Errorf("Could not initialize volume plugins for PersistentVolumeController: %+v", err)
	}

	ctrl.conditionQueues = map[api.PersistentVolumeConditionType]*workqueue.Type{api.PersistentVolumeBound: workqueue.New()}
	ctrl.conditionHandlers = map[api.PersistentVolumeConditionType]func(obj interface{}) error{
		api.PersistentVolumeBound:    ctrl.syncBoundCondition,
		api.PersistentVolumeRecycled: ctrl.syncRecycledCondition,
	}
	ctrl.conditionForgiveness = map[api.PersistentVolumeConditionType]time.Duration{
		api.PersistentVolumeBound:    syncPeriod,
		api.PersistentVolumeRecycled: syncPeriod,
	}

	ctrl.volumeStore, ctrl.volumeController = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return ctrl.client.ListPersistentVolumes(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return ctrl.client.WatchPersistentVolumes(options)
			},
		},
		&api.PersistentVolume{},
		syncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if pv, ok := obj.(*api.PersistentVolume); ok {
					ctrl.enqueueVolume(pv)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				if pv, ok := newObj.(*api.PersistentVolume); ok {
					ctrl.enqueueVolume(pv)
				}
			},
		},
	)

	return ctrl, nil
}

func (ctrl *PersistentVolumeController) enqueueVolume(pv *api.PersistentVolume) error {
	// might be *api.PVC or DeletionFinalStateUnknown
	key, err := controller.KeyFunc(pv)
	if err != nil {
		glog.Errorf("Couldn't get key for object %+v: %v", pv, err)
		return err
	}

	ctrl.volumeQueue.Add(key)
	return nil
}

func (ctrl *PersistentVolumeController) syncVolume(key interface{}) (err error) {
	glog.V(5).Infof("PersistentVolume[%v] syncing\n", key)

	obj, exists, err := ctrl.volumeStore.GetByKey(key.(string))
	if err != nil {
		glog.Infof("PersistentVolume[%v] not found in local store: %v", key, err)
		ctrl.volumeQueue.Add(key)
		return err
	}

	if !exists {
		glog.Infof("PersistentVolume[%v] has been deleted %v", key)
		return nil
	}

	volume := *obj.(*api.PersistentVolume)

	glog.V(5).Infof("PersistentVolume[%s] %d conditions:  %#v", key, len(volume.Status.Conditions), volume.Status.Conditions)

	for c, condition := range volume.Status.Conditions {
		ctrl.conditionQueues[condition.Type].Add(key)
		glog.V(5).Infof("PersistentVolume[%s] equeue for condition %v", key, c)
	}
	return nil
}

func (ctrl *PersistentVolumeController) syncBoundCondition(key interface{}) (err error) {
	glog.V(5).Infof("PersistentVolume[%s] syncing Bound condition\n", key)

	obj, exists, err := ctrl.volumeStore.GetByKey(key.(string))
	if err != nil {
		glog.Infof("PersistentVolume[%s] not found in local store: %v", key, err)
		return err
	}

	if !exists {
		glog.Infof("PersistentVolume[%s] has been deleted", key)
		return nil
	}

	pvc := obj.(*api.PersistentVolume)

	for _, condition := range pvc.Status.Conditions {
		glog.V(5).Infof("PersistentVolume[%s] syncing %v condition", key, condition)
		if condition.Type == api.PersistentVolumeBound && ctrl.hasExceededForgiveness(condition) {
			glog.V(5).Infof("PersistentVolume[%s] %v Condition has exceeded its forgiveness, attempting sync", key, api.PersistentVolumeBound)
			err := ctrl.syncBoundConditionForVolume(pvc)
			if err != nil {
				ctrl.conditionQueues[api.PersistentVolumeBound].Add(key)
				glog.V(5).Info("PersistentVolume[%s] is re-queued for %v: %v", key, api.PersistentVolumeBound, err)
				return err
			}
			glog.V(5).Infof("PersistentVolume[%s] is in sync with %v Condition", key, api.PersistentVolumeBound)
		}
	}
	return nil
}

func (ctrl *PersistentVolumeController) syncRecycledCondition(key interface{}) (err error) {
	glog.V(5).Infof("PersistentVolume[%s] syncing Recycled condition\n", key)

	obj, exists, err := ctrl.volumeStore.GetByKey(key.(string))
	if err != nil {
		glog.Infof("PersistentVolume[%s] not found in local store: %v", key, err)
		return err
	}

	if !exists {
		glog.Infof("PersistentVolume[%s] has been deleted %v", key)
		return nil
	}

	volume := obj.(*api.PersistentVolume)

	for _, condition := range volume.Status.Conditions {
		glog.V(5).Infof("PersistentVolume[%s] syncing %v condition", key, condition)
		if condition.Type == api.PersistentVolumeBound && ctrl.hasExceededForgiveness(condition) {
			glog.V(5).Infof("PersistentVolume[%s] %v Condition has exceeded its forgiveness, attempting sync", key, api.PersistentVolumeBound)
			err := ctrl.syncRecycledConditionForVolume(volume)
			if err != nil {
				ctrl.conditionQueues[api.PersistentVolumeBound].Add(key)
				glog.V(5).Info("PersistentVolume[%s] is re-queued for %v: %v", key, api.PersistentVolumeBound, err)
				return err
			}
			glog.V(5).Infof("PersistentVolume[%s] is in sync with %v Condition", key, api.PersistentVolumeBound)
		}
	}
	return nil
}

func (ctrl *PersistentVolumeController) syncBoundConditionForVolume(pv *api.PersistentVolume) (err error) {
	if len(pv.Status.Conditions) == 0 {
		return fmt.Errorf("PersistentVolume[%s] expected more than 0 Conditions, found %d\n", pv.Name, len(pv.Status.Conditions))
	}

	// a cloned volume is used to avoid mutating the object stored in local cache.
	// this is a locally scoped object clone (i.g, two instances of the same resource)
	clone, err := conversion.NewCloner().DeepCopy(pv)
	if err != nil {
		return fmt.Errorf("Error cloning pv: %v", err)
	}
	volume, ok := clone.(*api.PersistentVolume)
	if !ok {
		return fmt.Errorf("Unexpected pv cast error : %v\n", clone)
	}

	// set the latest heartbeat time on the object to take ownership of the volume.
	// a resource version error from the API means another process altered this volume.
	// the local volume may no longer reflect the current state of the resource.

	// any competing processes that attempts to persist the heartbeat will receive version error and return.
	boundIndex := 0
	for i, condition := range volume.Status.Conditions {
		if condition.Type == api.PersistentVolumeBound {
			boundIndex = i
			volume.Status.Conditions[i].LastProbeTime = unversioned.Now()
			updatedVolume, err := ctrl.client.UpdatePersistentVolumeStatus(volume)
			if err != nil {
				return fmt.Errorf("Error saving PersistentVolume.Status: %+v", err)
			}
			volume = updatedVolume
			break
		}
		return fmt.Errorf("PersistentVolume[%s] has no bound condition", volume.Name)
	}

	// volume is last updated by this process.
	// any concurrent changes to this volume while this sync is running will result
	// in a failure when attemping to persist to the API.

	if volume.Spec.ClaimRef == nil {
		volume.Status.Conditions[boundIndex] = api.PersistentVolumeCondition{
			Status:             api.ConditionFalse,
			Reason:             "NoClaimRef",
			Message:            "Volume did not have a binding claimRef",
			LastProbeTime:      unversioned.Now(),
			LastTransitionTime: unversioned.Now(),
		}
		_, err := ctrl.client.UpdatePersistentVolumeStatus(volume)
		if err != nil {
			return fmt.Errorf("Error saving PersistentVolume.Status: %+v", err)
		}
		return nil
	}

	// replace API lookup with a local claimStore w/ watch
	_, err = ctrl.client.GetPersistentVolumeClaim(volume.Spec.ClaimRef.Namespace, volume.Spec.ClaimRef.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			volume.Status.Conditions[0] = api.PersistentVolumeCondition{
				Status:             api.ConditionFalse,
				Reason:             "Released",
				Message:            "Claim was deleted",
				LastProbeTime:      unversioned.Now(),
				LastTransitionTime: unversioned.Now(),
			}

			// optionally add a new Condition for recycling
			if !hasCondition(volume.Status.Conditions, api.PersistentVolumeRecycled) {
				volume.Status.Conditions[1] = api.PersistentVolumeCondition{
					Type:               api.PersistentVolumeRecycled,
					Status:             api.ConditionFalse,
					Reason:             "New",
					LastProbeTime:      unversioned.Now(),
					LastTransitionTime: unversioned.Now(),
				}
			}
			_, err := ctrl.client.UpdatePersistentVolumeStatus(volume)
			if err != nil {
				return fmt.Errorf("Error saving PersistentVolume.Status: %+v", err)
			}
			// on next sync, recycler condition will handle the released volume
			return nil
		} else {
			return fmt.Errorf("Error retrieving PersistentVolumeClaim %s/%s: %v", volume.Spec.ClaimRef.Namespace, volume.Spec.ClaimRef.Name, err)
		}
	}
	return nil
}

func hasCondition(conditions []api.PersistentVolumeCondition, conditionType api.PersistentVolumeConditionType) bool {
	for _, c := range conditions {
		if c.Type == conditionType {
			return true
		}
	}
	return false
}

func (ctrl *PersistentVolumeController) syncRecycledConditionForVolume(volume *api.PersistentVolume) (err error) {
	return nil
}

func (ctrl *PersistentVolumeController) hasExceededForgiveness(condition api.PersistentVolumeCondition) bool {
	return condition.LastProbeTime.Add(ctrl.conditionForgiveness[condition.Type]).Before(time.Now())
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that syncClaim is never invoked concurrently with the same key.
func (ctrl *PersistentVolumeController) worker(queueName string, queue *workqueue.Type, handleFunc func(obj interface{}) error) {
	for {
		func() {
			glog.V(5).Infof("Waiting on queue %s with %d depth", queueName, queue.Len())
			key, quit := queue.Get()
			if quit {
				return
			}
			defer queue.Done(key)
			glog.V(5).Infof("Invoking handler %#v", handleFunc)
			err := handleFunc(key.(string))
			if err != nil {
				glog.Errorf("PersistentVolume[%s] error syncing: %v", err)
			}
		}()
	}
}

// Run starts this recycler's control loops
func (ctrl *PersistentVolumeController) Run(workers int, stopCh <-chan struct{}) {
	glog.V(5).Infof("Starting PersistentVolumeController\n")
	go ctrl.volumeController.Run(stopCh)
	for i := 0; i < workers; i++ {
		go wait.Until(func() { ctrl.worker("heartbeat", ctrl.volumeQueue, ctrl.syncVolume) }, time.Second, stopCh)
		for condition, queue := range ctrl.conditionQueues {
			if handler, exists := ctrl.conditionHandlers[condition]; exists {
				go wait.Until(func() { ctrl.worker("condition", queue, handler) }, time.Second, stopCh)
			}
		}
	}
}

// Stop gracefully shuts down this binder
func (recycler *PersistentVolumeController) Stop() {
	glog.V(5).Infof("Stopping PersistentVolumeController\n")
	if recycler.stopChannel != nil {
		close(recycler.stopChannel)
		recycler.stopChannel = nil
	}
}

// PersistentVolumeController is host to the volume plugins, but does not actually mount any volumes.
// Because no mounting is performed, most of the VolumeHost methods are not implemented.
func (f *PersistentVolumeController) GetPluginDir(podUID string) string {
	return ""
}

func (f *PersistentVolumeController) GetPodVolumeDir(podUID types.UID, pluginName, volumeName string) string {
	return ""
}

func (f *PersistentVolumeController) GetPodPluginDir(podUID types.UID, pluginName string) string {
	return ""
}

func (f *PersistentVolumeController) GetKubeClient() clientset.Interface {
	return f.kubeClient
}

func (f *PersistentVolumeController) NewWrapperBuilder(volName string, spec volume.Spec, pod *api.Pod, opts volume.VolumeOptions) (volume.Builder, error) {
	return nil, fmt.Errorf("NewWrapperBuilder not supported by PersistentVolumeController's VolumeHost implementation")
}

func (f *PersistentVolumeController) NewWrapperCleaner(volName string, spec volume.Spec, podUID types.UID) (volume.Cleaner, error) {
	return nil, fmt.Errorf("NewWrapperCleaner not supported by PersistentVolumeController's VolumeHost implementation")
}

func (f *PersistentVolumeController) GetCloudProvider() cloudprovider.Interface {
	return f.cloud
}

func (f *PersistentVolumeController) GetMounter() mount.Interface {
	return nil
}

func (f *PersistentVolumeController) GetWriter() io.Writer {
	return nil
}

func (f *PersistentVolumeController) GetHostName() string {
	return ""
}

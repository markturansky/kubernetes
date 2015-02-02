# Persistent Storage

This document proposes a model for managing persistent, cluster-scoped storage for applications requiring long lived data.

### tl;dr

`PersistentStorage` objects contain a VolumeSource with a lifecycle greater than the pods it is used by.  Many types of VolumeSources can be implemented as volume plugins, with each implementing an interface for use within a controller.  A pod with persistence requirements (specifically pod.spec.volumes.volume.persistenceRequirements) is bound to a matching and available VolumeSource from a pool of available storage.  PersistentStorage objects give that VolumeSource longevity.

A `PersistentStorageController` manages the creation, attachment, and reclamation of PersistentStorage objects for one VolumeSource.  Implementations must be pluggable to support many types of controllers for many types of VolumeSources. The controller can advise the scheduler on host selection, keeping volume rules contained within the provider.  All cloud provider credentials are isolated to the storage controller's host.

`PersistentStorageBinder` watches for pods with persistence requirements and matches the request with what is available in the pool.

`PersistenceRequirements` expresses the capabilities and performance attributes of the underlying storage.  Allow discovery
of storage by its capabilities and use those capabilities as pod storage requirements.

Kubernetes makes no guarantees at runtime that the underlying storage exists or is available.  High availability is left to the storage provider.

## Goals

* Allow administrators to describe available storage
* Allow pod authors to discover and request persistence for their volumes
* Enforce security through access control lists and securing storage to the same namespace as the pod volume
* Enforce quotas through admission control
* Ensure developers can rely on storage being available without being closely bound to a particular disk, server, network, or storage device.


#### Allow admins to describe and create available storage

Admins can describe storage with `PersistentStorageControllers` or create storage directly by using the `PersistentStorage` API.  Storage created manually still requires a PersistentStorageController and accompanying interface to allow management by the cluster.


```
	
	PersistentStorage
		TypeMeta
		ObjectMeta
			Name - internal name
			Labels - labeled like Volume.PersistenceRequirements.Selector
		Spec
			VolumeSource
				HostPath
				EmptyDir
				GitRepo	
				GCEPersistentDisk { modes: ReadWriteOnce | ReadOnlyMany }
				AWSElasticBlockStore { modes: ReadWriteOnce }
				NFSMount { modes: ReadWriteOnce | ReadOnlyMany | ReadWriteMany }
				*** many volume plugins
			RetentionPolicy	 -- volumes can be orphaned. allow never delete.
		Status
			Phase
			CurrentMounts	MountList
			LastMounts		MountList
			PodRef			ObjectReference -- the pod originally requesting persistence
			

	// Manages one type of PersistentStorage
	PersistentStorageController
		Name
		Spec
			VolumeSource
				GCEPersistentDisk { modes: ReadWriteOnce | ReadOnlyMany, resources: 5gb 30iops, .48 throughput  }
			MaxInstances  100
			IncrementBy   10
		Status
			InstanceCount
			

	//
	//  addition to VolumeSource
	//	
	type MountMode byte
	
	const (
		ReadWriteOne MountMode = 1 << iota
		ReadOnlyMany
		ReadWriteMany
	)
	
	func (vs *VolumeSource) canMount(mode MountMode) bool {
		return vs.modes & mode != 0
	}
	
```

> To manually make storage available, the admin would, for example, create a GCE VM, create, attach, and format 20 persistent disks.  The IDs are saved and the admin then creates 20 persistent-disk.json files and POSTs each GCE_PD to the API.  There are many ways to script adding storage to a cluster.



#### Pod to specify storage requirements

```

	Pod.Spec.Volumes.Volume 
			Name
			VolumeSource -- to be bound with available storage from the pool
			PersistenceRequirements
				Mode				ReadWriteOnce | ReadOnlyMany
				RetentionPolicy		DeleteOnPodExit | RetainOnPodExit
				Resources			ResourcesList (size, iops, throughput)
				Selector			Labels to identify the PersistentStorage backing implementation

```

`PersistenceRequirements` are the capabilities and performance characteristics a pod author can request and rely on for the entire lifecycle of the volume.  Requests for storage that do not match available storage will go unfulfilled.


##### Storage discovery 

An API and CLI is needed to view available and in-use storage by namespace.

**Use Case**:  Susie needs to crunch some data and serve it afterwards.  A single pod to create data is sufficient, but she estimates needing 10 web front ends to serve the data afterwards.  For this use case, the data does not change once created.  Susie first looks to see what is available in the cluster.

```
$ cluster/kubectl.sh get storage

NAME                LABELS              MODES               RESOURCES
_pv_vol_01          name=my-data        RWO,ROX             size=5gb,iops=30
_pv_vol_02                              RWO,ROX             size=5gb,iops=30
_pv_vol_03          name=other-data     RWO,ROX             size=25gb,iops=30

```

##### Request persistent storage for a pod

Susie is happy to see storage that can be mounted many times (ReadOnlyMany).  That satisfies her load balancer requirement.  Susie creates an pod image with her number crunching code and requests persistence for her volume.

Susie's YAML looks something like this for the pod that creates her data:


```yaml

id: creator-of-data
kind: Pod
namespace: ns
apiVersion: v1beta1
desiredState:
  manifest:
    version: v1beta1
    id: creator-of-data
    containers:
      - name: numbercruncher
        image: ns/makesomdata:v1
        command:
          - /run.sh
        volumeMounts:
          - name: data
            mountPath: /some_data
    volumes:
      - name: data
        persistenceRequirements:
        	mode: readonlymany
        	retentionPolicy: retainonpodexit
        	resources:
        		- size: 5
        	selector:
        		- name: my-persistent-data
labels:
  name: creator-of-data

```

##### Behind the curtain

`PersistentStorageBinder` is watching for new pods with `PersistenceRequirements`.  The binder compares the request to all available storage and binds it to the closest match.

To bind:

* Set PersistentStorage.Status.PodRef = pod that originally requested storage
* Set PersistentStorage.Labels = PersistenceRequirements.Selector

The scheduler will wait to schedule a pod until its storage requirements are fulfilled.  Invert responsibility for host selection when scheduling pods with persistent storage.  Allow the controller to give advice to the scheduler, keeping storage rules contained in that storage's controller. 

Storage controllers are watching bound pods to attach volumes for cloud providers.

Kubelet waits to mount a filesystem until a cloud provider has finished attaching the volume.


##### Surviving the pod lifecycle

Susie's pod finishes it works and exits.  Because her retention policy was "RetainOnPodExit", she is relying on Kubernetes to make the data available again to her in the same namespace using the same selector.

Susie can now use a ReplicaController to create many pods to serve her content.  Because Susie is a responsible kubitzen (a kube citizen?), she set the retention policy to DeleteOnPodExit, so that when all her replicas are done serving content, the persistent storage object can be recycled back into the pool.


```
id: server-of-data
kind: ReplicationController
apiVersion: v1beta1
desiredState:
  replicas: 10
  replicaSelector:
    name: server-of-data
  podTemplate:
    desiredState:
      manifest:
        version: v1beta1
        id: server-of-data
        containers:
          - name: server-of-data
            image: ns/servesomedata:v1
            command:
              - /run.sh
            volumeMounts:
              - name: data
                mountPath: /some_data
        volumes:
      - name: data
        persistenceRequirements:
        	mode: readonlymany
        	retentionPolicy: deleteonpodexit
        	resources:
        		- size: 5
        	selector:
        		- name: my-persistent-data
    labels:
      name: server-of-data

```

	

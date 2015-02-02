# PersistentVolume

This document proposes a model for managing persistent, cluster-scoped storage for applications requiring long lived data.

### tl;dr

Three new kinds:

A `PersistentVolume` is a long-lived entity that contains a volume backed by some underlying infrastructure.

A `PersistentVolumeController` manages one type of PersistentVolume through its lifecycle and helps schedule volume resources onto pods.  An interface will be designed to manage volumes and a plugin implementation is required per volume type.  Some plugin implementations will be able to implement dynamic provisioning more easily than others.  The controllers run in master.

A `PersistentVolumeClaim` is a request by a user for a persistent volume.  The claim lives in the user's namespace and is bound to an available PersistentVolume.  The user references her volume claim in her pod definition.  

One new component:

`PersistentVolumeBinder` watches for volume claims by users, matches the request to available volumes, and binds them together by setting the claim on the volume.

One struct to define common volume attributes:

`PersistenceSpec` expresses the capabilities and performance attributes of the underlying volume.  Allow discovery
of volumes by their capabilities and use those capabilities as when making volume claims.

Kubernetes makes no guarantees at runtime that the underlying storage exists or is available.  High availability is left to the storage provider.

## Goals

* Allow administrators to describe available storage
* Allow pod authors to discover and request persistence for their volumes
* Enforce security through access control lists and securing storage to the same namespace as the pod volume
* Enforce quotas through admission control
* Ensure developers can rely on storage being available without being closely bound to a particular disk, server, network, or storage device.

### Volumes, Claims, and Controllers

#### PersistentVolume

```
	PersistentVolume
		TypeMeta
		ObjectMeta
		Spec
			VolumeSource
				// one of these
				GCEPersistentDisk { modes: ReadWriteOnce | ReadOnlyMany }
				AWSElasticBlockStore { modes: ReadWriteOnce }
				NFSMount { modes: ReadWriteOnce | ReadOnlyMany | ReadWriteMany }
				*** other plugins to be developed (ceph, iscsi, gluster...)
			// volumes can be orphaned.  allow never delete.
			RetentionPolicy VolumeRetentionPolicy
			PersistenceSpec
				AvailableMountModes { modes: ReadWriteOnce | ReadOnlyMany }
				Resources (size, iops, throughput)
		Status
			Phase
			CurrentMounts	MountList
			LastMounts		MountList
			ClaimRef		ObjectReference -- the authoritative binding reference to a PersistentVolumeClaim 
			
```

A `PersistentVolume` is a non-namespaced resource containing a single long-lived volume.  A cluster admin manages pools of volumes either directly via the API or by controller with dynamic provisioning (dependent on the provider).  Every volume will be managed by a controller without regard for how it was provisioned.

`PersistenceSpec` expresses the capabilities and performance attributes of the underlying volume. Allow discovery of volumes by their capabilities and use those capabilities when making volume claims.


#### PersistentVolumeController

```
	PersistentVolumeController
		TypeMeta
		ObjectMeta
		Spec
			VolumeSource
				GCEPersistentDisk { modes: ReadWriteOnce | ReadOnlyMany, resources: 5gb 30iops, .48 throughput  }
			PersistenceSpec
				AvailableMountModes -- e.g, ReadWriteOnce | ReadOnlyMany
				Resources (size, iops, throughput)
			MaxInstances  100
			IncrementBy   10
		Status
			InstanceCount
	
```

A `PersistentVolumeController` is a non-namespaced manager of one type of `PersistentVolume`.  A provider interface must be implemented for each type of volume being managed.   

The controller has two core responsibilities:

* Manage the lifecycle of a volume
	* Watch for newly added volumes and put them in controllers
	* Allow attaching volumes to hosts
	* Lifecycle includes recycling volumes back into available storage
* Advise scheduler when placing pods on hosts
	* Tracks where volumes are mounted
	* Enforces mounting rules

Security bonus:  cloud credentials can be kept isolated to master because attaching is part of the volume's lifecycle.


#### PersistentVolumeClaim

```

	PersistentVolumeClaim
		TypeMeta
		ObjectMeta
		Spec - PersistenceSpec
			AvailableMountModes -- e.g, ReadWriteOnce | ReadOnlyMany
			CurrentMountMode -- ReadWriteOnce
			Resources
		Status
			InstanceCount
			PersistentVolumeRef ObjectReference -- non-authoritative reference to the PersistentVolume bound to this claim
	
		
	Pod.Spec.Volumes.Volume 
			Name
			VolumeSource -- left empty by user, will be bound with available storage from the pool
			PersistenceSpec
				Mode				ReadWriteOnce | ReadOnlyMany
				RetentionPolicy 	VolumeRetentionPolicy (RetainOnDelete | RecycleOnDelete)				Resources			ResourcesList (size, iops, throughput)
				Selector			Labels to identify the PersistentVolume backing implementation
```

A user requests persistent storage by creating a `PersistentVolumeClaim`.  This claim is namespaced scoped and is bound to the volume when the request is fulfilled.

The pod author attaches the claim to her pod's volume.  The scheduler and the volume's controller ensure the pod and volume are collocated on the host.



#### Pod to specify storage requirements

`PersistenceSpec` are the capabilities and performance characteristics a pod author can request and rely on for the entire lifecycle of the volume.  Requests for storage that do not match available storage will go unfulfilled.

```
	PersistenceSpec
			Mode				ReadWriteOnce | ReadOnlyMany
			RetentionPolicy 	VolumeRetentionPolicy (RetainOnDelete|RecycleOnDelete)			Resources			ResourcesList (size, iops, throughput)
			Selector			Labels to identify the PersistentVolume backing implementation
```



#### Storage discovery 

An API and CLI is needed to view available and in-use volumes by namespace.

**Use Case**:  Susie needs to crunch some data and serve it afterwards.  A single pod to create data is sufficient, but she estimates needing 10 web front ends to serve the data afterwards.  For this use case, the data does not change once created.  Susie first looks to see what is available in the cluster.

```
$ cluster/kubectl.sh get storage

NAME                LABELS              MODES               RESOURCES
_pv_vol_01          name=my-data        RWO,ROX             size=5gb,iops=30
_pv_vol_02                              RWO,ROX             size=5gb,iops=30
_pv_vol_03          name=other-data     RWO,ROX             size=25gb,iops=30

```

#### Request persistent storage for a pod

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
        PersistenceSpec:
        	availableModes: readwriteonce | readonlymany
        	currentMode: readwriteonce
        	retentionPolicy: retainondelete
        	resources:
        		- size: 5
        	selector:
        		- name: my-persistent-data
labels:
  name: creator-of-data

```

#### PersistentVolumeBinder

`PersistentVolumeBinder` is running in master, watching for new `PersistentVolumeClaims` and matching them to a volume from the pool.  

``` 
	// bind a volume with a claim 
	PersistentVolume.Status.ClaimantRef = PersistentVolumeClaim object reference
```


The scheduler will wait to schedule a pod until the pod is bound with a persistent volume.  Invert responsibility for host selection when scheduling pods with persistent volumes.  Allow the controller to give advice to the scheduler, keeping volume rules contained in the provider. 

volume controllers are watching bound pods to attach volumes for cloud providers.

Kubelet waits to mount a filesystem until a cloud provider has finished attaching the volume.


#### Surviving the pod lifecycle

Susie's pod finishes its work and exits.  Because her retention policy was "RetainOnDelete", she is relying on Kubernetes to make the data available again to her in the same namespace using the same selector.

Susie can now use a ReplicaController to create many pods to serve her content.  Because Susie is a responsible kubitzen (a kube citizen?), she set the retention policy to RecycleOnDelete, so that when all her replicas are done serving content, the persistent volume can be recycled back into the pool.


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
        PersistenceSpec:
        	availableModes: readwriteonce | readonlymany
        	currentMode: readonlymany
        	retentionPolicy: recycleondelete
        	resources:
        		- size: 5
        	selector:
        		- name: my-persistent-data
    labels:
      name: server-of-data

```


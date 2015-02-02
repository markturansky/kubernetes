# PersistentVolume

This document proposes a model for managing persistent, cluster-scoped storage for applications requiring long lived data.

### tl;dr

Two new kinds:

A `PersistentVolume` is created by a cluster admin and is a piece of persistent storage exposed as a volume.  It is resource analogous to a node.

A `PersistentVolumeClaim` is a user's request for a persistent volume to use in a pod. It is a request for a resource analogous to a pod.  

One new component:

`PersistentVolumeController` watches for new volumes to put into the pool.  Also watches for claims by users and binds them with available volumes in the pool.

Kubernetes makes no guarantees at runtime that the underlying storage exists or is available.  High availability is left to the storage provider.

### Goals

* Allow administrators to describe available storage
* Allow pod authors to discover and request persistent volumes to use with pods
* Enforce security through access control lists and securing storage to the same namespace as the pod volume
* Enforce quotas through admission control
* Enforce scheduler rules by resource counting
* Ensure developers can rely on storage being available without being closely bound to a particular disk, server, network, or storage device.


#### Describe available storage

Cluster adminstrators use the API to manage *PersistentVolumes*.  All volumes are managed and made available by a controller.  The controller also watches for new volumes to be created to bring into the pool.

Many means of dynamic provisioning will be eventually be implemented for various storage types. 

```

	$ cluster/kubectl.sh get pv

```

##### API Implementation:

| Action | HTTP Verb | Path | Description |
| ---- | ---- | ---- | ---- |
| CREATE | POST | /api/{version}{resourceType}/ | Create instance of {resourceType} in system namespace  |
| GET | GET | /api/{version}{resourceType}/{name} | Get instance of {resourceType} in system namespace with {name} |
| UPDATE | PUT | /api/{version}/{resourceType}/{name} | Update instance of {resourceType} in system namespace with {name} |
| DELETE | DELETE | /api/{version}/{resourceType}/{name} | Delete instance of {resourceType} in system namespace with {name} |
| LIST | GET | /api/{version}/{resourceType} | List instances of {resourceType} in namespace {ns} |
| WATCH | GET | /api/{version}/watch/{resourceType} | Watch for changes to a {resourceType} in system namespace |



#### Storage discovery 


Kubernetes users request a persistent volume for their pod by creating a *PersistentVolumeClaim*.  Their request for storage is described by their requirements for resource and mount capabilities.

Requests for volumes are bound to available volumes from the pool, if a suitable match is found.  Requests for resources can go unfulfilled.

Users attach their claim to their pod using a new *PersistentVolumeClaimAttachment* volume source.


##### Users require a full API to manage their claims.


| Action | HTTP Verb | Path | Description |
| ---- | ---- | ---- | ---- |
| CREATE | POST | /api/{version}/ns/{ns}/{resourceType}/ | Create instance of {resourceType} in namespace {ns} |
| GET | GET | /api/{version}/ns/{ns}/{resourceType}/{name} | Get instance of {resourceType} in namespace {ns} with {name} |
| UPDATE | PUT | /api/{version}/ns/{ns}/{resourceType}/{name} | Update instance of {resourceType} in namespace {ns} with {name} |
| DELETE | DELETE | /api/{version}/ns/{ns}/{resourceType}/{name} | Delete instance of {resourceType} in namespace {ns} with {name} |
| LIST | GET | /api/{version}/ns/{ns}/{resourceType} | List instances of {resourceType} in namespace {ns} |
| WATCH | GET | /api/{version}/watch/ns/{ns}/{resourceType} | Watch for changes to a {resourceType} in namespace {ns} |



#### Scheduling constraints

Scheduling constraints are to be handled similar to pod resource constraints.  Pods will need to be annotated or decorated with the number of resources it requires on a node.  Similarly, a node will need to list how many it has used or available.

TBD


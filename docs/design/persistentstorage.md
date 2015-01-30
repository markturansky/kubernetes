# Persistent Volumes

This document defines persistent, cluster-scoped storage for applications requiring long lived data.  


## Goals

* Allow administrators to create and manage available persistent storage devices
* Allow developers and pod authors to request storage and 
* Ensure security can be enforced through access control lists
* Ensure quotas can be enforced through admission control
* Ensure developers can rely on storage being available, without being closely bound to a particular disk, server, network topology, or storage technology.
* Implement storage for GCE, AWS, and NFS
* Provide working examples for each type of storage and cloud provider.

## Constraints and Assumptions

* Kubernetes makes no guarantees at runtime that the storage devices exist or are available.  High availability is left to the storage provider.
* ReplicationControllers with pod specs that require persistent storage can only have 1 replica per controller.  Many similar pods with requirements for persistent data will need many replication controllers.
* Sharing storage between many pods is out of scope. 
* All storage is read/write.  Read-only persistent volumes is out of scope.
* All persistent storage is considered "new"
 	* Migrating legacy data is not in scope
 	* However, legacy data and existing volumes can possibly be manually created by administrative action.  See "Legacy Volumes"

## New Types

`PersistentVolume` -- new long lived entity that represents a storage request by pod authors.  Contains Spec and Status.

`PersistentVolumeSpec` -- a description of the storage being requested

`PersistentVolumeStatus` -- represents information about the status of a storage device and whether or not the device was successfully attached

`PersistentStorageDevice` -- an available storage device created by an administrator.  Contains Spec and Status.

`PersistentStorageDeviceSpec` -- a description of the storage device with a list of Resources (size, iops, throughput) attributes common to all providers.  Provider-specific attributes are found in `VolumeSource`

`PersistentStorageDeviceStatus` -- status and recent mount history of the storage device 

    
## New Controllers

New controller processes running on master will facilitate the creation and attaching of disks/volumes to a host.

### `PersistentVolumeBindingController`

Watches etcd for new storage requests from pod authors.  PersistentVolume objects are posted to the API server.  The PersistentVolumeBindingController satisfies requests with a PersistentStorageDevice from the pool.  Storage devices are made unavailable by marking them used and creating an ObjectReference to the PersistentVolume it is bound to.

### `CloudStorageAttachingController`

As pods are scheduled onto hosts, the CloudStorageAttachingController performs the task of attaching a block device (GCE PD or AWS EBS volume) to a host.  Kubelet mounts the filesystem and exposed it as a volume to the pod.  `CloudStorageAttachingController` ensures the device will already be attached by the time Kubelet performs the mount task.


## Security

Storage is secured by restricting the PersistentVolume to the same namespace as the pod requesting access.  [ACLs](../authorization.md) can restrict users to specific namespaces to prevent inadvertent access to a storage device.

Iaas credentials are restricted to Master running `CloudStorageAttachingController`.  Hosts will not have access to the underlying infrastructure because they are running untrusted code in pods.  Kubelet continues to have responsibility to mount a filesystem from the storage device.


## High Level Flow

1. Admin makes storage available by creating PersistentStorageDevice instances in etcd
    1.  Storage is previously created manually by an admin and a list of unique identifiers for each is maintained by the admin (enhance with automation tools)
    2.  Admin posts `PersistentStorageDevice` objects to etcd to make the storage "available".
    3.  Description (size, speed) and ID (from AWS/GCE) required.
    4.  Namespace is intentionally empty.
2. Users request storage by posting `PersistentVolume` objects to the API.
    1.  Name and performance attributes (size, speed) are required.  ID of existing volume is optional (see Legacy Data use case)
    2.  Namespace is required for security.
    3.  PersistentVolume is referenced by Pod->Spec->Volume by name.
3. `PersistentVolumeBindingController`
    1. Watches etcd for new PersistentVolume objects 
    2. Matches storage request to available storage, binding them with `ObjectReference` on PersistentStorage referencing a PersistentVolume.
    3. PersistentStorageDevice.Status.PersistentVolumeRef is the authoritative reference of which PeristentVolume owns the storage device
    4. PersistentVolume.Status.PersistentStorageDeviceRef is a non-authoritative reference to the underlying storage device
    5. Lines 3 and 4 above are atomic updates, but the latter may fail.  The controller must first check the PersistentStorageDevices to determine if the PersistentVolume request has already been fulfilled.
    6. Fire event if no storage is available.
4. Scheduler places pod on node with constraints
	1. New predicates are required for volumes (e.g, only 16 GCEPDs attached to a host)
	2. Host is set and Pod.Status.Phase set to 'PendingAttachment'
5. `CloudStorageAttachingController` watches pod changes and performs the task of attaching the GCEPD/AWSEBS to the host
	1. Pod.Status.Phase set back to 'Pending' (or a new forward phase to indicate readiness)
6.    Kubelet mounts the filesystem and exposes the volume to the pod.  
	1.  A wait loop is entered if the pod is still 'PendingAttachment'
	2.  Wait loop is X seconds and exits after Y attempts.   Fire event for failed attachment.


## Questions

1. Where does reconciliation happens if host crashes and PersistentStorage.Status is not updated?  The status needs to be correct for the storage device to be reattachement elsewhere.
2. Where does reconciliation happens if pod crashes and PersistentStorage.Status is not updated.  
3. What happens to a mounted filesystem if the host stops responding on the network interface? How long to wait before the disk is detached?
4. What happens to a mounted filesystem if detach is called but the storage device is still mounted on the host?
5. Does Kubelet still run a pod even if the required PersistentVolume fails to attach/mount?

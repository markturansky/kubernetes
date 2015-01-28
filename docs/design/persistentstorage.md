# Persistent Storage


## Abstract

This document defines persistent, cluster-scoped storage for applications requiring long lived data.  The term "PersistentStorage" was used to avoid overloading the term "Volume."  Storage is exposed to a pod as a volume.  Developers and pod authors request storage by defining their performance needs and are matched with an appropriate object from a pool maintained by administrators.

Kubernetes makes no guarantees at runtime that these volumes exist or are available. High availability is left to the storage provider.


## Goals

* Allow administrators to create and manage pools of persistent storage devices
* Allow developers and pod authors to request storage from the pool
* Implement dynamic storage for cloud providers (using GCE/AWS APIs to create disks/volumes)
* Provide working examples for each type of storage and cloud provider.
* Ensure developers and pod authors can rely on storage being available, without being closely bound to a particular disk, server, network topology, or storage technology.
* Use or don't impede [ #2598 - Tim Hockin's volumes framework](https://github.com/GoogleCloudPlatform/kubernetes/pull/2598)


## New Types

`PersistentStorage` -- new long lived entity that represents a storage request by pod authors.  Contains Spec and Status.

`PersistentStorageSpec` -- a description of the storage being requested

`PersistentStorageStatus` -- represents information about the status of a storage device and whether or not the device was successfully attached

`PersistentStorageBinding` -- a binding between PersistentStorageDevice and a PersistentStorage request.

`PersistentStorageDevice` -- an available storage device created by an administrator.  Contains Spec and Status.

`PersistentStorageDeviceSpec` -- a description of the storage device with attributes common to all providers.  Provider-specific attributes are found in `PersistentStorageDeviceSource`

`PersistentStorageDeviceStatus` -- status and recent mount history of the storage device 

`PersistentStorageDeviceSource` -- the specific underlying provider.  Patterned after Volume->VolumeSource and the many types of volumes available.  GCE example:  PersistentStorageDevice->Spec->Source->GCEPersistentStorage

    
## New Controllers

New controller processes running on master will facilitate the creation and attaching of disks/volumes to a host.

### `StorageBindingController`

Watches etcd for new storage requests from pod authors.  PersistentStorage objects are posted to the API server.  The VolumeBindingController satisfies requests with a PersistentStorageDevice from the pool.  Storage devices are made unavailable by marking them used and creating a PersistentStorageBinding with the PersistentStorageDevice.ID and PersistentStorage.ID of the request.

### `CloudStorageAttachingController`

As pods are scheduled onto hosts, the CloudStorageAttachingController performs the task of attaching a block device (GCE PD or AWS EBS volume) to a host.  Kubelet mounts the volume for the pod.  `CloudStorageAttachingController` ensures the device will already be attached by the time Kubelet performs this task.

Question:  Where does reconciliation happens if host crashes and PersistentStorage.Status is not updated?  The status needs to be correct for the storage device to be reattachement elsewhere.

Question:  Where does reconciliation happens if pod crashes and PersistentStorage.Status is not updated.  

Question:  What happens to a mounted filesystem if the host stops responding on the network interface? How long to wait before the disk is detached?

Question: What happens to a mounted filesystem if detach is called but the storage device is still mounted on the host?


## Security

Storage is secured by restricting it to the same namespace as the pod requesting access.  [ACLs](../authorization.md) can restrict users to specific namespaces to prevent inadvertent access to a storage device.

Iaas credentials are restricted to Master running `CloudStorageAttachingController`.  Hosts will not have access to the underlying infrastructure because they are running untrusted code in pods.  Kubelet continues to have responsibility to mount a filesystem from the storage device.

Legacy storage and data is supported by administrative action by creating the requisite data objects in etcd.

## Constraints and Assumptions

* ReplicationControllers that need storage can only have 1 replica per controller
	* Each pod template needs to specifically ask for 1 available and previously requested PersistentStorage object
	* Replicas and mounting many as "read only" is out of scope for this proposal 
 * All persistent storage is considered "new"
 	* Migrating legacy data is not in scope
 	* Legacy storage and data can be manually created by administrative action when manual solutions exist

## Concepts & High Level Flow


1. Admind fills a pool with available storage by creating PersistentStorageDevice instances in etcd
    1.  Description (size, speed) and ID (from AWS/GCE) required
    2.  Storage must already be available with a list of unique identifiers.
    3.  Tools will be made to help with this task.  Manual until then.
2. Users request storage from the pool by posting PersistentStorage to the API
    1.  Size and speed required in request.  ID of existing volume is optional.  
    2.  Namespace required for security
    3.  PersistentStorage is referenced by a volume in a pod by name.
3. StorageBindingController
    1. Watches etcd for new PersistentStorage 
    2. Matches storage request to available storage in the pool, creating a StorageBinding
        1.  Create PersistentStorageBinding with PersistentStorageDevice.ID and PersistentStorage.ID
        2.  The presence of PersistentStorageBinding with that PersistentStorageDevice.ID means it is in use and unavailable for any future requests.
4. Scheduler places pod on node with constraints
    1. New predicates are required for volumes (e.g, only 16 GCEPDs attached to a host)
    2. Requires a way to validate without having the scheduler know the types of storage devices being used.
5. CloudStorageAttachingController watches pod changes and performs the task of attaching the GCEPD/AWSEBS to the host
    1.  Kubelet continues to mount but the volume is expected to be attached by this point
    2.  Future refactor -- remove attaching from GCEPersistentDisk, allow StorageAttachingController to attach the PD


# Use Cases and System Effects

## Creating Volumes for use
 User Story | System Action
 -----|-----
Admin Ed manually starts an EC2 instance, creates, attaches, and formats 20 EBS volumes (list below).  All volumes are subsequently detached from the instance and the instance shutdown, but the volumes remain in AWS.  Admin Ed maintains a list of all EBS volume IDs.	 | System does nothing.  Automation scripts can help future Admin Ed handle the task of creating volumes and tracking their IDs
Admin Ed creates a PersistentStorageDevice.json file for each EBS created (20 in all), with each containing the EBS volumes description and ID.  Each is posted to the API server								 | API server validates each and saves them as a new `PersistentStorageDevice` object in etcd.
Admin Ed takes a break because those previous two steps were tedious.  | Manual tasks like these are easy automation targets.


### Volumes created by Admin Ed

Quantity | Size (gb) | Type
---- | ---- | ----
 5 | 5 | General Purpose SSD (3000 max burst IOPS)
 5 | 10 | General Purpose SSD (3000 max burst IOPS)
 5 | 10 | Provisioned IOPS (4000 IOPS)
 5 | 25 | Provisioned IOPS (4000 IOPS)




### In all User Stories, assume the following:

> Admin Ed usets up Kubernetes with an ACL list for two teams using two namespaces:  "Team Kramden" and "Team Norton".

> Ralph and Alice are on "Team Kramden".  Trixie and Alice are on "Team Norton".  All three users are restricted to their projects' namespaces but retain all other permissions, including creating pods, volumes, services, etc.

>  Admin Ed, naturally, has all permissions across all namespaces.



### The GO PATH -- Everything Just Works

 User Story | System Action
 -----|-----
Trixie requests 1gb of storage in namespace 'Team Norton' with name  'WP Blog' by posting a `PersistentStorage` json object to the API server. | After  validation, the API server creates a `PersistentVolume` object in etcd in phase  'Pending'
Trixie waits and queries the cluster for the status of her storage.  She sees it in phase 'Created'.  There was much rejoicing. | `StorageBindingController` is watching etcd for new storage requests and fulfills them by creating `PersistentStorageBinding` objects between a matching `PersistentStorageDevice` object and Trixie's `PersistentStorage` request object. <br><br>  `PersistentStorage.Status.Phase` is updated.  `PersistentStorageDevice.Status` is also updated.
Trixie creates pod.json (or replController.json) with a WordPress/LAMP image.  `Pod.Spec.Volumes[0].Source.PersistentStorageName = 'WP Blog'`  | API server validates the storage exists by name in the namespace of the pod with along with existing pod validation.  A `Pod` is created and stored.  When the pod is scheduled, Pod.Status.Phase = 'PendingAttachment'
Trixie queries the cluster for pod status.  The pod is still 'Pending'. She waits and queries again.  The pod is  'Running'. | `CloudStorageAttachingController` is watching for pods to be scheduled onto nodes.  This controller takes pods that are 'PendingAttachment' and uses the cloud provider API to attach the device.<br><br>Kubelet attempts to start a pod but enters a wait loop until PersistentStorage.Status.Phase = 'Attached'. <br><br>When 'Attached', Kubelet mounts the volume and proceeds to start the pod.

### Security Violation Story

Add details showing how requesting storage across namespaces is enforced

 User Story | System Action
 -----|-----
Ralph requests storage in 'Team Kramden' and using the name 'WP Blog' | Valid in the namespace, created in etcd
Ralph changes the namespace to access Trixie's blog | ACL denies Ralph access to the 'Team Kramden' namespace
Alice changes the namespace to 'Team Norton' from 'Team Kramden' | Namespace change is denied.

### Failed Validation Stories

Add details showing how storage requests can fail when a match is not found in the pool or is unavailable at attach/mount time.

 User Story | System Action
 -----|-----
tbd | tbd 


 
## Storage Implementations & Validation Rules

### All *PersistentStorage

1. All *PersistentStorage rules are enforced by the scheduler and predicates.
1. Storage has identity that can outlive a pod.
1. Storage must share the same namespace as a pod.
1. Pods can have many volumes of many types except for cloud provider disks (GCE, AWS).
1. Pods can have only a single type of cloud provider disk (GCE, AWS, not both).

### GCEPersistentStorage

1. A PersistentDisk in GCE must already be created and formatted before it can be used as GCEPersistentStorage.
1. GCEPD must exist in the same availability zone as the VM.
1. GCEPDs can be attached once as read/write or many times as read-only, not both.
1. Hard limit 16 disk attachments per VM.
1. High-availability subject to the GCE Service Level Agreement.

### AWSPersistentStorage 

1. An AWS EBS volume must already be created and formatted before it can be used as AWSPersistentStorage
1. Must exist in the same availability zone as the VM.
1. AWS EBS volumes can be attached once as read/write.
1. AWS Encrypted disks can only be attached to VMs that support EBS encryption
1. Attachments limited by Amazon account, not ES2 instance limit.
1. High-availability subject to the AWS Service Level Agreement.


## Code Changes & Additions

#### /cmd/kube-storage-controller

#### /cmd/kube-cloud-attaching-controller

#### /pkg/kubelet/kubelet.go

Add wait loop when a pod is 'PendingAttachment'.  Continue after phase = 'Pending' (original state of pod) -- use another phase to explicit here?
 
 


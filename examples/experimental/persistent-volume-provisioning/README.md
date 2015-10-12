<!-- BEGIN MUNGE: UNVERSIONED_WARNING -->

<!-- BEGIN STRIP_FOR_RELEASE -->

<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">

<h2>PLEASE NOTE: This document applies to the HEAD of the source tree</h2>

If you are using a released version of Kubernetes, you should
refer to the docs that go with that version.

<strong>
The latest 1.0.x release of this document can be found
[here](http://releases.k8s.io/release-1.0/examples/experimental/persistent-volume-provisioning/README.md).

Documentation for other releases can be found at
[releases.k8s.io](http://releases.k8s.io).
</strong>
--

<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->

## Persistent Volume Provisioning

This example shows how to use experimental persistent volume provisioning.

### Pre-requisites

This example assumes that you have an understanding of Kubernetes administration and can modify the scripts that launch kube-controller-manager.

### Admin Configuration

Admins configure their clusters with Provisioners tied to quality-of-service classes.  The correct configuration will be
the class name and the volume plugin responsible for provisioning that request separated by a slash.  The configuration is
added as a CLI flag to kube-controller-manager.

For example, an admin running in AWS can add class "foo" that provisioned using EBS:

```
# where 'foo' is the class and 'kubernetes.io/aws-ebs' is the EBS plugin
--storage-class="foo/kubernetes.io/aws-ebs"
```

The CLI flag "--storage-class" can be used many times to add many provisioners. As the number and types of provisioners increase, admins
can add many QoS classes with various backends to satisfy their needs.

```
--storage-class="foo/kubernetes.io/aws-ebs" 
--storage-class="gold/kubernetes.io/aws-ebs-ssd"       # future implementation
--storage-class="silver/kubernetes.io/aws-ebs-hd"      # future implementation
--storage-class="bronze/kubernetes.io/aws-ebs-glacial" # future implementation
```

#### Provisioners

Four Kubernetes persistent volume plugins have generic implementations for dynamic provisioning.

The plugin names are:

1. GCE PD  - "kubernetes.io/gce-pd"
2. AWS EBS - "kubernetes.io/aws-ebs"
3. OpenStack Cinder - "kubernetes.io/cinder"
4. HostPath - "kubernetes.io/host-path"

> Important! HostPath as a provisioner is meant for local development and testing only.  It _will not work_ in a multi-node cluster
and is not supported in Kubernetes in any way outside local testing.

Note that the first three are mutually independent and only 1 would be available at a time in a cloud environment.

### User storage requests

Users request dynamically provisioned storage by including a quality-of-service class in their `PersistentVolumeClaim`.
An annotation is used to access this experimental feature.  In the future, the QoS class request will likely be a field on the claim itself.

Assuming an admin configures a "foo" provisioner, a user would create a PVC with a special annotation requesting a quality-of-service class:

```
{
  "kind": "PersistentVolumeClaim",
  "apiVersion": "v1",
  "metadata": {
    "name": "claim1",
    "annotations": {
      "volume.experimental.kubernetes.io/quality-of-service": "foo"
    }
  },
  "spec": {
    "accessModes": [
      "ReadWriteOnce"
    ],
    "resources": {
      "requests": {
        "storage": "3Gi"
      }
    }
  }
}
```

### Sample output

This example uses HostPath but any provisioner would follow the same flow.

First we note there are no Persistent Volumes in the cluster.  After creating a claim, we see a new PV is created
and automatically bound to the claim requesting storage.


``` 
$ kubectl get pv

$ kubectl create -f examples/experimental/persistent-volume-provisioning/claim1.yaml 
I1012 13:07:57.666759   22875 decoder.go:141] decoding stream as JSON
persistentvolumeclaim "claim1" created

$ kubectl get pv
NAME                LABELS                                   CAPACITY   ACCESSMODES   STATUS    CLAIM            REASON    AGE
pv-hostpath-r6z5o   createdby=hostpath-dynamic-provisioner   3Gi        RWO           Bound     default/claim1             2s

$ kubectl get pvc
NAME      LABELS    STATUS    VOLUME              CAPACITY   ACCESSMODES   AGE
claim1    <none>    Bound     pv-hostpath-r6z5o   3Gi        RWO           7s

# delete the claim to release the volume
$ kubectl delete pvc claim1
persistentvolumeclaim "claim1" deleted

# the volume is deleted in response to being release of its claim
$ k get pv


```




<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/examples/experimental/persistent-volume-provisioning/README.md?pixel)]()
<!-- END MUNGE: GENERATED_ANALYTICS -->

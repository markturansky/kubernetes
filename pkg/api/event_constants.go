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

package api

// Event Reason constants that are specific to the internal API
// representation.
const (

	// Jobs
	EventReasonDeadlineExceeded         = "DeadlineExceeded"
	EventReasonDeadlineExceededDesc     = "Job was active longer than specified deadline"
	EventReasonTooManyActivePods        = "TooManyActivePods"
	EventReasonTooManyActivePodsDesc    = "Too many active pods running after completion count reached"
	EventReasonTooManySucceededPods     = "TooManySucceededPods"
	EventReasonTooManySucceededPodsDesc = "Too many succeeded pods running after completion count reached"

	// Autoscaler
	EventReasonDefaultPolicy           = "DefaultPolicy"
	EventReasonDefaultPolicyDesc       = "No scaling policy specified - will use default one. See documentation for details"
	EventReasonSelectorRequired        = "SelectorRequired"
	EventReasonInvalidSelector         = "InvalidSelector"
	EventReasonSelectorRequired        = "SelectorRequired"
	EventReasonFailedGetMetrics        = "FailedGetMetrics"
	EventReasonFailedGetCustomMetrics  = "FailedGetCustomMetrics"
	EventReasonFailedGetScale          = "FailedGetScale"
	EventReasonFailedComputeReplicas   = "FailedComputeReplicas"
	EventReasonFailedComputeCMReplicas = "FailedComputeCMReplicas"
	EventReasonFailedUpdateStatus      = "FailedUpdateStatus"

	// Services
	EventReasonDeletingLoadBalancer       = "DeletingLoadBalancer"
	EventReasonDeletingLoadBalancerDesc   = "Deleting load balancer"
	EventReasonDeletedLoadBalancer        = "DeletedLoadBalancer"
	EventReasonDeletedLoadBalancerDesc    = "Deleted load balancer"
	EventReasonDeletingLoadBalancerFailed = "DeletingLoadBalancerFailed"
	EventReasonCreatingLoadBalancer       = "CreatingLoadBalancerFailed"
	EventReasonCreatingLoadBalancerDesc   = "Creating load balancer"
	EventReasonCreatedLoadBalancer        = "CreatedLoadBalancerFailed"
	EventReasonCreatedLoadBalancerDesc    = "Created load balancer"
	EventReasonUpdatedLoadBalancer        = "UpdatedLoadBalancerFailed"
	EventReasonUpdatedLoadBalancerDesc    = "Updated load balancer with new hosts"

	// Scheduler
	FailedScheduling = "FailedScheduling"
	Scheduled = "Scheduled"

	EventReasonStarting = "Starting"
	EventReasonStarted  = "Started"
	EventReasonStopped = "Stopped"
	EventReasonKilled  = "Killed"
)

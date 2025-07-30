---
title: "Conditions"
---

[TOC]

The conditions specified in the DPF Kubernetes objects represent the various states and requirements of the resource during its lifecycle. 
These conditions are critical for the reconciliation loop of the DPF controllers, providing insights into the current status, potential issues, and the overall health of the system.

Each condition consists of a Reason and a Message that can describe the current state of the reconciliation.

* reason contains a programmatic identifier indicating the reason for the condition's last transition. 
* message is a human-readable message indicating details about the transition.

Note: Not all DPF objects follow this condition standard. Future work should align condition design across all Kubernetes objects.

### Condition type categories

DPF has three categories of condition:

**XXXReconciled**:
if the condition has been reconciled successfully. Reconciliation means that changes to the object have been applied.
Reasons can be: Success, Error, Pending or AwaitingDeletion.

**XXXReady**:
Check the objects status and reflect the ready state
Reasons can be: Success, Failure, Pending or AwaitingDeletion.

**Ready** (singleton):
the overall status of the controller – only True if ALL conditions are True.
Reasons can be: Success, Failure, Pending or AwaitingDeletion.


### Condition reasons

* **AwaitingDeletion**: A resource is marked for deletion.
* **Pending**: If the controller has not completed its initialization.
* **Error**: Something the system CAN recover from.
* **Failure**: Terminal state the system CANNOT recover from.
* **Success**: the overall success reason.


### DPUService example
The DPUService has the following condition types:

* **Ready**: The overall state of the DPU Service Controller reconciliation.
* **ApplicationPrereqsReconciled**: Represents the status of the prereqs for an Application, which consists of the ArgoSecrets, AppProjects and ImagePullSecrets
* **ApplicationsReconciled**: Represents the status of the application deployments for the host and DPU clusters. This means only that the resources are reconciled.
* **ApplicationsReady**: Represents the status of the application ready state for the host and DPU clusters. If a failure occurs a message is added to the condition with the hint taking a look at the ArgoCD resources.

#### Go implementation
These types are implemented in go in the DPUService API package. 

[embedmd]:# (../../../api/dpuservice/v1alpha1/dpuservice_types.go go /\/\/ Condition types/ /}\n\)/)
```go
// Condition types
const (
	// ConditionDPUServiceInterfaceReconciled is the condition type that indicates that the
	// DPUServiceInterface is reconciled.
	ConditionDPUServiceInterfaceReconciled conditions.ConditionType = "DPUServiceInterfaceReconciled"
	// ConditionApplicationPrereqsReconciled is the condition type that indicates that the
	// application prerequisites are reconciled.
	ConditionApplicationPrereqsReconciled conditions.ConditionType = "ApplicationPrereqsReconciled"
	// ConditionApplicationsReconciled is the condition type that indicates that the
	// applications are reconciled.
	ConditionApplicationsReconciled conditions.ConditionType = "ApplicationsReconciled"
	// ConditionConfigPortsReconciled is the condition type that indicates that the
	// applications config ports are reconciled.
	ConditionConfigPortsReconciled conditions.ConditionType = "ConfigPortsReconciled"
	// ConditionApplicationsReady is the condition type that indicates that the
	// applications are ready.
	ConditionApplicationsReady conditions.ConditionType = "ApplicationsReady"
)

var (
	// Conditions is the list of conditions that the DPUService can have.
	Conditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionApplicationPrereqsReconciled,
		ConditionApplicationsReconciled,
		ConditionApplicationsReady,
		ConditionDPUServiceInterfaceReconciled,
		ConditionConfigPortsReconciled,
	}
)
```
#### Kubernetes object conditions

A ApplicationsReady condition looks like:

```yaml 
status:
conditions:
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: "Applications couldn't be deployed: cluster1/multus.applications.argoproj.io, cluster2/flannel.applications.argoproj.io"
  reason: Pending
  status: "False"
  type: ApplicationsReady
```

Where a ready DPUApplicationsReady condition looks like:

```yaml
status:
conditions:
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: "Reconciliation successful"
  reason: Succesful
  status: "True"
  type: ApplicationsReady
```

The full set of conditions on the DPUService will look like:
```yaml 
status:
conditions:
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: "Resources are pending in deletion"
  reason: Pending
  status: "False"
  type: Ready
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: "Applications %s are pending in deletion"
  reason: AwaitingDeletion
  status: "False"
  type: ApplicationsReady
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: "Application Prereqs are pending in deletion"
  reason: AwaitingDeletion
  status: "False"
  type: ApplicationPrereqsReady
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: "Application Prereqs are pending in deletion"
  reason: Error
  status: "False"
  type: ApplicationPrereqsReady
- lastTransitionTime: "2024-07-31T10:23:36Z"
  message: ""
  reason: Pending
  status: "Unknown"
  type: ApplicationReady
```

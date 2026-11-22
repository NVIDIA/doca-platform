/*
Copyright 2026 NVIDIA

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

package inventory

import (
	"slices"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"

	corev1 "k8s.io/api/core/v1"
)

// BFBRegistryLayout is who runs bfb-registry and how its replicas are placed.
type BFBRegistryLayout string

const (
	// BFBRegistryLayoutProvisioningController is the layout where the
	// provisioning-controller leader creates a single bfb-registry Pod. It is
	// used with hostPath, or with a PVC that is not ReadWriteMany. This is the
	// zero value. The Operator still owns the ConfigMap, the Service and the
	// server Certificate.
	BFBRegistryLayoutProvisioningController BFBRegistryLayout = ""
	// BFBRegistryLayoutSpread is the layout where the Operator also runs the
	// registry, as a Deployment. It mounts a ReadWriteMany PVC read-only and
	// spreads replicas across control-plane nodes.
	BFBRegistryLayoutSpread BFBRegistryLayout = "spread"
)

// pvcHasReadWriteMany reports whether spec.accessModes contains ReadWriteMany.
// Phase (Bound / Pending) is not part of the decision.
func pvcHasReadWriteMany(pvc *corev1.PersistentVolumeClaim) bool {
	return slices.Contains(pvc.Spec.AccessModes, corev1.ReadWriteMany)
}

// ClassifyBFBRegistryLayout selects the layout from the named BFB PVC. Only a
// live claim with ReadWriteMany selects spread; pvc is nil when no PVC is
// configured or the configured one does not exist. volumeClaim is the PVC name
// the registry Deployment mounts, and is empty unless the layout is spread.
func ClassifyBFBRegistryLayout(pvcName *string, pvc *corev1.PersistentVolumeClaim) (layout BFBRegistryLayout, volumeClaim string) {
	if pvcName == nil || *pvcName == "" || pvc == nil || !pvcHasReadWriteMany(pvc) {
		return BFBRegistryLayoutProvisioningController, ""
	}
	return BFBRegistryLayoutSpread, *pvcName
}

// BFBRegistryReplicasConfigured is spec.provisioningController.registry.replicas,
// or 0 when the field is unset.
func BFBRegistryReplicasConfigured(vars Variables) int32 {
	if vars.DPFProvisioningController.Registry == nil {
		return 0
	}
	return vars.DPFProvisioningController.Registry.Replicas
}

// bfbRegistryReplicas is the replica count for the Operator-owned registry
// Deployment. A configured value wins; otherwise the default is used.
func bfbRegistryReplicas(vars Variables) int32 {
	if n := BFBRegistryReplicasConfigured(vars); n > 0 {
		return n
	}
	return bfbregistry.DefaultBFBRegistryReplicas
}

// BFBRegistryDisabled reports whether the bfb-registry is disabled. The Operator then does not run
// the registry Deployment. It still emits the ConfigMap, the Service and the server Certificate,
// because the provisioning controller still creates the registry Pod, which needs them.
func BFBRegistryDisabled(vars Variables) bool {
	if vars.DisableSystemComponents[operatorv1.BFBRegistryName] {
		return true
	}
	ii := vars.DPFProvisioningController.InstallInterface
	if ii != nil && ii.InstallViaRedfish != nil && ii.InstallViaRedfish.BFBRegistry != nil {
		return ii.InstallViaRedfish.BFBRegistry.Disabled()
	}
	return false
}

// BFBRegistryManagedByOperator reports whether the Operator runs the registry
// Pods, as a Deployment (spread layout, registry enabled). Otherwise the
// provisioning controller creates a single registry Pod. It does not decide who
// owns the ConfigMap, the Service and the server Certificate: the Operator
// always does.
func BFBRegistryManagedByOperator(vars Variables) bool {
	return vars.DPFProvisioningController.BFBRegistryLayout == BFBRegistryLayoutSpread && !BFBRegistryDisabled(vars)
}

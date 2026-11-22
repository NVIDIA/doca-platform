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

package controller

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// prepareBFBRegistry is the bfb-registry work that must finish before the
// provisioning-controller component is applied. It fills vars with what the
// manifests depend on:
//   - the layout: a ReadWriteMany BFB PVC makes the Operator own the registry
//     Deployment (spread layout), anything else leaves the registry Pod to the
//     provisioning controller;
//   - the NodePort to pin on the bfb-registry Service (see readBFBRegistryNodePort).
//
// Any failure skips the apply: an unknown layout would flip the Deployment owner
// and prune live objects, and an unreadable NodePort ConfigMap would let the
// Service be re-allocated a different port. Disabled provisioning skips this so a
// read cannot block the inventory that removes the currently applied registry
// objects. A disabled registry skips the layout only: the Operator runs no
// Deployment then, so the BFB PVC does not matter, but the Service is still
// rendered and keeps its port.
func (r *DPFOperatorConfigReconciler) prepareBFBRegistry(ctx context.Context, namespace string, vars *inventory.Variables) error {
	if vars.DisableSystemComponents[operatorv1.ProvisioningControllerName] {
		return nil
	}
	if !inventory.BFBRegistryDisabled(*vars) {
		if err := r.resolveBFBRegistryLayout(ctx, namespace, vars); err != nil {
			return err
		}
		if vars.DPFProvisioningController.BFBRegistryLayout == inventory.BFBRegistryLayoutSpread {
			r.bfbRegistryReplicasIgnoredLogged = false
		} else {
			r.logIgnoredBFBRegistryReplicas(ctx, vars)
		}
	}
	return r.readBFBRegistryNodePort(ctx, namespace, vars)
}

// readBFBRegistryNodePort reads the bfb-registry-nodeport ConfigMap and, when it
// holds a port, puts it in vars so the Operator renders it as the Service's
// nodePort. Releases before the Operator owned the Service let the leader
// provisioning-controller create it (with a Pod ownerRef) and record the
// allocated NodePort in this ConfigMap. When the Operator takes the Service
// over, the old leader's Service is garbage-collected once that leader rolls;
// the Operator then recreates it on the same port, so registry URLs already
// written into DPU bf.cfg keep working. Fresh installs have no ConfigMap and let
// Kubernetes allocate the port. Nothing writes the ConfigMap any more, and the
// Operator never reads the Service, which would start a cluster-wide Service informer.
func (r *DPFOperatorConfigReconciler) readBFBRegistryNodePort(ctx context.Context, namespace string, vars *inventory.Variables) error {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: bfbregistry.NodePortConfigMapName}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get %s ConfigMap: %w", bfbregistry.NodePortConfigMapName, err)
	}
	raw := cm.Data[bfbregistry.NodePortConfigMapKey]
	if raw == "" {
		return nil
	}
	port, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || port < 1 || port > 65535 {
		ctrllog.FromContext(ctx).Info("Ignoring invalid NodePort in ConfigMap; Kubernetes will allocate one",
			"configMap", bfbregistry.NodePortConfigMapName, "value", raw)
		return nil
	}
	vars.DPFProvisioningController.BFBRegistryNodePort = int32(port)
	return nil
}

// resolveBFBRegistryLayout classifies the named BFB PVC. A configured PVC that
// does not exist yet is not an error: the registry stays with the provisioning
// controller (its Pod waits for the claim, as without the Operator) and the PVC
// watch re-evaluates once the claim appears.
func (r *DPFOperatorConfigReconciler) resolveBFBRegistryLayout(ctx context.Context, namespace string, vars *inventory.Variables) error {
	pvcName := vars.DPFProvisioningController.BFBPersistentVolumeClaimName
	var pvc *corev1.PersistentVolumeClaim
	if pvcName != nil && *pvcName != "" {
		got := &corev1.PersistentVolumeClaim{}
		err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: *pvcName}, got)
		switch {
		case apierrors.IsNotFound(err):
			ctrllog.FromContext(ctx).Info("BFB PVC not found; the provisioning controller keeps ownership of bfb-registry until it exists",
				"namespace", namespace, "name", *pvcName)
		case err != nil:
			return fmt.Errorf("get BFB PVC %s/%s: %w", namespace, *pvcName, err)
		default:
			pvc = got
		}
	}
	layout, claim := inventory.ClassifyBFBRegistryLayout(pvcName, pvc)
	vars.DPFProvisioningController.BFBRegistryLayout = layout
	vars.DPFProvisioningController.BFBRegistryVolumeClaim = claim
	return nil
}

// logIgnoredBFBRegistryReplicas tells the user once that registry.replicas has
// no effect while the provisioning controller runs the registry: that is a
// single Pod. Only a ReadWriteMany BFB PVC enables multiple replicas.
func (r *DPFOperatorConfigReconciler) logIgnoredBFBRegistryReplicas(ctx context.Context, vars *inventory.Variables) {
	replicas := inventory.BFBRegistryReplicasConfigured(*vars)
	if replicas <= 1 {
		r.bfbRegistryReplicasIgnoredLogged = false
		return
	}
	if r.bfbRegistryReplicasIgnoredLogged {
		return
	}
	r.bfbRegistryReplicasIgnoredLogged = true
	ctrllog.FromContext(ctx).Info("spec.provisioningController.registry.replicas is ignored: multiple bfb-registry replicas need a ReadWriteMany PVC in spec.provisioningController.bfbPVCName; running a single bfb-registry Pod",
		"replicas", replicas)
}

// bfbPVCToDPFOperatorConfig requeues the operator config when a PVC in the
// operator namespace is created, deleted or changes its access modes.
func (r *DPFOperatorConfigReconciler) bfbPVCToDPFOperatorConfig(_ context.Context, o client.Object) []ctrl.Request {
	if r.Settings == nil || r.Settings.ConfigSingletonNamespaceName == nil {
		return nil
	}
	if o.GetNamespace() != r.Settings.ConfigSingletonNamespaceName.Namespace {
		return nil
	}
	return []ctrl.Request{{NamespacedName: *r.Settings.ConfigSingletonNamespaceName}}
}

// bfbPVCPredicate limits the PVC watch to what decides the registry layout:
// a claim appearing or disappearing, or its access modes changing. Status,
// resize and annotation updates do not reconcile the whole config.
func bfbPVCPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPVC, ok := e.ObjectOld.(*corev1.PersistentVolumeClaim)
			if !ok {
				return false
			}
			newPVC, ok := e.ObjectNew.(*corev1.PersistentVolumeClaim)
			if !ok {
				return false
			}
			return !slices.Equal(oldPVC.Spec.AccessModes, newPVC.Spec.AccessModes)
		},
	}
}

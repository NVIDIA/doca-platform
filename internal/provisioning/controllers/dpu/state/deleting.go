/*
Copyright 2024 NVIDIA

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

package state

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Deleting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
	ctrlCtx.ClusterAllocator.ReleaseDPU(dpu)

	if err := RemoveDpuDeviceFinalizer(ctx, dpu, ctrlCtx); err != nil {
		err = fmt.Errorf("failed to remove DpuDevice finalizer: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "RemoveDpuDeviceFinalizerError", err.Error()))
		return *state, err
	}

	if err := RemoveRequestorFromDPUNodeMaintenance(ctx, dpu, ctrlCtx); err != nil {
		err = fmt.Errorf("failed to remove requestor from dpunodemaintenance: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "RemoveRequestorFromDPUNodeMaintenanceError", err.Error()))
		return *state, err
	}

	cfgVersion := cutil.GenerateBFCFGFileName(dpu.Name, string(dpu.UID))

	// Make sure there is no old bf cfg file in the shared volume
	cfgFile := cutil.GenerateBFBCFGFilePath(cfgVersion)
	if err := cutil.RemoveFileEx(cfgFile); err != nil {
		err = fmt.Errorf("delete BFB CFG file %s failed: %w", cfgFile, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "DeleteBFBCFGFileError", err.Error()))
		return *state, err
	}

	certificate := &unstructured.Unstructured{}
	certificate.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	certificate.SetName(cutil.GenerateDMSServerCertName(dpu.Name))
	certificate.SetNamespace(dpu.Namespace)

	// The BMC server-cert CertificateRequest is owned by the DPUDevice and named after it
	// (<dpuDeviceName>-server); owner-ref GC covers cleanup when the DPUDevice is deleted, and this
	// explicit delete remains as a best-effort fast path during DPU teardown.
	certificateRequest := &unstructured.Unstructured{}
	certificateRequest.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	certificateRequest.SetName(cutil.GenerateBMCServerCertRequestName(dpu.Spec.DPUDeviceName))
	certificateRequest.SetNamespace(dpu.Namespace)

	// Template-mode DPUs own a generated DPUFlavor named after the DPU. Release its
	// protective finalizer and include it in the objects to delete so the DPU's own
	// finalizer is only removed once the generated flavor is fully gone.
	if dpu.Labels[cutil.DPUFlavorTemplateNameLabel] != "" {
		if err := releaseGeneratedFlavorFinalizer(ctx, ctrlCtx, dpu); err != nil {
			err = fmt.Errorf("failed to release generated DPUFlavor finalizer: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "ReleaseGeneratedFlavorFinalizerError", err.Error()))
			return *state, err
		}
	}

	deleteObjects := []crclient.Object{
		certificate,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.GenerateDMSServerSecretName(dpu.Name),
				Namespace: dpu.Namespace,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.KubeadmJoinSecretName(dpu.Name),
				Namespace: dpu.Namespace,
			},
		},
		certificateRequest,
	}

	if dpu.Labels[cutil.DPUFlavorTemplateNameLabel] != "" {
		deleteObjects = append(deleteObjects, &provisioningv1.DPUFlavor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpu.Spec.DPUFlavor,
				Namespace: dpu.Namespace,
			},
		})
	}

	objects, err := cutil.GetObjects(ctx, ctrlCtx.Client, deleteObjects)
	if err != nil {
		err = fmt.Errorf("failed to get objects: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "GetObjectsError", err.Error()))
		return *state, err
	}
	for _, object := range objects {
		logger.V(3).Info(fmt.Sprintf("delete object %s/%s", object.GetNamespace(), object.GetName()))
		if err := cutil.DeleteObjects(ctx, ctrlCtx.Client, object); err != nil {
			err = fmt.Errorf("failed to delete objects: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "DeleteObjectsError", err.Error()))
			return *state, err
		}
	}

	if err := cutil.DeleteDPUAgentBootstrapTokens(ctx, ctrlCtx.Client, dpu.Name, dpu.Namespace); err != nil {
		err = fmt.Errorf("failed to delete bootstrap tokens: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "DeleteBootstrapTokensError", err.Error()))
		return *state, err
	}

	if err := deleteNode(ctx, ctrlCtx.Client, dpu); err != nil {
		err = fmt.Errorf("failed to delete node: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "DeleteNodeError", err.Error()))
		return *state, err
	}

	if dpu.Status.DPUInstallInterface != nil {
		switch *dpu.Status.DPUInstallInterface {
		case string(provisioningv1.InstallViaRedFish), string(provisioningv1.InstallViaGNOI), string(provisioningv1.InstallViaHostAgent):
			// TODO: Deploy a K8S Job to run "cleanup-dpf" script to remove any containers and api server reference in the dpu
		default:
			err = fmt.Errorf("invalid DPUInstallInterface: %s", *dpu.Status.DPUInstallInterface)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "InvalidDPUInstallInterface", err.Error()))
			return *state, err
		}
	}

	if len(objects) == 0 {
		controllerutil.RemoveFinalizer(dpu, provisioningv1.DPUFinalizer)
		if err := ctrlCtx.Update(ctx, dpu); err != nil {
			err = fmt.Errorf("failed to update DPU: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "UpdateError", err.Error()))
			return *state, err
		}
	}

	return *state, nil
}

// RemoveDpuDeviceFinalizer removes the DpuDevice finalizer when DPU is being deleted.
// Exported so mock.Deleting can reuse it for tests that expect finalizer removal.
func RemoveDpuDeviceFinalizer(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) error {
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Client.Get(ctx, crclient.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		if apierrors.IsNotFound(err) {
			// DpuDevice not found, this is expected in some cases
			return nil
		}
		return fmt.Errorf("failed to get DpuDevice %s: %w", dpu.Spec.DPUDeviceName, err)
	}

	if controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.DPUDeviceFinalizer) {
		controllerutil.RemoveFinalizer(dpuDevice, provisioningv1.DPUDeviceFinalizer)
		if err := ctrlCtx.Client.Update(ctx, dpuDevice); err != nil {
			return fmt.Errorf("failed to remove DpuDevice finalizer: %w", err)
		}
	}
	return nil
}

// releaseGeneratedFlavorFinalizer removes the GeneratedDPUFlavorFinalizer from the generated
// DPUFlavor owned by this DPU, so the subsequent delete (and owner-reference GC) can complete.
func releaseGeneratedFlavorFinalizer(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU) error {
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !controllerutil.ContainsFinalizer(flavor, cutil.GeneratedDPUFlavorFinalizer) {
		return nil
	}
	base := flavor.DeepCopy()
	controllerutil.RemoveFinalizer(flavor, cutil.GeneratedDPUFlavorFinalizer)
	return ctrlCtx.Client.Patch(ctx, flavor, crclient.MergeFromWithOptions(base, crclient.MergeFromWithOptimisticLock{}))
}

func deleteNode(ctx context.Context, client crclient.Client, dpu *provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	if dpu.Spec.Cluster.Name == "" {
		logger.Info("DPU not assigned, skip deleting Node")
		return nil
	}

	nn := types.NamespacedName{
		Namespace: dpu.Spec.Cluster.Namespace,
		Name:      dpu.Spec.Cluster.Name,
	}
	dc := &provisioningv1.DPUCluster{}
	if err := client.Get(ctx, nn, dc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DPUCluster has been deleted, skip deleting Node")
			return nil
		}
		return err
	}
	dpuClient, _, err := cutil.GetClientset(ctx, client, dc)
	if err != nil {
		return fmt.Errorf("failed to create client for DPU cluster, err: %v", err)
	}
	err = dpuClient.CoreV1().Nodes().Delete(ctx, cutil.GenerateNodeName(dpu), metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	logger.Info("deleted Node from DPU cluster")
	return nil
}

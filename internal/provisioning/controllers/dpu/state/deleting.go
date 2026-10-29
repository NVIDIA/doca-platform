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
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"

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

	if err := RemoveRequestorFromDPUNodeMaintenance(ctx, dpu, ctrlCtx); err != nil {
		err = fmt.Errorf("failed to remove requestor from dpunodemaintenance: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "RemoveRequestorFromDPUNodeMaintenanceError", err.Error()))
		return *state, err
	}

	cfgVersion := cutil.GenerateBFCFGFileName(dpu.Name, string(dpu.UID))

	// Make sure there is no old bf cfg file in the shared volume
	cfgFile := cutil.GenerateBFBCFGFilePath(cfgVersion)
	if err := os.Remove(cfgFile); err != nil && !os.IsNotExist(err) {
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

	certificateRequest := &unstructured.Unstructured{}
	certificateRequest.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	certificateRequest.SetName(dpu.Name)
	certificateRequest.SetNamespace(dpu.Namespace)

	deleteObjects := []crclient.Object{
		certificate,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.GenerateDMSServerSecretName(dpu.Name),
				Namespace: dpu.Namespace,
			},
		},
		certificateRequest,
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

	if err := deleteNode(ctx, ctrlCtx.Client, dpu); err != nil {
		err = fmt.Errorf("failed to delete node: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "DeleteNodeError", err.Error()))
		return *state, err
	}

	if dpu.Status.DPUInstallInterface != nil {
		switch *dpu.Status.DPUInstallInterface {
		case string(provisioningv1.InstallViaGNOI):
			// TODO: Use DMS debug command to run script "cleanup-dpf-host" to remove any containers and api server reference in the dpu and rollback host network configuration
			// Call DMS Debug command to run hostnetwork.sh script with delete
			conn, err := dutil.CreateGRPCConnection(ctx, ctrlCtx.Client, dpu, ctrlCtx)
			if err != nil {
				err = fmt.Errorf("Error creating gRPC connection: %w", err)
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "CreateGRPCConnectionError", err.Error()))
				return *state, err
			}
			defer conn.Close() //nolint: errcheck

			pciAddress, err := cutil.GetPCIAddrFromDPU(dpu, false)
			if err != nil {
				err = fmt.Errorf("failed to get pci address from DPU label: %w", err)
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "GetPCIAddrFromDPUError", err.Error()))
				return *state, err
			}

			// hostnetwork.sh is always put under /opt/dpf/
			command := fmt.Sprintf("%s --delete --device_pci_address %s", dms.HostNetworkScript, pciAddress)
			_, err = dms.ExecuteDMSDebugCmd(ctx, conn, command)
			if err != nil {
				err = fmt.Errorf("failed to execute DMS debug command: %w", err)
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "ExecuteDMSDebugCmdError", err.Error()))
				return *state, err
			}
		case string(provisioningv1.InstallViaRedFish):
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

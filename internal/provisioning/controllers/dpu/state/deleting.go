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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuDeletingState struct {
	dpu   *provisioningv1.DPU
	alloc allocator.Allocator
}

func (st *dpuDeletingState) Handle(ctx context.Context, client crclient.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := st.dpu.Status.DeepCopy()
	st.alloc.ReleaseDPU(st.dpu)

	if err := RemoveNodeEffect(ctx, client, *st.dpu.Spec.NodeEffect, st.dpu.Spec.NodeName, st.dpu.Namespace); err != nil {
		return *state, err
	}

	cfgVersion := cutil.GenerateBFCFGFileName(st.dpu.Name)

	// Make sure there is no old bf cfg file in the shared volume
	cfgFile := cutil.GenerateBFBCFGFilePath(cfgVersion)
	if err := os.Remove(cfgFile); err != nil && !os.IsNotExist(err) {
		msg := fmt.Sprintf("Delete BFB CFG file %s failed", cfgFile)
		logger.Error(err, msg)
		return *state, err
	}

	deleteObjects := []crclient.Object{
		&certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.GenerateDMSServerCertName(st.dpu.Name),
				Namespace: st.dpu.Namespace,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.GenerateDMSServerSecretName(st.dpu.Name),
				Namespace: st.dpu.Namespace,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.GenerateDMSPodName(st.dpu.Name),
				Namespace: st.dpu.Namespace,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cutil.GenerateHostnetworkPodName(st.dpu.Name),
				Namespace: st.dpu.Namespace,
			},
		},
	}

	objects, err := cutil.GetObjects(client, deleteObjects)
	if err != nil {
		return *state, err
	}
	for _, object := range objects {
		logger.V(3).Info(fmt.Sprintf("delete object %s/%s", object.GetNamespace(), object.GetName()))
		if err := cutil.DeleteObject(client, object); err != nil {
			return *state, err
		}
	}
	if err := deleteNode(ctx, client, st.dpu); err != nil {
		logger.Error(err, "failed to delete Node from DPU cluster, retry")
		return *state, fmt.Errorf("failed to delete node, err: %v", err)
	}
	if len(objects) == 0 {
		controllerutil.RemoveFinalizer(st.dpu, provisioningv1.DPUFinalizer)
		if err := client.Update(ctx, st.dpu); err != nil {
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

/*
Copyright 2025 NVIDIA

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
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/bfcfg"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BFCFGDir = "bfcfg"
)

func PrepareBFB(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	logger := log.FromContext(ctx)
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dpu.Spec.DPUFlavor,
	}, flavor); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "DPUFlavorNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGetFlavor", err.Error()))
		return *state, err
	}
	dc := &provisioningv1.DPUCluster{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Spec.Cluster.Namespace, Name: dpu.Spec.Cluster.Name}, dc); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "DPUClusterNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGetDPUCluster", err.Error()))
		return *state, err
	}
	dpuNode := &provisioningv1.DPUNode{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "DPUNodeNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGetDPUNode", err.Error()))
		return *state, err
	}
	bfCFGPath := filepath.Join("/", cutil.BFBBaseDir, BFCFGDir, fmt.Sprintf("%s_%s_%s", dpu.Namespace, dpu.Name, dpu.UID))
	if err := os.MkdirAll(filepath.Dir(bfCFGPath), os.ModePerm); err != nil {
		err = fmt.Errorf("failed to create directory %s: %w", filepath.Dir(bfCFGPath), err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToCreateDirectory", err.Error()))
		return *state, err
	}

	joinCommand, err := ctrlCtx.JoinCommandGenerator.GenerateJoinCommand(ctx, dc)
	if err != nil {
		err = fmt.Errorf("failed to generate join command: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGenerateJoinCommand", err.Error()))
		return *state, err
	}

	kubeadmSecretName := cutil.KubeadmJoinSecretName(dpu.Name)
	kubeadmSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubeadmSecretName,
			Namespace: dpu.Namespace,
		},
		Data: map[string][]byte{
			"join": []byte(joinCommand),
		},
	}
	if err := controllerutil.SetOwnerReference(dpu, kubeadmSecret, ctrlCtx.Client.Scheme()); err != nil {
		err = fmt.Errorf("failed to set owner reference on kubeadm join secret: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToCreateKubeadmSecret", err.Error()))
		return *state, err
	}
	if err := ctrlCtx.Client.Create(ctx, kubeadmSecret); err != nil && !apierrors.IsAlreadyExists(err) {
		err = fmt.Errorf("failed to create kubeadm join secret: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToCreateKubeadmSecret", err.Error()))
		return *state, err
	}

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "DPUDeviceNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGetDPUDevice", err.Error()))
		return *state, err
	}

	cfg, err := bfcfg.GenerateBFConfig(ctx, ctrlCtx, dpu, flavor)
	if err != nil {
		err = fmt.Errorf("failed to generate bf.cfg: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGenerateBFConfig", err.Error()))
		return *state, err
	}
	logger.Info(fmt.Sprintf("write bf.cfg to %s", bfCFGPath))
	if err := os.WriteFile(bfCFGPath, cfg, os.ModePerm); err != nil {
		err = fmt.Errorf("failed to write bf.cfg to %s: %w", bfCFGPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToPushBFCFG", err.Error()))
		return *state, err
	}
	state.BFCFGFile = bfCFGPath
	cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), nil, "", ""))
	state.Phase = provisioningv1.DPUOSInstalling
	return *state, nil
}

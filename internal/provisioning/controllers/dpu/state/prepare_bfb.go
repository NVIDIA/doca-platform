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
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"
	"github.com/nvidia/doca-platform/internal/spire"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BFCFGDir    = "bfcfg"
	UserDataDir = "user-data"
)

func prepareBF4ISO(ctx context.Context, dpu *provisioningv1.DPU, artifact dutil.BF4Artifact, state *provisioningv1.DPUStatus) (string, error) {
	logger := log.FromContext(ctx)
	userDataBasePath := filepath.Join("/", cutil.BFBBaseDir, UserDataDir, fmt.Sprintf("%s_%s_%s", dpu.Namespace, dpu.Name, dpu.UID))
	if err := os.MkdirAll(userDataBasePath, os.ModePerm); err != nil {
		err = fmt.Errorf("failed to create directory %s: %w", userDataBasePath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToCreateDirectory", err.Error()))
		return "", err
	}

	userDataPath := filepath.Join(userDataBasePath, UserDataDir)

	if err := cutil.RemoveFileEx(userDataPath); err != nil {
		err = fmt.Errorf("failed to remove user-data at %s: %w", userDataPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToRemoveUserData", err.Error()))
		return "", err
	}

	logger.Info(fmt.Sprintf("write user-data to %s", userDataPath))
	if err := os.WriteFile(userDataPath, artifact.UserData, os.ModePerm); err != nil {
		err = fmt.Errorf("failed to write user-data to %s: %w", userDataPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToWriteUserData", err.Error()))
		return "", err
	}

	networkCfgPath := filepath.Join(userDataBasePath, "network-config")
	if err := cutil.RemoveFileEx(networkCfgPath); err != nil {
		err = fmt.Errorf("failed to remove network-config at %s: %w", networkCfgPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToRemoveNetworkCfg", err.Error()))
		return "", err
	}
	logger.Info(fmt.Sprintf("write network-config to %s", networkCfgPath))
	if err := os.WriteFile(networkCfgPath, artifact.NetworkConfig, os.ModePerm); err != nil {
		err = fmt.Errorf("failed to write network-config to %s: %w", networkCfgPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToWriteNetworkCfg", err.Error()))
		return "", err
	}

	isoBasePath := filepath.Join(userDataBasePath, "seed")
	isoPath := isoBasePath + ".iso"

	if err := cutil.RemoveFileEx(isoPath); err != nil {
		err = fmt.Errorf("failed to remove ISO image at %s: %w", isoPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToRemoveISO", err.Error()))
		return "", err
	}
	logger.Info(fmt.Sprintf("create ISO image at %s", isoPath))

	// Match mkisofs argument order: user-data meta-data network-config
	createdPath, err := dutil.MkIso(isoBasePath, "cidata", []dutil.IsoRootFile{
		{Name: "user-data", Data: artifact.UserData},
		{Name: "meta-data", Data: []byte{}},
		{Name: "network-config", Data: artifact.NetworkConfig},
	})

	if err != nil {
		err = fmt.Errorf("failed to create ISO image at %s: %w", isoPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToCreateISO", err.Error()))
		return "", err
	}

	return createdPath, nil
}

func prepareBF3BFB(ctx context.Context, dpu *provisioningv1.DPU, bfCFG []byte, state *provisioningv1.DPUStatus) (string, error) {
	logger := log.FromContext(ctx)
	bfCFGPath := filepath.Join("/", cutil.BFBBaseDir, BFCFGDir, fmt.Sprintf("%s_%s_%s", dpu.Namespace, dpu.Name, dpu.UID))
	if err := os.MkdirAll(filepath.Dir(bfCFGPath), os.ModePerm); err != nil {
		err = fmt.Errorf("failed to create directory %s: %w", filepath.Dir(bfCFGPath), err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToCreateDirectory", err.Error()))
		return "", err
	}

	logger.Info(fmt.Sprintf("write bf.cfg to %s", bfCFGPath))
	if err := os.WriteFile(bfCFGPath, bfCFG, os.ModePerm); err != nil {
		err = fmt.Errorf("failed to write bf.cfg to %s: %w", bfCFGPath, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToPushBFCFG", err.Error()))
		return "", err
	}

	return bfCFGPath, nil
}

// ensureRBAC creates the per-DPU Role and RoleBinding for the DPU agent and returns
// the bootstrap token to embed in the agent kubeconfig. SPIFFE-mode DPUs authenticate
// with a SPIRE-issued JWT-SVID rather than a bootstrap token, so they bind the literal
// SPIFFE-ID subject and return an empty token (the cloud-init render then omits the
// bootstrap kubeconfig).
func ensureRBAC(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU, flavor *provisioningv1.DPUFlavor, dpuDevice *provisioningv1.DPUDevice) (string, error) {
	if err := cutil.CreateDPUAgentRole(ctx, ctrlCtx.Client, ctrlCtx.Client.Scheme(), dpu, flavor); err != nil {
		return "", fmt.Errorf("creating DPU agent role: %w", err)
	}

	subject, err := dpuAgentRoleBindingSubject(ctx, ctrlCtx, dpu, dpuDevice)
	if err != nil {
		return "", err
	}
	if err := cutil.CreateDPUAgentRoleBinding(ctx, ctrlCtx.Client, ctrlCtx.Client.Scheme(), dpu, subject); err != nil {
		return "", fmt.Errorf("creating DPU agent role binding: %w", err)
	}

	if cutil.IsSpiffeDPU(dpu) {
		return "", nil
	}

	token, err := cutil.CreateDPUAgentBootstrapToken(ctx, ctrlCtx.Client, dpu)
	if err != nil {
		return "", fmt.Errorf("creating DPU agent bootstrap token: %w", err)
	}
	return token, nil
}

// dpuAgentRoleBindingSubject returns the RBAC subject name for the per-DPU
// RoleBinding: the post-exchange SPIFFE ID for SPIFFE-mode DPUs, or the
// certificate username (da-<dpu>) for bootstrap-token DPUs.
func dpuAgentRoleBindingSubject(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU, dpuDevice *provisioningv1.DPUDevice) (string, error) {
	if !cutil.IsSpiffeDPU(dpu) {
		return providentity.DPUAgentUsername(dpu.Name), nil
	}
	cfg, err := dpfutils.GetDPFOperatorConfig(ctx, ctrlCtx.Client)
	if err != nil {
		return "", fmt.Errorf("getting DPFOperatorConfig for SPIFFE RBAC subject: %w", err)
	}
	if !cutil.SpiffeEnabled(cfg) {
		return "", fmt.Errorf("DPU %s is SPIFFE-mode but cluster spec.security.spiffe is unset", dpu.Name)
	}
	renderer, err := spire.NewDPUAgentIdentityRenderer(cfg.Spec.Security.SPIFFE)
	if err != nil {
		return "", fmt.Errorf("validating SPIFFE identity templates for DPU %s: %w", dpu.Name, err)
	}
	identities, err := renderer.Render(dpu, dpuDevice)
	if err != nil {
		return "", fmt.Errorf("building SPIFFE RBAC subject for DPU %s: %w", dpu.Name, err)
	}
	return identities.ExchangedSPIFFEID, nil
}

func PrepareBFB(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
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

	joinCommand, err := ctrlCtx.JoinCommandGenerator.GenerateJoinCommand(ctx, dc, dpu)
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

	bootstrapToken, err := ensureRBAC(ctx, ctrlCtx, dpu, flavor, dpuDevice)
	if err != nil {
		err = fmt.Errorf("failed to ensure DPU agent RBAC: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToEnsureRBAC", err.Error()))
		return *state, err
	}

	artifactGenerator := ctrlCtx.DPUArtifactGenerator
	artifactRequest := dutil.DPUArtifactRequest{
		ControllerContext: ctrlCtx,
		DPU:               dpu,
		Flavor:            flavor,
		BootstrapToken:    bootstrapToken,
	}

	var artifactPath string
	var prepErr error
	if dpuDevice.Status.DPUType == provisioningv1.DPUTypeBlueField4 {
		artifact, err := artifactGenerator.GenerateBF4(ctx, artifactRequest)
		if err != nil {
			err = fmt.Errorf("failed to generate BF4 artifact: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGenerateBF4Artifact", err.Error()))
			return *state, err
		}
		artifactPath, prepErr = prepareBF4ISO(ctx, dpu, artifact, state)
	} else {
		cfg, err := artifactGenerator.GenerateBF3(ctx, artifactRequest)
		if err != nil {
			err = fmt.Errorf("failed to generate bf.cfg: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), err, "FailedToGenerateBFConfig", err.Error()))
			return *state, err
		}
		artifactPath, prepErr = prepareBF3BFB(ctx, dpu, cfg, state)
	}
	if prepErr != nil {
		return *state, prepErr
	}

	state.BFCFGFile = artifactPath
	cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBPrepared.String(), nil, "", ""))
	state.Phase = provisioningv1.DPUOSInstalling
	return *state, nil
}

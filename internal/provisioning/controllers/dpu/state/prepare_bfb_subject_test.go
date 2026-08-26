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

package state

import (
	"context"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestDPUAgentRoleBindingSubject covers the RBAC subject selection: bootstrap-token DPUs bind
// the cert username (da-<dpu>); SPIFFE DPUs bind the literal SPIFFE-ID URI; an unrepresentable
// serial fails closed.
func TestDPUAgentRoleBindingSubject(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))

	spiffeConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "dpf-operator-system"},
		Spec: operatorv1.DPFOperatorConfigSpec{
			Security: &operatorv1.SecurityConfiguration{
				SPIFFE: &operatorv1.SPIFFEConfiguration{
					SPIRETrustDomain:                  "cs.internal",
					DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/dummy-operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
					DPUAgentExchangedSPIFFEIDTemplate: "spiffe://dummy-operator.az51-dev2.dsx.nvid.id/dpu/{{ .SerialNumber }}/process/dpu-agent",
				},
			},
		},
	}

	bootstrapDPU := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu-01"}}
	spiffeDPU := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-01"},
		Status:     provisioningv1.DPUStatus{IdentityMode: ptr.To(provisioningv1.IdentityModeSpiffe)},
	}
	device := func(serial string) *provisioningv1.DPUDevice {
		return &provisioningv1.DPUDevice{Spec: provisioningv1.DPUDeviceSpec{SerialNumber: serial}}
	}

	tests := []struct {
		name        string
		dpu         *provisioningv1.DPU
		device      *provisioningv1.DPUDevice
		objs        []client.Object
		wantSubject string
		wantErr     bool
	}{
		{
			name:        "bootstrap-token DPU binds cert username without consulting config",
			dpu:         bootstrapDPU,
			device:      device("SN123"),
			wantSubject: "da-dpu-01",
		},
		{
			name:        "SPIFFE DPU binds the literal SPIFFE-ID URI",
			dpu:         spiffeDPU,
			device:      device("SN123"),
			objs:        []client.Object{spiffeConfig},
			wantSubject: "spiffe://dummy-operator.az51-dev2.dsx.nvid.id/dpu/sn123/process/dpu-agent",
		},
		{
			name:    "SPIFFE DPU with unrepresentable serial fails closed",
			dpu:     spiffeDPU,
			device:  device("bad serial!"),
			objs:    []client.Object{spiffeConfig},
			wantErr: true,
		},
		{
			name:    "SPIFFE DPU without SPIFFE configuration fails closed",
			dpu:     spiffeDPU,
			device:  device("SN123"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()
			ctrlCtx := &dutil.ControllerContext{Client: fakeClient}

			subject, err := dpuAgentRoleBindingSubject(context.Background(), ctrlCtx, tt.dpu, tt.device)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got subject %q", subject)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if subject != tt.wantSubject {
				t.Fatalf("subject = %q, want %q", subject, tt.wantSubject)
			}
		})
	}
}

func TestEnsureRBACSkipsBootstrapTokenForSPIFFEDPU(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-01", Namespace: "dpf-operator-system"},
		Status:     provisioningv1.DPUStatus{IdentityMode: ptr.To(provisioningv1.IdentityModeSpiffe)},
	}
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: "device-01", Namespace: dpu.Namespace},
		Spec:       provisioningv1.DPUDeviceSpec{SerialNumber: "SN123"},
	}
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: dpu.Namespace},
		Spec: operatorv1.DPFOperatorConfigSpec{
			Security: &operatorv1.SecurityConfiguration{
				SPIFFE: &operatorv1.SPIFFEConfiguration{
					SPIRETrustDomain:                  "cs.internal",
					DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/dummy-operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
					DPUAgentExchangedSPIFFEIDTemplate: "spiffe://dummy-operator.az51-dev2.dsx.nvid.id/dpu/{{ .SerialNumber }}/process/dpu-agent",
				},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config).Build()
	ctrlCtx := &dutil.ControllerContext{Client: fakeClient, Scheme: scheme}

	token, err := ensureRBAC(context.Background(), ctrlCtx, dpu, nil, device)
	if err != nil {
		t.Fatalf("ensureRBAC() error = %v", err)
	}
	if token != "" {
		t.Fatalf("ensureRBAC() token = %q, want empty", token)
	}

	roleBinding := &rbacv1.RoleBinding{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Name: "da-dpu-01", Namespace: dpu.Namespace}, roleBinding); err != nil {
		t.Fatalf("get RoleBinding: %v", err)
	}
	if len(roleBinding.Subjects) != 1 || roleBinding.Subjects[0].Name != "spiffe://dummy-operator.az51-dev2.dsx.nvid.id/dpu/sn123/process/dpu-agent" {
		t.Fatalf("RoleBinding subjects = %#v", roleBinding.Subjects)
	}

	secrets := &corev1.SecretList{}
	if err := fakeClient.List(context.Background(), secrets, client.InNamespace("kube-system")); err != nil {
		t.Fatalf("list bootstrap secrets: %v", err)
	}
	if len(secrets.Items) != 0 {
		t.Fatalf("bootstrap secrets = %#v, want none", secrets.Items)
	}
}

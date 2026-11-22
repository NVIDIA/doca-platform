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
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/certmanagement"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/certmanager"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestResolveBFBRegistryLayout(t *testing.T) {
	scheme := runtime.NewScheme()
	g := NewWithT(t)
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())

	const ns = "dpf-operator-system"
	rwx := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "bfb-pvc", Namespace: ns},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	rwo := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "bfb-rwo", Namespace: ns},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	rwxRox := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "bfb-pvc", Namespace: ns},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany, corev1.ReadOnlyMany},
		},
	}

	tests := []struct {
		name       string
		pvcName    *string
		objs       []client.Object
		wantLayout inventory.BFBRegistryLayout
		wantClaim  string
	}{
		{name: "unset name uses hostPath under the provisioning controller", pvcName: nil, wantLayout: inventory.BFBRegistryLayoutProvisioningController},
		{name: "missing pvc stays with the provisioning controller", pvcName: ptr.To("bfb-pvc"), wantLayout: inventory.BFBRegistryLayoutProvisioningController},
		{name: "RWX pending", pvcName: ptr.To("bfb-pvc"), objs: []client.Object{rwx}, wantLayout: inventory.BFBRegistryLayoutSpread, wantClaim: "bfb-pvc"},
		{name: "RWX+ROX mix", pvcName: ptr.To("bfb-pvc"), objs: []client.Object{rwxRox}, wantLayout: inventory.BFBRegistryLayoutSpread, wantClaim: "bfb-pvc"},
		{name: "RWO stays with the provisioning controller", pvcName: ptr.To("bfb-rwo"), objs: []client.Object{rwo}, wantLayout: inventory.BFBRegistryLayoutProvisioningController},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()
			r := &DPFOperatorConfigReconciler{Client: c}
			vars := &inventory.Variables{
				DPFProvisioningController: inventory.DPFProvisioningVariables{
					BFBPersistentVolumeClaimName: tt.pvcName,
				},
			}
			g.Expect(r.resolveBFBRegistryLayout(context.Background(), ns, vars)).To(Succeed())
			g.Expect(vars.DPFProvisioningController.BFBRegistryLayout).To(Equal(tt.wantLayout))
			g.Expect(vars.DPFProvisioningController.BFBRegistryVolumeClaim).To(Equal(tt.wantClaim))
		})
	}

	t.Run("PVC read error is returned instead of flipping the registry owner", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fmt.Errorf("boom")
			},
		}).Build()
		r := &DPFOperatorConfigReconciler{Client: c}
		vars := &inventory.Variables{
			DPFProvisioningController: inventory.DPFProvisioningVariables{BFBPersistentVolumeClaimName: ptr.To("bfb-pvc")},
		}
		g.Expect(r.resolveBFBRegistryLayout(context.Background(), ns, vars)).To(MatchError(ContainSubstring("get BFB PVC dpf-operator-system/bfb-pvc")))
	})
}

func TestLogIgnoredBFBRegistryReplicas(t *testing.T) {
	g := NewWithT(t)
	r := &DPFOperatorConfigReconciler{}
	withReplicas := func(n int32) *inventory.Variables {
		return &inventory.Variables{DPFProvisioningController: inventory.DPFProvisioningVariables{
			Registry: &operatorv1.RegistryConfiguration{Replicas: n},
		}}
	}

	r.logIgnoredBFBRegistryReplicas(context.Background(), withReplicas(1))
	g.Expect(r.bfbRegistryReplicasIgnoredLogged).To(BeFalse(), "one replica is what the provisioning controller runs anyway")

	r.logIgnoredBFBRegistryReplicas(context.Background(), withReplicas(3))
	g.Expect(r.bfbRegistryReplicasIgnoredLogged).To(BeTrue())

	r.logIgnoredBFBRegistryReplicas(context.Background(), withReplicas(0))
	g.Expect(r.bfbRegistryReplicasIgnoredLogged).To(BeFalse(), "logs again if the setting is changed back")
}

func TestBFBPVCPredicate(t *testing.T) {
	g := NewWithT(t)
	pred := bfbPVCPredicate()
	pvc := func(modes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{Spec: corev1.PersistentVolumeClaimSpec{AccessModes: modes}}
	}

	g.Expect(pred.Create(event.CreateEvent{Object: pvc(corev1.ReadWriteMany)})).To(BeTrue())
	g.Expect(pred.Delete(event.DeleteEvent{Object: pvc(corev1.ReadWriteMany)})).To(BeTrue())

	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: pvc(corev1.ReadWriteOnce),
		ObjectNew: pvc(corev1.ReadWriteMany),
	})).To(BeTrue())

	bound := pvc(corev1.ReadWriteMany)
	bound.Status.Phase = corev1.ClaimBound
	bound.Annotations = map[string]string{"pv.kubernetes.io/bind-completed": "yes"}
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: pvc(corev1.ReadWriteMany),
		ObjectNew: bound,
	})).To(BeFalse(), "binding or annotation updates do not change the registry layout")
}

func TestBFBPVCToDPFOperatorConfig(t *testing.T) {
	g := NewWithT(t)
	nn := types.NamespacedName{Namespace: "dpf-operator-system", Name: "dpfoperatorconfig"}
	r := &DPFOperatorConfigReconciler{
		Settings: &DPFOperatorConfigReconcilerSettings{ConfigSingletonNamespaceName: &nn},
	}

	g.Expect(r.bfbPVCToDPFOperatorConfig(context.Background(), &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system", Name: "bfb-pvc"},
	})).To(Equal([]ctrl.Request{{NamespacedName: nn}}))

	g.Expect(r.bfbPVCToDPFOperatorConfig(context.Background(), &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "bfb-pvc"},
	})).To(BeEmpty())

	r.Settings = nil
	g.Expect(r.bfbPVCToDPFOperatorConfig(context.Background(), &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system", Name: "bfb-pvc"},
	})).To(BeEmpty())
}

func TestReconcileSystemComponentsSkipsProvisioningApplyWhenBFBPVCReadFails(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())

	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
				return fmt.Errorf("boom")
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	}).Build()
	generated := false
	r := &DPFOperatorConfigReconciler{
		Client: c,
		Inventory: stubSystemComponents(recordingComponent{
			StubComponent: inventory.StubComponentWithObjs(operatorv1.ProvisioningControllerName, nil),
			generated:     &generated,
		}),
		Defaults: testReleaseDefaults(),
	}

	err := r.reconcileSystemComponents(context.Background(), testOperatorConfigWithBFBPVC(), nil, certmanager.IssuerReference{Name: operatorv1.PlatformIssuerName}, certmanagement.MigrationState{})
	g.Expect(err).To(MatchError(ContainSubstring("get BFB PVC")))
	g.Expect(generated).To(BeFalse(), "an unknown registry owner must not flip or prune the registry")
}

// failingGetClient returns a client whose Get fails for the given kinds of object, like a transient
// API server error would.
func failingGetClient(g *WithT, failing ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			for _, f := range failing {
				if fmt.Sprintf("%T", obj) == fmt.Sprintf("%T", f) {
					return fmt.Errorf("boom")
				}
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	}).Build()
}

func TestReconcileSystemComponentsAppliesWhenProvisioningDisabledAndReadsFail(t *testing.T) {
	g := NewWithT(t)
	c := failingGetClient(g, &corev1.PersistentVolumeClaim{}, &corev1.ConfigMap{})
	generated := false
	r := &DPFOperatorConfigReconciler{
		Client: c,
		Inventory: stubSystemComponents(recordingComponent{
			StubComponent: inventory.StubComponentWithObjs(operatorv1.ProvisioningControllerName, nil),
			generated:     &generated,
		}),
		Defaults: testReleaseDefaults(),
	}
	cfg := testOperatorConfigWithBFBPVC()
	cfg.Spec.ProvisioningController.Disable = ptr.To(true)

	g.Expect(r.reconcileSystemComponents(context.Background(), cfg, nil, certmanager.IssuerReference{Name: operatorv1.PlatformIssuerName}, certmanagement.MigrationState{})).To(Succeed())
	g.Expect(generated).To(BeTrue(), "empty inventory must still apply so disabled provisioning objects can be pruned")
}

func testOperatorConfigWithDisabledBFBRegistry() *operatorv1.DPFOperatorConfig {
	cfg := testOperatorConfigWithBFBPVC()
	cfg.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
		InstallViaRedfish: &operatorv1.InstallViaRedfish{
			BFBRegistry: &operatorv1.BFBRegistryConfiguration{Disable: ptr.To(true)},
		},
	}
	return cfg
}

func TestReconcileSystemComponentsAppliesWhenRegistryDisabledAndBFBPVCReadFails(t *testing.T) {
	g := NewWithT(t)
	// Only the PVC read fails: the Operator runs no registry Deployment then, so the layout does not
	// matter, but the Service is still rendered and its NodePort is still read from the ConfigMap.
	c := failingGetClient(g, &corev1.PersistentVolumeClaim{})
	generated := false
	r := &DPFOperatorConfigReconciler{
		Client: c,
		Inventory: stubSystemComponents(recordingComponent{
			StubComponent: inventory.StubComponentWithObjs(operatorv1.ProvisioningControllerName, nil),
			generated:     &generated,
		}),
		Defaults: testReleaseDefaults(),
	}

	g.Expect(r.reconcileSystemComponents(context.Background(), testOperatorConfigWithDisabledBFBRegistry(), nil, certmanager.IssuerReference{Name: operatorv1.PlatformIssuerName}, certmanagement.MigrationState{})).To(Succeed())
	g.Expect(generated).To(BeTrue(), "provisioning must still apply so a disabled registry does not depend on its PVC")
}

func TestReconcileSystemComponentsSkipsProvisioningApplyWhenRegistryDisabledAndNodePortReadFails(t *testing.T) {
	g := NewWithT(t)
	c := failingGetClient(g, &corev1.ConfigMap{})
	generated := false
	r := &DPFOperatorConfigReconciler{
		Client: c,
		Inventory: stubSystemComponents(recordingComponent{
			StubComponent: inventory.StubComponentWithObjs(operatorv1.ProvisioningControllerName, nil),
			generated:     &generated,
		}),
		Defaults: testReleaseDefaults(),
	}

	err := r.reconcileSystemComponents(context.Background(), testOperatorConfigWithDisabledBFBRegistry(), nil, certmanager.IssuerReference{Name: operatorv1.PlatformIssuerName}, certmanagement.MigrationState{})
	g.Expect(err).To(MatchError(ContainSubstring("get bfb-registry-nodeport ConfigMap")))
	g.Expect(generated).To(BeFalse(), "the Service is rendered for a disabled registry too and must keep its NodePort")
}

type recordingComponent struct {
	inventory.StubComponent
	generated *bool
	// vars, when set, receives the variables the component was generated with.
	vars *inventory.Variables
}

func (c recordingComponent) GenerateManifests(ctx context.Context, vars inventory.Variables) ([]client.Object, error) {
	*c.generated = true
	if c.vars != nil {
		*c.vars = vars
	}
	return c.StubComponent.GenerateManifests(ctx, vars)
}

func stubSystemComponents(provisioning inventory.Component) *inventory.SystemComponents {
	stub := func(name operatorv1.ComponentName) inventory.Component {
		return inventory.StubComponentWithObjs(name, nil)
	}
	return &inventory.SystemComponents{
		DPUService:                      stub(operatorv1.DPUServiceControllerName),
		DPFProvisioning:                 provisioning,
		ServiceFunctionChainSet:         stub(operatorv1.ServiceSetControllerName),
		RemoveOVSCNI:                    stub(operatorv1.OVSCNIName),
		Multus:                          stub(operatorv1.MultusName),
		SRIOVDevicePlugin:               stub(operatorv1.SRIOVDevicePluginName),
		NVIPAM:                          stub(operatorv1.NVIPAMControllerName),
		Flannel:                         stub(operatorv1.FlannelName),
		SfcController:                   stub(operatorv1.SFCControllerName),
		KamajiClusterManager:            stub(operatorv1.KamajiClusterManagerName),
		StaticClusterManager:            stub(operatorv1.StaticClusterManagerName),
		DPUDetector:                     stub(operatorv1.DPUDetectorName),
		CNIInstaller:                    stub(operatorv1.CNIInstallerName),
		NodeSRIOVDevicePluginController: stub(operatorv1.NodeSRIOVDevicePluginControllerName),
		KubeStateMetrics:                stub(operatorv1.KubeStateMetricsName),
		DPUMonitoring:                   stub(operatorv1.DPUMonitoringName),
		NodeProblemDetector:             stub(operatorv1.NodeProblemDetectorName),
		OpenTelemetryCollector:          stub(operatorv1.OpenTelemetryCollectorName),
		KataContainers:                  stub(operatorv1.KataContainersName),
		SPIFFECSIDriver:                 stub(operatorv1.SPIFFECSIDriverName),
		VaultKMS:                        stub(operatorv1.VaultKMSName),
		SpireAgentRBAC:                  stub(operatorv1.SpireAgentRBACName),
		CoreDNS:                         stub(operatorv1.CoreDNSName),
	}
}

func testOperatorConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultDPFOperatorConfigSingletonName,
			Namespace: DefaultDPFOperatorConfigSingletonNamespace,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
		},
	}
}

func testOperatorConfigWithBFBPVC() *operatorv1.DPFOperatorConfig {
	cfg := testOperatorConfig()
	cfg.Spec.ProvisioningController.BFBPersistentVolumeClaimName = ptr.To("bfb-pvc")
	return cfg
}

// bfbTestNamespace is the namespace of the DPFOperatorConfig these tests reconcile.
const bfbTestNamespace = "dpf-operator-system"

func bfbTestPVC(mode corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "bfb-pvc", Namespace: bfbTestNamespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{mode},
		},
	}
}

func testReleaseDefaults() *release.Defaults {
	return &release.Defaults{
		DMSImage:                   "example.com/hostdriver:v0.1.0",
		DPFSystemImage:             "example.com/dpf-system:v0.1.0",
		CNIInstallerImage:          "example.com/cni-installer:v0.1.0",
		DPUNetworkingHelmChart:     "oci://example.com/dpu-networking:v0.1.0",
		BFBRegistryImage:           "example.com/bfb-registry:v0.1.0",
		KeepalivedImage:            "example.com/keepalived:v0.1.0",
		NodeSRIOVDevicePluginImage: "example.com/sriov:v0.1.0",
		KataDeployImage:            "example.com/kata:v0.1.0",
	}
}

func TestReadBFBRegistryNodePort(t *testing.T) {
	const ns = "dpf-operator-system"
	nodePortCM := func(value string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: bfbregistry.NodePortConfigMapName},
			Data:       map[string]string{bfbregistry.NodePortConfigMapKey: value},
		}
	}

	tests := []struct {
		name     string
		objs     []client.Object
		wantPort int32
	}{
		{name: "fresh install has no ConfigMap", wantPort: 0},
		{name: "pins the port recorded by an earlier release", objs: []client.Object{nodePortCM("31443")}, wantPort: 31443},
		{name: "empty value", objs: []client.Object{nodePortCM("")}, wantPort: 0},
		{name: "not a number", objs: []client.Object{nodePortCM("https")}, wantPort: 0},
		{name: "out of range", objs: []client.Object{nodePortCM("70000")}, wantPort: 0},
		{name: "zero", objs: []client.Object{nodePortCM("0")}, wantPort: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()
			r := &DPFOperatorConfigReconciler{Client: c}
			vars := &inventory.Variables{}

			g.Expect(r.readBFBRegistryNodePort(context.Background(), ns, vars)).To(Succeed())
			g.Expect(vars.DPFProvisioningController.BFBRegistryNodePort).To(Equal(tt.wantPort))
		})
	}

	t.Run("read error is returned instead of letting the port be re-allocated", func(t *testing.T) {
		g := NewWithT(t)
		scheme := runtime.NewScheme()
		g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return fmt.Errorf("boom")
			},
		}).Build()
		r := &DPFOperatorConfigReconciler{Client: c}
		g.Expect(r.readBFBRegistryNodePort(context.Background(), ns, &inventory.Variables{})).To(MatchError(ContainSubstring("get bfb-registry-nodeport ConfigMap")))
	})
}

func TestReconcileSystemComponentsPinsBFBRegistryNodePort(t *testing.T) {
	tests := []struct {
		name string
		pvc  *corev1.PersistentVolumeClaim
		cfg  func() *operatorv1.DPFOperatorConfig
		cm   *corev1.ConfigMap
		want int32
	}{
		{name: "RWX PVC", pvc: bfbTestPVC(corev1.ReadWriteMany), cfg: testOperatorConfigWithBFBPVC, want: 31443},
		{name: "RWO PVC", pvc: bfbTestPVC(corev1.ReadWriteOnce), cfg: testOperatorConfigWithBFBPVC, want: 31443},
		{name: "hostPath", cfg: testOperatorConfig, want: 31443},
		{name: "registry disabled", cfg: func() *operatorv1.DPFOperatorConfig {
			cfg := testOperatorConfig()
			cfg.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
				InstallViaRedfish: &operatorv1.InstallViaRedfish{
					BFBRegistry: &operatorv1.BFBRegistryConfiguration{Disable: ptr.To(true)},
				},
			}
			return cfg
		}, want: 31443},
		{name: "fresh install", cfg: testOperatorConfig, cm: &corev1.ConfigMap{}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(corev1.AddToScheme(scheme)).To(Succeed())

			var objs []client.Object
			if tt.pvc != nil {
				objs = append(objs, tt.pvc)
			}
			if tt.cm == nil {
				objs = append(objs, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: bfbTestNamespace, Name: bfbregistry.NodePortConfigMapName},
					Data:       map[string]string{bfbregistry.NodePortConfigMapKey: "31443"},
				})
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			generated := false
			var got inventory.Variables
			r := &DPFOperatorConfigReconciler{
				Client: c,
				Inventory: stubSystemComponents(recordingComponent{
					StubComponent: inventory.StubComponentWithObjs(operatorv1.ProvisioningControllerName, nil),
					generated:     &generated,
					vars:          &got,
				}),
				Defaults: testReleaseDefaults(),
			}

			g.Expect(r.reconcileSystemComponents(context.Background(), tt.cfg(), nil, certmanager.IssuerReference{Name: operatorv1.PlatformIssuerName}, certmanagement.MigrationState{})).To(Succeed())
			g.Expect(generated).To(BeTrue())
			g.Expect(got.DPFProvisioningController.BFBRegistryNodePort).To(Equal(tt.want))
		})
	}
}

func TestReconcileSystemComponentsSkipsProvisioningApplyWhenNodePortConfigMapReadFails(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())

	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return fmt.Errorf("boom")
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	}).Build()
	generated := false
	r := &DPFOperatorConfigReconciler{
		Client: c,
		Inventory: stubSystemComponents(recordingComponent{
			StubComponent: inventory.StubComponentWithObjs(operatorv1.ProvisioningControllerName, nil),
			generated:     &generated,
		}),
		Defaults: testReleaseDefaults(),
	}

	err := r.reconcileSystemComponents(context.Background(), testOperatorConfig(), nil, certmanager.IssuerReference{Name: operatorv1.PlatformIssuerName}, certmanagement.MigrationState{})
	g.Expect(err).To(MatchError(ContainSubstring("get bfb-registry-nodeport ConfigMap")))
	g.Expect(generated).To(BeFalse(), "a Service rendered without its pinned port would get a new NodePort")
}

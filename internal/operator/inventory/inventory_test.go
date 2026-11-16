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

package inventory

import (
	"context"
	"slices"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestManifests_Parse_Generate_All(t *testing.T) {
	g := NewGomegaWithT(t)
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	tests := []struct {
		name      string
		inventory *SystemComponents
		wantErr   bool
	}{
		{
			name:      "parse objects from release directory",
			inventory: New(),
		},
		// dpuServiceControllerObjects
		{
			name:      "fail if DPUService controller data is nil",
			inventory: New().setDPUService(dpuServiceControllerObjects{data: nil}),
			wantErr:   true,
		},
		// serviceChainSetControllerObjects
		{
			name: "fail if ServiceFunctionChainSet data is nil",
			inventory: New().setServiceFunctionChainSet(dpuServicePerDPUClusterObjects{
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if ServiceFunctionChainSet data has an unexpected object",
			inventory: New().setServiceFunctionChainSet(dpuServicePerDPUClusterObjects{
				data: addUnexpectedKindToObjects(g, serviceChainSetData),
			}),
			wantErr: true,
		},
		{
			name: "fail if ServiceFunctionChainSet is missing the DPUService",
			inventory: New().setServiceFunctionChainSet(dpuServicePerDPUClusterObjects{
				data: removeKindFromObjects(g, "DPUService", serviceChainSetData),
			}),
			wantErr: true,
		},
		{
			name: "fail if ServiceFunctionChainSet is missing the DPUServiceCredentialRequest",
			inventory: New().setServiceFunctionChainSet(dpuServicePerDPUClusterObjects{
				data: removeKindFromObjects(g, "DPUServiceCredentialRequest", serviceChainSetData),
			}),
			wantErr: true,
		},
		// multus
		{
			name: "fail if MultusConfiguration data is nil",
			inventory: New().setMultus(fromDPUService{
				name: operatorv1.MultusName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if MultusConfiguration data has an unexpected object",
			inventory: New().setMultus(fromDPUService{
				name: operatorv1.MultusName,
				data: addUnexpectedKindToObjects(g, serviceChainSetData),
			}),
			wantErr: true,
		},
		{
			name: "fail if MultusConfiguration is missing the DPUService",
			inventory: New().setMultus(fromDPUService{
				name: operatorv1.MultusName,
				data: removeKindFromObjects(g, "DPUService", serviceChainSetData),
			}),
			wantErr: true,
		},
		// sriovDevicePlugin
		{
			name: "fail if sriovDevicePlugin data is nil",
			inventory: New().setSRIOVDevicePlugin(fromDPUService{
				name: operatorv1.SRIOVDevicePluginName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if SRIOVDevicePluginConfiguration data has an unexpected object",
			inventory: New().setSRIOVDevicePlugin(fromDPUService{
				name: operatorv1.SRIOVDevicePluginName,
				data: addUnexpectedKindToObjects(g, serviceChainSetData),
			}),
			wantErr: true,
		},
		{
			name: "fail if SRIOVDevicePluginConfiguration is missing the DPUService",
			inventory: New().setSRIOVDevicePlugin(fromDPUService{
				name: operatorv1.SRIOVDevicePluginName,
				data: removeKindFromObjects(g, "DPUService", serviceChainSetData),
			}),
			wantErr: true,
		},
		// flannel
		{
			name: "fail if flannel data is nil",
			inventory: New().setFlannel(fromDPUService{
				name: operatorv1.FlannelName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if flannel data has an unexpected object",
			inventory: New().setFlannel(fromDPUService{
				name: operatorv1.FlannelName,
				data: addUnexpectedKindToObjects(g, flannelData),
			}),
			wantErr: true,
		},
		{
			name: "fail if flannel is missing the DPUService",
			inventory: New().setFlannel(fromDPUService{
				name: operatorv1.FlannelName,
				data: removeKindFromObjects(g, "DPUService", flannelData),
			}),
			wantErr: true,
		},
		// nv-ipam
		{
			name: "fail if nv-ipam data is nil",
			inventory: New().setNvK8sIpam(dpuServicePerDPUClusterObjects{
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if nv-ipam data has an unexpected object",
			inventory: New().setNvK8sIpam(dpuServicePerDPUClusterObjects{
				data: addUnexpectedKindToObjects(g, nvK8sIpamData),
			}),
			wantErr: true,
		},
		{
			name: "fail if nv-ipam is missing the DPUService",
			inventory: New().setNvK8sIpam(dpuServicePerDPUClusterObjects{
				data: removeKindFromObjects(g, "DPUService", nvK8sIpamData),
			}),
			wantErr: true,
		},
		{
			name: "fail if nv-ipam is missing the DPUServiceCredentialRequest",
			inventory: New().setNvK8sIpam(dpuServicePerDPUClusterObjects{
				data: removeKindFromObjects(g, "DPUServiceCredentialRequest", nvK8sIpamData),
			}),
			wantErr: true,
		},
		// sfc-controller
		{
			name: "fail if sfc-controller data is nil",
			inventory: New().setSfcController(fromDPUService{
				name: operatorv1.SFCControllerName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if sfc-controller data has an unexpected object",
			inventory: New().setSfcController(fromDPUService{
				name: operatorv1.SFCControllerName,
				data: addUnexpectedKindToObjects(g, sfcControllerData),
			}),
			wantErr: true,
		},
		{
			name: "fail if sfc-controller is missing the DPUService",
			inventory: New().setSfcController(fromDPUService{
				name: operatorv1.SFCControllerName,
				data: removeKindFromObjects(g, "DPUService", sfcControllerData),
			}),
			wantErr: true,
		},
		{
			name: "fail if cni-installer is missing the DPUService",
			inventory: New().setCNIInstaller(fromDPUService{
				name: operatorv1.CNIInstallerName,
				data: removeKindFromObjects(g, "DPUService", cniInstallerData),
			}),
			wantErr: true,
		},
		// kata-containers
		{
			name: "fail if kata-containers data is nil",
			inventory: New().setKataContainers(fromDPUService{
				name: operatorv1.KataContainersName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if kata-containers data has an unexpected object",
			inventory: New().setKataContainers(fromDPUService{
				name: operatorv1.KataContainersName,
				data: addUnexpectedKindToObjects(g, kataContainersData),
			}),
			wantErr: true,
		},
		{
			name: "fail if kata-containers is missing the DPUService",
			inventory: New().setKataContainers(fromDPUService{
				name: operatorv1.KataContainersName,
				data: removeKindFromObjects(g, "DPUService", kataContainersData),
			}),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			vars := newDefaultVariables(defaults)
			pvcName := bfbVolumeName
			vars.DPFProvisioningController = DPFProvisioningVariables{
				BFBPersistentVolumeClaimName: &pvcName,
				DeploymentMode:               operatorv1.DeploymentModeHostTrusted,
			}

			err := tt.inventory.ParseAll()
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			_, err = tt.inventory.generateAllManifests(context.Background(), vars)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

//nolint:unparam
func removeKindFromObjects(g Gomega, kindToRemove string, data []byte) []byte {
	// DPUService objects which is missing one of the expected Kinds.
	objsWithMissingKind, err := utils.BytesToUnstructured(data)
	g.Expect(err).NotTo(HaveOccurred())
	for i, obj := range objsWithMissingKind {
		if obj.GetObjectKind().GroupVersionKind().Kind == kindToRemove {
			objsWithMissingKind = append(objsWithMissingKind[:i], objsWithMissingKind[i+1:]...)
		}
	}
	dataWithMissingKind, err := utils.UnstructuredToBytes(objsWithMissingKind)
	g.Expect(err).NotTo(HaveOccurred())
	return dataWithMissingKind
}

func addUnexpectedKindToObjects(g Gomega, data []byte) []byte {
	objsWithUnexpectedKind, err := utils.BytesToUnstructured(data)
	g.Expect(err).NotTo(HaveOccurred())
	objsWithUnexpectedKind[0].SetKind("FakeKind")
	dataWithUnexpectedKind, err := utils.UnstructuredToBytes(objsWithUnexpectedKind)
	g.Expect(err).NotTo(HaveOccurred())
	return dataWithUnexpectedKind
}

func TestManifests_generateAllManifests(t *testing.T) {
	g := NewWithT(t)
	defaults := release.NewDefaults()
	g.Expect(defaults.Parse()).To(Succeed())
	i := New()
	g.Expect(i.ParseAll()).NotTo(HaveOccurred())
	vars := newDefaultVariables(defaults)
	pvcName := bfbVolumeName
	vars.DPFProvisioningController = DPFProvisioningVariables{
		BFBPersistentVolumeClaimName: &pvcName,
		DeploymentMode:               operatorv1.DeploymentModeHostTrusted,
	}
	tests := []struct {
		name               string
		componentToDisable operatorv1.ComponentName
		wantErr            bool
	}{
		{
			name:               "Generate all manifests",
			componentToDisable: "",
			wantErr:            false,
		},
		{
			name:               "Disable multus manifests",
			componentToDisable: operatorv1.MultusName,
			wantErr:            false,
		},
		{
			name:               "Disable sriovDevicePlugin manifests",
			componentToDisable: operatorv1.SRIOVDevicePluginName,
			wantErr:            false,
		},
		{
			name:               "Disable flannel manifests",
			componentToDisable: operatorv1.FlannelName,
			wantErr:            false,
		},
		{
			name:               "Disable nvidia-k8s-ipam manifests",
			componentToDisable: operatorv1.NVIPAMControllerName,
			wantErr:            false,
		},
		{
			name:               "Disable DPFProvisioningController manifests",
			componentToDisable: operatorv1.ProvisioningControllerName,
			wantErr:            false,
		},
		{
			name:               "Disable DPUServiceControllerConfiguration manifests",
			componentToDisable: operatorv1.SRIOVDevicePluginName,
			wantErr:            false,
		},
		{
			name:               "Disable sfc-controller manifests",
			componentToDisable: operatorv1.SFCControllerName,
			wantErr:            false,
		},
		{
			name:               "Disable cni-installer manifests",
			componentToDisable: operatorv1.CNIInstallerName,
			wantErr:            false,
		},
		{
			name:               "Disable kata-containers manifests",
			componentToDisable: operatorv1.KataContainersName,
			wantErr:            false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars.DisableSystemComponents = map[operatorv1.ComponentName]bool{
				tt.componentToDisable: true,
			}
			got, err := i.generateAllManifests(context.Background(), vars)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateAllManifests() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			for _, obj := range got {

				// Manifests should not be generated if disabled.
				if tt.componentToDisable.String() != "" {
					g.Expect(obj.GetName()).ToNot(ContainSubstring(tt.componentToDisable.String()))
				}

				// These labels should always be present.
				g.Expect(hasLabel(obj, operatorv1.DPFComponentLabelKey)).To(BeTrue())
			}
		})
	}
}

func hasLabel(obj client.Object, label string) bool {
	if len(obj.GetLabels()) == 0 {
		return false
	}
	for l := range obj.GetLabels() {
		if l == label {
			return true
		}
	}
	return false
}

func TestLegacyOVSCNIComponent(t *testing.T) {
	g := NewWithT(t)

	inventory := New()
	g.Expect(inventory.ParseAll()).To(Succeed())

	componentNames := map[operatorv1.ComponentName]bool{}
	for _, component := range inventory.AllComponents() {
		componentNames[component.Name()] = true
	}
	g.Expect(componentNames).To(HaveKey(operatorv1.OVSCNIName))

	systemDPUServiceNames := map[operatorv1.ComponentName]bool{}
	for _, component := range inventory.SystemDPUServices() {
		systemDPUServiceNames[component.Name()] = true
	}
	g.Expect(systemDPUServiceNames).NotTo(HaveKey(operatorv1.OVSCNIName))

	objs, err := inventory.RemoveOVSCNI.GenerateManifests(context.Background(), Variables{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(objs).To(BeEmpty())
	g.Expect(ApplySetName(inventory.RemoveOVSCNI)).To(Equal("ovs-cni-applyset"))
}

// Controller-runtime tests the election mechanism itself. This only guards how DPF wires it into
// the manifests of the host cluster controllers that run with multiple replicas.
func TestLeaderElectionManifests(t *testing.T) {
	g := NewWithT(t)
	defaults := release.NewDefaults()
	g.Expect(defaults.Parse()).To(Succeed())
	i := New()
	g.Expect(i.ParseAll()).To(Succeed())
	vars := newDefaultVariables(defaults)
	pvcName := bfbVolumeName
	vars.DPFProvisioningController = DPFProvisioningVariables{
		BFBPersistentVolumeClaimName: &pvcName,
		DeploymentMode:               operatorv1.DeploymentModeHostTrusted,
	}
	// Both are disabled by default.
	vars.DisableSystemComponents[operatorv1.StaticClusterManagerName] = false
	vars.DisableSystemComponents[operatorv1.NodeSRIOVDevicePluginControllerName] = false

	// Check the generated manifests, some components set the replicas in code.
	components := []Component{
		i.DPFProvisioning,
		i.DPUService,
		i.KamajiClusterManager,
		i.StaticClusterManager,
		i.NodeSRIOVDevicePluginController,
	}

	for _, component := range components {
		t.Run(component.Name().String(), func(t *testing.T) {
			g := NewWithT(t)
			objs, err := component.GenerateManifests(context.Background(), vars)
			g.Expect(err).ToNot(HaveOccurred())

			var deployments []appsv1.Deployment
			var roles []rbacv1.Role
			var bindings []rbacv1.RoleBinding
			for _, obj := range objs {
				u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
				g.Expect(err).ToNot(HaveOccurred())
				switch obj.GetObjectKind().GroupVersionKind().Kind {
				case string(DeploymentKind):
					d := appsv1.Deployment{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(u, &d)).To(Succeed())
					deployments = append(deployments, d)
				case "Role":
					r := rbacv1.Role{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(u, &r)).To(Succeed())
					roles = append(roles, r)
				case "RoleBinding":
					b := rbacv1.RoleBinding{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(u, &b)).To(Succeed())
					bindings = append(bindings, b)
				}
			}
			g.Expect(deployments).ToNot(BeEmpty())

			for _, d := range deployments {
				g.Expect(d.Spec.Replicas).ToNot(BeNil(), "deployment %s", d.Name)
				g.Expect(*d.Spec.Replicas).To(BeNumerically(">=", 2), "deployment %s is not HA", d.Name)

				i := slices.IndexFunc(d.Spec.Template.Spec.Containers, func(c corev1.Container) bool { return c.Name == "manager" })
				g.Expect(i).To(BeNumerically(">=", 0), "deployment %s has no manager container", d.Name)
				// Some manifests pass the flag in command, others in args.
				manager := d.Spec.Template.Spec.Containers[i]
				g.Expect(slices.Concat(manager.Command, manager.Args)).To(ContainElement("--leader-elect"), "deployment %s", d.Name)

				sa := d.Spec.Template.Spec.ServiceAccountName
				g.Expect(hasLeaseAccess(sa, roles, bindings)).To(BeTrue(),
					"service account %q of deployment %s cannot manage leases", sa, d.Name)
			}
		})
	}
}

// hasLeaseAccess reports whether a RoleBinding gives the service account a Role that can get,
// create and update leases.
func hasLeaseAccess(serviceAccount string, roles []rbacv1.Role, bindings []rbacv1.RoleBinding) bool {
	for _, b := range bindings {
		bound := slices.ContainsFunc(b.Subjects, func(s rbacv1.Subject) bool {
			return s.Kind == "ServiceAccount" && s.Name == serviceAccount
		})
		if !bound || b.RoleRef.Kind != "Role" {
			continue
		}
		for _, r := range roles {
			if r.Name == b.RoleRef.Name && grantsLeases(r.Rules) {
				return true
			}
		}
	}
	return false
}

// grantsLeases reports whether one rule grants get, create and update on leases.
func grantsLeases(rules []rbacv1.PolicyRule) bool {
	return slices.ContainsFunc(rules, func(rule rbacv1.PolicyRule) bool {
		return slices.Contains(rule.APIGroups, "coordination.k8s.io") &&
			slices.Contains(rule.Resources, "leases") &&
			slices.Contains(rule.Verbs, "get") &&
			slices.Contains(rule.Verbs, "create") &&
			slices.Contains(rule.Verbs, "update")
	})
}

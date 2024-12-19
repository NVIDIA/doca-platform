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
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
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
		// ServiceFunctionChainSetObjects
		{
			name: "fail if ServiceFunctionChainSet data is nil",
			inventory: New().setServiceFunctionChainSet(fromDPUService{
				name: operatorv1.ServiceSetControllerName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if ServiceFunctionChainSet data has an unexpected object",
			inventory: New().setServiceFunctionChainSet(fromDPUService{
				name: operatorv1.ServiceSetControllerName,
				data: addUnexpectedKindToObjects(g, serviceChainSetData),
			}),
			wantErr: true,
		},
		{
			name: "fail if ServiceFunctionChainSet is missing the DPUService",
			inventory: New().setServiceFunctionChainSet(fromDPUService{
				name: operatorv1.ServiceSetControllerName,
				data: removeKindFromObjects(g, "DPUService", serviceChainSetData),
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
			inventory: New().setNvK8sIpam(fromDPUService{
				name: operatorv1.NVIPAMName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if nv-ipam data has an unexpected object",
			inventory: New().setNvK8sIpam(fromDPUService{
				name: operatorv1.NVIPAMName,
				data: addUnexpectedKindToObjects(g, nvK8sIpamData),
			}),
			wantErr: true,
		},
		{
			name: "fail if nv-ipam is missing the DPUService",
			inventory: New().setNvK8sIpam(fromDPUService{
				name: operatorv1.NVIPAMName,
				data: removeKindFromObjects(g, "DPUService", nvK8sIpamData),
			}),
			wantErr: true,
		},
		// ovs-cni
		{
			name: "fail if ovs-cni data is nil",
			inventory: New().setOvsCni(fromDPUService{
				name: operatorv1.OVSCNIName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if ovs-cni data has an unexpected object",
			inventory: New().setOvsCni(fromDPUService{
				name: operatorv1.OVSCNIName,
				data: addUnexpectedKindToObjects(g, ovsCniData),
			}),
			wantErr: true,
		},
		{
			name: "fail if ovs-cni is missing the DPUService",
			inventory: New().setOvsCni(fromDPUService{
				name: operatorv1.OVSCNIName,
				data: removeKindFromObjects(g, "DPUService", ovsCniData),
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
		// ovs-helper
		{
			name: "fail if ovs-helper data is nil",
			inventory: New().setOVSHelper(fromDPUService{
				name: operatorv1.OVSHelperName,
				data: nil,
			}),
			wantErr: true,
		},
		{
			name: "fail if ovs-helper data has an unexpected object",
			inventory: New().setOVSHelper(fromDPUService{
				name: operatorv1.OVSHelperName,
				data: addUnexpectedKindToObjects(g, OVSHelperData),
			}),
			wantErr: true,
		},
		{
			name: "fail if ovs-helper is missing the DPUService",
			inventory: New().setSfcController(fromDPUService{
				name: operatorv1.OVSHelperName,
				data: removeKindFromObjects(g, "DPUService", OVSHelperData),
			}),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			vars := newDefaultVariables(defaults)
			vars.DPFProvisioningController = DPFProvisioningVariables{
				BFBPersistentVolumeClaimName: bfbVolumeName,
			}

			err := tt.inventory.ParseAll()
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			_, err = tt.inventory.generateAllManifests(vars)
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
	vars.DPFProvisioningController = DPFProvisioningVariables{
		BFBPersistentVolumeClaimName: bfbVolumeName,
	}
	tests := []struct {
		name               string
		componentToDisable string
		expectedMissing    string
		wantErr            bool
	}{
		{
			name:               "Generate all manifests",
			componentToDisable: "",
			wantErr:            false,
			expectedMissing:    "",
		},
		{
			name:               "Disable multus manifests",
			componentToDisable: operatorv1.MultusName,
			wantErr:            false,
			expectedMissing:    operatorv1.MultusName,
		},
		{
			name:               "Disable sriovDevicePlugin manifests",
			componentToDisable: operatorv1.SRIOVDevicePluginName,
			wantErr:            false,
			expectedMissing:    operatorv1.SRIOVDevicePluginName,
		},
		{
			name:               "Disable flannel manifests",
			componentToDisable: operatorv1.FlannelName,
			wantErr:            false,
			expectedMissing:    operatorv1.FlannelName,
		},
		{
			name:               "Disable nvidia-k8s-ipam manifests",
			componentToDisable: operatorv1.NVIPAMName,
			wantErr:            false,
			expectedMissing:    operatorv1.NVIPAMName,
		},
		{
			name:               "Disable DPFProvisioningController manifests",
			componentToDisable: operatorv1.ProvisioningControllerName,
			wantErr:            false,
			expectedMissing:    operatorv1.ProvisioningControllerName,
		},
		{
			name:               "Disable DPUServiceControllerConfiguration manifests",
			componentToDisable: operatorv1.SRIOVDevicePluginName,
			wantErr:            false,
			expectedMissing:    operatorv1.SRIOVDevicePluginName,
		},
		{
			name:               "Disable ovs-cni manifests",
			componentToDisable: operatorv1.OVSCNIName,
			wantErr:            false,
			expectedMissing:    operatorv1.OVSCNIName,
		},
		{
			name:               "Disable sfc-controller manifests",
			componentToDisable: operatorv1.SFCControllerName,
			wantErr:            false,
			expectedMissing:    operatorv1.SFCControllerName,
		},
		{
			name:               "Disable ovs-helper manifests",
			componentToDisable: operatorv1.OVSHelperName,
			wantErr:            false,
			expectedMissing:    operatorv1.OVSHelperName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars.DisableSystemComponents = map[string]bool{
				tt.componentToDisable: true,
			}
			got, err := i.generateAllManifests(vars)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateAllManifests() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			for _, obj := range got {

				// Manifests should not be generated if disabled.
				if tt.expectedMissing != "" {
					g.Expect(obj.GetName()).ToNot(ContainSubstring(tt.expectedMissing))
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

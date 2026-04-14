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

package inventory

import (
	"context"
	"fmt"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace = "test-namespace"
)

func Test_sfcControllerObjects_GenerateManifests(t *testing.T) {
	g := NewWithT(t)
	serviceName := operatorv1.SFCControllerName

	tests := []struct {
		name               string
		inputYAML          string
		vars               Variables
		wantDPUService     *dpuservicev1.DPUService
		wantDPUServiceNADs []*dpuservicev1.DPUServiceNAD
	}{
		{
			name: "DPUServiceNADs are mutated with MTU",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: sfc-controller
spec:
  helmChart:
    source:
      repoURL: helmchart.com
      chart: chart
      version: v1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: test-nad-1
spec:
  resourceType: sf
  bridge: br-sfc
  serviceMTU: 1500
  ipam: false
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: test-nad-2
spec:
  resourceType: sf
  bridge: br-sfc-2
  serviceMTU: 9000
  ipam: true`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = testNamespace
				vars.Networking.HighSpeedMTU = 9000
				return vars
			}(),
			wantDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DPUService",
					APIVersion: "svc.dpu.nvidia.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName.String(),
					Namespace: testNamespace,
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: serviceName.String(),
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "oci://example.com",
							Chart:   "dpu-networking",
							Version: "v0.1.0",
						},
						Values: &runtime.RawExtension{
							Raw: []byte(`{"sfc-controller":{"controllerManager":{"manager":{"image":{"repository":"example.com/dpf-system","tag":"v0.1.0"},"secureFlowDeletionTimeout":"0s"}},"enabled":true,"openvSwitchBinDir":"/usr/bin/","openvSwitchRunDir":"/var/run/openvswitch/","openvSwitchSharedLibraryDir":"/lib"}}`),
						},
					},
				},
			},
			wantDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceNAD",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: serviceName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{
						ResourceType: "sf",
						Bridge:       "br-sfc",
						ServiceMTU:   9000,
						IPAM:         false,
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceNAD",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-2",
						Namespace: testNamespace,
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: serviceName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{
						ResourceType: "sf",
						Bridge:       "br-sfc-2",
						ServiceMTU:   9000,
						IPAM:         true,
					},
				},
			},
		},
		{
			name: "DPUService is mutated with SFC controller specific values",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: sfc-controller
spec:
  helmChart:
    source:
      repoURL: helmchart.com
      chart: chart
      version: v1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: test-nad
spec:
  resourceType: sf
  bridge: br-sfc
  serviceMTU: 1500
  ipam: false`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = testNamespace
				vars.HelmCharts[serviceName] = "oci://some-registry.com/some-chart:v0.5.0"
				vars.Networking.HighSpeedMTU = 9000
				vars.DPUOpenvSwitchSharedLib64Path = ptr.To("/lib64")
				vars.SFCController.SecureFlowDeletionTimeout = 30 * time.Second
				return vars
			}(),
			wantDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DPUService",
					APIVersion: "svc.dpu.nvidia.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName.String(),
					Namespace: testNamespace,
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: serviceName.String(),
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "oci://some-registry.com",
							Chart:   "some-chart",
							Version: "v0.5.0",
						},
						Values: &runtime.RawExtension{
							Raw: []byte(`{"sfc-controller":{"controllerManager":{"manager":{"image":{"repository":"example.com/dpf-system","tag":"v0.1.0"},"secureFlowDeletionTimeout":"30s"}},"enabled":true,"openvSwitchBinDir":"/usr/bin/","openvSwitchRunDir":"/var/run/openvswitch/","openvSwitchSharedLibrary64Dir":"/lib64","openvSwitchSharedLibraryDir":"/lib"}}`),
						},
					},
				},
			},
			wantDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceNAD",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad",
						Namespace: testNamespace,
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: serviceName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{
						ResourceType: "sf",
						Bridge:       "br-sfc",
						ServiceMTU:   9000,
						IPAM:         false,
					},
				},
			},
		},
		{
			name: "component is disabled",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: sfc-controller
spec: {}
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: test-nad
spec:
  resourceType: sf
  bridge: br-sfc
  serviceMTU: 1500
  ipam: false`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.DisableSystemComponents[serviceName] = true
				return vars
			}(),
			wantDPUService:     nil,
			wantDPUServiceNADs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create sfcControllerObjects using the test YAML data
			sfc := newSFCControllerObjects([]byte(tt.inputYAML))

			// Parse the YAML data to populate the internal structure
			err := sfc.Parse()
			g.Expect(err).NotTo(HaveOccurred())

			got, err := sfc.GenerateManifests(context.Background(), tt.vars)
			g.Expect(err).NotTo(HaveOccurred())

			// Find DPUService in results
			var gotDPUService *dpuservicev1.DPUService
			var gotDPUServiceNADs []*dpuservicev1.DPUServiceNAD

			for _, obj := range got {
				if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
					t.Logf("Processing unstructured object: %s", unstructuredObj.GetKind())
					if unstructuredObj.GetKind() == DPUServiceKind.String() {
						gotDPUService = &dpuservicev1.DPUService{}
						err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), gotDPUService)
						g.Expect(err).ToNot(HaveOccurred())
						t.Logf("Found DPUService: %s", gotDPUService.Name)
					} else if unstructuredObj.GetKind() == "DPUServiceNAD" {
						gotNAD := &dpuservicev1.DPUServiceNAD{}
						err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), gotNAD)
						g.Expect(err).ToNot(HaveOccurred())
						gotDPUServiceNADs = append(gotDPUServiceNADs, gotNAD)
						t.Logf("Found DPUServiceNAD: %s", gotNAD.Name)
					}
				}
			}

			// Verify DPUService
			g.Expect(gotDPUService).To(BeComparableTo(tt.wantDPUService, cmpopts.IgnoreMapEntries(func(k, _ string) bool {
				return k == applysetPartOfLabel
			})))

			// Verify DPUServiceNADs
			g.Expect(gotDPUServiceNADs).To(HaveLen(len(tt.wantDPUServiceNADs)))
			for i, wantNAD := range tt.wantDPUServiceNADs {
				g.Expect(gotDPUServiceNADs[i]).To(BeComparableTo(wantNAD, cmpopts.IgnoreMapEntries(func(k, _ string) bool {
					return k == applysetPartOfLabel
				})))
			}
		})
	}
}

func Test_sfcControllerObjects_ReadyCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())

	tests := []struct {
		name string
		// Objects that exist in the cluster (via test client)
		clusterDPUService     *dpuservicev1.DPUService
		clusterDPUServiceNADs []*dpuservicev1.DPUServiceNAD
		// Objects that are part of the internal sfcControllerObjects structure
		internalSFCNADs    []*unstructured.Unstructured
		upgradeFromVersion *string
		wantErr            bool
		expectedErrorMsg   string
	}{
		{
			name: "SFC Controller is ready when both DPUService and all DPUServiceNADs are ready",
			// Cluster objects - these exist in the Kubernetes cluster
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-2",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - these are part of the sfcControllerObjects
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-2",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "SFC Controller is not ready when DPUService is not ready",
			// Cluster objects - DPUService exists but is not ready
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "False",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "DPUService test-namespace/sfc-controller is not ready",
		},
		{
			name: "SFC Controller is not ready when one DPUServiceNAD is not ready",
			// Cluster objects - DPUService is ready, but one NAD is not
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-2",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "False",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects both NADs to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-2",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "SFC Controller related DPUServiceNAD test-namespace/test-nad-2 is not ready",
		},
		{
			name: "SFC Controller is not ready when DPUService has no Ready condition",
			// Cluster objects - DPUService exists but has no Ready condition
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "UnrelatedCondition",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "DPUService test-namespace/sfc-controller is not ready",
		},
		{
			name: "SFC Controller is not ready when DPUServiceNAD has no Ready condition",
			// Cluster objects - DPUService is ready, but NAD has no Ready condition
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "UnrelatedCondition",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "SFC Controller related DPUServiceNAD test-namespace/test-nad-1 is not ready",
		},
		{
			name: "SFC Controller is not ready when DPUServiceNAD is not found",
			// Cluster objects - DPUService is ready, but NAD doesn't exist in cluster
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				// No DPUServiceNADs exist in the cluster
			},
			// Internal structure objects - SFC Controller expects this NAD to exist in cluster
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "failed to get DPUServiceNAD test-namespace/test-nad-1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clusterObjects []client.Object
			if tt.clusterDPUService != nil {
				clusterObjects = append(clusterObjects, tt.clusterDPUService)
			}
			for _, nad := range tt.clusterDPUServiceNADs {
				clusterObjects = append(clusterObjects, nad)
			}
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(clusterObjects...).Build()

			sfc := &sfcControllerObjects{
				fromDPUService: fromDPUService{
					name: "sfc-controller",
				},
				dpuServiceNADs: tt.internalSFCNADs,
			}

			var err error
			if tt.upgradeFromVersion != nil {
				config := &operatorv1.DPFOperatorConfig{}
				config.SetNamespace(testNamespace)
				config.Status.Version = tt.upgradeFromVersion
				err = sfc.IsReadyForUpgrade(context.Background(), testClient, config)
			} else {
				err = sfc.IsReady(context.Background(), testClient, testNamespace)
			}

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErrorMsg))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func Test_sfcControllerObjects_ReadyAndVersionUpdatedCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())

	tests := []struct {
		name string
		// Objects that exist in the cluster (via test client)
		clusterDPUService     *dpuservicev1.DPUService
		clusterDPUServiceNADs []*dpuservicev1.DPUServiceNAD
		// Objects that are part of the internal sfcControllerObjects structure
		internalSFCNADs  []*unstructured.Unstructured
		wantErr          bool
		expectedErrorMsg string
	}{
		{
			name: "SFC Controller is ready and version updated when both DPUService and all DPUServiceNADs are ready with correct versions",
			// Cluster objects - these exist in the Kubernetes cluster with correct versions
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
					Labels: map[string]string{
						release.DPFVersionLabelKey: release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							release.DPFVersionLabelKey: release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-2",
						Namespace: testNamespace,
						Labels: map[string]string{
							release.DPFVersionLabelKey: release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - these are part of the sfcControllerObjects
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-2",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "SFC Controller is not ready when DPUService has incorrect version",
			// Cluster objects - DPUService exists but has wrong version
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
					Labels: map[string]string{
						release.DPFVersionLabelKey: "v0.0.9", // Wrong version
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							release.DPFVersionLabelKey: release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: fmt.Sprintf("DPUService test-namespace/sfc-controller has version v0.0.9, want %s", release.DPFVersion()),
		},
		{
			name: "SFC Controller is not ready when DPUServiceNAD has incorrect version",
			// Cluster objects - DPUService is ready with correct version, but NAD has wrong version
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
					Labels: map[string]string{
						release.DPFVersionLabelKey: release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							release.DPFVersionLabelKey: "v0.0.9", // Wrong version
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: fmt.Sprintf("DPUServiceNAD test-namespace/test-nad-1 has version v0.0.9, want %s", release.DPFVersion()),
		},
		{
			name: "SFC Controller is ready when objects have no version labels (empty version is allowed)",
			// Cluster objects - these exist in the Kubernetes cluster with no version labels
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
					// No version label - this is allowed
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						// No version label - this is allowed
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - these are part of the sfcControllerObjects
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "SFC Controller is not ready when DPUService is not ready (version check is secondary)",
			// Cluster objects - DPUService exists but is not ready (version is correct but readiness fails first)
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
					Labels: map[string]string{
						release.DPFVersionLabelKey: release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "False",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							release.DPFVersionLabelKey: release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "DPUService test-namespace/sfc-controller is not ready",
		},
		{
			name: "SFC Controller is not ready when DPUServiceNAD is not ready (version check is secondary)",
			// Cluster objects - DPUService is ready with correct version, but NAD is not ready
			clusterDPUService: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-controller",
					Namespace: testNamespace,
					Labels: map[string]string{
						release.DPFVersionLabelKey: release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{},
				Status: dpuservicev1.DPUServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: "True",
						},
					},
				},
			},
			clusterDPUServiceNADs: []*dpuservicev1.DPUServiceNAD{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceNAD"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-nad-1",
						Namespace: testNamespace,
						Labels: map[string]string{
							release.DPFVersionLabelKey: release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceNADSpec{},
					Status: dpuservicev1.DPUServiceNADStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "False",
							},
						},
					},
				},
			},
			// Internal structure objects - SFC Controller expects this NAD to exist
			internalSFCNADs: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-nad-1",
							"namespace": "internal-namespace",
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "SFC Controller related DPUServiceNAD test-namespace/test-nad-1 is not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clusterObjects []client.Object
			if tt.clusterDPUService != nil {
				clusterObjects = append(clusterObjects, tt.clusterDPUService)
			}
			for _, nad := range tt.clusterDPUServiceNADs {
				clusterObjects = append(clusterObjects, nad)
			}
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(clusterObjects...).Build()

			sfc := &sfcControllerObjects{
				fromDPUService: fromDPUService{
					name: "sfc-controller",
				},
				dpuServiceNADs: tt.internalSFCNADs,
			}

			err := sfc.IsReady(context.Background(), testClient, testNamespace)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErrorMsg))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

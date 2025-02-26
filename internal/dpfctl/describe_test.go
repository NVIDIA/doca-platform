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

package dpfctl

import (
	"bytes"
	"context"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/olekukonko/tablewriter"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_dpfctlTreeDiscovery(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	type objectsWithConditions struct {
		object     client.Object
		conditions []metav1.Condition
		argoStatus map[string]interface{}
	}

	tests := []struct {
		name           string
		objectsTree    []objectsWithConditions
		opts           ObjectTreeOptions
		expectedPrefix []string
	}{
		{
			name: "Add DPFOperatorConfig",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  True  Success",
			},
		},
		{
			name: "Add DPFOperatorConfig with false condition",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getFalseCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  False  SomethingWentWrong",
			},
		},
		{
			name: "Add DPUCluster",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUCluster(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  True  Success",
				"└─DPUClusters",
				"  └─DPUCluster/test     default  True  Success",
			},
		},
		{
			name: "Add DPUSet",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  True  Success",
				"└─DPUSets",
				"  └─DPUSet/test         default",
			},
		},
		{
			name: "Add DPU",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPU(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu    default  True  Success",
			},
		},
		{
			name: "Add DPUSet with DPU and DPU w/o DPUSet",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
				{object: defaultDPU(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSet(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  True  Success",
				"├─DPUSets",
				"│ └─DPUSet/test         default",
				"│   └─DPU/test          default  True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu    default  True  Success",
			},
		},
		{
			name: "Add DPUService without showing Applications",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUService(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  True  Success",
				"└─DPUServices",
				"  └─DPUService/test     default  True  Success",
			},
		},
		{
			name: "Add DPUDeployment with sub-resources",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceChainFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUSetFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSetsFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterfaceFromDPUDeployment(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default  True  Success",
				"└─DPUDeployments",
				"  └─DPUDeployment/test                               default  True  Success",
				"    ├─DPUServiceChains",
				"    │ └─DPUServiceChain/test-from-dpudeployment      default  True  Success",
				"    ├─DPUServiceInterfaces",
				"    │ └─DPUServiceInterface/test-from-dpudeployment  default  True  Success",
				"    ├─DPUServices",
				"    │ └─DPUService/test-from-dpudeployment           default  True  Success",
				"    └─DPUSets",
				"      └─DPUSet/test-from-dpudeployment               default",
				"        └─DPU/test-from-dpudeployment                default  True  Success",
			},
		},
		{
			name: "Add DPUDeployment with sub-resources and standalone resources",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceChainFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUSetFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSetsFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterfaceFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUService(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
				{object: defaultDPU(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSet(), conditions: getTrueCondition()},
				{object: defaultDPUServiceChain(), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterface(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default  True  Success",
				"├─DPUDeployments",
				"│ └─DPUDeployment/test                               default  True  Success",
				"│   ├─DPUServiceChain",
				"│   │ └─DPUServiceChain/test-from-dpudeployment      default  True  Success",
				"│   ├─DPUServiceInterfaces",
				"│   │ └─DPUServiceInterface/test-from-dpudeployment  default  True  Success",
				"│   ├─DPUServices",
				"│   │ └─DPUService/test-from-dpudeployment           default  True  Success",
				"│   └─DPUSets",
				"│     └─DPUSet/test-from-dpudeployment               default",
				"│       └─DPU/test-from-dpudeployment                default  True  Success",
				"├─DPUServiceChains",
				"│ └─DPUServiceChain/test                             default  True  Success",
				"├─DPUServiceInterfaces",
				"│ └─DPUServiceInterface/test                         default  True  Success",
				"├─DPUServices",
				"│ └─DPUService/test                                  default  True  Success",
				"├─DPUSets",
				"│ └─DPUSet/test                                      default",
				"│   └─DPU/test                                       default  True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu                                 default  True  Success",
			},
		},
		{
			name: "Add DPUService with very long random conditions messages",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getRandomConditionsWithVeryLongMessages()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: "all",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test              default  True   Success",
				"│           ├─RandomReady                    False  SomethingWentWrong",
				"│           └─RandomReconciled               True   Success",
				"└─DPUServices",
				"  └─DPUService/test                 default  True   Success",
				"                │",
				"                │",
				"                ├─RandomReady                False  SomethingWentWrong",
				"                │",
				"                │",
				"                └─RandomReconciled           True   Success",
			},
		},
		{
			name: "Add DPUService with very long ready condition message",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getReadyConditionWithVeryLongMessages()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: "all",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test          default  True   Success",
				"│           ├─RandomReady                False  SomethingWentWrong",
				"│           └─RandomReconciled           True   Success",
				"└─DPUServices",
				"  └─DPUService/test             default  True   Success",
				"                                                                        feature of the table.",
				"                                                                        test the wrapping feature of the table.",
			},
		},
		{
			name: "Add DPUService with ArgoCD Application and show conditions",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getRandomConditionsWithVeryLongMessages()},
				{object: defaultArgoCDApplication(), argoStatus: getRandomArgoCDApplicationConditions()}},
			opts: ObjectTreeOptions{
				ShowOtherConditions: "all",
				ExpandResources:     "dpuservice",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test              default  True    Success",
				"│           ├─RandomReady                    False   SomethingWentWrong",
				"│           └─RandomReconciled               True    Success",
				"└─DPUServices",
				"  └─DPUService/test                 default  True    Success",
				"    │           │",
				"    │           │",
				"    │           ├─RandomReady                False   SomethingWentWrong",
				"    │           │",
				"    │           │",
				"    │           └─RandomReconciled           True    Success",
				"    └─Application/test              default  True    Success",
				"                  ├─DaemonSet/bar            Synced  Progressing",
				"                  │",
				"                  │",
				"                  └─Deployment/foo           Synced  Progressing",
				"                                                                             feature of the table.",
				"                                                                             test the wrapping feature of the table.",
			},
		},
		{
			name: "Add only DPUService and DPUServiceIPAM with conditions",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUSet(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPU(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUFromDPUSet(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceChain(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceInterface(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceIPAM(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceCredentialRequest(), conditions: getRandomConditionsWithReadyTrueCondition()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: "all",
				ShowResources:       "DPUService,DPUServiceIPAM",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test              default  True   Success",
				"│           ├─RandomReady                    False  SomethingWentWrong",
				"│           └─RandomReconciled               True   Success",
				"├─DPUServiceIPAM",
				"│ └─DPUServiceIPAM/test             default  True   Success",
				"│               ├─RandomReady                False  SomethingWentWrong",
				"│               └─RandomReconciled           True   Success",
				"└─DPUServices",
				"  └─DPUService/test                 default  True   Success",
				"                ├─RandomReady                False  SomethingWentWrong",
				"                └─RandomReconciled           True   Success",
			},
		},
		{
			name: "Add all resources with Argo Applications",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUCluster(), conditions: getTrueCondition()},
				{object: defaultDPUService(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
				{object: defaultDPU(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSet(), conditions: getTrueCondition()},
				{object: defaultDPUServiceChain(), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterface(), conditions: getTrueCondition()},
				{object: defaultDPUServiceIPAM(), conditions: getTrueCondition()},
				{object: defaultDPUServiceCredentialRequest(), conditions: getTrueCondition()},
				{object: defaultDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceChainFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUSetFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSetsFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterfaceFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultArgoCDApplication(), argoStatus: getRandomArgoCDApplicationConditions()},
			},
			opts: ObjectTreeOptions{
				ExpandResources: "dpuservice",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default  True  Success",
				"├─DPUClusters",
				"│ └─DPUCluster/test                                  default  True  Success",
				"├─DPUDeployments",
				"│ └─DPUDeployment/test                               default  True  Success",
				"│   ├─DPUServiceChains",
				"│   │ └─DPUServiceChain/test-from-dpudeployment      default  True  Success",
				"│   ├─DPUServiceInterfaces",
				"│   │ └─DPUServiceInterface/test-from-dpudeployment  default  True  Success",
				"│   ├─DPUServices",
				"│   │ └─DPUService/test-from-dpudeployment           default  True  Success",
				"│   └─DPUSets",
				"│     └─DPUSet/test-from-dpudeployment               default",
				"│       └─DPU/test-from-dpudeployment                default  True  Success",
				"├─DPUServiceChains",
				"│ └─DPUServiceChain/test                             default  True  Success",
				"├─DPUServiceCredentialRequests",
				"│ └─DPUServiceCredentialRequest/test                 default  True  Success",
				"├─DPUServiceIPAMs",
				"│ └─DPUServiceIPAM/test                              default  True  Success",
				"├─DPUServiceInterfaces",
				"│ └─DPUServiceInterface/test                         default  True  Success",
				"├─DPUServices",
				"│ └─DPUService/test                                  default  True  Success",
				"│   └─Application/test                               default  True  Success",
				"├─DPUSets",
				"│ └─DPUSet/test                                      default",
				"│   └─DPU/test                                       default  True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu                                 default  True  Success",
			},
		},
		{
			name: "Add all resources with conditions and Argo Applications",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUCluster(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUSet(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPU(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUFromDPUSet(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceChain(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceInterface(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceIPAM(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceCredentialRequest(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceChainFromDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceFromDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUSetFromDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUFromDPUSetsFromDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceInterfaceFromDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultArgoCDApplication(), argoStatus: getRandomArgoCDApplicationConditions()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: "all",
				ExpandResources:     "dpuservice",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default  True    Success",
				"│           ├─RandomReady                                     False   SomethingWentWrong",
				"│           └─RandomReconciled                                True    Success",
				"├─DPUClusters",
				"│ └─DPUCluster/test                                  default  True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUDeployments",
				"│ └─DPUDeployment/test                               default  True    Success",
				"│   │           ├─RandomReady                                 False   SomethingWentWrong",
				"│   │           └─RandomReconciled                            True    Success",
				"│   ├─DPUServiceChains",
				"│   │ └─DPUServiceChain/test-from-dpudeployment      default  True    Success",
				"│   │               ├─RandomReady                             False   SomethingWentWrong",
				"│   │               └─RandomReconciled                        True    Success",
				"│   ├─DPUServiceInterfaces",
				"│   │ └─DPUServiceInterface/test-from-dpudeployment  default  True    Success",
				"│   │               ├─RandomReady                             False   SomethingWentWrong",
				"│   │               └─RandomReconciled                        True    Success",
				"│   ├─DPUServices",
				"│   │ └─DPUService/test-from-dpudeployment           default  True    Success",
				"│   │               ├─RandomReady                             False   SomethingWentWrong",
				"│   │               └─RandomReconciled                        True    Success",
				"│   └─DPUSets",
				"│     └─DPUSet/test-from-dpudeployment               default",
				"│       └─DPU/test-from-dpudeployment                default  True    Success",
				"│                     ├─RandomReady                           False   SomethingWentWrong",
				"│                     └─RandomReconciled                      True    Success",
				"├─DPUServiceChains",
				"│ └─DPUServiceChain/test                             default  True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServiceCredentialRequests",
				"│ └─DPUServiceCredentialRequest/test                 default  True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServiceIPAMs",
				"│ └─DPUServiceIPAM/test                              default  True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServiceInterfaces",
				"│ └─DPUServiceInterface/test                         default  True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServices",
				"│ └─DPUService/test                                  default  True    Success",
				"│   │           ├─RandomReady                                 False   SomethingWentWrong",
				"│   │           └─RandomReconciled                            True    Success",
				"│   └─Application/test                               default  True    Success",
				"│                 ├─DaemonSet/bar                             Synced  Progressing",
				"│                 │",
				"│                 │",
				"│                 └─Deployment/foo                            Synced  Progressing",
				"│",
				"│",
				"├─DPUSets",
				"│ └─DPUSet/test                                      default",
				"│   └─DPU/test                                       default  True    Success",
				"│                 ├─RandomReady                               False   SomethingWentWrong",
				"│                 └─RandomReconciled                          True    Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu                                 default  True    Success",
				"                ├─RandomReady                                 False   SomethingWentWrong",
				"                └─RandomReconciled                            True    Success",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, ot := range tt.objectsTree {
				g.Expect(testClient.Create(ctx, ot.object)).To(Succeed())

				// We have to convert the object to unstructured to set the status conditions.
				// We don't have access to the status field of the client.Object directly.
				u := unstructured.Unstructured{}
				g.Expect(scheme.Scheme.Convert(ot.object, &u, nil)).To(Succeed())

				// Normal status conditions can be set with the conditions package and updated with the client.
				if ot.conditions != nil {
					unstructuredGetSet(&u).SetConditions(ot.conditions)
					g.Expect(testClient.Status().Update(ctx, &u)).To(Succeed())
				}

				// ArgoCD does not have the subresource status.conditions. We can update the status field directly.
				if ot.argoStatus != nil {
					g.Expect(unstructured.SetNestedMap(u.Object, ot.argoStatus, "status")).To(Succeed())
					g.Expect(testClient.Update(ctx, &u)).To(Succeed())
				}
			}

			td, err := Discover(context.Background(), testClient, tt.opts, "all")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(td).ToNot(BeNil())

			// Creates the output table
			var output bytes.Buffer
			tbl := tablewriter.NewWriter(&output)

			formatTableTree(tbl)

			addObjectRow("", tbl, td, td.GetRoot())
			tbl.Render()
			g.Expect(output.String()).Should(MatchTable(tt.expectedPrefix))

			// Cleanup resources for next run
			for _, ot := range tt.objectsTree {
				g.Expect(testClient.Delete(ctx, ot.object)).To(Succeed())
			}
		})
	}
}

func defaultDPFOperatorConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "oof",
			},
		},
	}
}

func defaultDPUCluster() *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: provisioningv1.DPUClusterSpec{
			Type: "static",
		},
	}
}

func defaultDPUSet() *provisioningv1.DPUSet {
	return &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
}

func defaultDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "orphaned-dpu", Namespace: "default"},
	}
}

func defaultDPUFromDPUSet() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				util.DPUSetNameLabel:      "test",
				util.DPUSetNamespaceLabel: "default",
			},
		},
	}
}

func defaultDPUService() *dpuservicev1.DPUService {
	return &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: dpuservicev1.DPUServiceSpec{
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					RepoURL: "oci://foobar",
					Version: "1.0.0",
				},
			},
		},
	}
}

func defaultDPUServiceChain() *dpuservicev1.DPUServiceChain {
	sc := &dpuservicev1.DPUServiceChain{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	sc.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"foo": "bar"},
					},
				},
			},
		},
	}
	return sc
}

func defaultDPUServiceInterface() *dpuservicev1.DPUServiceInterface {
	si := &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	si.Spec.Template.Spec.Template.Spec.InterfaceType = "vf"
	si.Spec.Template.Spec.Template.Spec.VF = &dpuservicev1.VF{
		VFID:               1,
		PFID:               1,
		ParentInterfaceRef: "eth0",
	}
	return si
}

func defaultDPUServiceIPAM() *dpuservicev1.DPUServiceIPAM {
	return &dpuservicev1.DPUServiceIPAM{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
}

func defaultDPUServiceCredentialRequest() *dpuservicev1.DPUServiceCredentialRequest {
	return &dpuservicev1.DPUServiceCredentialRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: dpuservicev1.DPUServiceCredentialRequestSpec{
			Type: "kubeconfig",
		},
	}
}

func defaultDPUDeployment() *dpuservicev1.DPUDeployment {
	return &dpuservicev1.DPUDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: dpuservicev1.DPUDeploymentSpec{
			ServiceChains: []dpuservicev1.DPUDeploymentSwitch{
				{
					Ports: []dpuservicev1.DPUDeploymentPort{
						{
							Service: &dpuservicev1.DPUDeploymentService{},
						},
					},
				},
			},
			Services: map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
				"test": {
					ServiceTemplate: "test",
				},
			},
		},
	}
}

func defaultDPUServiceChainFromDPUDeployment() *dpuservicev1.DPUServiceChain {
	sc := &dpuservicev1.DPUServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-from-dpudeployment",
			Namespace: "default",
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: "default_test",
			},
		},
	}
	sc.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"foo": "bar"},
					},
				},
			},
		},
	}
	return sc
}

func defaultDPUServiceInterfaceFromDPUDeployment() *dpuservicev1.DPUServiceInterface {
	si := &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-from-dpudeployment",
			Namespace: "default",
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: "default_test",
			},
		},
	}
	si.Spec.Template.Spec.Template.Spec.InterfaceType = "vf"
	si.Spec.Template.Spec.Template.Spec.VF = &dpuservicev1.VF{
		VFID:               1,
		PFID:               1,
		ParentInterfaceRef: "eth0",
	}
	return si
}

func defaultDPUServiceFromDPUDeployment() *dpuservicev1.DPUService {
	return &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-from-dpudeployment",
			Namespace: "default",
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: "default_test",
			},
		},
		Spec: dpuservicev1.DPUServiceSpec{
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					RepoURL: "oci://foobar",
					Version: "1.0.0",
				},
			},
		},
	}
}

func defaultDPUSetFromDPUDeployment() *provisioningv1.DPUSet {
	return &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-from-dpudeployment",
			Namespace: "default",
			Labels: map[string]string{
				dpuservicev1.ParentDPUDeploymentNameLabel: "default_test",
			},
		},
	}
}

func defaultDPUFromDPUSetsFromDPUDeployment() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-from-dpudeployment",
			Namespace: "default",
			Labels: map[string]string{
				util.DPUSetNameLabel:      "test-from-dpudeployment",
				util.DPUSetNamespaceLabel: "default",
			},
		},
	}
}

func defaultArgoCDApplication() *argov1.Application {
	return &argov1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				dpuservicev1.DPUServiceNameLabelKey:      "test",
				dpuservicev1.DPUServiceNamespaceLabelKey: "default",
			},
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Application",
		},
	}
}

func getTrueCondition() []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(conditions.TypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             "Success",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
	}
}

func getFalseCondition() []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(conditions.TypeReady),
			Status:             metav1.ConditionFalse,
			Reason:             "SomethingWentWrong",
			Message:            "Failed",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
	}
}

func getRandomConditionsWithReadyTrueCondition() []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(conditions.TypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             "Success",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Type:               "RandomReady",
			Status:             metav1.ConditionFalse,
			Reason:             "SomethingWentWrong",
			Message:            "Failed",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Type:               "RandomReconciled",
			Status:             metav1.ConditionTrue,
			Reason:             "Success",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
	}
}

const veryLongTestMessage = "This is a very long message that should be wrapped around multiple lines to test the wrapping feature of the table. This is a very long message that should be wrapped around multiple lines to test the wrapping feature of the table."

func getRandomConditionsWithVeryLongMessages() []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(conditions.TypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             "Success",
			Message:            veryLongTestMessage,
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Type:               "RandomReady",
			Status:             metav1.ConditionFalse,
			Reason:             "SomethingWentWrong",
			Message:            veryLongTestMessage,
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
		{
			Type:               "RandomReconciled",
			Status:             metav1.ConditionTrue,
			Reason:             "Success",
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
	}
}

func getReadyConditionWithVeryLongMessages() []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               string(conditions.TypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             "Success",
			Message:            veryLongTestMessage,
			LastTransitionTime: metav1.Time{Time: time.Now()},
		},
	}
}

func getRandomArgoCDApplicationConditions() map[string]interface{} {
	return map[string]interface{}{
		"health": map[string]interface{}{
			"status": "Healthy",
		},
		"reconciledAt": time.Now().Format(time.RFC3339),
		"resources": []interface{}{
			map[string]interface{}{
				"kind":   "DaemonSet",
				"status": "Synced",
				"health": map[string]interface{}{
					"status":  "Progressing",
					"message": veryLongTestMessage,
				},
				"name": "bar",
			},
			map[string]interface{}{
				"kind":   "Deployment",
				"status": "Synced",
				"health": map[string]interface{}{
					"status":  "Progressing",
					"message": veryLongTestMessage,
				},
				"name": "foo",
			},
		},
	}
}

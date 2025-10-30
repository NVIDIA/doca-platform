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
	"fmt"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"
	argov1 "github.com/nvidia/doca-platform/third_party/api/argocd/api/application/v1alpha1"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/api/kamaji/api/v1alpha1"

	"github.com/olekukonko/tablewriter"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_dpfctlTreeDiscovery(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	setZeroConditionAge = true

	type objectsWithConditions struct {
		object       client.Object
		conditions   []metav1.Condition
		argoStatus   map[string]interface{}
		customStatus map[string]interface{}
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
				"DPFOperatorConfig/test  default  Ready: True  Success",
			},
		},
		{
			name: "Add DPFOperatorConfig with false condition",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getFalseCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default",
				"            └─Ready              False  SomethingWentWrong",
			},
		},
		{
			name: "Add DPUCluster",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUCluster(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  Ready: True  Success",
				"└─DPUClusters",
				"  └─DPUCluster/test     default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUSet",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
				{object: defaultBFBWithVersion(), customStatus: getBFBStatus()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  Ready: True  Success",
				"└─DPUSets",
				"  └─DPUSet/test         default",
				"    └─BFB/test          default  Ready: True  Ready    0s  File: bf-bundle-2.9.1-50.bfb, DOCA: 2.9.1",
			},
		},
		{
			name: "Add DPU",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPU(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  Ready: True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu    default  Ready: True  Success",
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
				"DPFOperatorConfig/test  default  Ready: True  Success",
				"├─DPUSets",
				"│ └─DPUSet/test         default  Ready: True  Success",
				"│   └─DPUs",
				"│     └─DPU/test        default  Ready: True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu    default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUSet with DPUNode and DPUDevice",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
				{object: dpuWithNodeReference(), conditions: getTrueCondition()},
				{object: defaultDPUNode(), conditions: getTrueCondition()},
				{object: defaultDPUDevice(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test             default  Ready: True  Success",
				"└─DPUSets",
				"  └─DPUSet/test                    default",
				"    ├─DPUNodes",
				"    │ └─DPUNode/test-node          default  Ready: True  Success",
				"    │   └─DPUDevices",
				"    │     └─DPUDevice/test-device  default  Ready: True  Success",
				"    └─DPUs",
				"      └─DPU/test-dpu-with-node     default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUSet with DPUNode, DPUDevice and DPUNodeMaintenance",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUSet(), conditions: getTrueCondition()},
				{object: dpuWithNodeReference(), conditions: getTrueCondition()},
				{object: defaultDPUNode(), conditions: getTrueCondition()},
				{object: defaultDPUDevice(), conditions: getTrueCondition()},
				{object: defaultDPUNodeMaintenance(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                                                                  default  Ready: True  Success",
				"└─DPUSets",
				"  └─DPUSet/test                                                                         default",
				"    ├─DPUNodes",
				"    │ └─DPUNode/test-node                                                               default  Ready: True  Success",
				"    │   ├─DPUDevices",
				"    │   │ └─DPUDevice/test-device                                                       default  Ready: True  Success",
				"    │   └─DPUNodeMaintenance/test-maintenance                                           default",
				"    │                 ├─Ready                                                                    True         Success",
				"    │                 ├─Requestor/dpf-operator-system/ovn-kubernetes/ovn-control-plane           False        MaintenanceInProgress  0s  Maintenance requested by DPUService dpf-operator-system/ovn-control-plane (service: ovn-kubernetes)",
				"    │                 └─Requestor/DPU/worker1-mt2413xz0awv                                       False        MaintenanceInProgress  0s  Maintenance requested by DPU worker1-mt2413xz0awv",
				"    └─DPUs",
				"      └─DPU/test-dpu-with-node                                                          default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUService without showing Applications",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUService(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  Ready: True  Success",
				"└─DPUServices",
				"  └─DPUService/test     default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUDeployment with sub-resources",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceChainFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceTemplate(), conditions: getTrueCondition()},
				{object: defaultDPUSetFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUFromDPUSetsFromDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterfaceFromDPUDeployment(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default  Ready: True  Success",
				"└─DPUDeployments",
				"  └─DPUDeployment/test                               default  Ready: True  Success",
				"    ├─DPUServiceChains",
				"    │ └─DPUServiceChain/test-from-dpudeployment      default  Ready: True  Success",
				"    ├─DPUServiceInterfaces",
				"    │ └─DPUServiceInterface/test-from-dpudeployment  default  Ready: True  Success",
				"    ├─DPUSets",
				"    │ └─DPUSet/test-from-dpudeployment               default  Ready: True  Success",
				"    │   └─DPUs",
				"    │     └─DPU/test-from-dpudeployment              default  Ready: True  Success",
				"    └─Services",
				"      ├─DPUServiceTemplates",
				"      │ └─DPUServiceTemplate/test                    default  Ready: True  Success",
				"      └─DPUServices",
				"        └─DPUService/test-from-dpudeployment         default  Ready: True  Success",
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
				{object: defaultDPUServiceTemplate(), conditions: getTrueCondition()},
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
				"DPFOperatorConfig/test                               default  Ready: True  Success",
				"├─DPUDeployments",
				"│ └─DPUDeployment/test                               default  Ready: True  Success",
				"│   ├─DPUServiceChains",
				"│   │ └─DPUServiceChain/test-from-dpudeployment      default  Ready: True  Success",
				"│   ├─DPUServiceInterfaces",
				"│   │ └─DPUServiceInterface/test-from-dpudeployment  default  Ready: True  Success",
				"│   ├─DPUSets",
				"│   │ └─DPUSet/test-from-dpudeployment               default  Ready: True  Success",
				"│   │   └─DPUs",
				"│   │     └─DPU/test-from-dpudeployment              default  Ready: True  Success",
				"│   └─Services",
				"│     ├─DPUServiceTemplates",
				"│     │ └─DPUServiceTemplate/test                    default  Ready: True  Success",
				"│     └─DPUServices",
				"│       └─DPUService/test-from-dpudeployment         default  Ready: True  Success",
				"├─DPUServiceChains",
				"│ └─DPUServiceChain/test                             default  Ready: True  Success",
				"├─DPUServiceInterfaces",
				"│ └─DPUServiceInterface/test                         default  Ready: True  Success",
				"├─DPUServices",
				"│ └─DPUService/test                                  default  Ready: True  Success",
				"├─DPUSets",
				"│ └─DPUSet/test                                      default  Ready: True  Success",
				"│   └─DPUs",
				"│     └─DPU/test                                     default  Ready: True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu                                 default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUService with very long random conditions messages",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getRandomConditionsWithVeryLongMessages()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: all,
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test              default",
				"│           ├─Ready                          True   Success",
				"│           ├─RandomReady                    False  SomethingWentWrong",
				"│           └─RandomReconciled               True   Success",
				"└─DPUServices",
				"  └─DPUService/test                 default",
				"                ├─Ready                      True   Success",
				"                │                                                           feature of the table.",
				"                │                                                           test the wrapping feature of the table.",
				"                ├─RandomReady                False  SomethingWentWrong",
				"                │                                                           feature of the table.",
				"                │                                                           test the wrapping feature of the table.",
				"                └─RandomReconciled           True   Success",
			},
		},
		{
			name: "Add DPUService with very long ready condition message",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getReadyConditionWithVeryLongMessage()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: all,
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test          default",
				"│           ├─Ready                      True   Success",
				"│           ├─RandomReady                False  SomethingWentWrong",
				"│           └─RandomReconciled           True   Success",
				"└─DPUServices",
				"  └─DPUService/test             default",
				"                └─Ready                  True   Success",
				"                                                                        feature of the table.",
				"                                                                        test the wrapping feature of the table.",
			},
		},
		{
			name: "Add multiple DPUServices with very long ready condition message",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUService(), conditions: getReadyConditionWithVeryLongMessage()},
				{object: customDPUService("test-2"), conditions: getReadyConditionWithVeryLongMessage()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: all,
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test          default",
				"│           ├─Ready                      True   Success",
				"│           ├─RandomReady                False  SomethingWentWrong",
				"│           └─RandomReconciled           True   Success",
				"└─DPUServices",
				"  ├─DPUService/test             default",
				"  │             └─Ready                  True   Success",
				"  │                                                                     feature of the table.",
				"  │                                                                     test the wrapping feature of the table.",
				"  └─DPUService/test-2           default",
				"                └─Ready                  True   Success",
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
				ShowOtherConditions: all,
				ExpandResources:     "dpuservice",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test              default",
				"│           ├─Ready                          True    Success",
				"│           ├─RandomReady                    False   SomethingWentWrong",
				"│           └─RandomReconciled               True    Success",
				"└─DPUServices",
				"  └─DPUService/test                 default",
				"    │           ├─Ready                      True    Success",
				"    │           │                                                            feature of the table.",
				"    │           │                                                            test the wrapping feature of the table.",
				"    │           ├─RandomReady                False   SomethingWentWrong",
				"    │           │                                                            feature of the table.",
				"    │           │                                                            test the wrapping feature of the table.",
				"    │           └─RandomReconciled           True    Success",
				"    └─Application/test              default",
				"                  ├─DaemonSet/bar            Synced  Progressing",
				"                  │                                                          feature of the table.",
				"                  │                                                          test the wrapping feature of the table.",
				"                  ├─Deployment/foo           Synced  Progressing",
				"                  │                                                          feature of the table.",
				"                  │                                                          test the wrapping feature of the table.",
				"                  └─Ready                    True    Success",
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
				ShowOtherConditions: all,
				ShowResources:       "DPUService,DPUServiceIPAM",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test              default",
				"│           ├─Ready                          True   Success",
				"│           ├─RandomReady                    False  SomethingWentWrong",
				"│           └─RandomReconciled               True   Success",
				"├─DPUServiceIPAMs",
				"│ └─DPUServiceIPAM/test             default",
				"│               ├─Ready                      True   Success",
				"│               ├─RandomReady                False  SomethingWentWrong",
				"│               └─RandomReconciled           True   Success",
				"└─DPUServices",
				"  └─DPUService/test                 default",
				"                ├─Ready                      True   Success",
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
				{object: defaultDPUServiceTemplate(), conditions: getTrueCondition()},
				{object: defaultArgoCDApplication(), argoStatus: getRandomArgoCDApplicationConditions()},
			},
			opts: ObjectTreeOptions{
				ExpandResources: "dpuservice",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default  Ready: True  Success",
				"├─DPUClusters",
				"│ └─DPUCluster/test                                  default  Ready: True  Success",
				"├─DPUDeployments",
				"│ └─DPUDeployment/test                               default  Ready: True  Success",
				"│   ├─DPUServiceChains",
				"│   │ └─DPUServiceChain/test-from-dpudeployment      default  Ready: True  Success",
				"│   ├─DPUServiceInterfaces",
				"│   │ └─DPUServiceInterface/test-from-dpudeployment  default  Ready: True  Success",
				"│   ├─DPUSets",
				"│   │ └─DPUSet/test-from-dpudeployment               default  Ready: True  Success",
				"│   │   └─DPUs",
				"│   │     └─DPU/test-from-dpudeployment              default  Ready: True  Success",
				"│   └─Services",
				"│     ├─DPUServiceTemplates",
				"│     │ └─DPUServiceTemplate/test                    default  Ready: True  Success",
				"│     └─DPUServices",
				"│       └─DPUService/test-from-dpudeployment         default  Ready: True  Success",
				"├─DPUServiceChains",
				"│ └─DPUServiceChain/test                             default  Ready: True  Success",
				"├─DPUServiceCredentialRequests",
				"│ └─DPUServiceCredentialRequest/test                 default  Ready: True  Success",
				"├─DPUServiceIPAMs",
				"│ └─DPUServiceIPAM/test                              default  Ready: True  Success",
				"├─DPUServiceInterfaces",
				"│ └─DPUServiceInterface/test                         default  Ready: True  Success",
				"├─DPUServices",
				"│ └─DPUService/test                                  default  Ready: True  Success",
				"│   └─Application/test                               default",
				"│                 ├─DaemonSet/bar                             Synced       Progressing",
				"│                 │                                                                         feature of the table.",
				"│                 │                                                                         test the wrapping feature of the table.",
				"│                 ├─Deployment/foo                            Synced       Progressing",
				"│                 │                                                                         feature of the table.",
				"│                 │                                                                         test the wrapping feature of the table.",
				"│                 └─Ready                                     True         Success",
				"├─DPUSets",
				"│ └─DPUSet/test                                      default",
				"│   └─DPUs",
				"│     └─DPU/test                                     default  Ready: True  Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu                                 default  Ready: True  Success",
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
				{object: defaultDPUServiceTemplate(), conditions: getTrueCondition()},
				{object: defaultArgoCDApplication(), argoStatus: getRandomArgoCDApplicationConditions()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: all,
				ExpandResources:     "dpuservice",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                               default",
				"│           ├─Ready                                           True    Success",
				"│           ├─RandomReady                                     False   SomethingWentWrong",
				"│           └─RandomReconciled                                True    Success",
				"├─DPUClusters",
				"│ └─DPUCluster/test                                  default",
				"│               ├─Ready                                       True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUDeployments",
				"│ └─DPUDeployment/test                               default",
				"│   │           ├─Ready                                       True    Success",
				"│   │           ├─RandomReady                                 False   SomethingWentWrong",
				"│   │           └─RandomReconciled                            True    Success",
				"│   ├─DPUServiceChains",
				"│   │ └─DPUServiceChain/test-from-dpudeployment      default",
				"│   │               ├─Ready                                   True    Success",
				"│   │               ├─RandomReady                             False   SomethingWentWrong",
				"│   │               └─RandomReconciled                        True    Success",
				"│   ├─DPUServiceInterfaces",
				"│   │ └─DPUServiceInterface/test-from-dpudeployment  default",
				"│   │               ├─Ready                                   True    Success",
				"│   │               ├─RandomReady                             False   SomethingWentWrong",
				"│   │               └─RandomReconciled                        True    Success",
				"│   ├─DPUSets",
				"│   │ └─DPUSet/test-from-dpudeployment               default",
				"│   │   │           ├─Ready                                   True    Success",
				"│   │   │           ├─RandomReady                             False   SomethingWentWrong",
				"│   │   │           └─RandomReconciled                        True    Success",
				"│   │   └─DPUs",
				"│   │     └─DPU/test-from-dpudeployment              default",
				"│   │                   ├─Ready                               True    Success",
				"│   │                   ├─RandomReady                         False   SomethingWentWrong",
				"│   │                   └─RandomReconciled                    True    Success",
				"│   └─Services",
				"│     ├─DPUServiceTemplates",
				"│     │ └─DPUServiceTemplate/test                    default",
				"│     │               └─Ready                                 True    Success",
				"│     └─DPUServices",
				"│       └─DPUService/test-from-dpudeployment         default",
				"│                     ├─Ready                                 True    Success",
				"│                     ├─RandomReady                           False   SomethingWentWrong",
				"│                     └─RandomReconciled                      True    Success",
				"├─DPUServiceChains",
				"│ └─DPUServiceChain/test                             default",
				"│               ├─Ready                                       True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServiceCredentialRequests",
				"│ └─DPUServiceCredentialRequest/test                 default",
				"│               ├─Ready                                       True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServiceIPAMs",
				"│ └─DPUServiceIPAM/test                              default",
				"│               ├─Ready                                       True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServiceInterfaces",
				"│ └─DPUServiceInterface/test                         default",
				"│               ├─Ready                                       True    Success",
				"│               ├─RandomReady                                 False   SomethingWentWrong",
				"│               └─RandomReconciled                            True    Success",
				"├─DPUServices",
				"│ └─DPUService/test                                  default",
				"│   │           ├─Ready                                       True    Success",
				"│   │           ├─RandomReady                                 False   SomethingWentWrong",
				"│   │           └─RandomReconciled                            True    Success",
				"│   └─Application/test                               default",
				"│                 ├─DaemonSet/bar                             Synced  Progressing",
				"│                 │                                                                           feature of the table.",
				"│                 │                                                                           test the wrapping feature of the table.",
				"│                 ├─Deployment/foo                            Synced  Progressing",
				"│                 │                                                                           feature of the table.",
				"│                 │                                                                           test the wrapping feature of the table.",
				"│                 └─Ready                                     True    Success",
				"├─DPUSets",
				"│ └─DPUSet/test                                      default",
				"│   │           ├─Ready                                       True    Success",
				"│   │           ├─RandomReady                                 False   SomethingWentWrong",
				"│   │           └─RandomReconciled                            True    Success",
				"│   └─DPUs",
				"│     └─DPU/test                                     default",
				"│                   ├─Ready                                   True    Success",
				"│                   ├─RandomReady                             False   SomethingWentWrong",
				"│                   └─RandomReconciled                        True    Success",
				"└─DPUs",
				"  └─DPU/orphaned-dpu                                 default",
				"                ├─Ready                                       True    Success",
				"                ├─RandomReady                                 False   SomethingWentWrong",
				"                └─RandomReconciled                            True    Success",
			},
		},
		{
			name: "Add DPUVPC with IsolationClass, DPUVirtualNetwork and DPUServiceInterface",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultIsolationClass("my-isolation-class"), conditions: nil},
				{object: defaultDPUVPC("my-vpc", "my-isolation-class"), conditions: getTrueCondition()},
				{object: defaultDPUVirtualNetwork("testnet1", "my-vpc"), conditions: getTrueCondition()},
				{object: defaultDPUVirtualNetwork("testnet2", "my-vpc"), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterfaceWithVirtualNetwork("dpusi-1", "testnet1"), conditions: getTrueCondition()},
				{object: defaultDPUServiceInterfaceWithVirtualNetwork("dpusi-2", "testnet1"), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                   default  Ready: True  Success",
				"├─DPUServiceInterfaces",
				"│ ├─DPUServiceInterface/dpusi-1          default  Ready: True  Success",
				"│ └─DPUServiceInterface/dpusi-2          default  Ready: True  Success",
				"└─DPUVPCs",
				"  └─DPUVPC/my-vpc                        default  Ready: True  Success",
				"    ├─IsolationClass/my-isolation-class           Ready: True  Available  0s  Provisioner: my-isolation-class-provisioner",
				"    ├─DPUVirtualNetwork/testnet1         default  Ready: True  Success",
				"    │ ├─DPUServiceInterface/dpusi-1      default  Ready: True  Success",
				"    │ └─DPUServiceInterface/dpusi-2      default  Ready: True  Success",
				"    └─DPUVirtualNetwork/testnet2         default  Ready: True  Success",
			},
		},
		{
			name: "Add DPUVPC with missing IsolationClass",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUVPC("my-vpc", "my-isolation-class"), conditions: getTrueCondition()},
				{object: defaultDPUVirtualNetwork("testnet1", "my-vpc"), conditions: getFalseCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test            default  Ready: True  Success",
				"└─DPUVPCs",
				"  └─DPUVPC/my-vpc                 default  Ready: True  Success",
				"    └─DPUVirtualNetwork/testnet1  default",
				"                  └─Ready                  False        SomethingWentWrong",
			},
		},
		{
			name: "Add DPUServiceTemplate and DPUService with conditions",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUDeployment(), conditions: getTrueCondition()},
				{object: defaultDPUServiceTemplate(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUServiceFromDPUDeployment(), conditions: getRandomConditionsWithReadyTrueCondition()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: all,
				ShowResources:       "all",
				ShowChildResources:  true,
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                        default",
				"│           └─Ready                                    True   Success",
				"└─DPUDeployments",
				"  └─DPUDeployment/test                        default",
				"    │           └─Ready                                True   Success",
				"    └─Services",
				"      ├─DPUServiceTemplates",
				"      │ └─DPUServiceTemplate/test             default",
				"      │               ├─Ready                          True   Success",
				"      │               ├─RandomReady                    False  SomethingWentWrong",
				"      │               └─RandomReconciled               True   Success",
				"      └─DPUServices",
				"        └─DPUService/test-from-dpudeployment  default",
				"                      ├─Ready                          True   Success",
				"                      ├─RandomReady                    False  SomethingWentWrong",
				"                      └─RandomReconciled               True   Success",
			},
		},
		{
			name: "Storage resources are not shown by default",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUStoragePolicy(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUStorageVendor(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUVolume(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUVolumeAttachment(), conditions: getRandomConditionsWithReadyTrueCondition()},
			},
			opts: ObjectTreeOptions{
				ShowResources: "all",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test     default",
				"            ├─Ready                 True   Success",
				"            └─RandomReady           False  SomethingWentWrong",
			},
		},
		{
			name: "Storage resources are shown when ShowStorage is true",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUStoragePolicy(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUStorageVendor(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUVolume(), conditions: getRandomConditionsWithReadyTrueCondition()},
				{object: defaultDPUVolumeAttachment(), conditions: getRandomConditionsWithReadyTrueCondition()},
			},
			opts: ObjectTreeOptions{
				ShowStorage:         true,
				ShowOtherConditions: all,
				ShowResources:       "all",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                default",
				"│           ├─Ready                            True   Success",
				"│           ├─RandomReady                      False  SomethingWentWrong",
				"│           └─RandomReconciled                 True   Success",
				"└─Storage Resources                   default",
				"  ├─DPUStoragePolicies",
				"  │ └─DPUStoragePolicy/test           default",
				"  │               ├─Ready                      True   Success",
				"  │               ├─RandomReady                False  SomethingWentWrong",
				"  │               └─RandomReconciled           True   Success",
				"  ├─DPUStorageVendors",
				"  │ └─DPUStorageVendor/test           default",
				"  │               ├─Ready                      True   Success",
				"  │               ├─RandomReady                False  SomethingWentWrong",
				"  │               └─RandomReconciled           True   Success",
				"  ├─DPUVolumeAttachments",
				"  │ └─DPUVolumeAttachment/test        default",
				"  │               ├─Ready                      True   Success",
				"  │               ├─RandomReady                False  SomethingWentWrong",
				"  │               └─RandomReconciled           True   Success",
				"  └─DPUVolumes",
				"    └─DPUVolume/test                  default",
				"                  ├─Ready                      True   Success",
				"                  ├─RandomReady                False  SomethingWentWrong",
				"                  └─RandomReconciled           True   Success",
			},
		},
		{
			name: "Show only DPUSets and DPUs if show-resources=dpuset",
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
				{object: defaultDPUServiceTemplate(), conditions: getTrueCondition()},
				{object: defaultArgoCDApplication(), argoStatus: getRandomArgoCDApplicationConditions()},
			},
			opts: ObjectTreeOptions{
				ShowResources: "dpuset",
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test               default  Ready: True  Success  0s",
				"└─DPUSets",
				"  ├─DPUSet/test                      default  Ready: True  Success  0s",
				"  │ └─DPUs",
				"  │   └─DPU/test                     default  Ready: True  Success  0s",
				"  └─DPUSet/test-from-dpudeployment   default  Ready: True  Success  0s",
				"    └─DPUs",
				"      └─DPU/test-from-dpudeployment  default  Ready: True  Success  0s",
			},
		},
		{
			name: "Add DPUCluster with Kamaji TenantControlPlane",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUCluster(), conditions: getTrueCondition()},
				{object: defaultTenantControlPlane(), customStatus: getTenantControlPlaneStatus()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test         default  Ready: True  Success  0s",
				"└─DPUClusters",
				"  └─DPUCluster/test            default  Ready: True  Success  0s",
				"    └─TenantControlPlane/test  default  Ready: True  Ready    0s",
			},
		},
		{
			name: "Add DPUCluster with Kamaji TenantControlPlane with failed conditions",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultDPUCluster(), conditions: getTrueCondition()},
				{object: defaultTenantControlPlane(), customStatus: getTenantControlPlaneStatusWithFailures()},
			},
			opts: ObjectTreeOptions{
				ShowOtherConditions: all,
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test                    default",
				"│           └─Ready                                True   Success",
				"└─DPUClusters",
				"  └─DPUCluster/test                       default",
				"    │           └─Ready                            True   Success",
				"    └─TenantControlPlane/test             default",
				"                  ├─Service/Ready                  False  NotReady  0s  Service is not ready",
				"                  ├─Deployment/Available           False  NotReady  0s  Deployment is not ready",
				"                  └─Ready                          False  Unknown   0s  The TenantControlPlane is not ready",
			},
		},
		{
			name: "Add static DPUCluster without TenantControlPlane",
			objectsTree: []objectsWithConditions{
				{object: defaultDPFOperatorConfig(), conditions: getTrueCondition()},
				{object: defaultStaticDPUCluster(), conditions: getTrueCondition()},
			},
			expectedPrefix: []string{
				"DPFOperatorConfig/test  default  Ready: True  Success",
				"└─DPUClusters",
				"  └─DPUCluster/test     default  Ready: True  Success",
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

				if ot.customStatus != nil {
					g.Expect(unstructured.SetNestedMap(u.Object, ot.customStatus, "status")).To(Succeed())
					g.Expect(testClient.Status().Update(ctx, &u)).To(Succeed())
				}
			}

			td, err := Discover(context.Background(), testClient, tt.opts, all)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(td).ToNot(BeNil())

			// Creates the output table
			var output bytes.Buffer
			tbl := tablewriter.NewWriter(&output)

			formatTableTree(tbl)

			addObjectRow("", tbl, td, td.GetRoot())
			tbl.Render()
			g.Expect(output.String()).Should(MatchTable(tt.expectedPrefix), "output:\n%s\n", output.String())

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
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "oof",
			},
		},
	}
}

func defaultDPUCluster() *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: provisioningv1.DPUClusterSpec{
			Type: string(provisioningv1.KamajiCluster),
		},
	}
}

func defaultDPUSet() *provisioningv1.DPUSet {
	return &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: provisioningv1.DPUSetSpec{
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					BFB: provisioningv1.BFBReference{
						Name: "test",
					},
				},
			},
		},
	}
}

func defaultDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "orphaned-dpu", Namespace: "default"}, Spec: provisioningv1.DPUSpec{DPUDeviceName: "dpudevice-dpfctl-test", SerialNumber: "MT25066004C7"},
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
		Spec: provisioningv1.DPUSpec{
			DPUDeviceName: "dpudevice-dpfctl-test",
			SerialNumber:  "MT25066004C7",
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

func customDPUService(name string) *dpuservicev1.DPUService {
	return &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
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
		VFID: 1,
		PFID: 1,
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
			ServiceChains: &dpuservicev1.ServiceChains{
				Switches: []dpuservicev1.DPUDeploymentSwitch{
					{
						Ports: []dpuservicev1.DPUDeploymentPort{
							{
								Service: &dpuservicev1.DPUDeploymentService{
									Name:          "test",
									InterfaceName: "test",
								},
							},
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
		ParentInterfaceRef: ptr.To("eth0"),
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
		Spec: provisioningv1.DPUSpec{
			DPUDeviceName: "dpudevice-dpfctl-test",
			SerialNumber:  "MT25066004C7",
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

func defaultBFBWithVersion() *provisioningv1.BFB {
	return &provisioningv1.BFB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: provisioningv1.BFBSpec{
			URL: "https://dummy/bf-bundle-2.9.1-50.bfb",
		},
	}
}

func getBFBStatus() map[string]interface{} {
	return map[string]interface{}{
		"phase": "Ready",
		"versions": map[string]interface{}{
			"doca": "2.9.1",
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

func getReadyConditionWithVeryLongMessage() []metav1.Condition {
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

func defaultIsolationClass(name string) *vpcv1.IsolationClass {
	return &vpcv1.IsolationClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: vpcv1.IsolationClassSpec{
			Provisioner: fmt.Sprintf("%s-provisioner", name),
			Parameters: map[string]string{
				"param1": "value1",
			},
		},
	}
}

func defaultDPUVPC(name, isoName string) *vpcv1.DPUVPC {
	return &vpcv1.DPUVPC{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: vpcv1.DPUVPCSpec{
			Tenant:             "test-tenant",
			IsolationClassName: isoName,
			InterNetworkAccess: true,
		},
	}
}

func defaultDPUVirtualNetwork(name, vpcName string) *vpcv1.DPUVirtualNetwork {
	return &vpcv1.DPUVirtualNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: vpcv1.DPUVirtualNetworkSpec{
			VPCName:          vpcName,
			Type:             vpcv1.BridgedVirtualNetworkType,
			ExternallyRouted: false,
			BridgedNetwork: &vpcv1.BridgedNetworkSpec{
				IPAM: &vpcv1.BridgedNetworkIPAMSpec{
					IPv4: &vpcv1.BridgedNetworkIPAMIPv4Spec{
						DHCP:   true,
						Subnet: "192.168.1.0/24",
					},
				},
			},
		},
	}
}

func defaultDPUServiceInterfaceWithVirtualNetwork(name, vnName string) *dpuservicev1.DPUServiceInterface {
	si := defaultDPUServiceInterface()
	si.SetName(name)
	si.Spec.Template.Spec.Template.Spec.VF.VirtualNetwork = &vnName
	return si
}

func defaultDPUServiceTemplate() *dpuservicev1.DPUServiceTemplate {
	return &dpuservicev1.DPUServiceTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				defaultDPUDeployment().GetDependentLabelKey(): dpuservicev1.DependentDPUDeploymentLabelValue,
			},
		},
		Spec: dpuservicev1.DPUServiceTemplateSpec{
			DeploymentServiceName: "test-service",
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					RepoURL: "oci://foobar",
					Version: "1.0.0",
				},
			},
			ResourceRequirements: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}
}

func defaultDPUStoragePolicy() *storagev1.DPUStoragePolicy {
	return &storagev1.DPUStoragePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: storagev1.DPUStoragePolicySpec{
			DPUStorageVendors: []string{"test-vendor"},
			Parameters:        map[string]string{"param1": "value1"},
		},
	}
}

func defaultDPUStorageVendor() *storagev1.DPUStorageVendor {
	return &storagev1.DPUStorageVendor{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: storagev1.DPUStorageVendorSpec{
			StorageClassName: "test-storage-class",
			PluginName:       "test-plugin",
		},
	}
}

func defaultDPUVolume() *storagev1.DPUVolume {
	return &storagev1.DPUVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: storagev1.DPUVolumeSpec{
			DPUStoragePolicyName: "test",
			AccessModes:          []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
}

func defaultDPUVolumeAttachment() *storagev1.DPUVolumeAttachment {
	return &storagev1.DPUVolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: storagev1.DPUVolumeAttachmentSpec{
			DPUNodeName:   "test-node",
			DPUVolumeName: "test",
			FunctionTypeConfig: storagev1.FunctionTypeConfig{
				FunctionType:    storagev1.FunctionTypeVF,
				HotplugFunction: false,
			},
		},
	}
}

func defaultTenantControlPlane() *kamajiv1.TenantControlPlane {
	return &kamajiv1.TenantControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				provisioningv1.DPUClusterNameLabelKey: "test",
			},
		},
		Spec: kamajiv1.TenantControlPlaneSpec{
			DataStore:       "default",
			DataStoreSchema: "default",
			ControlPlane: kamajiv1.ControlPlane{
				Deployment: kamajiv1.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Service: kamajiv1.ServiceSpec{
					ServiceType: kamajiv1.ServiceTypeClusterIP,
				},
			},
			Kubernetes: kamajiv1.KubernetesSpec{
				Version: util.KubernetesVersion,
			},
		},
	}
}

func getTenantControlPlaneStatus() map[string]interface{} {
	tcp := &kamajiv1.TenantControlPlane{
		Status: kamajiv1.TenantControlPlaneStatus{
			Kubernetes: kamajiv1.KubernetesStatus{
				Service: kamajiv1.KubernetesServiceStatus{
					Name:      "test-service",
					Namespace: "default",
					Port:      6443,
					ServiceStatus: corev1.ServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Ready",
								Status:             metav1.ConditionTrue,
								Reason:             "Ready",
								Message:            "Service is ready",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Deployment: kamajiv1.KubernetesDeploymentStatus{
					Name:      "test-deployment",
					Namespace: "default",
					Selector:  "app=test",
					DeploymentStatus: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Type:               appsv1.DeploymentAvailable,
								Status:             corev1.ConditionTrue,
								Reason:             "Ready",
								Message:            "Deployment is ready",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		},
	}

	// Convert to unstructured for the test
	u := &unstructured.Unstructured{}
	if err := scheme.Scheme.Convert(tcp, u, nil); err != nil {
		panic(fmt.Sprintf("Failed to convert TenantControlPlane.Status: %v", err))
	}

	return u.Object["status"].(map[string]interface{})
}

func getTenantControlPlaneStatusWithFailures() map[string]interface{} {
	tcp := &kamajiv1.TenantControlPlane{
		Status: kamajiv1.TenantControlPlaneStatus{
			Kubernetes: kamajiv1.KubernetesStatus{
				Service: kamajiv1.KubernetesServiceStatus{
					Name:      "test-service",
					Namespace: "default",
					Port:      6443,
					ServiceStatus: corev1.ServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Ready",
								Status:             metav1.ConditionFalse,
								Reason:             "NotReady",
								Message:            "Service is not ready",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Deployment: kamajiv1.KubernetesDeploymentStatus{
					Name:      "test-deployment",
					Namespace: "default",
					Selector:  "app=test",
					DeploymentStatus: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Type:               appsv1.DeploymentAvailable,
								Status:             corev1.ConditionFalse,
								Reason:             "NotReady",
								Message:            "Deployment is not ready",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		},
	}

	// Convert to unstructured for the test
	u := &unstructured.Unstructured{}
	if err := scheme.Scheme.Convert(tcp, u, nil); err != nil {
		panic(fmt.Sprintf("Failed to convert TenantControlPlane.Status: %v", err))
	}

	return u.Object["status"].(map[string]interface{})
}

func defaultStaticDPUCluster() *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: provisioningv1.DPUClusterSpec{
			Type: string(provisioningv1.StaticCluster),
		},
	}
}

func defaultDPUNode() *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node", Namespace: "default"},
		Spec:       provisioningv1.DPUNodeSpec{},
	}
}

func defaultDPUDevice() *provisioningv1.DPUDevice {
	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: "test-device", Namespace: "default"},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: "MT25066004C7",
		},
	}
}

func defaultDPUNodeMaintenance() *provisioningv1.DPUNodeMaintenance {
	return &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-maintenance", Namespace: "default"},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{
			DPUNodeName: "test-node",
			Requestor:   []string{"dpf-operator-system_ovn-kubernetes_ovn-control-plane", "worker1-mt2413xz0awv"},
		},
	}
}

func dpuWithNodeReference() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dpu-with-node",
			Namespace: "default",
			Labels: map[string]string{
				util.DPUSetNameLabel:      "test",
				util.DPUSetNamespaceLabel: "default",
			},
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName:   "test-node",
			DPUDeviceName: "test-device",
			SerialNumber:  "MT25066004C7",
		},
	}
}

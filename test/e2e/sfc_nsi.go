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

package e2e

import (
	"context"
	"encoding/json"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"

	. "github.com/onsi/gomega"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sfcComponentArgs holds the desired controllerManager.manager.args per dpu-networking subchart component, keyed by component name.
var sfcComponentArgs = map[string][]string{
	"sfc-controller": {
		"--feature-gates=NSIPathForSFC=true",
	},
	"servicechainset-controller": {
		"--leader-elect",
		"--leader-election-namespace=$(POD_NAMESPACE)",
		"--feature-gates=NSIPathForSFC=true",
	},
}

// EnableNSIPathForSFC turns on the NSIPathForSFC feature gate for sfc-controller and every servicechainset-controller, returning a revert function.
func EnableNSIPathForSFC(ctx context.Context, testClient client.Client) func() {
	unpause := pauseDPFOperatorConfig(ctx, testClient)
	rollback := true
	defer func() {
		if rollback {
			unpause()
		}
	}()

	for component, args := range sfcComponentArgs {
		dpuServices := listComponentDPUServices(ctx, testClient, component)
		Expect(dpuServices).NotTo(BeEmpty(), "no DPUServices found for component %q", component)
		for _, dpuSvc := range dpuServices {
			setDPUServiceManagerArgs(ctx, testClient, dpuSvc, component, args)
		}
	}

	waitForSFCComponentsReady(ctx, testClient)
	rollback = false

	return func() {
		unpause()
		waitForSFCComponentsReady(ctx, testClient)
	}
}

// waitForSFCComponentsReady waits for every DPUService backing sfcComponentArgs to be Ready.
func waitForSFCComponentsReady(ctx context.Context, testClient client.Client) {
	var names []string
	for component := range sfcComponentArgs {
		for _, dpuSvc := range listComponentDPUServices(ctx, testClient, component) {
			names = append(names, dpuSvc.Name)
		}
	}
	dpuservice.WaitForDPUServices(ctx, testClient, dpfOperatorSystemNamespace, names)
}

// pauseDPFOperatorConfig pauses DPFOperatorConfig reconciliation (so overrides survive) and returns a function restoring its prior paused value.
func pauseDPFOperatorConfig(ctx context.Context, testClient client.Client) func() {
	cfg := &operatorv1.DPFOperatorConfig{}
	Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, cfg)).To(Succeed())
	wasPaused := cfg.Spec.Overrides != nil && ptr.Deref(cfg.Spec.Overrides.Paused, false)

	setPaused(ctx, testClient, cfg, true)

	return func() {
		current := &operatorv1.DPFOperatorConfig{}
		Expect(testClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, current)).To(Succeed())
		setPaused(ctx, testClient, current, wasPaused)
	}
}

func setPaused(ctx context.Context, testClient client.Client, cfg *operatorv1.DPFOperatorConfig, paused bool) {
	patch := client.MergeFrom(cfg.DeepCopy())
	if cfg.Spec.Overrides == nil {
		cfg.Spec.Overrides = &operatorv1.Overrides{}
	}
	cfg.Spec.Overrides.Paused = ptr.To(paused)
	Expect(testClient.Patch(ctx, cfg, patch)).To(Succeed())
}

// listComponentDPUServices returns the live DPUServices for component, excluding same-labeled companions whose name lacks the component prefix.
func listComponentDPUServices(ctx context.Context, testClient client.Client, component string) []*dpuservicev1.DPUService {
	list := &dpuservicev1.DPUServiceList{}
	Expect(testClient.List(ctx, list,
		client.InNamespace(dpfOperatorSystemNamespace),
		client.MatchingLabels{operatorv1.DPFComponentLabelKey: component},
	)).To(Succeed())

	svcs := make([]*dpuservicev1.DPUService, 0, len(list.Items))
	for i := range list.Items {
		if name := list.Items[i].Name; name == component || strings.HasPrefix(name, component+"-") {
			svcs = append(svcs, &list.Items[i])
		}
	}
	return svcs
}

// setDPUServiceManagerArgs patches dpuSvc's Helm values so that values.<component>.controllerManager.manager.args equals args.
func setDPUServiceManagerArgs(ctx context.Context, testClient client.Client, dpuSvc *dpuservicev1.DPUService, component string, args []string) {
	original := dpuSvc.DeepCopy()

	values := map[string]interface{}{}
	if dpuSvc.Spec.HelmChart.Values != nil && dpuSvc.Spec.HelmChart.Values.Raw != nil {
		Expect(json.Unmarshal(dpuSvc.Spec.HelmChart.Values.Raw, &values)).To(Succeed())
	}
	componentValues, _ := values[component].(map[string]interface{})
	if componentValues == nil {
		componentValues = map[string]interface{}{}
	}
	controllerManager, _ := componentValues["controllerManager"].(map[string]interface{})
	if controllerManager == nil {
		controllerManager = map[string]interface{}{}
	}
	manager, _ := controllerManager["manager"].(map[string]interface{})
	if manager == nil {
		manager = map[string]interface{}{}
	}
	manager["args"] = args
	controllerManager["manager"] = manager
	componentValues["controllerManager"] = controllerManager
	values[component] = componentValues

	raw, err := json.Marshal(values)
	Expect(err).NotTo(HaveOccurred())
	dpuSvc.Spec.HelmChart.Values = &machineryruntime.RawExtension{Raw: raw}

	Expect(testClient.Patch(ctx, dpuSvc, client.MergeFrom(original))).To(Succeed())
}

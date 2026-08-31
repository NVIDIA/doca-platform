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
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createDPUServiceTemplate creates a DPUServiceTemplate from a config-loaded
// manifest, with the dummy chart override that Phase 1 needs. No-op if the
// manifest pointer is nil.
func createDPUServiceTemplate(ctx context.Context, input *systemTestInput, manifest *dpuservicev1.DPUServiceTemplate) {
	Expect(manifest).ToNot(BeNil())
	obj := manifest.DeepCopy()
	obj.SetLabels(CleanupScope.Suite)
	useDummyDPUServiceChart(obj)
	By(fmt.Sprintf("Creating DPUServiceTemplate %s", obj.Name))
	Expect(input.client.Create(ctx, obj)).To(Succeed())
}

// createDPUServiceConfiguration creates a DPUServiceConfiguration from a
// config-loaded manifest. No-op if nil.
func createDPUServiceConfiguration(ctx context.Context, input *systemTestInput, manifest *dpuservicev1.DPUServiceConfiguration) {
	Expect(manifest).ToNot(BeNil())
	obj := manifest.DeepCopy()
	obj.SetLabels(CleanupScope.Suite)
	By(fmt.Sprintf("Creating DPUServiceConfiguration %s", obj.Name))
	Expect(input.client.Create(ctx, obj)).To(Succeed())
}

func createAdditionalDPUServiceDependencies(ctx context.Context, input *systemTestInput) {
	if input.additionalDPUServiceTemplate == nil && input.additionalDPUServiceConfiguration == nil {
		return
	}
	Expect(input.additionalDPUServiceTemplate).NotTo(BeNil(),
		"additional DPUService configuration requires additional DPUService template")
	Expect(input.additionalDPUServiceConfiguration).NotTo(BeNil(),
		"additional DPUService template requires additional DPUService configuration")
	createDPUServiceTemplate(ctx, input, input.additionalDPUServiceTemplate)
	createDPUServiceConfiguration(ctx, input, input.additionalDPUServiceConfiguration)
}

// createDPUServiceIPAMPool1 creates the dpudeployment-ipam-pool1 IPAM resource
// the upgrade tests reference. The base IPAM manifest is reused across many
// test suites, so the upgrade-specific bits (name override, no NodeSelector)
// are set in code here.
func createDPUServiceIPAMPool1(ctx context.Context, input *systemTestInput) {
	dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
	dpuServiceIPAM.SetLabels(CleanupScope.Suite)
	dpuServiceIPAM.SetName("dpudeployment-ipam-pool1")
	dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
	dpuServiceIPAM.Spec.NodeSelector = nil
	By("Creating DPUServiceIPAM dpudeployment-ipam-pool1")
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())
}

// applyDPFOperatorConfigFromPhaseConfig applies the DPFOperatorConfig for the
// current phase from input.config (the phase's YAML manifest) plus any runtime
// values that cannot be hardcoded in the YAML (e.g. the OTel endpoint).
func applyDPFOperatorConfigFromPhaseConfig(ctx context.Context, input *systemTestInput) {
	cfg := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{
		Name:      configName,
		Namespace: dpfOperatorSystemNamespace,
	}, cfg)).To(Succeed())

	original := cfg.DeepCopy()

	cfg.Spec = input.config.Spec

	// Inject runtime values not representable in the YAML (e.g. OTel endpoint).
	if cfg.MonitoringEnabled() &&
		(cfg.Spec.Monitoring == nil || cfg.Spec.Monitoring.OpenTelemetryCollector == nil) {
		if cfg.Spec.Monitoring == nil {
			cfg.Spec.Monitoring = &operatorv1.MonitoringConfiguration{}
		}
		controlPlaneIP := getClusterControlPlaneIP(ctx, input.client)
		cfg.Spec.Monitoring.OpenTelemetryCollector = &operatorv1.OpenTelemetryCollectorConfiguration{
			Logging: &operatorv1.OpenTelemetryCollectorLoggingConfiguration{
				Endpoint: fmt.Sprintf("%s%s:%d", otelEndpointSchema, controlPlaneIP, otelNodePort),
			},
			Metrics: &operatorv1.OpenTelemetryCollectorMetricsConfiguration{
				Endpoint: fmt.Sprintf("%s%s:%d", otelEndpointSchema, controlPlaneIP, otelNodePort),
			},
		}
	}

	By("Applying DPFOperatorConfig from phase manifest")
	Expect(input.client.Patch(ctx, cfg, client.MergeFrom(original))).To(Succeed())
}

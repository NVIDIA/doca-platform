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

package release

import (
	"slices"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
)

// vpcChart is a chart the dpf-vpc repository builds and the DPF release publishes with its own
// registry and tag (Makefile: release-build-vpc, release-push-vpc, collect-vpc-release-manifest).
// Its sources are not in this repository, so it cannot be rendered: its images are listed here.
type vpcChart struct {
	name   string
	images []bom.Image
}

// vpcChartsRef is the dpf-vpc commit whose charts vpcCharts lists. It must equal VPC_REF of the
// Makefile, the commit DPF builds: TestVPCChartsMatchVPCRef fails when they differ. On a VPC
// bump, update vpcCharts to the charts of the new commit, then set vpcChartsRef to it.
const vpcChartsRef = "6082556e2b4bec0ebadf46c7a6f3d4c46e6b363a"

// vpcCharts are the charts of the dpf-vpc repository at vpcChartsRef, with the first-party
// images their templates use (image names: VPC_OVN_SYSTEM_IMAGE_NAME, OVN_IMAGE_NAME and
// WEAVE_SYSTEM_IMAGE_NAME of the dpf-vpc Makefile). The scopes follow where the components run:
// the OVN central database and the VPC OVN controller in the host cluster, the OVN controller, the
// VPC OVN node and the Weave agents on the DPUs.
//
// This list is maintained by hand: generation does not read the dpf-vpc charts, so it cannot
// detect a change in them. At release time, airgap-bom check-release compares the image names
// of this list with the images of the VPC release manifest.
var vpcCharts = []vpcChart{
	{
		// management.ovnCentral (host) and dpu.ovnController (dpu).
		name:   "ovn-chart",
		images: []bom.Image{{Name: "ovn", Scope: []bom.Scope{bom.ScopeDPU, bom.ScopeHost}}},
	},
	{
		// host.vpcOVNController (host) and dpu.vpcOVNNode (dpu).
		name:   "dpf-vpc-ovn",
		images: []bom.Image{{Name: "vpc-system", Scope: []bom.Scope{bom.ScopeDPU, bom.ScopeHost}}},
	},
	{
		// weaveFlowController and weaveDHCPAgent (dpu).
		name:   "dpf-weave",
		images: []bom.Image{{Name: "weave-system", Scope: []bom.Scope{bom.ScopeDPU}}},
	},
}

// vpcChartNames returns the names of the VPC charts, sorted. The VPC release manifest merged into
// the DPF release manifest records them with the vpc scope.
func vpcChartNames() []string {
	names := make([]string, 0, len(vpcCharts))
	for _, c := range vpcCharts {
		names = append(names, c.name)
	}
	slices.Sort(names)
	return names
}

// VPCImageNames returns the names of the first-party images of the VPC charts, sorted.
func VPCImageNames() []string {
	var names []string
	for _, c := range vpcCharts {
		for _, img := range c.images {
			names = append(names, img.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// vpcServices returns a first-party service per VPC chart: the DPF release publishes them all.
func vpcServices() []bom.Service {
	services := make([]bom.Service, 0, len(vpcCharts))
	for _, c := range vpcCharts {
		services = append(services, bom.Service{Chart: bom.Chart{Name: c.name}, Images: slices.Clone(c.images)})
	}
	return services
}

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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
)

// leaderElectionTargets enumerates the in-scope controllers for the failover
// test (see leader_election.go for the inclusion criteria and exclusion list).
var leaderElectionTargets = []leaderElectionTarget{
	{
		component:      "provisioning-controller",
		deploymentName: "dpf-provisioning-controller-manager",
		leaseName:      "provisioning.dpu.nvidia.com",
	},
	{
		component:      "dpuservice-controller",
		deploymentName: "dpuservice-controller-manager",
		leaseName:      "dpuservice.dpu.nvidia.com",
	},
	{
		component:      "kamaji-cluster-manager",
		deploymentName: "kamaji-cm-controller-manager",
		leaseName:      "kamaji-cluster-manager.dpu.nvidia.com",
	},
	{
		component:      "static-cluster-manager",
		deploymentName: "static-cm-controller-manager",
		leaseName:      "static-cluster-manager.dpu.nvidia.com",
	},
}

var _ = Describe("DPF Leader-election failover", Labels{Domain.DPFSystem}, func() {
	for _, target := range leaderElectionTargets {
		It(fmt.Sprintf("hands over the lease when the %s leader pod is deleted", target.component), func() {
			ValidateLeaderElectionFailover(Ctx, TestClient, target)
		})
	}
})

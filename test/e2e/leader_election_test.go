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
var leaderElectionTargets = []LeaderElectionTarget{
	{
		Component:      "provisioning-controller",
		DeploymentName: "dpf-provisioning-controller-manager",
		LeaseName:      "provisioning.dpu.nvidia.com",
	},
	{
		Component:      "dpuservice-controller",
		DeploymentName: "dpuservice-controller-manager",
		LeaseName:      "dpuservice.dpu.nvidia.com",
	},
	{
		Component:      "kamaji-cluster-manager",
		DeploymentName: "kamaji-cm-controller-manager",
		LeaseName:      "kamaji-cluster-manager.dpu.nvidia.com",
	},
	{
		Component:      "static-cluster-manager",
		DeploymentName: "static-cm-controller-manager",
		LeaseName:      "static-cluster-manager.dpu.nvidia.com",
	},
}

var _ = Describe("DPF Leader-election failover", Labels{Domain.DPFSystem}, func() {
	for _, target := range leaderElectionTargets {
		It(fmt.Sprintf("hands over the lease when the %s leader pod is deleted", target.Component), func() {
			ValidateLeaderElectionFailover(Ctx, TestClient, target)
		})
	}
})

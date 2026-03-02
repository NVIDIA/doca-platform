/*
Copyright 2026 NVIDIA.

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
package controller

import (
	"context"
	"fmt"
	"strings"

	oflow "github.com/nvidia/doca-platform/pkg/openflow"

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -copyright_file ../../../hack/boilerplate.go.txt -package controller -destination mock_servicechain.go . ServiceChainAPI

type ServiceChainAPI interface {
	GenerateAndApplyOpenFlows(ctx context.Context, ports [][]string, hashedName uint64) error
}

var _ ServiceChainAPI = &ServiceChain{}

type ServiceChain struct {
	OPFlow oflow.OpenFlowAPI
}

// GenerateAndApplyOpenFlows generates and applies OpenFlow rules to the service chain.
// This loop processes each switch in the service chain.
// For each switch (represented by an array of ports), (chain is an array of switches),
// builds OpenFlow rules that enable communication between all ports in the switch.
//
// The pipeline uses two tables:
//   - Table 0 (LearningTable): MAC learning via learn() actions, then resubmit to switching table
//   - Table 1 (SwitchingTable): Forwarding decisions (multicast flood, learnt unicast, default unicast flood)
//
// For each port in a switch, three flow types are generated:
//
// 1. Learning flow (table 0, priority UnicastPriority):
//
//	match in_port → learn() for each peer port (creates unicast entries in table 1), then resubmit to table 1
//
// 2. Multicast/broadcast flow (table 1, priority MulticastPriority):
//
//	match in_port + dl_dst multicast bit → output to all peer ports
//
// 3. Default unicast flood flow (table 1, priority UnicastPriority):
//
//	match in_port → output to all peer ports (catch-all for unknown unicast)
//
// The learn() actions dynamically create unicast entries in the switching table (priority UnicastLearntPriority)
// that forward known destination MACs directly to the learned port.
//
// Don't fail immediately, operate on best effort basis to enable partial chains
// to enable some of the traffic to pass.
func (s *ServiceChain) GenerateAndApplyOpenFlows(ctx context.Context, ports [][]string, hashedName uint64) error {
	log := ctrllog.FromContext(ctx)
	var errs []error
	for arrayPos := range ports {
		if len(ports[arrayPos]) < 2 {
			// We need at least two elements to construct flows
			continue
		}

		var flowLines []string
		for i, arrayPort := range ports[arrayPos] {
			var learnActions []string
			var outputParts []string

			for j, iter := range ports[arrayPos] {
				if i == j {
					// skip self
					continue
				}
				learnActions = append(learnActions, fmt.Sprintf(
					"learn(cookie=%d,idle_timeout=10,table=%d,priority=%d,in_port=%s,dl_dst=dl_src,output:NXM_OF_IN_PORT[])",
					hashedName, SwitchingTable, UnicastLearntPriority, iter))
				outputParts = append(outputParts, fmt.Sprintf("output:%s", iter))
			}

			outputActions := strings.Join(outputParts, ",")

			// Table 0: learning flow — learn MACs then resubmit to switching table
			flowLines = append(flowLines, fmt.Sprintf(
				"cookie=%d, table=%d, priority=%d, in_port=%s actions=%s,resubmit(,%d)",
				hashedName, LearningTable, UnicastPriority, arrayPort,
				strings.Join(learnActions, ","), SwitchingTable))

			// Table 1: multicast/broadcast flow — flood
			flowLines = append(flowLines, fmt.Sprintf(
				"cookie=%d, table=%d, priority=%d, in_port=%s, dl_dst=%s actions=%s",
				hashedName, SwitchingTable, MulticastPriority, arrayPort,
				BroadcastMulticastMask, outputActions))

			// Table 1: default unicast flood — catch-all for unknown destinations
			flowLines = append(flowLines, fmt.Sprintf(
				"cookie=%d, table=%d, priority=%d, in_port=%s actions=%s",
				hashedName, SwitchingTable, UnicastPriority, arrayPort, outputActions))
		}

		err := s.OPFlow.AddFlows(ctx, strings.Join(flowLines, "\n"), BridgeSFC)
		if err != nil {
			log.Error(err, "failed to add flows")
			errs = append(errs, err)
			continue
		}
	}
	return kerrors.NewAggregate(errs)
}

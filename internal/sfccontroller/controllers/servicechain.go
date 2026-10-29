/*
Copyright 2025 NVIDIA.

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

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate mockgen -copyright_file ../../../hack/boilerplate.go.txt -package controller -destination mock_servicechain.go . ServiceChainAPI

type ServiceChainAPI interface {
	GenerateAndApplyOpenFlows(ctx context.Context, ports [][]string, hashedName uint64) error
}

var _ ServiceChainAPI = &ServiceChain{}

type ServiceChain struct {
	OPFlow OpenFlowAPI
}

// GenerateAndApplyOpenFlows generates and applies OpenFlow rules to the service chain.
// This loop processes each switch in the service chain.
// For each switch (represented by an array of ports), (chain is an array of switches),
// builds OpenFlow rules that enable communication between all ports in the switch
// The flows implement a simple L2 learning switch behavior where:
//   - Unknown destination traffic is flooded to all other ports
//   - Known destination traffic is forwarded only to the learned port
//
// For each port in a switch, create
//   - Learn actions for mac learning that dynamically create flows based on observed traffic
//   - Output actions to forward traffic to all other ports in the same switch
//
// Sample of generated learning flows inside an array
// ovs-ofctl add-flow br-sfc "in_port=$a,actions=learn(idle_timeout=10,priority=1,in_port=$b,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(idle_timeout=10,priority=1,
//
//	in_port=$c,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:$b,output:$c"
//
// ovs-ofctl add-flow br-sfc "in_port=$b,actions=learn(idle_timeout=10,priority=1,in_port=$a,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(idle_timeout=10,priority=1,
//
//	in_port=$c,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:$a,output:$c"
//
// ovs-ofctl add-flow br-sfc "in_port=$c,actions=learn(idle_timeout=10,priority=1,in_port=$a,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(idle_timeout=10,priority=1,
//
//	in_port=$b,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:$a,output:$b"
//
// don't fail immediately, operate on best effort basis to enable partial chains
// to enable some of the traffic to pass
func (s *ServiceChain) GenerateAndApplyOpenFlows(ctx context.Context, ports [][]string, hashedName uint64) error {
	log := ctrllog.FromContext(ctx)
	var errs []error
	for arrayPos := range ports {
		if len(ports[arrayPos]) < 2 {
			// We need at least two elements to construct flows
			continue
		}
		// Reset flows string
		flowsPerArray := ""
		for i, arrayPort := range ports[arrayPos] {
			if flowsPerArray != "" {
				// Add new line for each position
				flowsPerArray += "\n"
			}

			// Add unique cookie based on hashing the namespace name together with the table, priority constants and input port
			// this will result in the following string:
			//  cookie=0x24592fc503504d3, table=0, priority=20, in_port=97 actions=
			flowsPerArray += fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s actions=", hashedName, PriorityDynamicLearnFlows, arrayPort)

			// Reset output string
			outputFlowPart := ""
			// Reset learn string
			learnAction := ""

			for j, iter := range ports[arrayPos] {
				if i == j {
					// Skip self
					continue
				}

				if learnAction != "" {
					// If it's not the first learn action add comma
					learnAction += ","
				}

				// Add learn action
				learnAction += fmt.Sprintf(
					"learn(cookie=%d,idle_timeout=10,table=0,priority=%d,in_port=%s,dl_dst=dl_src,output:NXM_OF_IN_PORT[])",
					hashedName, PriorityLearntFlows, iter)

				if outputFlowPart != "" {
					// If it's not the first output action add comma
					outputFlowPart += ","
				}
				// Add output action
				outputFlowPart += fmt.Sprintf("output:%s", iter)
			}
			if learnAction != "" && outputFlowPart != "" {
				flowsPerArray += learnAction + "," + outputFlowPart
			}
		}

		// Try adding flows to vswitchd
		err := s.OPFlow.Add(ctx, flowsPerArray)
		if err != nil {
			log.Error(err, "failed to add flows")
			errs = append(errs, err)
			continue
		}
	}
	return kerrors.NewAggregate(errs)
}

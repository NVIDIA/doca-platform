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

package dpuagent

import (
	"fmt"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/netplan"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/vfmac"

	"k8s.io/klog/v2"
)

type DPUAgent struct {
	context    operations.Context
	operations []operations.Operation
}

func NewDPUAgent(context operations.Context) *DPUAgent {
	// The DPU Agent executes operations sequentially in the order defined in the slice.
	operations := []operations.Operation{
		&netplan.NetplanOperation{},
		&vfmac.VFMACOperation{},
	}
	return &DPUAgent{
		context:    context,
		operations: operations,
	}
}

func (d *DPUAgent) Run() error {
	for _, op := range d.operations {
		// TODO: skip "run once" operations that are already executed.
		if err := op.Execute(d.context); err != nil {
			return fmt.Errorf("error executing operation %s: %v", op.Name(), err)
		}
		klog.Infof("Successfully executed operation %s", op.Name())
	}
	return nil
}

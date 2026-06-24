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

// Command dpu-hw-agent is the agent-side SPIRE NodeAttestor plugin for DPU
// node attestation. SPIRE Agent launches it as an external plugin.
package main

import (
	"github.com/nvidia/doca-platform/internal/spire/dpu_hw/agent"

	"github.com/spiffe/spire-plugin-sdk/pluginmain"
	nodeattestorv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/plugin/agent/nodeattestor/v1"
)

func main() {
	plugin := agent.New()
	pluginmain.Serve(
		nodeattestorv1.NodeAttestorPluginServer(plugin),
	)
}

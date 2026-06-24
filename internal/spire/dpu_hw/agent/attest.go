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

// Package agent implements the agent half of the dpu_hw SPIRE NodeAttestor.
// It runs on the DPU's Arm cores, reads the local hardware serial, and sends
// it as the attestation payload to the server half.
package agent

import (
	"github.com/nvidia/doca-platform/internal/spire/identity"

	"github.com/hashicorp/go-hclog"
	"github.com/spiffe/spire-plugin-sdk/pluginsdk"
	nodeattestorv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/plugin/agent/nodeattestor/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ pluginsdk.NeedsLogger = (*Plugin)(nil)

// Plugin is the dpu_hw agent-side NodeAttestor.
type Plugin struct {
	nodeattestorv1.UnimplementedNodeAttestorServer

	reader SerialReader
}

// SetLogger propagates the SPIRE-provided logger into the reader so fallback
// diagnostics share the agent's log stream.
func (p *Plugin) SetLogger(logger hclog.Logger) {
	if reader, ok := p.reader.(loggerAwareSerialReader); ok {
		reader.SetLogger(logger)
	}
}

// New returns an agent Plugin using the default file-backed SerialReader.
func New() *Plugin {
	return &Plugin{reader: NewFileSerialReader()}
}

// NewWithReader returns an agent Plugin using the supplied SerialReader.
// A nil reader falls back to the default FileSerialReader.
func NewWithReader(r SerialReader) *Plugin {
	if r == nil {
		r = NewFileSerialReader()
	}
	return &Plugin{reader: r}
}

// AidAttestation implements the agent NodeAttestor RPC.
func (p *Plugin) AidAttestation(stream nodeattestorv1.NodeAttestor_AidAttestationServer) error {
	raw, err := p.reader.ReadSerial(stream.Context())
	if err != nil {
		return status.Errorf(codes.Internal, "failed to read DPU serial: %v", err)
	}

	serial, err := identity.NormalizeSerial(raw)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid DPU serial: %v", err)
	}

	return stream.Send(&nodeattestorv1.PayloadOrChallengeResponse{
		Data: &nodeattestorv1.PayloadOrChallengeResponse_Payload{
			Payload: []byte(serial),
		},
	})
}

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

// Package server implements the server half of the dpu_hw SPIRE
// NodeAttestor. It receives an attestation payload from the agent half,
// verifies it via an EvidenceVerifier, and mints the DPU agent SPIFFE ID.
package server

import (
	"context"
	"sync"

	"github.com/nvidia/doca-platform/internal/spire/identity"

	nodeattestorv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/plugin/server/nodeattestor/v1"
	configv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/service/common/config/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Plugin is the dpu_hw server-side NodeAttestor. It also implements the SPIRE
// Config service so that SPIRE can pass the trust domain at load time.
type Plugin struct {
	nodeattestorv1.UnimplementedNodeAttestorServer
	configv1.UnimplementedConfigServer

	verifier EvidenceVerifier

	mu          sync.RWMutex
	trustDomain string
}

// New returns a server Plugin using the default PlaintextVerifier.
func New() *Plugin {
	return &Plugin{verifier: PlaintextVerifier{}}
}

// NewWithVerifier returns a server Plugin using the supplied EvidenceVerifier.
// A nil verifier falls back to the default PlaintextVerifier.
func NewWithVerifier(v EvidenceVerifier) *Plugin {
	if v == nil {
		v = PlaintextVerifier{}
	}
	return &Plugin{verifier: v}
}

// Configure implements the SPIRE Config service.
func (p *Plugin) Configure(_ context.Context, req *configv1.ConfigureRequest) (*configv1.ConfigureResponse, error) {
	core := req.GetCoreConfiguration()
	if core == nil {
		return nil, status.Error(codes.InvalidArgument, "core configuration is missing the trust domain")
	}
	trustDomain, err := identity.ValidateTrustDomain(core.GetTrustDomain())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid trust domain: %v", err)
	}
	p.mu.Lock()
	p.trustDomain = trustDomain
	p.mu.Unlock()
	return &configv1.ConfigureResponse{}, nil
}

// Validate implements the SPIRE Config service validation path used by
// `spire-server validate`.
func (p *Plugin) Validate(_ context.Context, req *configv1.ValidateRequest) (*configv1.ValidateResponse, error) {
	core := req.GetCoreConfiguration()
	if core == nil {
		return &configv1.ValidateResponse{
			Valid: false,
			Notes: []string{"core configuration is missing the trust domain"},
		}, nil
	}
	if _, err := identity.ValidateTrustDomain(core.GetTrustDomain()); err != nil {
		return &configv1.ValidateResponse{
			Valid: false,
			Notes: []string{err.Error()},
		}, nil
	}
	return &configv1.ValidateResponse{Valid: true}, nil
}

// Attest implements the server NodeAttestor RPC.
func (p *Plugin) Attest(stream nodeattestorv1.NodeAttestor_AttestServer) error {
	p.mu.RLock()
	trustDomain := p.trustDomain
	p.mu.RUnlock()
	if trustDomain == "" {
		return status.Error(codes.FailedPrecondition, "plugin not configured: trust domain is empty")
	}

	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if len(req.GetPayload()) == 0 {
		return status.Error(codes.InvalidArgument, "expected non-empty attestation payload as the first message")
	}

	rawSerial, err := p.verifier.VerifyAndExtractSerial(stream.Context(), req.GetPayload())
	if err != nil {
		return status.Errorf(codes.PermissionDenied, "attestation evidence rejected: %v", err)
	}

	serial, err := identity.NormalizeSerial(rawSerial)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid DPU serial: %v", err)
	}

	return stream.Send(&nodeattestorv1.AttestResponse{
		Response: &nodeattestorv1.AttestResponse_AgentAttributes{
			AgentAttributes: &nodeattestorv1.AgentAttributes{
				SpiffeId:       identity.MakeAgentID(trustDomain, serial),
				SelectorValues: []string{"serial:" + serial},
				CanReattest:    true,
			},
		},
	})
}

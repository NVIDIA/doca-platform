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

package server_test

import (
	"context"
	"testing"

	"github.com/nvidia/doca-platform/internal/spire/dpu_hw/server"

	"github.com/spiffe/spire-plugin-sdk/pluginsdk"
	"github.com/spiffe/spire-plugin-sdk/plugintest"
	nodeattestorv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/plugin/server/nodeattestor/v1"
	configv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/service/common/config/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testTrustDomain = "example.org"

// start wires the server plugin (NodeAttestor + Config service) in-process and
// returns initialized gRPC clients for both.
func start(t *testing.T) (nodeattestorv1.NodeAttestorClient, configv1.ConfigClient) {
	t.Helper()
	plugin := server.New()
	naClient := new(nodeattestorv1.NodeAttestorPluginClient)
	cfgClient := new(configv1.ConfigServiceClient)
	plugintest.ServeInBackground(t, plugintest.Config{
		PluginServer:   nodeattestorv1.NodeAttestorPluginServer(plugin),
		PluginClient:   naClient,
		ServiceServers: []pluginsdk.ServiceServer{configv1.ConfigServiceServer(plugin)},
		ServiceClients: []pluginsdk.ServiceClient{cfgClient},
	})
	require.True(t, naClient.IsInitialized())
	require.True(t, cfgClient.IsInitialized())
	return naClient, cfgClient
}

func configure(ctx context.Context, t *testing.T, cfg configv1.ConfigClient, trustDomain string) error {
	t.Helper()
	_, err := cfg.Configure(ctx, &configv1.ConfigureRequest{
		CoreConfiguration: &configv1.CoreConfiguration{TrustDomain: trustDomain},
	})
	return err
}

// attest drives a single payload through the bidi Attest RPC and returns the
// minted agent attributes or the RPC error.
func attest(ctx context.Context, t *testing.T, na nodeattestorv1.NodeAttestorClient, payload []byte) (*nodeattestorv1.AgentAttributes, error) {
	t.Helper()
	stream, err := na.Attest(ctx)
	require.NoError(t, err)
	// The server may return before reading; ignore Send errors and let the
	// terminal status surface on Recv.
	_ = stream.Send(&nodeattestorv1.AttestRequest{
		Request: &nodeattestorv1.AttestRequest_Payload{Payload: payload},
	})
	_ = stream.CloseSend()
	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	return resp.GetAgentAttributes(), nil
}

func TestConfigureThenAttest(t *testing.T) {
	ctx := context.Background()
	na, cfg := start(t)

	require.NoError(t, configure(ctx, t, cfg, testTrustDomain))

	attrs, err := attest(ctx, t, na, []byte("MT2152X00ABC"))
	require.NoError(t, err)
	require.NotNil(t, attrs)
	require.Equal(t, "spiffe://example.org/spire/agent/dpu_hw/mt2152x00abc", attrs.GetSpiffeId())
	require.True(t, attrs.GetCanReattest())
	require.Contains(t, attrs.GetSelectorValues(), "serial:mt2152x00abc")
}

func TestAttestBeforeConfigureFails(t *testing.T) {
	ctx := context.Background()
	na, _ := start(t)

	_, err := attest(ctx, t, na, []byte("MT2152X00ABC"))
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestAttestInvalidSerialFails(t *testing.T) {
	ctx := context.Background()
	na, cfg := start(t)
	require.NoError(t, configure(ctx, t, cfg, testTrustDomain))

	// A space is outside the allowed charset, so NormalizeSerial rejects it.
	_, err := attest(ctx, t, na, []byte("bad serial"))
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAttestEmptyPayloadFails(t *testing.T) {
	ctx := context.Background()
	na, cfg := start(t)
	require.NoError(t, configure(ctx, t, cfg, testTrustDomain))

	_, err := attest(ctx, t, na, []byte{})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestConfigureRequiresTrustDomain(t *testing.T) {
	ctx := context.Background()
	_, cfg := start(t)

	err := configure(ctx, t, cfg, "")
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestConfigureRejectsInvalidTrustDomain(t *testing.T) {
	ctx := context.Background()
	_, cfg := start(t)

	err := configure(ctx, t, cfg, "example.org/foo")
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestValidate covers the `spire-server validate` path, which returns a
// structured response (never an RPC error) describing config validity.
func TestValidate(t *testing.T) {
	ctx := context.Background()
	_, cfg := start(t)

	t.Run("valid when trust domain is present", func(t *testing.T) {
		resp, err := cfg.Validate(ctx, &configv1.ValidateRequest{
			CoreConfiguration: &configv1.CoreConfiguration{TrustDomain: testTrustDomain},
		})
		require.NoError(t, err)
		require.True(t, resp.GetValid())
		require.Empty(t, resp.GetNotes())
	})

	t.Run("invalid with a note when trust domain is missing", func(t *testing.T) {
		resp, err := cfg.Validate(ctx, &configv1.ValidateRequest{
			CoreConfiguration: &configv1.CoreConfiguration{TrustDomain: ""},
		})
		require.NoError(t, err)
		require.False(t, resp.GetValid())
		require.NotEmpty(t, resp.GetNotes())
	})

	t.Run("invalid with a note when trust domain is malformed", func(t *testing.T) {
		resp, err := cfg.Validate(ctx, &configv1.ValidateRequest{
			CoreConfiguration: &configv1.CoreConfiguration{TrustDomain: "example.org/foo"},
		})
		require.NoError(t, err)
		require.False(t, resp.GetValid())
		require.NotEmpty(t, resp.GetNotes())
	})
}

func TestNewWithVerifierNilDefaults(t *testing.T) {
	require.NotNil(t, server.NewWithVerifier(nil))
}

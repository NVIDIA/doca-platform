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

package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nvidia/doca-platform/internal/spire/dpu_hw/agent"

	"github.com/spiffe/spire-plugin-sdk/plugintest"
	nodeattestorv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/plugin/agent/nodeattestor/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeReader struct {
	serial string
	err    error
}

func (f fakeReader) ReadSerial(context.Context) (string, error) {
	return f.serial, f.err
}

// startAgent wires the agent plugin in-process with the given reader and
// returns an initialized NodeAttestor client.
func startAgent(t *testing.T, reader agent.SerialReader) nodeattestorv1.NodeAttestorClient {
	t.Helper()
	plugin := agent.NewWithReader(reader)
	client := new(nodeattestorv1.NodeAttestorPluginClient)
	plugintest.ServeInBackground(t, plugintest.Config{
		PluginServer: nodeattestorv1.NodeAttestorPluginServer(plugin),
		PluginClient: client,
	})
	require.True(t, client.IsInitialized())
	return client
}

// aidAttestation drives the agent RPC and returns the emitted payload (or the
// RPC error). The agent sends first, so the test only needs to receive.
func aidAttestation(ctx context.Context, t *testing.T, na nodeattestorv1.NodeAttestorClient) ([]byte, error) {
	t.Helper()
	stream, err := na.AidAttestation(ctx)
	require.NoError(t, err)
	_ = stream.CloseSend()
	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	return resp.GetPayload(), nil
}

func TestAidAttestationSendsNormalizedSerial(t *testing.T) {
	ctx := context.Background()
	na := startAgent(t, fakeReader{serial: "MT2152X00ABC"})

	payload, err := aidAttestation(ctx, t, na)
	require.NoError(t, err)
	require.Equal(t, "mt2152x00abc", string(payload))
}

func TestAidAttestationRejectsInvalidSerial(t *testing.T) {
	ctx := context.Background()
	na := startAgent(t, fakeReader{serial: "bad serial"})

	_, err := aidAttestation(ctx, t, na)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAidAttestationReaderErrorIsInternal(t *testing.T) {
	ctx := context.Background()
	na := startAgent(t, fakeReader{err: errors.New("sysfs unavailable")})

	_, err := aidAttestation(ctx, t, na)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestNewWithReaderNilDefaults(t *testing.T) {
	require.NotNil(t, agent.NewWithReader(nil))
}

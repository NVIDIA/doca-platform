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

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/nvidia/doca-platform/internal/spire/identity"

	"github.com/stretchr/testify/require"
)

func TestPlaintextVerifier(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
		wantErr bool
	}{
		{name: "valid payload", payload: []byte("mt2152x00abc"), want: "mt2152x00abc"},
		{name: "empty payload rejected", payload: []byte{}, wantErr: true},
		{name: "oversized payload rejected", payload: []byte(strings.Repeat("a", identity.MaxSerialLen+1)), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (PlaintextVerifier{}).VerifyAndExtractSerial(context.Background(), tc.payload)
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

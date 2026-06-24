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
	"errors"
	"fmt"

	"github.com/nvidia/doca-platform/internal/spire/identity"
)

// EvidenceVerifier validates the agent attestation payload and extracts the
// raw (un-normalized) DPU serial.
type EvidenceVerifier interface {
	// VerifyAndExtractSerial inspects the agent payload and returns the raw
	// serial it asserts. The returned serial is not normalized.
	VerifyAndExtractSerial(ctx context.Context, payload []byte) (rawSerial string, err error)
}

// PlaintextVerifier treats the payload as the raw serial without cryptographic
// verification.
type PlaintextVerifier struct{}

// VerifyAndExtractSerial returns the payload bytes as the raw serial.
func (PlaintextVerifier) VerifyAndExtractSerial(_ context.Context, payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", errors.New("attestation payload is empty")
	}
	if len(payload) > identity.MaxSerialLen {
		return "", fmt.Errorf("attestation payload length %d exceeds maximum %d", len(payload), identity.MaxSerialLen)
	}
	return string(payload), nil
}

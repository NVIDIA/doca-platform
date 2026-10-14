/*
Copyright 2025 NVIDIA

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

package dpucluster

import (
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/version"
)

// mockServerVersionInterface implements the ServerVersionInterface for testing
type mockServerVersionInterface struct {
	shouldFail bool
	err        error
}

func (m *mockServerVersionInterface) ServerVersion() (*version.Info, error) {
	if m.shouldFail {
		return nil, m.err
	}
	return &version.Info{}, nil
}

func TestHealth_Check(t *testing.T) {
	testErr := errors.New("test error")

	tests := []struct {
		name            string
		serverFails     bool
		serverError     error
		attempts        int
		maxRetries      int
		wantStatus      string
		wantError       error
		wantNextAttempt int
	}{
		{
			name:            "healthy server",
			serverFails:     false,
			attempts:        0,
			maxRetries:      3,
			wantStatus:      HealthStatusHealthy,
			wantError:       nil,
			wantNextAttempt: 0,
		},
		{
			name:            "unhealthy server first attempt",
			serverFails:     true,
			serverError:     testErr,
			attempts:        0,
			maxRetries:      3,
			wantStatus:      HealthStatusUnhealthy,
			wantError:       nil,
			wantNextAttempt: 1,
		},
		{
			name:            "unhealthy server last attempt",
			serverFails:     true,
			serverError:     testErr,
			attempts:        3,
			maxRetries:      3,
			wantStatus:      HealthStatusUnhealthy,
			wantError:       nil,
			wantNextAttempt: 4,
		},
		{
			name:            "max retries exceeded",
			serverFails:     true,
			serverError:     testErr,
			attempts:        4,
			maxRetries:      3,
			wantStatus:      HealthStatusUnhealthy,
			wantError:       testErr,
			wantNextAttempt: 4, // should not increment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockServerVersionInterface{
				shouldFail: tt.serverFails,
				err:        tt.serverError,
			}
			g := NewWithT(t)
			h := &Health{
				client:          mockClient,
				attempt:         tt.attempts,
				maxRetries:      tt.maxRetries,
				lastCheckErrror: tt.serverError,
				status:          HealthStatusUnhealthy,
				maxBackoff:      DefaultMaxBackoff,
				Backoff:         *defaultBackoff,
			}

			gotStatus, gotErr := h.Check()
			g.Expect(tt.wantStatus).To(Equal(gotStatus))
			if tt.wantError != nil {
				g.Expect(gotErr).To(HaveOccurred())
				g.Expect(gotErr).To(Equal(tt.wantError))
			} else {
				g.Expect(gotErr).NotTo(HaveOccurred())
			}
			g.Expect(tt.wantNextAttempt).To(Equal(h.attempt))
		})
	}
}

func TestHealth_GetNextCheckTime(t *testing.T) {
	tests := []struct {
		name           string
		attempt        int
		duration       time.Duration
		maxBackoff     time.Duration
		expectedOutput time.Duration
	}{
		{
			name:           "first attempt",
			attempt:        0,
			duration:       30 * time.Second,
			maxBackoff:     5 * time.Minute,
			expectedOutput: 30 * time.Second,
		},
		{
			name:           "second attempt",
			attempt:        1,
			duration:       30 * time.Second,
			maxBackoff:     5 * time.Minute,
			expectedOutput: 60 * time.Second,
		},
		{
			name:           "third attempt",
			attempt:        2,
			duration:       30 * time.Second,
			maxBackoff:     5 * time.Minute,
			expectedOutput: 120 * time.Second,
		},
		{
			name:           "exceeds max backoff",
			attempt:        5,
			duration:       30 * time.Second,
			maxBackoff:     2 * time.Minute,
			expectedOutput: 2 * time.Minute, // capped at maxBackoff
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			b := NewBackoff(tt.duration, 2, 0.1)
			h := &Health{
				client:     &mockServerVersionInterface{},
				attempt:    tt.attempt,
				maxBackoff: tt.maxBackoff,
				Backoff:    *b,
			}

			got := h.GetNextCheckTime()

			// Calculate the base expected value (without jitter)
			baseExpected := min(tt.duration*time.Duration(int64(1<<tt.attempt)), tt.maxBackoff)

			// With 10% jitter, the result should be within ±10% of the base expected value
			jitterMargin := time.Duration(float64(baseExpected) * 0.1)

			// Verify the result is within acceptable range
			g.Expect(got).To(BeNumerically(">=", baseExpected-jitterMargin))
			g.Expect(got).To(BeNumerically("<=", baseExpected+jitterMargin))
		})
	}
}

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
	"time"

	"k8s.io/client-go/discovery"
)

const (
	// HealthStatusHealthy is the status of the dpu cluster when it is healthy
	HealthStatusHealthy = "Healthy"
	// HealthStatusUnhealthy is the status of the dpu cluster when it is unhealthy
	HealthStatusUnhealthy = "Unhealthy"
	// HealthStatusUnknown is the status of the dpu cluster when it is unknown
	HealthStatusUnknown = "Unknown"
)

const (
	// DefaultMaxRetries is the default maximum number of retries
	DefaultMaxRetries = 5
	// DefaultMaxBackoff is the default maximum backoff duration
	DefaultMaxBackoff = 10 * time.Minute
)

// DefaultBackoff is the default backoff used for the health check
// It uses a duration of 60 second, a factor of 2 and a jitter of 0.1
var defaultBackoff = NewBackoff(60*time.Second, 2, 0.1)

// Health is the struct that implements the health check for the dpu cluster.
type Health struct {
	Backoff
	client          discovery.ServerVersionInterface
	attempt         int
	maxRetries      int
	lastCheckErrror error
	status          string
	maxBackoff      time.Duration
}

// NewHealthServer creates a new Health instance.
func NewHealthServer(client discovery.ServerVersionInterface, maxBackoff time.Duration, maxRetries int, interval time.Duration) *Health {
	h := &Health{
		client:     client,
		maxRetries: maxRetries,
		status:     HealthStatusUnknown,
		maxBackoff: maxBackoff,
	}

	var b *Backoff
	if interval > 0 {
		b = NewBackoff(interval, 2, 0.1)
	} else {
		b = defaultBackoff
	}
	h.Backoff = *b
	return h
}

// Check checks the health of the dpu cluster.
// It returns the status of the dpu cluster and an error if the check fails
// after all retries.
// If the check is successful, it returns the status as "Healthy" and nil error.
func (h *Health) Check() (string, error) {
	if h.attempt > h.maxRetries {
		return h.status, h.lastCheckErrror
	}
	if _, err := h.client.ServerVersion(); err != nil {
		h.lastCheckErrror = err
		h.status = HealthStatusUnhealthy
		h.attempt++
		return h.status, nil
	}
	// If we get here, it means the server is healthy
	// reset the retry number
	h.attempt = 0
	h.lastCheckErrror = nil
	h.status = HealthStatusHealthy

	return h.status, nil
}

// GetNextCheckTime returns the next check time for the health check.
// It uses the backoff algorithm to calculate the next check time.
func (h *Health) GetNextCheckTime() time.Duration {
	// If the last check time is zero, it means we haven't checked yet
	b := h.Backoff.GetNextBackOff(h.attempt)
	if b > h.maxBackoff {
		return h.maxBackoff
	}
	return b
}

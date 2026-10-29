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
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func Test_ExponentialBackoff(t *testing.T) {
	tests := []struct {
		name     string
		attempt  int
		backoff  *Backoff
		expected time.Duration
	}{
		{
			name:     "zero attempt",
			attempt:  0,
			backoff:  NewBackoff(1*time.Second, 2, 0.1),
			expected: 1 * time.Second,
		},
		{
			name:     "one attempt",
			attempt:  1,
			backoff:  NewBackoff(1*time.Second, 2, 0.1),
			expected: 2 * time.Second,
		},
		{
			name:     "two attempts",
			attempt:  2,
			backoff:  NewBackoff(1*time.Second, 2, 0.1),
			expected: 4 * time.Second,
		},
		{
			name:     "three attempts",
			attempt:  3,
			backoff:  NewBackoff(1*time.Second, 2, 0.1),
			expected: 8 * time.Second,
		},
		{
			name:     "four attempts",
			attempt:  4,
			backoff:  NewBackoff(1*time.Second, 2, 0.1),
			expected: 16 * time.Second,
		},
		{
			name:     "five attempts",
			attempt:  5,
			backoff:  NewBackoff(1*time.Second, 2, 0.1),
			expected: 32 * time.Second,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			got := test.backoff.GetNextBackOff(test.attempt)
			if !(got >= time.Duration(float64(test.expected)*0.9) && got <= time.Duration(float64(test.expected)*1.1)) {
				g.Expect(got).To(BeNumerically("~", test.expected, 0.1*float64(test.expected)))
			}
		})
	}
}

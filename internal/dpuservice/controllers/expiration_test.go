/*
Copyright 2024 NVIDIA

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

package controllers

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Expiration", func() {
	DescribeTable("Is refresh required",
		func(exp, iat time.Time, expected bool) {
			resp := requiresRefresh(exp, iat)
			Expect(resp).To(Equal(expected))
		},
		Entry("should refresh", time.Now().Add(-1*time.Hour), time.Now().Add(-2*time.Hour), true),
		Entry("should refresh ttl > 24h", time.Now().Add(1*time.Hour), time.Now().Add(-25*time.Hour), true),
		Entry("should not refresh", time.Now().Add(time.Hour), time.Now(), false),
		Entry("should not refresh with wrong iat", time.Now().Add(-1*time.Hour), time.Time{}, false),
		Entry("should not refresh with wrong exp", time.Time{}, time.Now().Add(-1*time.Hour), false),
	)
	DescribeTable("Calculate next requeue time",
		func(exp, iat, expected time.Time, expectedErr error) {
			resp, err := calculateNextRequeueTime(exp, iat)
			if expectedErr != nil {
				Expect(err).To(Equal(expectedErr))
			} else {
				Expect(resp).To(BeTemporally("~", expected, maxJitter))
			}
		},
		Entry("should return valid time", time.Now().Add(1*time.Hour), time.Now(), time.Now().Add(48*time.Minute), nil),
		Entry("should error with wrong iat", time.Now().Add(-1*time.Hour), time.Time{}, nil, fmt.Errorf("expiration or issued at time is not set")),
		Entry("should error with wrong exp", time.Time{}, time.Now().Add(-1*time.Hour), nil, fmt.Errorf("expiration or issued at time is not set")),
	)
})

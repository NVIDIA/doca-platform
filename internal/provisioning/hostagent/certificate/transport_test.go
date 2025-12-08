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

package certificate

import (
	"context"
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	restclient "k8s.io/client-go/rest"
)

var _ = Describe("Transport", func() {
	Context("UpdateTransport", Label("UpdateTransport"), func() {
		It("should return error when transport is already configured", func() {
			clientConfig := &restclient.Config{
				Transport: http.DefaultTransport,
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("there is already a transport or dialer configured"))
		})

		It("should return error when dialer is already configured", func() {
			clientConfig := &restclient.Config{
				Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, nil
				},
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("there is already a transport or dialer configured"))
		})

		It("should succeed with nil certificate manager", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should set up dial function when certificate manager is nil", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(clientConfig.Dial).NotTo(BeNil())
		})

		It("should return a working close function", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := UpdateTransport(stopCh, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
			// Should not panic when called
			Expect(func() { closeFunc() }).NotTo(Panic())
		})
	})

	Context("updateTransport", Label("updateTransport"), func() {
		It("should accept custom period for certificate checking", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 1*time.Second, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should return error when both transport and dialer are configured", func() {
			clientConfig := &restclient.Config{
				Transport: http.DefaultTransport,
				Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, nil
				},
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			_, err := updateTransport(stopCh, 10*time.Second, clientConfig, nil, 0)
			Expect(err).To(HaveOccurred())
		})

		It("should work with zero exit duration", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 10*time.Second, clientConfig, nil, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})

		It("should work with non-zero exit duration", func() {
			clientConfig := &restclient.Config{
				Host: "https://localhost:6443",
			}
			stopCh := make(chan struct{})
			defer close(stopCh)

			closeFunc, err := updateTransport(stopCh, 10*time.Second, clientConfig, nil, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(closeFunc).NotTo(BeNil())
		})
	})
})

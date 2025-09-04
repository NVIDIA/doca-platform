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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("StaticClusterClientProvider", func() {
	var (
		provider    ClusterClientProvider
		testClient1 client.Client
		testClient2 client.Client
		clusterKey1 client.ObjectKey
		clusterKey2 client.ObjectKey
		unknownKey  client.ObjectKey
	)

	BeforeEach(func() {
		testClient1 = fake.NewClientBuilder().Build()
		testClient2 = fake.NewClientBuilder().Build()

		clusterKey1 = client.ObjectKey{Name: "cluster-1", Namespace: "test-namespace"}
		clusterKey2 = client.ObjectKey{Name: "cluster-2", Namespace: "test-namespace"}
		unknownKey = client.ObjectKey{Name: "unknown-cluster", Namespace: "test-namespace"}

		clients := map[client.ObjectKey]client.Client{
			clusterKey1: testClient1,
			clusterKey2: testClient2,
		}
		provider = NewStaticClusterClientProvider(clients)
	})

	Context("GetClient", func() {
		It("should return the correct client when cluster key exists", func() {
			client, err := provider.GetClient(clusterKey1)
			Expect(err).NotTo(HaveOccurred())
			Expect(client).To(Equal(testClient1))
		})

		It("should return the correct client for different cluster keys", func() {
			client1, err1 := provider.GetClient(clusterKey1)
			Expect(err1).NotTo(HaveOccurred())
			Expect(client1).To(Equal(testClient1))

			client2, err2 := provider.GetClient(clusterKey2)
			Expect(err2).NotTo(HaveOccurred())
			Expect(client2).To(Equal(testClient2))
		})

		It("should return ErrDPUClusterClientNotAvailable when cluster key does not exist", func() {
			client, err := provider.GetClient(unknownKey)
			Expect(client).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrDPUClusterNotConnected)).To(BeTrue())
		})
	})

	Context("ListClients", func() {
		It("should return the correct clients", func() {
			clients, err := provider.ListClients()
			Expect(err).NotTo(HaveOccurred())
			Expect(clients).To(HaveLen(2))
		})
	})
})

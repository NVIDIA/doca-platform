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

package dpuclusterhelper

import (
	"context"
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testNamespace = "test-namespace"
)

var (
	errTest = errors.New("test error")
)

func getDPUCluster(name string) provisioningv1.DPUCluster {
	return provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
	}
}

func getFakeClientBuilder() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithStatusSubresource(&provisioningv1.DPUCluster{})
}

var _ = Describe("DPUClusterHelper", func() {
	var (
		ctx        context.Context
		hostClient client.Client
		provider   dpucluster.ClusterClientProvider
		helper     DPUClusterHelper
	)

	BeforeEach(func() {
		ctx = context.Background()
		hostClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(&provisioningv1.DPUCluster{}).Build()
		provider = dpucluster.NewStaticClusterClientProvider(make(map[client.ObjectKey]client.Client))
		helper = New(hostClient, provider)
	})

	Context("GetTargetDPUClusters", func() {
		It("should return only ready DPUClusters when no required cluster is specified", func() {
			readyCluster1 := getDPUCluster("ready-cluster-1")
			readyCluster1.Status.Phase = provisioningv1.PhaseReady

			readyCluster2 := getDPUCluster("ready-cluster-2")
			readyCluster2.Status.Phase = provisioningv1.PhaseReady

			notReadyCluster := getDPUCluster("not-ready-cluster")
			notReadyCluster.Status.Phase = provisioningv1.PhaseCreating

			testClient := getFakeClientBuilder().WithObjects(&readyCluster1, &readyCluster2, &notReadyCluster).Build()
			testHelper := New(testClient, provider)

			clusters, err := testHelper.GetTargetDPUClusters(ctx, []client.ObjectKey{})
			Expect(err).NotTo(HaveOccurred())
			Expect(clusters).To(HaveLen(2))
			Expect(clusters).To(ContainElements(HaveField("Name", "ready-cluster-1"), HaveField("Name", "ready-cluster-2")))
		})
		It("should return all ready clusters when required cluster is already ready", func() {
			readyCluster1 := getDPUCluster("ready-cluster-1")
			readyCluster1.Status.Phase = provisioningv1.PhaseReady

			readyCluster2 := getDPUCluster("ready-cluster-2")
			readyCluster2.Status.Phase = provisioningv1.PhaseReady

			testClient := getFakeClientBuilder().WithObjects(&readyCluster1, &readyCluster2).Build()
			testHelper := New(testClient, provider)

			requiredCluster := client.ObjectKey{Name: readyCluster1.Name, Namespace: readyCluster1.Namespace}
			clusters, err := testHelper.GetTargetDPUClusters(ctx, []client.ObjectKey{requiredCluster})
			Expect(err).NotTo(HaveOccurred())
			Expect(clusters).To(HaveLen(2))
			Expect(clusters).To(ContainElements(HaveField("Name", "ready-cluster-1"), HaveField("Name", "ready-cluster-2")))
		})
		It("should return ready clusters plus required cluster when required cluster is not ready", func() {
			readyCluster1 := getDPUCluster("ready-cluster-1")
			readyCluster1.Status.Phase = provisioningv1.PhaseReady

			readyCluster2 := getDPUCluster("ready-cluster-2")
			readyCluster2.Status.Phase = provisioningv1.PhaseReady

			notReadyCluster := getDPUCluster("not-ready-cluster")
			notReadyCluster.Status.Phase = provisioningv1.PhaseCreating

			testClient := getFakeClientBuilder().WithObjects(&readyCluster1, &readyCluster2, &notReadyCluster).Build()
			testHelper := New(testClient, provider)

			requiredCluster := client.ObjectKey{Name: notReadyCluster.Name, Namespace: notReadyCluster.Namespace}
			clusters, err := testHelper.GetTargetDPUClusters(ctx, []client.ObjectKey{requiredCluster})
			Expect(err).NotTo(HaveOccurred())
			Expect(clusters).To(HaveLen(3))
			Expect(clusters).To(ContainElements(HaveField("Name", "ready-cluster-1"),
				HaveField("Name", "ready-cluster-2"), HaveField("Name", "not-ready-cluster")))
		})
		It("should return only ready clusters when required cluster does not exist", func() {
			readyCluster1 := getDPUCluster("ready-cluster-1")
			readyCluster1.Status.Phase = provisioningv1.PhaseReady

			readyCluster2 := getDPUCluster("ready-cluster-2")
			readyCluster2.Status.Phase = provisioningv1.PhaseReady

			testClient := getFakeClientBuilder().WithObjects(&readyCluster1, &readyCluster2).Build()
			testHelper := New(testClient, provider)

			requiredCluster := client.ObjectKey{Name: "non-existent-cluster", Namespace: testNamespace}
			clusters, err := testHelper.GetTargetDPUClusters(ctx, []client.ObjectKey{requiredCluster})
			Expect(err).NotTo(HaveOccurred())
			Expect(clusters).To(HaveLen(2))
			Expect(clusters).To(ContainElements(HaveField("Name", "ready-cluster-1"), HaveField("Name", "ready-cluster-2")))
		})
		It("should return error when host client fails to list DPUClusters", func() {
			failingClient := getFakeClientBuilder().
				WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch,
						list client.ObjectList, opts ...client.ListOption) error {
						return errTest
					},
				}).
				Build()

			failingHelper := New(failingClient, provider)
			_, err := failingHelper.GetTargetDPUClusters(ctx, []client.ObjectKey{})
			Expect(err).To(MatchError(errTest))
		})
	})

	Context("GetDPUCluster", func() {
		It("should return DPUCluster object when cluster exists", func() {
			cluster := getDPUCluster("test-cluster")

			testClient := getFakeClientBuilder().WithObjects(&cluster).Build()
			testHelper := New(testClient, provider)

			clusterKey := client.ObjectKey{Name: "test-cluster", Namespace: testNamespace}
			result, err := testHelper.GetDPUCluster(ctx, clusterKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Name).To(Equal("test-cluster"))
			Expect(result.Namespace).To(Equal(testNamespace))
		})
		It("should return error when cluster does not exist", func() {
			clusterKey := client.ObjectKey{Name: "non-existent-cluster", Namespace: testNamespace}
			_, err := helper.GetDPUCluster(ctx, clusterKey)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("GetClient", func() {
		It("should return ClientForDPUCluster from cluster client provider", func() {
			cluster := getDPUCluster("test-cluster")
			clusterKey := client.ObjectKeyFromObject(&cluster)
			expectedClient := getFakeClientBuilder().Build()
			clients := map[client.ObjectKey]client.Client{
				clusterKey: expectedClient,
			}
			testProvider := dpucluster.NewStaticClusterClientProvider(clients)
			testHelper := New(hostClient, testProvider)

			clientResult, err := testHelper.GetClient(ctx, &cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(clientResult.Client).To(Equal(expectedClient))
			Expect(clientResult.DPUCluster).To(Equal(&cluster))
		})
		It("should return ErrDPUClusterClientNotAvailable when provider fails to get client", func() {
			cluster := getDPUCluster("non-existent-cluster")
			// Create an empty provider so the cluster won't be found
			testProvider := dpucluster.NewStaticClusterClientProvider(map[client.ObjectKey]client.Client{})
			testHelper := New(hostClient, testProvider)

			_, err := testHelper.GetClient(ctx, &cluster)
			Expect(errors.Is(err, ErrDPUClusterClientNotAvailable)).To(BeTrue())
		})
	})

	Context("GetDPUClusterClients", func() {
		It("should return clients for all available clusters", func() {
			cluster1 := getDPUCluster("cluster-1")
			cluster2 := getDPUCluster("cluster-2")

			client1 := getFakeClientBuilder().Build()
			client2 := getFakeClientBuilder().Build()

			clientsMap := map[client.ObjectKey]client.Client{
				client.ObjectKeyFromObject(&cluster1): client1,
				client.ObjectKeyFromObject(&cluster2): client2,
			}
			testProvider := dpucluster.NewStaticClusterClientProvider(clientsMap)
			testHelper := New(hostClient, testProvider)

			clients, err := testHelper.GetDPUClusterClients(ctx, []provisioningv1.DPUCluster{cluster1, cluster2})
			Expect(err).NotTo(HaveOccurred())
			Expect(clients).To(HaveLen(2))
			namesToClients := map[string]client.Client{"cluster-1": client1, "cluster-2": client2}
			for _, c := range clients {
				Expect(namesToClients).To(HaveKey(c.DPUCluster.Name))
				Expect(c.Client).To(Equal(namesToClients[c.DPUCluster.Name]))
			}
		})
		It("should return aggregate error when some clients are not available", func() {
			cluster1 := getDPUCluster("cluster-1")
			cluster2 := getDPUCluster("cluster-2")

			client1 := getFakeClientBuilder().Build()
			// Only include client1 in the provider, so cluster2 will not be found
			clientsMap := map[client.ObjectKey]client.Client{
				client.ObjectKeyFromObject(&cluster1): client1,
			}
			testProvider := dpucluster.NewStaticClusterClientProvider(clientsMap)
			testHelper := New(hostClient, testProvider)

			_, err := testHelper.GetDPUClusterClients(ctx, []provisioningv1.DPUCluster{cluster1, cluster2})
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(And(ContainSubstring("failed to get client for DPUCluster"), ContainSubstring("cluster-2"))))
			Expect(errors.Is(err, ErrDPUClusterClientNotAvailable)).To(BeTrue())
		})
		It("should return empty client list when cluster list is empty", func() {
			clients, err := helper.GetDPUClusterClients(ctx, []provisioningv1.DPUCluster{})
			Expect(err).NotTo(HaveOccurred())
			Expect(clients).To(BeEmpty())
		})
	})
})

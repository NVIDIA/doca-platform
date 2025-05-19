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

package dpucluster

import (
	"fmt"
	"net/url"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_GetDPFClusters(t *testing.T) {
	t.Run("list clusters", func(t *testing.T) {
		g := NewWithT(t)
		clusters := []provisioningv1.DPUCluster{
			testDPUCluster("one", "cluster-one"),
			testDPUCluster("two", "cluster-two"),
			testDPUCluster("three", "cluster-three"),
		}
		secrets := []*corev1.Secret{
			testKamajiClusterSecret(clusters[0], t),
			testKamajiClusterSecret(clusters[1], t),
			testKamajiClusterSecret(clusters[2], t),
		}

		objects := []client.Object{}
		for _, s := range secrets {
			objects = append(objects, s)
		}
		for _, cl := range clusters {
			objects = append(objects, &cl)
		}
		c := fakeTestEnv.fakeKubeClient(withObjects(objects...))

		gotConfigs, err := GetConfigs(ctx, c)
		g.Expect(err).NotTo(HaveOccurred())

		// validate secrets
		for _, config := range gotConfigs {
			config, err := config.Kubeconfig(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(config).NotTo(BeNil())
		}
	})

	t.Run("list clusters with one cluster not ready", func(t *testing.T) {
		g := NewWithT(t)
		clusters := []provisioningv1.DPUCluster{
			testDPUCluster("one", "cluster-one"),
			testDPUCluster("two", "cluster-two"),
			testDPUCluster("three", "cluster-three"),
		}
		secrets := []*corev1.Secret{
			testKamajiClusterSecret(clusters[0], t),
			testKamajiClusterSecret(clusters[1], t),
			testKamajiClusterSecret(clusters[2], t),
		}
		clusters[2].Status.Phase = provisioningv1.PhaseNotReady
		objects := []client.Object{}
		for _, s := range secrets {
			objects = append(objects, s)
		}
		for _, cl := range clusters {
			objects = append(objects, &cl)
		}
		c := fakeTestEnv.fakeKubeClient(withObjects(objects...))

		gotClusters, err := GetConfigs(ctx, c)
		g.Expect(err).To(Not(HaveOccurred()))

		// Expect all clusters to be returned
		g.Expect(gotClusters).To(HaveLen(len(clusters)))
	})
}

func testDPUCluster(ns, name string) provisioningv1.DPUCluster {
	return provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Kubeconfig: fmt.Sprintf("%v-admin-kubeconfig", name),
		},
		Status: provisioningv1.DPUClusterStatus{
			Phase: provisioningv1.PhaseReady,
		},
	}
}

func testKamajiClusterSecret(cluster provisioningv1.DPUCluster, t *testing.T) *corev1.Secret {
	g := NewWithT(t)
	config := &api.Config{
		Clusters: map[string]*api.Cluster{
			cluster.Name: {
				Server:                   "https://localhost.com:6443",
				CertificateAuthorityData: []byte("lotsofdifferentletterstobesecure"),
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			"not-used": {
				ClientKeyData:         []byte("lotsofdifferentletterstobesecure"),
				ClientCertificateData: []byte("lotsofdifferentletterstobesecure"),
			},
		},
	}

	confData, err := clientcmd.Write(*config)
	g.Expect(err).To(Not(HaveOccurred()))
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%v-admin-kubeconfig", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"kamaji.clastix.io/component": "admin-kubeconfig",
				"kamaji.clastix.io/project":   "kamaji",
			},
		},
		Data: map[string][]byte{
			"admin.conf": confData,
		},
	}
	// TODO: Test for ownerReferences.
}

func TestConfig_Server(t *testing.T) {
	g := NewWithT(t)

	dpuCluster := testutils.GetTestDPUCluster("default", "envtest")
	secret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Expect(testClient.Create(ctx, secret)).To(Succeed())

	t.Run("get valid server URL", func(t *testing.T) {
		clusters, err := GetConfigs(ctx, testClient)
		g.Expect(err).NotTo(HaveOccurred())
		for _, c := range clusters {
			got, err := c.Server(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).NotTo(BeEmpty())
			_, err = url.Parse(got)
			g.Expect(err).NotTo(HaveOccurred(), "Server URL should be parseable")
		}
	})
}

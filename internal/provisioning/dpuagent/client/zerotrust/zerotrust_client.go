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

package zerotrust

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ZerotrustClient struct {
	dpfClient    client.Client
	k8sClient    kubernetes.Interface
	dpuName      string
	dpuNamespace string
}

func NewZerotrustClient(kubeconfig string, dpuName string, dpuNamespace string) *ZerotrustClient {
	dpfClient, k8sClient := buildClientOrDie(kubeconfig)
	return &ZerotrustClient{
		dpfClient:    dpfClient,
		k8sClient:    k8sClient,
		dpuName:      dpuName,
		dpuNamespace: dpuNamespace,
	}
}

func (c *ZerotrustClient) HealthCheck() error {
	_, err := c.k8sClient.Discovery().ServerVersion()
	return err
}

func (c *ZerotrustClient) UpdateStatus(ctx context.Context, dpuInfo provisioningv1.DPUInternalStatus) error {
	latestDPU := &provisioningv1.DPU{}
	if err := c.dpfClient.Get(ctx, client.ObjectKey{Namespace: c.dpuNamespace, Name: c.dpuName}, latestDPU); err != nil {
		return err
	}
	patch := client.MergeFrom(latestDPU.DeepCopy())
	if latestDPU.Status.DPUInternalStatus == nil {
		latestDPU.Status.DPUInternalStatus = &provisioningv1.DPUInternalStatus{
			Conditions: []metav1.Condition{},
		}
	}
	if dpuInfo.HostRebootRequired != nil {
		latestDPU.Status.DPUInternalStatus.HostRebootRequired = dpuInfo.HostRebootRequired
	}
	if dpuInfo.InitialBootID != nil {
		latestDPU.Status.DPUInternalStatus.InitialBootID = dpuInfo.InitialBootID
	}
	if dpuInfo.RebootMethod != nil {
		latestDPU.Status.DPUInternalStatus.RebootMethod = dpuInfo.RebootMethod
	}
	for _, condition := range dpuInfo.Conditions {
		meta.SetStatusCondition(&latestDPU.Status.DPUInternalStatus.Conditions, condition)
	}
	return c.dpfClient.Status().Patch(ctx, latestDPU, patch)
}

func (c *ZerotrustClient) GetObject(ctx context.Context, namespace string, name string, obj client.Object) error {
	return c.dpfClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
}

func buildClientOrDie(kubeconfig string) (dpfClient client.Client, k8sClient kubernetes.Interface) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	clientCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Fatalf("failed to build client config: %v", err)
	}
	dpfClient, err = client.New(clientCfg, client.Options{Scheme: scheme})
	if err != nil {
		klog.Fatalf("failed to create un-cached client: %v", err)
	}
	k8sClient, err = kubernetes.NewForConfig(clientCfg)
	if err != nil {
		klog.Fatalf("failed to create k8s client: %v", err)
	}
	return dpfClient, k8sClient
}

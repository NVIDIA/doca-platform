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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ZerotrustClient struct {
	dpfClient    client.Client
	k8sClient    kubernetes.Interface
	dpuName      string
	dpuNamespace string
	dpuUID       string
}

func NewZerotrustClient(config *restclient.Config, dpuName string, dpuNamespace string, dpuUID string) (*ZerotrustClient, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))

	dpfClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create dpf client: %w", err)
	}
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}
	return &ZerotrustClient{
		dpfClient:    dpfClient,
		k8sClient:    k8sClient,
		dpuName:      dpuName,
		dpuNamespace: dpuNamespace,
		dpuUID:       dpuUID,
	}, nil
}

func (c *ZerotrustClient) HealthCheck() error {
	_, err := c.k8sClient.Discovery().ServerVersion()
	return err
}

func (c *ZerotrustClient) UpdateStatus(ctx context.Context, agentStatus provisioningv1.AgentStatus) error {
	latestDPU := &provisioningv1.DPU{}
	if err := c.dpfClient.Get(ctx, client.ObjectKey{Namespace: c.dpuNamespace, Name: c.dpuName}, latestDPU); err != nil {
		return err
	}
	if string(latestDPU.UID) != c.dpuUID {
		return fmt.Errorf("stale DPU object: expected UID %s but got %s", c.dpuUID, latestDPU.UID)
	}
	patch := client.MergeFrom(latestDPU.DeepCopy())
	if latestDPU.Status.AgentStatus == nil {
		latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{
			Conditions: []metav1.Condition{},
		}
	}
	if agentStatus.LastStartupTime != nil {
		latestDPU.Status.AgentStatus.LastStartupTime = agentStatus.LastStartupTime
	}
	if agentStatus.InitialBootID != nil {
		latestDPU.Status.AgentStatus.InitialBootID = agentStatus.InitialBootID
	}
	if agentStatus.RebootMethod != nil {
		latestDPU.Status.AgentStatus.RebootMethod = agentStatus.RebootMethod
	}
	if agentStatus.RebootSequenceCount != nil {
		latestDPU.Status.AgentStatus.RebootSequenceCount = agentStatus.RebootSequenceCount
	}
	for _, condition := range agentStatus.Conditions {
		meta.SetStatusCondition(&latestDPU.Status.AgentStatus.Conditions, condition)
	}
	return c.dpfClient.Status().Patch(ctx, latestDPU, patch)
}

func (c *ZerotrustClient) GetObject(ctx context.Context, namespace string, name string, obj client.Object) error {
	return c.dpfClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
}

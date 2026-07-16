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

package bmcdump

import (
	"context"
	"errors"
	"fmt"
	"sort"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type logTarget struct {
	IP               string
	Port             uint32
	Password         string
	CredentialSecret string
	DPUDevices       []string
}

type logTargetResolver struct {
	client            client.Client
	namespace         string
	targetsByBMC      map[string]logTarget
	passwordsBySecret map[string]string
}

func getLogTargets(ctx context.Context, c client.Client, opts CollectOptions) ([]logTarget, error) {
	if c == nil {
		return nil, fmt.Errorf("bmc dump collection skipped: Kubernetes client is not initialized")
	}

	devices, listErr := listDPUDevices(ctx, c, opts.Namespace, opts.Devices)

	resolver := newLogTargetResolver(c, opts.Namespace)
	discoveryErrs := []error{listErr}
	for i := range devices {
		if err := resolver.addDevice(ctx, devices[i]); err != nil {
			discoveryErrs = append(discoveryErrs, fmt.Errorf("DPUDevice %s: %w", devices[i].Name, err))
			continue
		}
	}

	return resolver.targets(), errors.Join(discoveryErrs...)
}

func newLogTargetResolver(c client.Client, namespace string) *logTargetResolver {
	return &logTargetResolver{
		client:            c,
		namespace:         namespace,
		targetsByBMC:      map[string]logTarget{},
		passwordsBySecret: map[string]string{},
	}
}

func listDPUDevices(ctx context.Context, c client.Client, namespace string, names []string) ([]provisioningv1.DPUDevice, error) {
	if len(names) > 0 {
		devices := make([]provisioningv1.DPUDevice, 0, len(names))
		var errs []error
		for _, name := range names {
			device := &provisioningv1.DPUDevice{}
			if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, device); err != nil {
				errs = append(errs, fmt.Errorf("getting DPUDevice %s/%s for bmc dump collection: %w", namespace, name, err))
				continue
			}
			devices = append(devices, *device)
		}
		return devices, errors.Join(errs...)
	}

	devices := &provisioningv1.DPUDeviceList{}
	if err := c.List(ctx, devices, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing DPUDevices for bmc dump collection: %w", err)
	}
	return devices.Items, nil
}

func (r *logTargetResolver) addDevice(ctx context.Context, device provisioningv1.DPUDevice) error {
	bmcIP := dpuDeviceBMCIP(device)
	if bmcIP == "" {
		return nil
	}

	bmcPort := dpuDeviceBMCPort(device)
	credentialSecret := dpuDeviceBMCCredentialSecret(device)
	password, err := r.cachedDumpPassword(ctx, credentialSecret)
	if err != nil {
		return err
	}

	targetKey := logTargetKey(bmcIP, bmcPort, credentialSecret)
	target := r.targetsByBMC[targetKey]
	target.IP = bmcIP
	target.Port = bmcPort
	target.Password = password
	target.CredentialSecret = credentialSecret
	target.DPUDevices = append(target.DPUDevices, device.Name)
	r.targetsByBMC[targetKey] = target
	return nil
}

func (r *logTargetResolver) cachedDumpPassword(ctx context.Context, credentialSecret string) (string, error) {
	if password, ok := r.passwordsBySecret[credentialSecret]; ok {
		return password, nil
	}
	password, err := getDumpPassword(ctx, r.client, r.namespace, credentialSecret)
	if err != nil {
		return "", err
	}
	r.passwordsBySecret[credentialSecret] = password
	return password, nil
}

func (r *logTargetResolver) targets() []logTarget {
	return sortedLogTargets(r.targetsByBMC)
}

func getDumpPassword(ctx context.Context, c client.Client, namespace string, secretName string) (string, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret); err != nil {
		return "", fmt.Errorf("reading bmc dump password secret %s/%s: %w", namespace, secretName, err)
	}
	if password := string(secret.Data[passwordSecretDataKey]); password != "" {
		return password, nil
	}
	return "", fmt.Errorf("bmc dump collection skipped: %s/%s secret key %q is empty or missing",
		namespace, secretName, passwordSecretDataKey)
}

func dpuDeviceBMCIP(device provisioningv1.DPUDevice) string {
	if device.Status.BMCIP != nil {
		return *device.Status.BMCIP
	}
	if device.Spec.BMCIP != nil {
		return *device.Spec.BMCIP
	}
	return ""
}

func dpuDeviceBMCPort(device provisioningv1.DPUDevice) uint32 {
	if device.Status.BMCPort != nil {
		return *device.Status.BMCPort
	}
	if device.Spec.BMCPort != nil {
		return *device.Spec.BMCPort
	}
	return defaultPort
}

func dpuDeviceBMCCredentialSecret(device provisioningv1.DPUDevice) string {
	if device.Status.BMCCredentialSecretName != nil && *device.Status.BMCCredentialSecretName != "" {
		return *device.Status.BMCCredentialSecretName
	}
	if device.Spec.BMCCredentialSecretName != nil && *device.Spec.BMCCredentialSecretName != "" {
		return *device.Spec.BMCCredentialSecretName
	}
	return sharedPasswordSecretName
}

func logTargetKey(bmcIP string, bmcPort uint32, credentialSecret string) string {
	return fmt.Sprintf("%s:%d:%s", bmcIP, bmcPort, credentialSecret)
}

func sortedLogTargets(targetsByBMC map[string]logTarget) []logTarget {
	targets := make([]logTarget, 0, len(targetsByBMC))
	for _, target := range targetsByBMC {
		sort.Strings(target.DPUDevices)
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].IP != targets[j].IP {
			return targets[i].IP < targets[j].IP
		}
		if targets[i].Port != targets[j].Port {
			return targets[i].Port < targets[j].Port
		}
		return targets[i].CredentialSecret < targets[j].CredentialSecret
	})
	return targets
}

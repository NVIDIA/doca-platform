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

package inventory

import (
	"fmt"
	"maps"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	bfbRegistryContainerName    = "bfb-registry"
	bfbRegistryCertReloaderName = "cert-reloader"
	bfbRegistryBFBVolumeName    = "bfb"
	bfbRegistryHTTPSPortName    = "https"
)

// editRegistryObjs returns the bfb-registry objects of the embedded manifest with the edits of the
// Operator applied.
//
// The Operator owns the ConfigMap, the Service and the server Certificate in every mode, so that
// they do not depend on which Pod serves the registry. The Deployment is only emitted when the BFB
// PVC is ReadWriteMany and the registry is not disabled; otherwise the provisioning controller
// creates the registry Pod itself.
//
// Disabling the registry does not remove the Service and the Certificate. The provisioning
// controller still creates the registry Pod then, as it always did (nothing but the Operator
// layout reads the setting), and that Pod needs the server certificate and the Service to be
// usable.
func (p *provisioningControllerObjects) editRegistryObjs(vars Variables, labelsToAdd map[string]string) ([]*unstructured.Unstructured, error) {
	objs := make([]*unstructured.Unstructured, 0, len(p.bfbRegistryObjects))
	for i := range p.bfbRegistryObjects {
		if !bfbRegistryObjectEmitted(p.bfbRegistryObjects[i], vars) {
			continue
		}
		objs = append(objs, p.bfbRegistryObjects[i].DeepCopy())
	}
	// LabelsEdit mutates the map it is given; clone so shared-storage objects keep
	// only the ApplySet / component labels, not ConfigMap labels copied in-place.
	err := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace), LabelsEdit(maps.Clone(labelsToAdd))).
		AddForKind(ServiceKind, bfbRegistryServiceEdit(vars)).
		AddForKind(CertificateKind, bfbRegistryCertificateEdit(vars), p.holdLeafIssuerEdit(vars)).
		AddForKindS(DeploymentKind,
			ImagePullSecretsEditForDeploymentEdit(vars.ImagePullSecrets...),
			NodeAffinityEdit(&controlPlaneNodeAffinity),
			TolerationsEdit(controlPlaneTolerations),
			bfbRegistryDeploymentEdit(vars)).
		Apply(objs)
	if err != nil {
		return nil, err
	}
	return objs, nil
}

// bfbRegistryObjectEmitted reports whether obj of the bfb-registry manifest is part of the desired
// inventory for vars.
func bfbRegistryObjectEmitted(obj *unstructured.Unstructured, vars Variables) bool {
	switch ObjectKind(obj.GetKind()) {
	case DeploymentKind:
		return BFBRegistryManagedByOperator(vars)
	default:
		return true
	}
}

// bfbRegistryServiceEdit carries the NodePort of a previous release over to the Service. A Service
// created by an earlier release is owned by the provisioning controller Pod that was leader, so it
// is garbage collected when that Pod goes away, and the Operator creates it again. The port it had
// is written into the cloud-init of every DPU provisioned meanwhile and has to stay.
func bfbRegistryServiceEdit(vars Variables) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		nodePort := vars.DPFProvisioningController.BFBRegistryNodePort
		if nodePort == 0 {
			return nil
		}
		ports, found, err := unstructured.NestedSlice(obj.Object, "spec", "ports")
		if err != nil {
			return fmt.Errorf("read ports of Service %s: %w", obj.GetName(), err)
		}
		if !found {
			return fmt.Errorf("service %s has no ports", obj.GetName())
		}
		for i := range ports {
			port, ok := ports[i].(map[string]interface{})
			if !ok || port["name"] != bfbRegistryHTTPSPortName {
				continue
			}
			port["nodePort"] = int64(nodePort)
		}
		return unstructured.SetNestedSlice(obj.Object, ports, "spec", "ports")
	}
}

// bfbRegistryCertificateEdit sets the names and addresses the clients connect to on the server
// Certificate.
func bfbRegistryCertificateEdit(vars Variables) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		if obj.GetName() != bfbregistry.ServerCertSecretName {
			return nil
		}
		vip := ""
		if vars.KubernetesAPIServerVIP != nil {
			vip = *vars.KubernetesAPIServerVIP
		}
		lb := ""
		if vars.DPFProvisioningController.Registry != nil && vars.DPFProvisioningController.Registry.LoadBalancerAddress != nil {
			lb = *vars.DPFProvisioningController.Registry.LoadBalancerAddress
		}
		dnsNames, ipAddresses := bfbregistry.ServerCertSANs(vars.Namespace, vip, lb)
		if err := unstructured.SetNestedStringSlice(obj.Object, dnsNames, "spec", "dnsNames"); err != nil {
			return fmt.Errorf("set dnsNames of Certificate %s: %w", obj.GetName(), err)
		}
		if len(ipAddresses) == 0 {
			unstructured.RemoveNestedField(obj.Object, "spec", "ipAddresses")
			return nil
		}
		if err := unstructured.SetNestedStringSlice(obj.Object, ipAddresses, "spec", "ipAddresses"); err != nil {
			return fmt.Errorf("set ipAddresses of Certificate %s: %w", obj.GetName(), err)
		}
		return nil
	}
}

// bfbRegistryDeploymentEdit sets the replica count, the images and the BFB PVC on the registry
// Deployment.
func bfbRegistryDeploymentEdit(vars Variables) StructuredEdit {
	return func(obj client.Object) error {
		deploy, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected object %s. expected Deployment", obj.GetObjectKind().GroupVersionKind())
		}
		image := vars.Images[operatorv1.BFBRegistryName.String()]
		if image == "" {
			return fmt.Errorf("image for %q not found in variables", operatorv1.BFBRegistryName)
		}
		deploy.Spec.Replicas = ptr.To(bfbRegistryReplicas(vars))

		podSpec := &deploy.Spec.Template.Spec
		for i := range podSpec.Containers {
			switch podSpec.Containers[i].Name {
			case bfbRegistryContainerName, bfbRegistryCertReloaderName:
				podSpec.Containers[i].Image = image
			}
		}
		claim := vars.DPFProvisioningController.BFBRegistryVolumeClaim
		if claim == "" {
			return fmt.Errorf("the registry Deployment needs the name of the BFB PVC")
		}
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name == bfbRegistryBFBVolumeName && podSpec.Volumes[i].PersistentVolumeClaim != nil {
				podSpec.Volumes[i].PersistentVolumeClaim.ClaimName = claim
			}
		}
		return nil
	}
}

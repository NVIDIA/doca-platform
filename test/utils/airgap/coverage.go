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

package airgap

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodImage is an image a container of a pod runs.
type PodImage struct {
	Image string
	// Where is <namespace>/<pod>/<container>.
	Where string
}

// String returns the pod image as <namespace>/<pod>/<container>: <image>.
func (p PodImage) String() string {
	return p.Where + ": " + p.Image
}

// PodImages returns the images of the containers of all pods of the cluster of c.
func PodImages(ctx context.Context, c client.Client) ([]PodImage, error) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	var out []PodImage
	for _, pod := range pods.Items {
		add := func(name, image string) {
			out = append(out, PodImage{Image: image, Where: pod.Namespace + "/" + pod.Name + "/" + name})
		}
		for _, ctr := range pod.Spec.InitContainers {
			add(ctr.Name, ctr.Image)
		}
		for _, ctr := range pod.Spec.Containers {
			add(ctr.Name, ctr.Image)
		}
		for _, ctr := range pod.Spec.EphemeralContainers {
			add(ctr.Name, ctr.Image)
		}
	}
	return out, nil
}

// Unmirrored returns the pod images that are not pulled from the mirror.
func (m *Mirror) Unmirrored(images []PodImage) []PodImage {
	var out []PodImage
	for _, img := range images {
		if !m.Contains(img.Image) {
			out = append(out, img)
		}
	}
	return out
}

// NodesWithoutSandbox returns the nodes of the cluster of c whose image list lacks the mirror copy
// of the sandbox image: containerd of such a node did not pull its sandbox image from the mirror.
func (m *Mirror) NodesWithoutSandbox(ctx context.Context, c client.Client) ([]string, error) {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	var out []string
	for _, node := range nodes.Items {
		if !slices.ContainsFunc(node.Status.Images, func(img corev1.ContainerImage) bool {
			return slices.Contains(img.Names, m.sandbox)
		}) {
			out = append(out, node.Name)
		}
	}
	return out, nil
}

// SandboxImage is the mirror copy of the containerd sandbox image.
func (m *Mirror) SandboxImage() string {
	return m.sandbox
}

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

package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// BytesToUnstructured converts a slice of Bytes to Unstructured objects. This function is useful when you want to read
// a yaml manifest and covert it to Kubernetes objects.
func BytesToUnstructured(b []byte) ([]*unstructured.Unstructured, error) {
	objs := []*unstructured.Unstructured{}
	decoder := apiyaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), 4096)
	for {
		obj := unstructured.Unstructured{}
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to unmarshal content into unstructured: %w", err)
		}

		if obj.GetKind() == "" {
			continue
		}

		objs = append(objs, &obj)
	}
	return objs, nil
}

// UnstructuredToBytes converts a list of Unstructured objects to YAML.
func UnstructuredToBytes(objs []*unstructured.Unstructured) ([]byte, error) {
	yamls := [][]byte{}

	for _, o := range objs {
		content, err := yaml.Marshal(o.UnstructuredContent())
		if err != nil {
			return nil, err
		}
		yamls = append(yamls, content)
	}

	yamlData := [][]byte{}
	lineSeperator := []byte("\n")
	yamlSeparator := []byte("---")
	for _, y := range yamls {
		if !bytes.HasPrefix(y, lineSeperator) {
			y = append(lineSeperator, y...)
		}
		if !bytes.HasSuffix(y, lineSeperator) {
			y = append(y, lineSeperator...)
		}
		yamlData = append(yamlData, y)
	}

	out := bytes.Join(yamlData, yamlSeparator)
	out = bytes.TrimPrefix(out, lineSeperator)
	out = bytes.TrimSuffix(out, lineSeperator)

	return out, nil
}

// EnsureNamespace ensures the namespace exists in the cluster by creating it if it does not exist.
func EnsureNamespace(ctx context.Context, client client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := client.Create(ctx, ns); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

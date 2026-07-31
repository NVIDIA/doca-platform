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

// Package v1alpha1 contains code that was copied from the upstream ArgoCD types.
// This was done to prevent needing to import ArgoCD as a library to:
// 1) Use strong types for ArgoCD objects.
// 2) Avoid adding a large number of dependencies to the go.mod.
// 3) Allow the DPF operator to keep Kubernetes library versions independent of the ArgoCD versions.
//
// Copying the API was done by copying the relevant files and removing all functions from the files.
// The alternative to this would be to generate our own types with just the fields we care about from ArgoCD or to
// work with unstructure objects and utility methods.
//
//nolint:unused
package v1alpha1

// TODO: (killianmuldoon) replace the argoCD vendored types with an unstructured representation and a group of accessors when we have a good idea of which fields we need.

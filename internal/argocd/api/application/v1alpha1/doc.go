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

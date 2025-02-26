// Package v1alpha1 contains code that was copied from the upstream Kamaji@v1.0.0 types.
// This was done to prevent needing to import Kamaji as a library to:
// 1) Use strong types for Kamaji objects.
// 2) Avoid adding a large number of dependencies to the go.mod.
// 3) Allow the DPF operator to keep Kubernetes library versions independent of the Kamaji versions.
//
// Copying the API was done by copying the relevant files and removing all functions from the files.
// The alternative to this would be to generate our own types with just the fields we care about from Kamaji or to
// work with unstructured objects and utility methods.
//
//nolint:unused
package v1alpha1

const TenantControlPlaneKind = "TenantControlPlane"

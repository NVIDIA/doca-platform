Forked from https://github.com/spiffe/spire-controller-manager@v0.7.0 / 9f60f11470be5b0ca095bfaaa90b95ad7acd4aa0 to prevent an import of spire-controller-manager as library to:

1. Use strong types for the `ClusterStaticEntry` objects DPF registers with SPIRE.
2. Avoid adding a large number of dependencies to the go.mod.
3. Allow the DPF operator to keep Kubernetes library versions independent of the spire-controller-manager versions.

Point 3 is the blocking one: spire-controller-manager v0.6.6 and later require Go 1.26, `k8s.io/*` v0.36 and `sigs.k8s.io/controller-runtime` v0.24, while DPF pins Go 1.25, `k8s.io/*` v0.35 and controller-runtime v0.22. Importing the module would force all three up. Upstream `api/v1alpha1` is also not a leaf package: its loader and webhook files pull in `pkg/spireapi`, `go-spiffe` and gRPC.

Only `ClusterStaticEntry` is forked. Everything else in the upstream package is dropped, so `zz_generated.deepcopy.go` is regenerated rather than copied, and the scheme registration in `groupversion_info.go` is trimmed to the remaining kinds. The two source files kept here depend only on `k8s.io/apimachinery`, so this fork adds no dependency at all.

The upstream `ClusterStaticEntry` CRD is copied to `test/objects/crd/spire/` in the same step, so envtest validates the entries DPF writes against the real schema.

Copying the API was done via `hack/scripts/go-sync-third-party-forks.sh`.

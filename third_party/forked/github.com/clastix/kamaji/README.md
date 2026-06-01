Forked from https://github.com/clastix/kamaji@26.4.4-edge / e51909fae3f5221b14c83413aaa5df07b64d3336 to prevent an import of Kamaji as library to:

1. Use strong types for Kamaji objects.
2. Avoid adding a large number of dependencies to the go.mod.
3. Allow the DPF operator to keep Kubernetes library versions independent of the Kamaji versions.

Copying the API was done via `hack/scripts/go-sync-third-party-forks.sh`.

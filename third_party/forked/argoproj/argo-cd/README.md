Forked from github.com/argoproj/argo-cd/v3@v3.3.0 / fd6b7d5b3cba5e7aa7ad400b0fb905a81018a77b to prevent an import of ArgoCD as library to:

1. Use strong types for ArgoCD objects.
2. Avoid adding a large number of dependencies to the go.mod.
3. Allow the DPF operator to keep Kubernetes library versions independent of the ArgoCD versions.

Copying the API was done by copying the relevant files and removing all functions from the files.
The alternative to this would be to generate our own types with just the fields we care about from ArgoCD or to
work with unstructure objects and utility methods.

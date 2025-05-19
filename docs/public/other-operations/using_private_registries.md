---
title: Using Private Registries
---

The system components and images for DPF are published to publicly available repos that have no authentication. Users who consume these artifacts from registries with authentication and will need to create Kubernetes Secrets to manage access.

DPF uses needs to authenticate with registries in the following ways:

* **DPF Operator Installation**: For pulling the operator image.
* **Helm login**: To install the DPFOperatorConfig helm chart.
* **DPFOperatorConfig**: contains a field `.spec.imagePullSecrets` which injects secrets into system components.
* **DPUServices**: To pull DPUService images.
* **ArgoCD repository Secret**: To enable ArgoCD to pull DPUService helm charts.

## Image pull secrets

Kubernetes Pods which run images from an authenticated registry will need a secret to access the registry. 

To create an image pull secret, you need to specify the following environment variables:
```bash
## The registry the image pull secret will be created for.
export REGISTRY=${REGISTRY:?Must specify the registry}

## The namespace to which the image pull secret will be created. 
## Note: If you're creating DPUServices in other namespaces, you'll need to create the secret for each namespace.
export IMAGE_PULL_SECRET_NAMESPACE="${IMAGE_PULL_SECRET_NAMESPACE:-dpf-operator-system}"

## The username used to log in to the registry.
export IMAGE_REGISTRY_USERNAME=${IMAGE_REGISTRY_USERNAME:?Must specify the registry username}

## The image pull key for the registry.
export IMAGE_PULL_KEY=${IMAGE_PULL_KEY:?Must specify the image pull key}
```

Log in to the registry to ensure the variables are correct:
```bash
echo "$IMAGE_PULL_KEY" | docker login --username "$IMAGE_REGISTRY_USERNAME" --password-stdin $REGISTRY
```

Create the image pull secret:
```bash 
echo "Creating image pull secret in namespace: $ns"
kubectl -n "$ns" create secret docker-registry dpf-pull-secret --docker-server="$REGISTRY" --docker-username="$IMAGE_REGISTRY_USERNAME" --docker-password="$IMAGE_PULL_KEY" --dry-run=client -o yaml | kubectl apply -f -
```

#### Using the DPF pull secret for DPUServices

DPUServices run on a DPUCluster and image pull secrets must be explicitly mirrored to them. This mirroring is done by labelling the secret:

```bash
kubectl -n $IMAGE_PULL_SECRET_NAMESPACE label secret dpf-pull-secret dpu.nvidia.com/image-pull-secret=""
```

Any Secret with this label will be mirrored to the DPUCluster and can be used there. 

## ArgoCD repository secrets

DPUServices which reference helm charts from public registries will need a secret to access the helm chart repository.

To create an ArgoCD repository secret, you need to specify the following environment variables:
```bash
## The registry the image pull secret will be created for.
export HELM_REPOSITORY_URL=${HELM_REPOSITORY_URL:?Must specify the helm repository url}

## The name of the repository secret and the registry
export HELM_REPOSITORY_NAME=${HELM_REPOSITORY_NAME:-dpf-helm-repository}

## The username used to log in to the registry.
export HELM_REPOSITORY_USERNAME=${HELM_REPOSITORY_USERNAME:?Must specify the helm repository username}

## The key/password used to authenticate with the helm repository
export HELM_REPOSITORY_KEY=${HELM_REPOSITORY_KEY:?Must specify the helm repository key}
```

```bash
envsubst < argocd-repository-secret.yaml | kubectl apply -f -
```

[embedmd]:#(argocd-repository-secret.yaml)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: $HELM_REPOSITORY_NAME
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: $HELM_REPOSITORY_NAME
  url: $HELM_REPOSITORY_URL
  type: helm
  username: $HELM_REPOSITORY_USERNAME
  password: $HELM_REPOSITORY_KEY
```

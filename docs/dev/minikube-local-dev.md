# Minikube Local Dev

This guide helps the developer in setting and running a local Minikube env that can be used to deploy the various DPF components and run tests. The minikube env doesn't require a DPU. 

## Prerequisites
1. [GO](https://go.dev/doc/install) >= 1.22
2. [Docker Engine](https://docs.docker.com/engine/install/)
3. [kubectl](https://www.liberiangeek.net/2024/04/install-kubectl-on-ubuntu-24-04/)

## Dev Flow

1. Go to your local dev project git repo
```
cd <path_to_local_repo/doca-platform-foundation
```
2. export minikube env vars (Linux):
```
export REGISTRY=<insert_your_registry>
export MINIKUBE_CNI=flannel
export MINIKUBE_DRIVER=docker
export MINIKUBE_DOCKER_MIRROR="https://registry-1.docker.io"
export DEPLOY_KSM=true
export DEPLOY_GRAFANA=true
export DEPLOY_PROMETHEUS=true
export IMAGE_PULL_KEY=<IMAGE_PULL_TOKEN>
export TAG=v0.1.0-$(git rev-parse --short HEAD)-$USER-test
```
_Note:_ For MAC users add:
```
export MINIKUBE_DRIVER=qemu
export MINIKUBE_EXTRA_ARGS="--network=socket_vmnet"
```
3. Generate all 
```
make generate
```
4. Build images required for the quick DPF e2e test.
```
make test-release-e2e-quick
```
5. Deploy Minikube
```
make clean-test-env test-env-e2e
```
6. Deploy DPF Operator
```
make test-deploy-operator-helm
```
7. Run e2e tests
```
make test-e2e
```
_Note:_
If you want to keep the created resources (e.g., `DPFOperatorConfig`, `DPU` and such) you can use the environment variable `E2E_SKIP_CLEANUP=true`
```
E2E_SKIP_CLEANUP=true make test-e2e
```
8. Clean Minikube env
```
make clean-test-env
```
## Troubleshooting
1. the make target ```clean-test-env``` is stuck

try with:
```
minikube delete --all --alsologtostderr
```
2.  Enable metrics API in minikube
```
minikube -p dpf-test addons enable metrics-server
```
3. Machine Doesn't Have Enough Resources To Run Minikube:

Reduce the Resource Demands of Minikube. e.g.,
```
export NODE_MEMORY=4g
export NODE_CPUS=2
export NODE_DISK=50g
make test-env-e2e
```
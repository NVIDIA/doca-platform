---
title: "Local Development"
---

[TOC]

This guide helps the developer in setting and running a local env that can be used to deploy the various DPF components and run tests. The local env doesn't require a DPU. 

## Prerequisites
1. [GO](https://go.dev/doc/install) >= 1.24
2. [Docker Engine](https://docs.docker.com/engine/install/)
3. [kubectl](https://www.liberiangeek.net/2024/04/install-kubectl-on-ubuntu-24-04/)

## Dev Flow

**1.** Go to your local dev project git repo

```
cd <path_to_local_repo/doca-platform-foundation
```

**2.** export env vars (Linux):

```
export REGISTRY=<insert_your_registry>
export IMAGE_PULL_KEY=<IMAGE_PULL_TOKEN>
export TAG=v0.1.0-$(git rev-parse --short HEAD)-$USER-test
```

On Mac the development environment is designed to work with Docker Desktop running with Apple Virtualization.

**3.** Generate all 

```
make generate
```

**4.** Build images required for the quick DPF e2e test.

```
make test-release-e2e-quick
```

**5.** Deploy the test environment

```
make clean-test-env test-env-e2e
```

**6.** Deploy DPF Operator

```
make test-deploy-operator-helm
```

**7.** Run e2e tests

```
make test-e2e
```

_Note:_
If you want to keep the created resources (e.g., `DPFOperatorConfig`, `DPU` and such) you can use the `-e2e.skip-cleanup` flag:

```
make test-e2e E2E_TEST_ARGS="-e2e.skip-cleanup"
```

**8.** Clean test env

```
make clean-test-env
```

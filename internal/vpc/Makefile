#
# SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: LicenseRef-NvidiaProprietary
#
# NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
# property and proprietary rights in and to this material, related
# documentation and any modifications thereto. Any use, reproduction,
# disclosure or distribution of this material and related documentation
# without an express license agreement from NVIDIA CORPORATION or
# its affiliates is strictly prohibited.
#

# Get the Makefile's directory as an absolute path while preserving symlinks
MAKEFILE_DIR := $(shell cd $(dir $(lastword $(MAKEFILE_LIST))) && pwd -L)
# Remove trailing slash to prevent paths like "internal/vpc//../.."
# Example: $(dir ...) returns "internal/vpc/" but we want "internal/vpc"
MAKEFILE_DIR := $(patsubst %/,%,$(MAKEFILE_DIR))

# PROJECT_DIR must be set BEFORE including tools.mk
PROJECT_DIR := $(shell cd $(MAKEFILE_DIR)/../.. && pwd -L)

## Include Make modules which are split up in this repo for better structure.
include $(MAKEFILE_DIR)/../../hack/tools/tools.mk

PROJECT_NAME="DOCA Platform Framework"
PROJECT_REPO="https://github.com/NVIDIA/doca-platform"
export DATE="$(shell date --rfc-3339=seconds)"
export FULL_COMMIT ?= $(shell git rev-parse HEAD)

# Export is needed here so that the envsubst used in make targets has access to those variables even when they are not
# explicitly set when calling make.
# The tag must have three digits with a leading v - i.e. v9.9.1
export TAG ?= v0.1.0
# Note: Registry defaults to non-existing registry intentionally to avoid overriding useful images.
export REGISTRY ?= example.com
# This variable should be overwritten with the registry of the upstream artifacts. Needed when making a release upstream.
# This variable ensures that the values injected in the operator and charts point to the upstream artifacts.
export UPSTREAM_REGISTRY ?= $(REGISTRY)

# If V is set to 1 the output will be verbose.
Q = $(if $(filter 1,$V),,@)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.33.0

# Get the current OS and Architecture
ARCH ?= $(shell go env GOARCH)
OS ?= $(shell go env GOOS)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GO_VERSION ?= $(shell awk '/^go /{print $$2}' go.mod | awk -F '.' '{print $$1 "." $$2}')

# Allows for defining additional Go test args, e.g. '-tags integration'.
GO_TEST_ARGS ?= -race

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

LOCALBIN ?= $(PROJECT_DIR)/bin
export CHARTSDIR ?= $(PROJECT_DIR)/hack/charts
DPUSERVICESDIR ?= $(PROJECT_DIR)/dpuservices
HELMDIR ?= $(PROJECT_DIR)/charts
TOOLSDIR = $(PROJECT_DIR)/hack/tools/bin
CONFDIR = $(PROJECT_DIR)/config

$(LOCALBIN) $(CHARTSDIR):
	@mkdir -p $@

.PHONY: clean
clean: ; $(info  Cleaning...)	 @ ## Clean non-essential files from the repo
	@rm -rf $(CHARTSDIR)
	@rm -rf $(TOOLSDIR)

# Note: This helps resolve errors with `docker manifest create`
.PHONY: clean-images-for-registry
clean-images-for-registry: ## Clean release deletes local images with the $REGISTRY
	for image in $$(docker images $$REGISTRY/* --format "{{.ID}}"); do \
	docker rmi -f $$image ; \
	done

##@ Dependencies

##@ Development
GENERATE_TARGETS ?= vpc-ovn ovn

.PHONY: generate
generate: ## Run all generate-* targets.
	$(MAKE) generate-mocks generate-dbmodels generate-modules generate-manifests generate-docs

.PHONY: generate-mocks
generate-mocks: mockgen ## Generate mocks
	## Prepend the TOOLSDIR to the path for this command as `mockgen` is called from the $PATH inline in the code.
	## The DPF TOOLSDIR should be first in the path to ensure user tools are not used.
	## See go:generate comments for examples.

	export PATH="$(TOOLSDIR):$(PATH)"; go generate $$(go list ./... | grep -Ev '/nbdb|/sbdb')

OVN_SCHEMA_VERSION ?= v24.09.0
.PHONY: generate-dbmodels ## Generate database models for OVN
generate-dbmodels: generate-ovn-dbmodels

## fetch ovn northbound database schema version with specific version $OVN_SCHEMA_VERSION
ovn/nbdb/ovn-nb.ovsschema: ## Fetch OVN northbound database schema
	curl -sSL https://raw.githubusercontent.com/ovn-org/ovn/$(OVN_SCHEMA_VERSION)/ovn-nb.ovsschema -o $@

.PHONY: generate-ovn-dbmodels ## Generate OVN database models
generate-ovn-dbmodels: generate-ovnnb-dbmodel generate-ovnsb-dbmodel

generate-ovnnb-dbmodel: modelgen ovn/nbdb/ovn-nb.ovsschema ## Generate model using modelgen
				export PATH="$(TOOLSDIR):$(PATH)"; go generate ./ovn/nbdb/...
				rm -f ovn/nbdb/ovn-nb.ovsschema

## fetch ovn southbound database schema version with specific version $OVN_SCHEMA_VERSION
ovn/sbdb/ovn-sb.ovsschema: ## Fetch OVN southbound database schema
	curl -sSL https://raw.githubusercontent.com/ovn-org/ovn/$(OVN_SCHEMA_VERSION)/ovn-sb.ovsschema -o $@

generate-ovnsb-dbmodel: modelgen ovn/sbdb/ovn-sb.ovsschema ## Generate model using modelgen
				export PATH="$(TOOLSDIR):$(PATH)"; go generate ./ovn/sbdb/...
				rm -f ovn/sbdb/ovn-sb.ovsschema

.PHONY: generate-modules
generate-modules: ## Run go mod tidy to update go modules
	go mod tidy

.PHONY: generate-manifests
generate-manifests: $(addprefix generate-manifests-,$(GENERATE_TARGETS)) ## Run all generate-manifests-* targets

.PHONY: generate-manifests-vpc-ovn
generate-manifests-vpc-ovn: generate-manifests-vpc-ovn-controller generate-manifests-vpc-ovn-node ## Generate manifests for VPC OVN

.PHONY: generate-manifests-vpc-ovn-controller
generate-manifests-vpc-ovn-controller: envsubst controller-gen kustomize ## Generate manifests e.g. CRD, RBAC for VPC OVN controller
	$(CONTROLLER_GEN) \
	paths="./cmd/vpc-ovn-controller/..." \
	paths="./ovn/controllers/..." \
	rbac:roleName=manager-role \
	output:rbac:dir=$(CONFDIR)/vpc/ovn/rbac
	$(ENVSUBST) < $(VPC_HELM_CHART)/values.yaml.tmpl > $(VPC_HELM_CHART)/values.yaml

.PHONY: generate-manifests-vpc-ovn-node
generate-manifests-vpc-ovn-node: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC for VPC OVN node controller
	$(CONTROLLER_GEN) \
	paths="./cmd/vpc-ovn-node/..." \
	paths="./ovn/node/controllers/..." \
	rbac:roleName=vpc-ovn-node-role \
	output:rbac:dir=$(CONFDIR)/vpc/dpu/rbac

.PHONY: generate-manifests-ovn
generate-manifests-ovn: envsubst ## Generate values for ovn helm chart.
	$(ENVSUBST) < $(OVN_HELM_CHART)/values.yaml.tmpl > $(OVN_HELM_CHART)/values.yaml


##@ Documentation
GENERATE_DOC_TARGETS ?= helm
.PHONY: generate-docs
generate-docs: $(addprefix generate-docs-,$(GENERATE_DOC_TARGETS))

.PHONY: generate-docs-helm
generate-docs-helm: helm-docs ## Generate helm chart documentation.
	$(HELM_DOCS) --ignore-file=.helmdocsignore

##@ Testing

TESTPKGS ?= $$(go list ./... | grep -v /e2e)

.PHONY: test-build-images
test-build-images: build-ovn-central ## Build required images for unit tests

.PHONY: test
test: envtest test-build-images ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test $(TESTPKGS) $(GO_TEST_ARGS)

.PHONY: test-report
test-report: envtest gotestsum test-build-images ## Run tests and generate a junit style report
	set +o errexit; KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test -count 1 -race -json $(TESTPKGS) -coverprofile $(MAKEFILE_DIR)/vpc-cover.out > $(MAKEFILE_DIR)/vpc-junit.stdout; echo $$? > $(MAKEFILE_DIR)/vpc-junit.exitcode;
	$(GOTESTSUM) --junitfile vpc-junit.xml --raw-command cat vpc-junit.stdout
	exit $$(cat vpc-junit.exitcode)

##@ validate commit
.PHONY: commit-check
commit-check: conform ## Run conform to validate commit message
	$(CONFORM) enforce

##@ lint and verify
GOLANGCI_LINT_GOGC ?= "100"
.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	GOOS=linux GOGC=$(GOLANGCI_LINT_GOGC) $(GOLANGCI_LINT) run --timeout 5m

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	GOOS=linux $(GOLANGCI_LINT) run --fix

VERIFY_TARGETS ?= generate copyright

.PHONY: verify
verify: $(addprefix verify-,$(VERIFY_TARGETS)) ## Run all verify-* targets

.PHONY: verify-generate
verify-generate: generate ## Verify auto-generated code did not change
	$(info checking for git diff after running 'make generate')
	# Use intent-to-add to check for untracked files after generation.
	git add -N .
	$Q git diff --quiet ; if [ $$? -eq 1 ] ; then echo "Please, commit manifests after running 'make generate'"; exit 1 ; fi

.PHONY: verify-copyright
verify-copyright: ## Verify copyrights for project files
	$Q $(MAKEFILE_DIR)/hack/scripts/copyright-validation.sh


LINT_HELM_TARGETS ?= ovn vpc-ovn
.PHONY: lint-helm
lint-helm: $(addprefix lint-helm-,$(LINT_HELM_TARGETS))

.PHONY: lint-helm-ovn
lint-helm-ovn: helm ## Run helm lint for ovn chart
	$Q $(HELM) lint $(OVN_HELM_CHART)

.PHONY: lint-helm-vpc-ovn
lint-helm-vpc-ovn: helm ## Run helm lint for vpc-ovn chart
	$Q $(HELM) lint $(VPC_HELM_CHART)

##@ Release

.PHONY: release-build
release-build: generate ## Build helm and container images for release.
	# Build multiarch images which will run on both DPUs and x86 hosts.
	$(MAKE)  $(addprefix docker-build-,$(MULTI_ARCH_DOCKER_BUILD_TARGETS))

	# Package the helm charts.
	$(MAKE)  helm-package-all


# controls whether the charts should be pushed in both REGISTRY and HELM_REGISTRY, in case the two are different
export RELEASE_PUSH_HELM_CHARTS_TO_REGISTRY ?= false

.PHONY: release
release: release-build ## Build and push helm and container images for release.

	# Push all of the images
	$(MAKE)  docker-push-all

	# Push the helm charts.
	$(MAKE)  helm-push-all

ifeq ($(RELEASE_PUSH_HELM_CHARTS_TO_REGISTRY),true)
ifneq ($(HELM_REGISTRY),$(DEFAULT_HELM_REGISTRY))
	# Push the helm charts to the REGISTRY as well in case the 2 conditions are satisfied. This assumes that the REGISTRY
	# is an OCI compliant registry.
	$(MAKE)  HELM_REGISTRY=$(DEFAULT_HELM_REGISTRY) helm-push-all
endif
endif

.PHONY: warm-cache
warm-cache: ## Warm the cache for the tests.
	$(MAKE)  release-build test lint

##@ Build

# Build flags to reduce binary size:
# GO_LDFLAGS:
#  -s: strips debug information from the binary
#  -w: strips DWARF symbol table information
#  -extldflags '-static': enables static linking of external libraries
# NOTE: Using -s and -w flags will:
#  - Make debugging with delve impossible as debug information is stripped
#  - Make CPU/memory profiling with pprof impossible as DWARF symbols are removed
#  - Significantly reduce ability to get stack traces on crashes
# Consider removing these flags during development/debugging sessions
GO_LDFLAGS="-s -w -extldflags '-static' -X github.com/nvidia/doca-platform/internal/release.dpfVersion=$(TAG)"

# GO_GCFLAGS:
#  -trimpath: removes file system paths from the binary
# Example of -trimpath effect:
#  Without: /home/user/go/src/example.com/project/cmd/operator/main.go
#  With:    cmd/operator/main.go
GO_GCFLAGS="-trimpath"

VPC_SYSTEM_BUILD_TARGETS ?= vpc-ovn-controller vpc-ovn-node
BUILD_TARGETS ?= $(VPC_SYSTEM_BUILD_TARGETS)

BUILD_IMAGE ?= docker.io/library/golang:$(GO_VERSION)

HOST_ARCH = amd64
DPU_ARCH = arm64

# Use distroless as minimal base image to package the manager binary
BASE_IMAGE = nvcr.io/nvidia/doca/dpf_containers:1.0.2-ubuntu22.04-distroless
ALPINE_IMAGE = alpine:3.19

.PHONY: binaries
binaries: $(addprefix binary-,$(BUILD_TARGETS)) ## Build all binaries

.PHONY: binaries-vpc-system
binaries-vpc-system: $(addprefix binary-,$(VPC_SYSTEM_BUILD_TARGETS)) ## Build binaries for the vpc-system image.

.PHONY: binary-vpc-ovn-controller
binary-vpc-ovn-controller: ## Build ovn vpc-ovn-controller binary.
	cCGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
	-ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/vpc-ovn-controller gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/cmd/vpc-ovn-controller

.PHONY: binary-vpc-ovn-node
binary-vpc-ovn-node: ## Build vpc-ovn-node binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
	-ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/ipallocator github.com/nvidia/doca-platform/cmd/ipallocator
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
	-ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/vpc-ovn-node gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/cmd/vpc-ovn-node
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
	-ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/vpc-ovn-dpu-provisioner gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/cmd/vpc-ovn-dpu-provisioner
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
	-ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/vfmac gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/cmd/vfmac

MULTI_ARCH_DOCKER_BUILD_TARGETS = vpc-system ovn
DOCKER_BUILD_TARGETS = $(MULTI_ARCH_DOCKER_BUILD_TARGETS)

.PHONY: docker-build-all
docker-build-all: $(addprefix docker-build-,$(DOCKER_BUILD_TARGETS)) ## Build docker images for all DOCKER_BUILD_TARGETS. Architecture defaults to build system architecture unless overridden or hardcoded.

VPC_SYSTEM_IMAGE_NAME = vpc-system
export VPC_SYSTEM_IMAGE ?= $(REGISTRY)/$(VPC_SYSTEM_IMAGE_NAME)
export VPC_SYSTEM_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(VPC_SYSTEM_IMAGE_NAME)

OVN_IMAGE_NAME = ovn
export OVN_IMAGE = $(REGISTRY)/$(OVN_IMAGE_NAME)
export OVN_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(OVN_IMAGE_NAME)

VPC_SYSTEM_ARCH ?= $(HOST_ARCH) $(DPU_ARCH)

## Ubuntu mirror for building the images.
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu/

## Download and package aptitude source code for all related images.
## This is used to speedup our CI builds by disabling the source code download&package step.
## Note: not all images have additional packages, so this env does not apply to all images.
PACKAGE_SOURCES ?= true

.PHONY: docker-build-vpc-system # Build a multi-arch image for vpc System. The variable VPC_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-vpc-system: $(addprefix docker-build-vpc-system-for-,$(VPC_SYSTEM_ARCH))

docker-build-vpc-system-for-%:
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$* \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg TAG=$(TAG) \
		-f Dockerfile.vpc-system \
		../.. \
		-t $(VPC_SYSTEM_IMAGE):$(TAG)-$*

.PHONY: docker-push-vpc-system # Push a multi-arch image for VPC System using `docker manifest`. The variable VPC_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-vpc-system: $(addprefix docker-push-vpc-system-for-,$(VPC_SYSTEM_ARCH))
	docker manifest push --purge $(VPC_SYSTEM_IMAGE):$(TAG)

docker-push-vpc-system-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(VPC_SYSTEM_IMAGE):$(TAG)-$* $(VPC_SYSTEM_IMAGE):$(TAG)
	docker push $(VPC_SYSTEM_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE)  docker-create-manifest-for-vpc-system

docker-create-manifest-for-vpc-system:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(VPC_SYSTEM_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(VPC_SYSTEM_IMAGE):$(TAG))

.PHONY: docker-build-ovn # Build a multi-arch image for OVN. The variable VPC_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-ovn: $(addprefix docker-build-ovn-for-,$(VPC_SYSTEM_ARCH))

docker-build-ovn-for-%:
# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		--provenance=false \
		--platform=linux/$* \
		-f Dockerfile.ovn\
		. \
		-t $(OVN_IMAGE):$(TAG)-$*


.PHONY: docker-push-ovn # Push a multi-arch image for ovn.
docker-push-ovn: $(addprefix docker-push-ovn-for-,$(VPC_SYSTEM_ARCH))
	docker manifest push --purge $(OVN_IMAGE):$(TAG)

docker-push-ovn-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(OVN_IMAGE):$(TAG)-$* $(OVN_IMAGE):$(TAG)
	docker push $(OVN_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE)  docker-create-manifest-for-ovn

docker-create-manifest-for-ovn:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(OVN_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(OVN_IMAGE):$(TAG))

.PHONY: docker-push-all
docker-push-all: $(addprefix docker-push-,$(DOCKER_BUILD_TARGETS))  ## Push the docker images for all DOCKER_BUILD_TARGETS.

# helm charts

# By default the helm registry is assumed to be an OCI registry.
DEFAULT_HELM_REGISTRY=oci://$(REGISTRY)
# This variable should be overwritten when using a https helm repository.
export HELM_REGISTRY ?= $(DEFAULT_HELM_REGISTRY)

HELM_TARGETS ?= ovn vpc-ovn

# metadata for ovn chart.
export OVN_HELM_CHART_NAME = ovn-chart
OVN_HELM_CHART ?= $(DPUSERVICESDIR)/ovnvpc/ovn/chart

# metadata for VPC chart.
VPC_HELM_CHART_NAME = dpf-vpc-ovn
VPC_HELM_CHART ?= $(DPUSERVICESDIR)/ovnvpc/vpc/chart

.PHONY: helm-package-all
helm-package-all: $(addprefix helm-package-,$(HELM_TARGETS))  ## Package the helm charts for all components.

.PHONY: helm-package-ovn
helm-package-ovn: $(DPUSERVICESDIR) $(CHARTSDIR) helm yq generate-manifests-ovn ## Package helm chart for ovn
	HELM_CHART_DIR="$(OVN_HELM_CHART)" \
	HELM_CHART_TAGS="$(TAG)" \
	../..//hack/scripts/release-helm-package.sh

.PHONY: helm-package-vpc-ovn
helm-package-vpc-ovn: $(DPUSERVICESDIR) $(CHARTSDIR) helm yq generate-manifests-vpc-ovn-controller ## Package helm chart for vpc-ovn
	HELM_CHART_DIR="$(VPC_HELM_CHART)" \
	HELM_CHART_TAGS="$(TAG)" \
	../../hack/scripts/release-helm-package.sh

HELM_CM_PUSH_OPTS ?=
HELM_PUSH_CMD ?= $(shell if echo $(HELM_REGISTRY) | grep -q '^http'; then echo cm-push $(HELM_CM_PUSH_OPTS); else echo push; fi)

.PHONY: helm-push-all
helm-push-all: $(addprefix helm-push-,$(HELM_TARGETS))  ## Push the helm charts for all components.

.PHONY: helm-push-ovn
helm-push-ovn: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for ovn
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(OVN_HELM_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)

.PHONY: helm-push-vpc-ovn
helm-push-vpc-ovn: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for vpc-ovn
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(VPC_HELM_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)

.PHONY: build-ovn-central
build-ovn-central: ## Build OVN central Docker image
	docker build -t ovn-central:$(OVN_SCHEMA_VERSION) -f ./Dockerfile.ovn .

.PHONY: run-ovn-central
run-ovn-central: ## Run OVN central Docker container (not in use - for developing purposes)
	docker run --rm --name ovn-central -d -p 6641:6641 ovn-central:$(OVN_SCHEMA_VERSION) bash -c \
	"/usr/share/ovn/scripts/ovn-ctl --db-nb-create-insecure-remote=yes \
	--db-sb-create-insecure-remote=yes start_northd && tail -f /dev/null"

.PHONY: prepare-ovn-central
prepare-ovn-central: build-ovn-central run-ovn-central ## Build and run OVN central (not in use - for developing purposes)

.PHONY: test-ovn-central
test-ovn-central: build-ovn-central ## Run OVN central tests, this run containarized setup within the testsuite
	OVN_CENTRAL_IMAGE_NAME=ovn-central:$(OVN_SCHEMA_VERSION) go test ./ovn/ovnlib/... -v -ginkgo.v \
		-ginkgo.focus="$(FOCUS)"

.PHONY: clear-ovn-central
clear-ovn-central: ## Stop OVN central Docker container (not in use - for developing purposes)
	docker stop ovn-central

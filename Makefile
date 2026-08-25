#
#Copyright 2024 NVIDIA
#
#Licensed under the Apache License, Version 2.0 (the "License");
#you may not use this file except in compliance with the License.
#You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
#Unless required by applicable law or agreed to in writing, software
#distributed under the License is distributed on an "AS IS" BASIS,
#WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#See the License for the specific language governing permissions and
#limitations under the License.

# Get the directory of this Makefile, regardless of where make was invoked
# This must be done BEFORE including other Makefiles
# Get the Makefile's directory as an absolute path while preserving symlinks
PROJECT_DIR := $(shell cd $(dir $(lastword $(MAKEFILE_LIST))) && pwd -L)
# Remove any trailing slash for consistency
# Example: $(dir ...) returns "/path/to/project/" but we want "/path/to/project"
PROJECT_DIR := $(patsubst %/,%,$(PROJECT_DIR))

GO_VERSION ?= $(shell awk '/^toolchain /{print $$2}' go.mod | awk -F 'go' '{print $$2}')
GOTOOLCHAIN ?= go$(GO_VERSION)+auto

## Include Make modules which are split up in this repo for better structure.
include hack/tools/tools.mk

PROJECT_NAME="DOCA Platform Framework"
PROJECT_REPO="https://github.com/NVIDIA/doca-platform"
export DATE="$(shell date --rfc-3339=seconds)"
export FULL_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Export is needed here so that the envsubst used in make targets has access to those variables even when they are not
# explicitly set when calling make.
# The tag must have three digits with a leading v - i.e. v9.9.1
export TAG ?= v0.1.0
# Note: Registry defaults to non-existing registry intentionally to avoid overriding useful images.
export REGISTRY ?= example.com
# This variable should be overwritten with the registry of the upstream artifacts. Needed when making a release upstream.
# This variable ensures that the values injected in the operator and charts point to the upstream artifacts.
export UPSTREAM_REGISTRY ?= $(REGISTRY)

# The latest stable tag is used in various places to refer to the latest stable release of DPF.
LATEST_STABLE_TAG = v25.10.1

# If V is set to 1 the output will be verbose.
Q = $(if $(filter 1,$V),,@)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.32.0

# Get the current OS and Architecture
ARCH ?= $(shell go env GOARCH)
OS ?= $(shell go env GOOS)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Allows for defining additional Go test args, e.g. '-tags integration'.
# The linkmode=internal flag is used to force using Go linker to do the linking.
# This suppresses warnings like ".../00NNNN.o has malformed LC_DYSYMTAB".
# See the following issue for more details: https://github.com/golang/go/issues/61229#issuecomment-1988965927
GO_TEST_ARGS ?= -race -ldflags=-linkmode=internal

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
REPOSDIR ?= $(PROJECT_DIR)/hack/repos
HELMDIR ?= $(PROJECT_DIR)/deploy/charts
CRDDIR ?= $(HELMDIR)/dpf-operator/templates/crds
THIRDPARTYDIR ?= $(PROJECT_DIR)/third_party/forked
EXAMPLE ?= $(PROJECT_DIR)/example

$(LOCALBIN) $(CHARTSDIR) $(DPUSERVICESDIR) $(REPOSDIR):
	@mkdir -p $@

.PHONY: clean
clean: ; $(info  Cleaning...)	 @ ## Clean non-essential files from the repo
	@rm -rf $(CHARTSDIR)
	@rm -rf $(TOOLSDIR)
	@rm -rf $(REPOSDIR)

# Note: This helps resolve errors with `docker manifest create`
.PHONY: clean-images-for-registry
clean-images-for-registry: ## Clean release deletes local images with the $REGISTRY
	for image in $$(docker images $$REGISTRY/* --format "{{.ID}}"); do \
	docker rmi -f $$image ; \
	done

##@ Dependencies

DOCA_SOSREPORT_REPO_URL=https://github.com/NVIDIA/doca-sosreport/archive/$(DOCA_SOSREPORT_REF).tar.gz
DOCA_SOSREPORT_REF=6b4289b9f0d9f26af177b0d1c4c009ca74bb514a
SOS_REPORT_DIR=$(REPOSDIR)/doca-sosreport-$(DOCA_SOSREPORT_REF)
$(SOS_REPORT_DIR): | $(REPOSDIR)
	curl -sL ${DOCA_SOSREPORT_REPO_URL} | tar -xz -C ${REPOSDIR}

# nvidia-external-attacher dependencies to be able to build its docker image
EXTERNAL_ATTACHER_BRANCH=release-4.11
NVIDIA_EXTERNAL_ATTACHER_DIR=third_party/forked/nvidia-external-attacher

# Image for the SR-IOV device plugin, deployed by the NodeSRIOVDevicePlugin controller in the host cluster
export NODE_SRIOV_DEVICE_PLUGIN_IMAGE=nvcr.io/nvidia/mellanox/sriov-network-device-plugin
export NODE_SRIOV_DEVICE_PLUGIN_TAG=network-operator-v26.1.0

# Kata Containers kata-deploy image, deployed as a dpu-networking subchart
export KATA_DEPLOY_IMAGE=quay.io/kata-containers/kata-deploy
export KATA_DEPLOY_TAG=3.32.0@sha256:47c4cf29c9e8cf9bb7debab58761b672e1d4c93ee5e92db7b442ee00e7d561cc

# VPC dependencies to be able to build/push images and charts
VPC_REF=640274cd6ef3bc02c632201ccd0b5ef13cde3025
VPC_DIR=$(REPOSDIR)/ovn-vpc/ovn-vpc-$(VPC_REF)
# Token used for gitlab reporistory access, usually needed for CI/CD pipelines.
# dev envs usually have those set in git credentials.
GITLAB_CLONE_TOKEN?=
$(VPC_DIR): | $(REPOSDIR)
	if [ -z "$(GITLAB_CLONE_TOKEN)" ]; then \
		git clone https://gitlab-master.nvidia.com/doca-platform-foundation/dpf-vpc.git $(VPC_DIR)-tmp; \
	else \
		git clone https://token:$(GITLAB_CLONE_TOKEN)@gitlab-master.nvidia.com/doca-platform-foundation/dpf-vpc.git $(VPC_DIR)-tmp; \
	fi
	cd $(VPC_DIR)-tmp && git reset --hard $(VPC_REF)
	mv $(VPC_DIR)-tmp $(VPC_DIR)
	# delete old ovn-vpc directories.
	find $(REPOSDIR)/ovn-vpc/ -mindepth 1 -maxdepth 1 -not -name ovn-vpc-$(VPC_REF) -exec rm -rf '{}' \;

##@ GRPC

# go package for generated code
API_PKG_GO_MOD ?= github.com/nvidia/doca-platform/api/grpc

## Temporary location for GRPC files
GRPC_TMP_DIR  ?= $(PROJECT_DIR)/_tmp
$(GRPC_TMP_DIR):
	@mkdir -p $@

# GRPC DIRs
GRPC_DIR ?= $(PROJECT_DIR)/api/grpc
PROTO_DIR ?= $(GRPC_DIR)/proto
GENERATED_CODE_DIR ?= $(GRPC_DIR)

.PHONY: grpc-generate
grpc-generate: protoc protoc-gen-go protoc-gen-go-grpc ## Generate GO client and server GRPC code
	@echo "generate GRPC API"; \
	echo "   go module: $(API_PKG_GO_MOD)"; \
	echo "   output dir: $(GENERATED_CODE_DIR) "; \
	echo "   proto dir: $(PROTO_DIR) "; \
	cd $(PROTO_DIR) && \
	TARGET_FILES=""; \
	PROTOC_OPTIONS="--plugin=protoc-gen-go=$(PROTOC_GEN_GO) \
					--plugin=protoc-gen-go-grpc=$(PROTOC_GEN_GO_GRPC) \
					--go_out=$(GENERATED_CODE_DIR) \
					--go_opt=module=$(API_PKG_GO_MOD) \
					--proto_path=$(PROTO_DIR) \
					--go-grpc_out=$(GENERATED_CODE_DIR) \
					--go-grpc_opt=module=$(API_PKG_GO_MOD)"; \
	echo "discovered proto files:"; \
	for proto_file in $$(find . -name "*.proto"); do \
		proto_file=$$(echo $$proto_file | cut -d'/' -f2-); \
		proto_dir=$$(dirname $$proto_file); \
		pkg_name=M$$proto_file=$(API_PKG_GO_MOD)/$$proto_dir; \
		echo "    $$proto_file"; \
		TARGET_FILES="$$TARGET_FILES $$proto_file"; \
		PROTOC_OPTIONS="$$PROTOC_OPTIONS \
						--go_opt=$$pkg_name \
						--go-grpc_opt=$$pkg_name" ; \
	done; \
	$(PROTOC) $$PROTOC_OPTIONS $$TARGET_FILES

.PHONY: grpc-check
grpc-check: grpc-format grpc-lint protoc protoc-gen-go protoc-gen-go-grpc $(GRPC_TMP_DIR)  ## Check that generated GO client code match proto files
	@rm -rf $(GRPC_TMP_DIR)/nvidia/
	@$(MAKE) GENERATED_CODE_DIR=$(GRPC_TMP_DIR) grpc-generate
	@diff -Naur $(GRPC_TMP_DIR)/nvidia/ $(GENERATED_CODE_DIR)/nvidia/ || \
		(printf "\n\nOutdated files detected!\nPlease, run 'make generate' to regenerate GO code\n\n" && exit 1)
	@echo "generated files are up to date"

.PHONY: grpc-lint
grpc-lint: buf  ## Lint GRPC files
	@echo "lint protobuf files";
	cd $(PROTO_DIR) && \
	$(BUF) lint --config ../buf.yaml .

.PHONY: grpc-format
grpc-format: buf  ## Format GRPC files
	@echo "format protobuf files";
	cd $(PROTO_DIR) && \
	$(BUF) format -w --exit-code

##@ Development
GENERATE_TARGETS ?= dpuservice provisioning servicechainset sfc-controller vpc-crds operator \
	operator-embedded release-defaults kamaji-cluster-manager static-cluster-manager \
	storage mock-dms nodesriovdeviceplugin

.PHONY: generate
generate: ## Run all generate-* targets: generate-modules generate-manifests-* and generate-go-deepcopy-*.
	$(MAKE) generate-mocks generate-modules generate-manifests generate-go-deepcopy generate-docs generate-client-for-storage-nvidia-external-attacher generate-deprecation-warnings

.PHONY: generate-mocks
generate-mocks: mockgen ## Generate mocks
	## Prepend the TOOLSDIR to the path for this command as `mockgen` is called from the $PATH inline in the code.
	## The DPF TOOLSDIR should be first in the path to ensure user tools are not used.
	## See go:generate comments for examples.

	export PATH="$(TOOLSDIR):$(PATH)"; go generate $$(go list ./...)

.PHONY: generate-modules
generate-modules: ## Run go mod tidy to update go modules
	go mod tidy

.PHONY: generate-manifests
generate-manifests: $(addprefix generate-manifests-,$(GENERATE_TARGETS)) ## Run all generate-manifests-* targets

.PHONY: generate-manifests-operator
generate-manifests-operator: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC. for the operator controller.
	$(MAKE) clean-generated-yaml SRC_DIRS=$(CRDDIR)
	$(CONTROLLER_GEN) \
	paths="./cmd/operator/..." \
	paths="./cmd/kamaji-cluster-manager/..." \
	paths="./cmd/static-cluster-manager/..." \
	paths="./internal/operator/..." \
	paths="./internal/clustermanager/..." \
	paths="./internal/provisioning/controllers/..." \
	paths="./internal/provisioning/bfbregistry/..." \
	paths="./internal/provisioning/webhooks/..." \
	paths="./internal/provisioning/utils/..." \
	paths="./api/operator/..." \
	crd:crdVersions=v1 \
	rbac:roleName="dpf-operator-manager-role" \
	output:crd:dir=./config/operator-crds \
	output:rbac:dir=./deploy/charts/dpf-operator/templates
	## Copy CRD definitions to the operator helm directory
	$(KUSTOMIZE) build config/operator-crds -o $(CRDDIR);

.PHONE: generate-manifests-mock-dms
generate-manifests-mock-dms: controller-gen
	$(CONTROLLER_GEN) \
	paths="./test/mock/dms/..." \
	rbac:roleName=mock-dms-manager-role \
	output:rbac:dir=./test/mock/dms/chart/templates/

.PHONY: generate-manifests-dpuservice
generate-manifests-dpuservice: controller-gen ## Generate manifests e.g. CRD, RBAC. for the dpuservice controller.
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/dpuservice/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./cmd/dpuservice/..." \
	paths="./internal/dpuservice/..." \
	paths="./internal/dpuservicechain/..." \
	paths="./api/dpuservice/..." \
	crd:crdVersions=v1 \
	rbac:roleName=manager-role \
	output:crd:dir=./config/dpuservice/crd/bases \
	output:rbac:dir=./config/dpuservice/rbac \
	output:webhook:dir=./config/dpuservice/webhook \
	webhook

.PHONY: generate-manifests-servicechainset
generate-manifests-servicechainset: controller-gen kustomize envsubst ## Generate manifests e.g. CRD, RBAC. for the servicechainset controller.
	# TODO: Clean up pod-ipam-injector generation
	$(CONTROLLER_GEN) \
	paths="./cmd/servicechainset/..." \
	paths="./internal/servicechainset/..." \
	paths="./internal/pod-ipam-injector/..." \
	rbac:roleName=servicechainset-controller-manager \
	output:rbac:dir=deploy/charts/dpu-networking/charts/servicechainset-controller/templates;
	# Make the CRDs to only be deployed if the deployDPUManifests flag is set.
	find config/dpuservice/crd/bases/ -type f -not -name '*_dpu*' -exec cp {} deploy/charts/dpu-networking/charts/servicechainset-controller/templates/crds/ \;
	sed -i -e '1i{{- if .Values.deployDPUManifests }}' -e '$$a{{- end }}' deploy/charts/dpu-networking/charts/servicechainset-controller/templates/crds/*.yaml

	# Make the role.yaml compatible with the chart design so that multiple charts can be deployed and the manifest is skipped in specific cases.
	sed -i 's/name: servicechainset-controller-manager/name: {{ include "servicechain.fullname" . }}/g' deploy/charts/dpu-networking/charts/servicechainset-controller/templates/role.yaml
	sed -i -e '1i{{ if .Values.deployDPUManifests }}' -e '$$a{{- end }}' deploy/charts/dpu-networking/charts/servicechainset-controller/templates/role.yaml

.PHONY: generate-manifests-storage
generate-manifests-storage: controller-gen kustomize embedmd yq ## Generate CRDs for SNAP storage in DPU cluster
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/storage/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./api/storage/..." \
	crd:crdVersions=v1,generateEmbeddedObjectMeta=true \
	output:crd:dir=./config/storage/crd/bases
	rm -rf $(STORAGE_CHART)/templates/crd && mkdir -p $(STORAGE_CHART)/templates/crd
	@for f in config/storage/crd/bases/*.yaml; do \
		if echo $$(basename "$$f") | grep -qv "nvidia.com_dpu"; then \
			cp "$$f" $(STORAGE_CHART)/templates/crd/; \
		fi \
	done
	@for f in $(STORAGE_CHART)/templates/crd/*.yaml; do \
		(echo "{{- if .Values.dpu.deployCrds }}" && cat "$$f" && echo "{{- end }}") > "$$f.tmp" && mv "$$f.tmp" "$$f"; \
	done
	## Set the image names and tags for storage-related charts
	$(ENVSUBST) < $(STORAGE_CHART)/values.yaml.tmpl > $(STORAGE_CHART)/values.yaml
	cd $(DPUSERVICESDIR)/storage/examples/_src/ && ./update.sh
	grep -rl --include \*.md -e '\[embedmd\]' $(DPUSERVICESDIR)/storage/examples | xargs $(EMBEDMD) -w

RELEASE_FILE = ./internal/release/manifests/defaults.yaml

.PHONY: generate-manifests-release-defaults
generate-manifests-release-defaults: envsubst ## Generates manifests that contain the default values that should be used by the operators
	mkdir -p ./build
	$(ENVSUBST) < ./internal/release/templates/defaults.yaml.tmpl > $(RELEASE_FILE)
	## Copy the generated release defaults to the build directory to be able to copy them during docker build.
	## This is needed as the internal/release directory is not in the docker build context.
	cp $(RELEASE_FILE) ./build/defaults.yaml

TEMPLATES_DIR ?= $(PROJECT_DIR)/internal/operator/inventory/templates
EMBEDDED_MANIFESTS_DIR ?= $(PROJECT_DIR)/internal/operator/inventory/manifests
.PHONY: generate-manifests-operator-embedded
generate-manifests-operator-embedded: kustomize envsubst generate-manifests-dpuservice generate-manifests-provisioning generate-manifests-hostagent generate-manifests-release-defaults generate-manifests-kamaji-cluster-manager generate-manifests-static-cluster-manager generate-manifests-nodesriovdeviceplugin ## Generates manifests that are embedded into the operator binary.
	# Reorder none here ensure that we generate the kustomize files in a specific order to be consumed by the DPF Operator.
	$(KUSTOMIZE) build --reorder=none config/provisioning/default > $(EMBEDDED_MANIFESTS_DIR)/provisioning-controller.yaml
	$(KUSTOMIZE) build --reorder=none config/dpu-detector > $(EMBEDDED_MANIFESTS_DIR)/dpu-detector.yaml
	$(KUSTOMIZE) build --reorder=none config/dpuservice/default > $(EMBEDDED_MANIFESTS_DIR)/dpuservice-controller.yaml
	$(KUSTOMIZE) build --reorder=none config/kamaji-cluster-manager/default > $(EMBEDDED_MANIFESTS_DIR)/kamaji-cluster-manager.yaml
	$(KUSTOMIZE) build --reorder=none config/static-cluster-manager/default > $(EMBEDDED_MANIFESTS_DIR)/static-cluster-manager.yaml
	$(KUSTOMIZE) build --reorder=none config/bfb_registry > $(EMBEDDED_MANIFESTS_DIR)/bfb-registry.yaml
	$(KUSTOMIZE) build --reorder=none config/nodesriovdeviceplugin/default > $(EMBEDDED_MANIFESTS_DIR)/nodesriovdeviceplugin-controller.yaml

.PHONY: generate-manifests-sfc-controller
generate-manifests-sfc-controller: envsubst generate-manifests-servicechainset
	cp config/dpuservice/crd/bases/svc.dpu.nvidia.com_servicechains.yaml deploy/charts/dpu-networking/charts/sfc-controller/templates/crds/
	cp config/dpuservice/crd/bases/svc.dpu.nvidia.com_serviceinterfaces.yaml deploy/charts/dpu-networking/charts/sfc-controller/templates/crds/
	cp config/dpuservice/crd/bases/svc.dpu.nvidia.com_nodeserviceinterfaces.yaml deploy/charts/dpu-networking/charts/sfc-controller/templates/crds/

.PHONY: generate-manifests-provisioning
generate-manifests-provisioning: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC. for the DPF provisioning controller.
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/provisioning/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./cmd/provisioning/..." \
	paths="./internal/provisioning/controllers/..." \
	paths="./internal/provisioning/bfbregistry/..." \
	paths="./internal/provisioning/webhooks/..." \
	paths="./internal/provisioning/utils/..." \
	paths="./api/provisioning/..." \
	crd:crdVersions=v1,generateEmbeddedObjectMeta=true \
	rbac:roleName=manager-role \
	output:crd:dir=./config/provisioning/crd/bases \
	output:rbac:dir=./config/provisioning/rbac \
	output:webhook:dir=./config/provisioning/webhook \
	webhook

.PHONY: generate-manifests-hostagent
generate-manifests-hostagent: controller-gen ## Generate RBAC for the hostagent.
	@rm -rf /tmp/hostagent-rbac && mkdir -p /tmp/hostagent-rbac
	$(CONTROLLER_GEN) \
	paths="./internal/provisioning/hostagent/..." \
	rbac:roleName=hostagent-role \
	output:rbac:dir=/tmp/hostagent-rbac
	mv /tmp/hostagent-rbac/role.yaml ./config/provisioning/rbac/hostagent_role.yaml

.PHONY: generate-manifests-kamaji-cluster-manager
generate-manifests-kamaji-cluster-manager: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC. for the DPF provisioning controller.
	$(CONTROLLER_GEN) \
	paths="./cmd/kamaji-cluster-manager/..." \
	paths="./internal/clustermanager/controller/..." \
	paths="./internal/clustermanager/kamaji/..." \
	rbac:roleName=manager-role \
	output:rbac:dir=./config/kamaji-cluster-manager/rbac

.PHONY: generate-manifests-static-cluster-manager
generate-manifests-static-cluster-manager: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC. for the DPF provisioning controller.
	$(CONTROLLER_GEN) \
	paths="./cmd/static-cluster-manager/..." \
	paths="./internal/clustermanager/controller/..." \
	paths="./internal/clustermanager/static/..." \
	rbac:roleName=manager-role \
	output:rbac:dir=./config/static-cluster-manager/rbac

.PHONY: generate-manifests-vpc-crds
generate-manifests-vpc-crds: controller-gen kustomize ## Generate manifests for VPC (CRDs)
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/vpc/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./api/vpc/..." \
	crd:crdVersions=v1 \
	output:crd:dir=./config/vpc/crd/bases

.PHONY: generate-manifests-nodesriovdeviceplugin
generate-manifests-nodesriovdeviceplugin: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC for nodesriovdeviceplugin controller.
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/nodesriovdeviceplugin/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./cmd/nodesriovdeviceplugin/controller/..." \
	paths="./internal/nodesriovdeviceplugin/controllers/..." \
	paths="./internal/nodesriovdeviceplugin/webhooks/..." \
	paths="./api/noderesources/..." \
	crd:crdVersions=v1,generateEmbeddedObjectMeta=true \
	rbac:roleName=manager-role \
	output:crd:dir=./config/nodesriovdeviceplugin/crd/bases \
	output:rbac:dir=./config/nodesriovdeviceplugin/rbac \
	output:webhook:dir=./config/nodesriovdeviceplugin/webhook \
	webhook


DEPRECATION_WARNINGS_DIR = deploy/charts/dpf-operator/templates/deprecation-warnings
.PHONY: generate-deprecation-warnings
generate-deprecation-warnings: binary-dpfdev ## Generate ValidatingAdmissionPolicy resources for deprecated CRD fields
	rm -rf $(DEPRECATION_WARNINGS_DIR)
	$(LOCALBIN)/dpfdev generate-deprecation-warnings \
		--crd-dir deploy/charts/dpf-operator/templates/crds \
		--output-dir $(DEPRECATION_WARNINGS_DIR)

.PHONY: clean-generated-yaml
clean-generated-yaml: ## Remove files generated by controller-tools from the mentioned dirs.
	(IFS=','; for i in $(SRC_DIRS); do find $$i -type f -name '*.yaml' -exec rm -f {} \;; done)

.PHONY: generate-go-deepcopy
generate-go-deepcopy: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(MAKE) clean-generated-deepcopy SRC_DIRS="./api"
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: clean-generated-deepcopy
clean-generated-deepcopy: ## Remove files generated by golang from the mentioned dirs.
	(IFS=','; for i in $(SRC_DIRS); do find $$i -type f -name 'zz_generated.deepcopy*' -exec rm -f {} \;; done)

##@ Documentation
GENERATE_DOC_TARGETS ?= mdtoc api helm embedmd
.PHONY: generate-docs
generate-docs: $(addprefix generate-docs-,$(GENERATE_DOC_TARGETS))
	$(MAKE)

generate-docs-mdtoc: mdtoc ## Generate table of contents for our documentation.
	@files=$$(grep -rl -e '<!-- toc -->' docs | grep '\.md$$' || true); \
	if [ -n "$$files" ]; then \
		echo "$$files" | xargs $(MDTOC) --inplace; \
	else \
		echo "No files with TOC markers found, skipping mdtoc"; \
	fi

.PHONY: generate-docs-api
generate-docs-api: gen-crd-api-reference-docs ## Generate docs for the API.
	$(GEN_CRD_API_REFERENCE_DOCS) --renderer=markdown --source-path=api --config=hack/tools/api-docs/config.yaml --output-path=docs/public/developer-guides/api/api.md.tmp
	@echo '---' > docs/public/developer-guides/api/api.md
	@echo 'title: "API reference"' >> docs/public/developer-guides/api/api.md
	@echo '---' >> docs/public/developer-guides/api/api.md
	@echo '' >> docs/public/developer-guides/api/api.md
	@cat docs/public/developer-guides/api/api.md.tmp >> docs/public/developer-guides/api/api.md
	@rm docs/public/developer-guides/api/api.md.tmp

.PHONY: generate-docs-helm
generate-docs-helm: helm-docs yq ## Generate helm chart documentation.
	## Generate helm docs for all charts in the helm directory.
	## --chart-search-root uses the resolved path because PROJECT_DIR keeps the symlink (pwd -L).
	$(HELM_DOCS) --chart-search-root=$(realpath $(PROJECT_DIR)) --ignore-file=.helmdocsignore

.PHONY: generate-docs-embedmd
generate-docs-embedmd: embedmd ## Embed additional files into markdown docs.
	grep -rl --include \*.md -e '\[embedmd\]' docs | xargs $(EMBEDMD) -w

.PHONY: init-external-attacher-submodule
init-external-attacher-submodule: ## Initialize external-attacher submodule if needed
	@if ! git rev-parse --git-dir >/dev/null 2>&1; then \
		echo "Not a functional git repo, initializing an empty git repo for the external-attacher submodule..."; \
		git -C $(NVIDIA_EXTERNAL_ATTACHER_DIR)/external-attacher init; \
	else \
		echo "Git repo present, updating submodules..."; \
		git submodule update --init --recursive; \
	fi


.PHONY: generate-client-for-storage-nvidia-external-attacher
generate-client-for-storage-nvidia-external-attacher: init-external-attacher-submodule client-gen lister-gen informer-gen deepcopy-gen # Generate client/lister/informer for sv-volumeattachment
	rm -rf $(NVIDIA_EXTERNAL_ATTACHER_DIR)/api/storage/v1alpha1/zz_generated.deepcopy.go
	rm -rf $(NVIDIA_EXTERNAL_ATTACHER_DIR)/client

	$(DEEPCOPY_GEN) --go-header-file hack/boilerplate.go.txt \
	--output-file zz_generated.deepcopy.go \
	github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/api/storage/v1alpha1

	$(CLIENT_GEN) --clientset-name versioned \
	--input-base $(PROJECT_DIR) --input third_party/forked/nvidia-external-attacher/api/storage/v1alpha1 \
	--output-dir $(NVIDIA_EXTERNAL_ATTACHER_DIR)/client/clientset \
	--output-pkg github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/client/clientset \
	--go-header-file hack/boilerplate.go.txt

	$(LISTER_GEN) $(PROJECT_DIR)/third_party/forked/nvidia-external-attacher/api/storage/v1alpha1 \
	--output-dir $(NVIDIA_EXTERNAL_ATTACHER_DIR)/client/listers \
	--output-pkg github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/client/listers \
	--go-header-file hack/boilerplate.go.txt

	$(INFORMER_GEN) $(PROJECT_DIR)/third_party/forked/nvidia-external-attacher/api/storage/v1alpha1 \
	--versioned-clientset-package github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/client/clientset/versioned \
	--listers-package github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/client/listers \
	--output-dir $(NVIDIA_EXTERNAL_ATTACHER_DIR)/client/informers \
	--output-pkg github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/client/informers \
	--go-header-file hack/boilerplate.go.txt

.PHONY: verify-shfmt
verify-shfmt: $(SHFMT) ## Check shell scripts are formatted
	@find . -name '*.sh' \
	  -not -path './hack/repos/*' \
	  -not -path './third_party/*' \
	  -not -path './.gocache/*' \
	  -exec $(SHFMT) -l -bn -sr {} + | \
	{ \
	  files=$$(cat); \
	  [ -z "$$files" ] && echo "All shell scripts are properly formatted" && exit 0; \
	  echo "ERROR: The following shell scripts require formatting:"; \
	  echo "$$files"; \
	  echo "$$files" | xargs -n1 $(SHFMT) -w -bn -sr; \
	  echo "Files have been formatted. Please commit the changes."; \
	  exit 1; \
	}

##@ Testing

TESTPKGS ?= $$(go list ./... | grep -v /e2e | grep -v /third_party) ./test/e2e/cleanup/...
COVERPKGS ?= $$(go list ./... | grep -v /e2e | grep -v /third_party | tr '\n' ',')
# Packages that contain build-tagged (`//go:build benchmark`) benchmark specs.
# Normalized to a leading ./ so `go test` treats them as directories, not import paths.
BENCHMARK_PKGS ?= $$(grep -rl 'go:build benchmark' --include='*_test.go' . | grep -v '/third_party/' | xargs -n1 dirname | sort -u | sed 's|^\./||;s|^|./|')

.PHONY: test
test: envtest test-benchmark ## Run tests (incl. benchmarks).
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test -timeout 0 $(TESTPKGS) $(GO_TEST_ARGS)

.PHONY: test-report
test-report: envtest gotestsum test-benchmark ## Run tests and generate a junit style report
	set +o errexit; GOTOOLCHAIN=$(GOTOOLCHAIN) KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test -timeout 0 -count 1 -race -json $(TESTPKGS) -coverprofile cover.out -coverpkg=$(COVERPKGS) > junit.stdout; echo $$? > junit.exitcode;
	$(GOTESTSUM) --junitfile junit.xml --raw-command cat junit.stdout
	exit $$(cat junit.exitcode)

.PHONY: test-benchmark
test-benchmark: envtest ## Run the build-tagged gmeasure benchmarks only
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test -timeout 0 -count 1 -tags benchmark $(BENCHMARK_PKGS) -args -ginkgo.focus='benchmarks' -ginkgo.v

.PHONY: test-release-e2e-quick
test-release-e2e-quick: # Build images required for the quick DPF e2e test.
	# Only the local architecture is needed here, so override the image's
	# architecture list to keep the build single-arch.
	$(MAKE) docker-build-and-push-dpf-system DPF_SYSTEM_ARCH=$(ARCH)
	$(MAKE) docker-build-and-push-dummydpuservice
	$(MAKE) docker-build-and-push-mock-dms
	$(MAKE) docker-build-and-push-bfb-registry DPF_SYSTEM_ARCH=$(ARCH)
	$(MAKE) docker-build-and-push-keepalived DPF_SYSTEM_ARCH=$(ARCH)
	# Build and push all the helm charts
	$(MAKE) helm-package-all helm-push-all
	$(MAKE) helm-package-dummydpuservice helm-push-dummydpuservice

.PHONY: test-helper-images
test-helper-images: # Build and push the e2e test-helper images and charts.
	$(MAKE) docker-build-and-push-dummydpuservice \
		docker-build-and-push-netutils \
		docker-build-and-push-fake-fs-storage \
		docker-build-and-push-rshim-console-collector \
		helm-package-dummydpuservice
	# The chart push has to wait for the packaging above to complete.
	$(MAKE) helm-push-dummydpuservice

.PHONY: test-release-e2e-slow
test-release-e2e-slow: ## Build images required for the slow DPF e2e tests.
	$(MAKE) release
	# release already records scope=release artifacts; also record test helpers.
	$(MAKE) RELEASE_MANIFEST_ENABLED=true test-helper-images

TEST_CLUSTER_NAME := dpf-test
ADD_CONTROL_PLANE_TAINTS ?= true
TEST_DEPLOY_PREREQS_NAMESPACE ?= dpf-operator-system
.PHONY: test-env-e2e
test-env-e2e: kind helm ## Setup a Kind Kubernetes environment to run tests.
	# Create a kind cluster to host the test.
	CLUSTER_NAME=$(TEST_CLUSTER_NAME) KIND_BIN=$(KIND) ADD_CONTROL_PLANE_TAINTS=$(ADD_CONTROL_PLANE_TAINTS) $(CURDIR)/hack/scripts/kind-install.sh

	$(KUBECTL) get namespace dpf-operator-system || $(KUBECTL) create namespace dpf-operator-system

	# Create secrets required for using artefacts if required.
	$(CURDIR)/hack/scripts/create-artefact-secrets.sh

.PHONY: clean-test-env
clean-test-env: kind ## Clean Kind test environment (delete Kind cluster)
	$(KIND) delete cluster --name $(TEST_CLUSTER_NAME)


OPERATOR_NAMESPACE ?= dpf-operator-system
HELMFILE_ENV ?=
NFD_WAIT ?= true
# Enable dependencies required by the default test-e2e label filter so the tests
# can be deployed and run directly through the documented Makefile workflow.
# HELMFILE_STATE_VALUES_SET remains a full override for other test configurations.
# The argo-cd and kamaji namespaces come from a state value instead of the release definition,
# so the sed below cannot move them. They are appended after any override to keep all
# prerequisites in TEST_DEPLOY_PREREQS_NAMESPACE.
TEST_DEPLOY_OPERATOR_HELMFILE_STATE_VALUES_SET = $(if $(HELMFILE_STATE_VALUES_SET),$(HELMFILE_STATE_VALUES_SET),openbao.enabled=true,external-secrets.enabled=true),namespaces.argoCD=$(TEST_DEPLOY_PREREQS_NAMESPACE),namespaces.kamaji=$(TEST_DEPLOY_PREREQS_NAMESPACE)
.PHONY: test-deploy-operator-helm
test-deploy-operator-helm: helm helm-package-operator ## Deploy the DPF Operator using helm
	# Deploy the DPF Operator prerequisites.
	sed "s/dpf-operator-system/$(TEST_DEPLOY_PREREQS_NAMESPACE)/g" $(CURDIR)/deploy/helmfiles/prereqs.yaml > $(CURDIR)/deploy/helmfiles/prereqs.yaml.tmp
ifeq ($(NFD_WAIT),false)
	# When NFD_WAIT=false, deploy all prereqs except NFD first, then deploy NFD without waiting.
	# This is needed when OVN Kubernetes is the CNI because NFD cannot become ready without a functioning CNI.
	$(MAKE) HELMFILE_FILE=$(CURDIR)/deploy/helmfiles/prereqs.yaml.tmp HELMFILE_SELECTOR="app!=node-feature-discovery" HELMFILE_STATE_VALUES_SET="$(TEST_DEPLOY_OPERATOR_HELMFILE_STATE_VALUES_SET)" test-deploy-helmfile
	$(MAKE) HELMFILE_FILE=$(CURDIR)/deploy/helmfiles/prereqs.yaml.tmp HELMFILE_WAIT=false HELMFILE_SELECTOR="app=node-feature-discovery" HELMFILE_STATE_VALUES_SET="$(TEST_DEPLOY_OPERATOR_HELMFILE_STATE_VALUES_SET)" test-deploy-helmfile
else
	$(MAKE) HELMFILE_FILE=$(CURDIR)/deploy/helmfiles/prereqs.yaml.tmp HELMFILE_STATE_VALUES_SET="$(TEST_DEPLOY_OPERATOR_HELMFILE_STATE_VALUES_SET)" test-deploy-helmfile
endif

	# Deploy the DPF Operator.
	$(HELM) upgrade --install --create-namespace --namespace $(OPERATOR_NAMESPACE) \
		--set controllerManager.image.repository=$(DPF_SYSTEM_IMAGE)\
		--set controllerManager.image.tag=$(TAG) \
		--set imagePullSecrets[0].name=dpf-pull-secret \
		--set kamajiEtcdDefrag.namespaceOverride=$(TEST_DEPLOY_PREREQS_NAMESPACE) \
		dpf-operator $(OPERATOR_HELM_CHART)

	# Deploy monitoring tools.
	$(MAKE) test-deploy-helmfile \
	  HELMFILE_FILE=$(CURDIR)/deploy/helmfiles/monitoring.yaml \
	  HELMFILE_ENV=

.PHONY: test-deploy-mock-dms
test-deploy-mock-dms: helm # Deploy mock-dms to the kind test cluster.
	## Add the test cluster node IPs to the cert generated for mock-dms.
	$(HELM) upgrade --install --create-namespace --namespace $(OPERATOR_NAMESPACE) \
		--set controllerManager.manager.image.repository=$(MOCK_DMS_IMAGE)\
		--set controllerManager.manager.image.tag=$(TAG) \
		--set imagePullSecrets[0].name=dpf-pull-secret \
		--set certIPAddresses=[$(shell kubectl get nodes $(TEST_CLUSTER_NAME)-control-plane -o yaml | $(YQ) .status.addresses | $(YQ) 'filter(.type == "InternalIP")' | $(YQ) .0.address)]\
		mock-dms $(MOCK_DMS_HELM_CHART)

HELMFILE_FILE ?= $(CURDIR)/deploy/helmfiles/prereqs.yaml
HELMFILE_SELECTOR ?=
HELMFILE_STATE_VALUES_SET ?=
HELMFILE_COLLECT_RESOURCES_ON_FAIL ?= true
HELMFILE_CLEANUP_ON_FAIL ?= false
HELMFILE_WAIT ?= true
.PHONY: test-deploy-helmfile
test-deploy-helmfile: helmfile helm helm-diff helm-git yq binary-dpfdev ## Deploy helm dependencies from local helmfile
	@DPFDEV_BIN=$(LOCALBIN)/dpfdev $(CURDIR)/hack/scripts/deploy-helmfile.sh \
		--file "$(HELMFILE_FILE)" \
		--wait "$(HELMFILE_WAIT)" \
		--cleanup-on-fail "$(HELMFILE_CLEANUP_ON_FAIL)" \
		--collect-resources-on-fail "$(HELMFILE_COLLECT_RESOURCES_ON_FAIL)" \
		--helm-bin "$(HELM)" \
		--helmfile-bin "$(HELMFILE)" \
		$(if $(strip $(HELMFILE_ENV)),--environment "$(HELMFILE_ENV)") \
		$(if $(strip $(HELMFILE_SELECTOR)),--selector "$(HELMFILE_SELECTOR)") \
		$(if $(strip $(HELMFILE_STATE_VALUES_SET)),--state-values-set "$(HELMFILE_STATE_VALUES_SET)")

ARTIFACTS_DIR ?= $(CURDIR)/artifacts
RELEASE_MANIFEST ?= $(ARTIFACTS_DIR)/release-manifest.yaml
# Set to true by release (and test-helper push targets) to record each pushed image/chart in RELEASE_MANIFEST.
RELEASE_MANIFEST_ENABLED ?= false

$(ARTIFACTS_DIR):
	@mkdir -p $(ARTIFACTS_DIR)

.PHONY: release-manifest-init
release-manifest-init: $(ARTIFACTS_DIR) ## Initialize the release manifest for the current build.
	@echo "registry: $(REGISTRY)" > $(RELEASE_MANIFEST)
	@echo "tag: $(TAG)" >> $(RELEASE_MANIFEST)
	@echo "commit: $(FULL_COMMIT)" >> $(RELEASE_MANIFEST)
	@echo "built_at: $$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> $(RELEASE_MANIFEST)
	@echo "artifacts:" >> $(RELEASE_MANIFEST)

# Append one pushed artifact to RELEASE_MANIFEST when RELEASE_MANIFEST_ENABLED=true.
# Usage: $(call record_release_artifact,type,name,ref,scope)
define record_release_artifact
if [ "$(RELEASE_MANIFEST_ENABLED)" = "true" ]; then \
	printf '  - type: %s\n    name: %s\n    ref: %s\n    scope: %s\n' \
		"$(1)" "$(2)" "$(3)" "$(4)" >> "$(RELEASE_MANIFEST)"; \
fi;
endef

# This is the default registry used for e2e tests, it can be overridden by setting the DOCKER_IO_REGISTRY environment variable.
# This is useful for testing with a local registry or a registry mirror.
export DOCKER_IO_REGISTRY ?= docker.io

E2E_TEST_DEFAULTS ?= -v -ginkgo.v -ginkgo.fail-fast -ginkgo.timeout=2h30m
E2E_TEST_ARGS ?= -ginkgo.label-filter="DPFSystem && !SDN && !DPFVPCOVN" -e2e.config=./config-quick.yaml
# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e ## Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: stern ## Run e2e tests
	PREREQS_NAMESPACE=$(TEST_DEPLOY_PREREQS_NAMESPACE) \
	STERN=$(STERN) $(CURDIR)/hack/scripts/log-collector.sh \
	  go test -timeout 0 ./test/e2e/ $(E2E_TEST_DEFAULTS) $(E2E_TEST_ARGS)

.PHONY: generate-htmlreports
generate-htmlreports: binary-dpfdev ## Generate HTML artifact viewers for all resource dumps under ARTIFACTS_DIR
	$(LOCALBIN)/dpfdev htmlreport "$(ARTIFACTS_DIR)"

##@ validate commit
.PHONY: commit-check
commit-check: conform ## Run conform to validate commit message
	$(CONFORM) enforce

##@ lint and verify
GOLANGCI_LINT_GOGC ?= "100"
.PHONY: lint
lint: golangci-lint kube-api-linter golangci-lint-custom ## Run golangci-lint, kube-api-linter and golangci-lint-custom
	GOOS=linux GOTOOLCHAIN=$(GOTOOLCHAIN) GOGC=$(GOLANGCI_LINT_GOGC) $(GOLANGCI_LINT) run --timeout 5m $(GOLANGCI_LINT_EXTRA_ARGS)
	GOOS=linux GOTOOLCHAIN=$(GOTOOLCHAIN) GOGC=$(GOLANGCI_LINT_GOGC) $(KUBE_API_LINTER) run --config $(PROJECT_DIR)/.golangci-kal.yml --timeout 5m $(GOLANGCI_LINT_EXTRA_ARGS)
	GOOS=linux GOTOOLCHAIN=$(GOTOOLCHAIN) GOGC=$(GOLANGCI_LINT_GOGC) $(GOLANGCI_LINT_CUSTOM) run --config $(PROJECT_DIR)/.golangci-custom.yml --timeout 5m $(GOLANGCI_LINT_EXTRA_ARGS)

.PHONY: lint-fix
lint-fix: ## Run golangci-lint and kube-api-linter and perform fixes
	GOLANGCI_LINT_EXTRA_ARGS=--fix $(MAKE) lint

VERIFY_TARGETS ?= generate copyright md-links shfmt crdify manifests-all

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
	$Q $(CURDIR)/hack/scripts/copyright-validation.sh

# Setting this variable to true, turns the very-md-links command into noop
IGNORE_VERIFY_MD_LINKS ?= false
.PHONY: verify-md-links
verify-md-links: $(LYCHEE) ## Check links in markdown docs are working
	@if [ "$$IGNORE_VERIFY_MD_LINKS" == true ]; then \
		echo "Ignoring verify-md-links since IGNORE_VERIFY_MD_LINKS is set to true"; \
		exit 0; \
	fi; \
	$(LYCHEE) --accept 200,429 --exclude '^https?://host(:|/|$$)' . *.md --exclude-path third_party --exclude-path ./deploy --exclude-path docs/do_not_publish # Exclude the external `third_party` docs, the generated `charts` docs and `host` placeholder URLs from the API docs.

export CRDIFY_BASE_REF ?= $(LATEST_STABLE_TAG)
export CRDIFY_COMPARE_REF ?= HEAD
export CRDIFY_CONFIG ?= $(PROJECT_DIR)/crdify.yaml
export CRDIFY_CRD_DIR = $(patsubst $(PROJECT_DIR)/%,%,$(CRDDIR))
.PHONY: verify-crdify
verify-crdify: binary-dpfdev ## Verify that the CRDs are valid
	hack/scripts/crd-validation.sh

ARTIFACTS_RENDERED_MANIFESTS_DIR ?= $(ARTIFACTS_DIR)/rendered-manifests
$(ARTIFACTS_RENDERED_MANIFESTS_DIR): $(ARTIFACTS_DIR)
	@mkdir -p $(ARTIFACTS_RENDERED_MANIFESTS_DIR)

# Not yet enabled charts: dpu-networking ovn-kubernetes ovn-kubernetes-resource-injector
VERIFY_MANIFEST_TARGETS ?= operator kamaji-keepalived vpc-ovn-host vpc-ovn-dpu weave-flow-controllers weave-dhcp-agent storage-host-snap-csi-plugin storage-host-snap-host-controller storage-dpu-snap-node-driver storage-dpu-block-storage-vendor-dpu-plugin storage-dpu-fs-storage-vendor-dpu-plugin storage-dpu-nfs-storage-vendor-dpu-plugin storage-dpu-doca-snap

# Manifest verification runs checkov in a container, so it needs a working docker daemon.
# Setting this to false turns the verify-manifests-all command into a noop.
VERIFY_MANIFESTS ?= true
.PHONY: verify-manifests-all
verify-manifests-all: ## Run all verify-manifest-* targets
	@if [ "$(VERIFY_MANIFESTS)" != true ]; then \
		echo "Skipping verify-manifests-all since VERIFY_MANIFESTS is not set to true"; \
		exit 0; \
	fi; \
	$(MAKE) $(addprefix verify-manifest-,$(VERIFY_MANIFEST_TARGETS)) verify-manifests-dpu-networking-all verify-manifests-operator-embedded-all

# Note: This simulates setting the correct digest for the image by using the @sha256:X syntax which is requirement to comply with CKV_K8S_15 and CKV_K8S_43.
.PHONY: verify-manifest-operator
verify-manifest-operator: helm-package-operator helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for dpf-operator
	$Q $(HELM) template dpf-operator $(CHARTSDIR)/dpf-operator-$(TAG).tgz -n dpf-operator \
	 --set controllerManager.image.tag=$(TAG)@sha256:A \
	 --set controllerManager.resources.limits.cpu=200m \
	 --set controllerManager.resources.limits.memory=256Mi \
	 --set kamajiEtcdDefrag.resources.limits.cpu=200m \
	 --set kamajiEtcdDefrag.resources.limits.memory=256Mi \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpf-operator-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpf-operator-$(TAG).yaml" \
	  MANIFEST_NAME="dpf-operator" \
	  hack/scripts/validate-manifest-checkov.sh

VERIFY_DPU_NETWORKING_MANIFESTS ?= flannel multus sriov-device-plugin nvidia-k8s-ipam servicechainset-controller sfc-controller cni-installer node-problem-detector kube-state-metrics dpu-monitoring opentelemetry-collector kata-containers coredns spire-agent-rbac

verify-manifests-dpu-networking-all: $(addprefix verify-manifest-dpu-networking-,$(VERIFY_DPU_NETWORKING_MANIFESTS)) ## Run manifest verification for manifests embedded into dpf-operator

.PHONY: verify-manifest-dpu-networking-flannel
verify-manifest-dpu-networking-flannel: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking flannel subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set flannel.enabled=true \
	  --set flannel.flannel.resources.limits.cpu=1m \
	  --set flannel.flannel.resources.limits.memory=1Mi \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-flannel-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-flannel-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-flannel" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-multus
verify-manifest-dpu-networking-multus: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking multus subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set multus.enabled=true \
	  --set multus.kubeMultusDs.installMultusBinary.resources.limits.cpu=10m \
	  --set multus.kubeMultusDs.installMultusBinary.resources.limits.memory=15Mi \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-multus-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-multus-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-multus" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-sriov-device-plugin
verify-manifest-dpu-networking-sriov-device-plugin: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking sriov-device-plugin subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set sriov-device-plugin.enabled=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-sriov-device-plugin-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-sriov-device-plugin-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-sriov-device-plugin" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-nvidia-k8s-ipam
verify-manifest-dpu-networking-nvidia-k8s-ipam: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking nvidia-k8s-ipam subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set nvidia-k8s-ipam.enabled=true \
	  --set nvidia-k8s-ipam.deployDPUManifests=true \
	  --set nvidia-k8s-ipam.deployHostManifests=true \
	  --set nvidia-k8s-ipam.nvIpam.controller.resources.limits.cpu=1m \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-nvidia-k8s-ipam-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-nvidia-k8s-ipam-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-nvidia-k8s-ipam" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-servicechainset-controller
verify-manifest-dpu-networking-servicechainset-controller: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking servicechainset-controller subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set servicechainset-controller.enabled=true \
	  --set servicechainset-controller.deployDPUManifests=true \
	  --set servicechainset-controller.deployHostManifests=true \
	  --set servicechainset-controller.controllerManager.manager.image.tag=$(TAG)@sha256:A \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-servicechainset-controller-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-servicechainset-controller-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-servicechainset-controller" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-sfc-controller
verify-manifest-dpu-networking-sfc-controller: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking sfc-controller subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set sfc-controller.enabled=true \
	  --set sfc-controller.controllerManager.manager.resources.limits.cpu=1m \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-sfc-controller-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-sfc-controller-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-sfc-controller" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-cni-installer
verify-manifest-dpu-networking-cni-installer: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking cni-installer subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set cni-installer.enabled=true \
	  --set cni-installer.cniInstaller.resources.limits.cpu=1m \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-cni-installer-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-cni-installer-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-cni-installer" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-node-problem-detector
verify-manifest-dpu-networking-node-problem-detector: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking node-problem-detector subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set node-problem-detector.enabled=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-node-problem-detector-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-node-problem-detector-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-node-problem-detector" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-kube-state-metrics
verify-manifest-dpu-networking-kube-state-metrics: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking kube-state-metrics subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set kube-state-metrics.enabled=true \
	  --set kube-state-metrics.deployDPUManifests=true \
	  --set kube-state-metrics.deployHostManifests=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-kube-state-metrics-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-kube-state-metrics-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-kube-state-metrics" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-dpu-monitoring
verify-manifest-dpu-networking-dpu-monitoring: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking dpu-monitoring subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set dpu-monitoring.enabled=true \
	  --set dpu-monitoring.deployDPUManifests=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-dpu-monitoring-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-dpu-monitoring-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-dpu-monitoring" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-opentelemetry-collector
verify-manifest-dpu-networking-opentelemetry-collector: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking opentelemetry-collector subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set opentelemetry-collector.enabled=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-opentelemetry-collector-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-opentelemetry-collector-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-opentelemetry-collector" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-kata-containers
verify-manifest-dpu-networking-kata-containers: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking kata-containers subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set kata-containers.enabled=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-kata-containers-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-kata-containers-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-kata-containers" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-dpu-networking-spire-agent-rbac
verify-manifest-dpu-networking-spire-agent-rbac: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking spire-agent-rbac subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set spire-agent-rbac.enabled=true \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-spire-agent-rbac-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-spire-agent-rbac-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-spire-agent-rbac" \
	  hack/scripts/validate-manifest-checkov.sh

# Note: The host and DPU manifests are rendered together, the DPU side is the RBAC the host side
# CoreDNS uses to read the DPU cluster. The service account name and the image digest are set here
# because the operator sets them when it renders the chart.
.PHONY: verify-manifest-dpu-networking-coredns
verify-manifest-dpu-networking-coredns: helm-package-dpu-networking helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the dpu-networking coredns subchart
	$Q $(HELM) template $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz \
	  --set coredns.enabled=true \
	  --set coredns.deployDPUManifests=true \
	  --set coredns.deployHostManifests=true \
	  --set coredns.serviceAccount.name=coredns \
	  --set coredns.rbac.serviceAccounts[0].name=coredns \
	  --set coredns.rbac.serviceAccounts[0].namespace=dpf-operator-system \
	  --set coredns.image.tag=v1.12.1@sha256:A \
	> $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-coredns-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/dpu-networking-coredns-$(TAG).yaml" \
	  MANIFEST_NAME="dpu-networking-coredns" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-vpc-ovn-host
verify-manifest-vpc-ovn-host: $(VPC_DIR) helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the vpc-ovn chart's controller
	$Q @cd $(VPC_DIR); $(MAKE) helm-package-all-vpc-ovn
	$Q $(HELM) template $(CHARTSDIR)/dpf-vpc-ovn-$(TAG).tgz \
	 --set host.vpcOVNController.enabled=true \
	 --set host.vpcOVNController.resources.limits.cpu=1m \
	 --set host.vpcOVNController.resources.limits.memory=1Mi \
	 --set host.vpcOVNController.image.tag=c@sha256:d \
	 > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/vpc-ovn-host-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/vpc-ovn-host-$(TAG).yaml" \
	  MANIFEST_NAME="vpc-ovn-host" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-vpc-ovn-dpu
verify-manifest-vpc-ovn-dpu: $(VPC_DIR) helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the vpc-ovn chart's node
	$Q @cd $(VPC_DIR); $(MAKE) helm-package-all-vpc-ovn
	$Q $(HELM) template $(CHARTSDIR)/dpf-vpc-ovn-$(TAG).tgz \
	 --set dpu.vpcOVNNode.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set dpu.vpcOVNNode.initContainers.allocator.resources.limits.cpu=1m \
	 --set dpu.vpcOVNNode.initContainers.allocator.resources.limits.memory=1Mi \
	 --set dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.resources.limits.cpu=1m \
	 --set dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.resources.limits.memory=1Mi \
	 --set dpu.vpcOVNNode.containers.dhcpCNIDaemon.resources.limits.cpu=1m \
	 --set dpu.vpcOVNNode.containers.dhcpCNIDaemon.resources.limits.memory=1Mi \
	 --set dpu.vpcOVNNode.initContainers.allocator.image.tag=c@sha256:d \
	 --set dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.image.tag=c@sha256:d \
	 --set dpu.vpcOVNNode.containers.vpcOVNNodeController.image.tag=c@sha256:d \
	 --set dpu.vpcOVNNode.containers.dhcpCNIDaemon.image.tag=c@sha256:d \
	 > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/vpc-ovn-dpu-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/vpc-ovn-dpu-$(TAG).yaml" \
	  MANIFEST_NAME="vpc-ovn-dpu" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-weave-flow-controllers
verify-manifest-weave-flow-controllers: $(VPC_DIR) helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the weave chart's flow controllers
	$Q @cd $(VPC_DIR); $(MAKE) helm-package-all-weave
	$Q $(HELM) template $(CHARTSDIR)/dpf-weave-$(TAG).tgz \
	 --set weaveFlowController.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set weaveFlowController.containers.weaveFlowController.image.tag=c@sha256:d \
	 > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/weave-flow-controllers-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/weave-flow-controllers-$(TAG).yaml" \
	  MANIFEST_NAME="weave-flow-controllers" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-weave-dhcp-agent
verify-manifest-weave-dhcp-agent: $(VPC_DIR) helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the weave chart's dhcp agent
	$Q @cd $(VPC_DIR); $(MAKE) helm-package-all-weave
	$Q $(HELM) template $(CHARTSDIR)/dpf-weave-$(TAG).tgz \
	 --set weaveDHCPAgent.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set weaveDHCPAgent.containers.weaveDHCPAgent.image.tag=c@sha256:d \
	 > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/weave-dhcp-agent-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/weave-dhcp-agent-$(TAG).yaml" \
	  MANIFEST_NAME="weave-dhcp-agent" \
	  hack/scripts/validate-manifest-checkov.sh

# Note: The sed strip Go template variables from the embedded controller manifest to allow Checkov scanning.
# If this gets too complex we should do a go templating approach.
.PHONY: verify-manifest-kamaji-keepalived
verify-manifest-kamaji-keepalived: $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for kamaji-keepalived
	$Q sed -E -e 's/\{\{\.[^}]+\}\}/placeholder/g' -e '/\{\{[^}]+\}\}/d' \
		internal/clustermanager/kamaji/manifests/keepalived.yaml > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/kamaji-keepalived.yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/kamaji-keepalived.yaml" \
	  MANIFEST_NAME="kamaji-keepalived" \
	  hack/scripts/validate-manifest-checkov.sh

VERIFY_OPERATOR_EMBEDDED_MANIFESTS ?= cni-installer dpu-detector dpuservice-controller flannel kamaji-cluster-manager multus nv-k8s-ipam provisioning-controller servicefunctionchainset-controller sfc-controller sriov-device-plugin static-cluster-manager vault-kms

verify-manifests-operator-embedded-all: $(addprefix verify-manifest-operator-embedded-,$(VERIFY_OPERATOR_EMBEDDED_MANIFESTS)) ## Run manifest verification for manifests embedded into dpf-operator

.PHONY: verify-manifest-operator-embedded-%
verify-manifest-operator-embedded-%: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev
	$Q RENDERED_MANIFEST="$(PROJECT_DIR)/internal/operator/inventory/manifests/$*.yaml" \
	  MANIFEST_NAME="$*" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-host-snap-csi-plugin
verify-manifest-storage-host-snap-csi-plugin: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's host snap-csi-plugin component
	$Q $(HELM) template dpuservices/storage/chart \
	  --set host.snapCsiPlugin.enabled=true \
	  --set host.snapCsiPlugin.node.enabled=true \
	  --set host.snapCsiPlugin.controller.enabled=true \
	  --set host.snapCsiPlugin.controller.plugin.resources.limits.cpu=1m \
	  --set host.snapCsiPlugin.controller.plugin.resources.limits.memory=1Mi \
	  --set host.snapCsiPlugin.controller.externalProvisioner.resources.limits.cpu=1m \
	  --set host.snapCsiPlugin.controller.externalProvisioner.resources.limits.memory=1Mi \
	  --set host.snapCsiPlugin.controller.externalAttacher.resources.limits.cpu=1m \
	  --set host.snapCsiPlugin.controller.externalAttacher.resources.limits.memory=1Mi \
	  --set host.snapCsiPlugin.controller.livenessProbe.resources.limits.cpu=1m \
	  --set host.snapCsiPlugin.controller.livenessProbe.resources.limits.memory=1Mi \
	  --set host.snapCsiPlugin.node.plugin.resources.limits.cpu=1m \
	  --set host.snapCsiPlugin.node.plugin.resources.limits.memory=1Mi \
	  --set host.snapCsiPlugin.node.livenessProbe.resources.limits.cpu=1m \
	  --set host.snapCsiPlugin.node.livenessProbe.resources.limits.memory=1Mi \
      --set serviceDaemonSet.resources.cpu=1m \
      --set serviceDaemonSet.resources.memory=1Mi \
      --set host.snapCsiPlugin.controller.plugin.image.tag=c@sha256:d \
      --set host.snapCsiPlugin.controller.externalProvisioner.image.tag=c@sha256:d \
      --set host.snapCsiPlugin.controller.externalAttacher.image.tag=c@sha256:d \
      --set host.snapCsiPlugin.controller.livenessProbe.image.tag=c@sha256:d \
      --set host.snapCsiPlugin.node.plugin.image.tag=c@sha256:d \
      --set host.snapCsiPlugin.node.livenessProbe.image.tag=c@sha256:d \
      --set host.snapCsiPlugin.node.nodeDriverRegistrar.image.tag=c@sha256:d \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-host-snap-csi-plugin-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-host-snap-csi-plugin-$(TAG).yaml" \
	  MANIFEST_NAME="storage-host-snap-csi-plugin" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-host-snap-host-controller
verify-manifest-storage-host-snap-host-controller: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's host snap-host-controller component
	$Q $(HELM) template dpuservices/storage/chart \
	 --set host.snapHostController.enabled=true \
	 --set host.snapHostController.config.targetNamespace=storage-system \
	 --set host.snapHostController.resources.limits.cpu=1m \
	 --set host.snapHostController.resources.limits.memory=1Mi \
	 --set host.snapHostController.image.tag=c@sha256:d \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-host-snap-host-controller-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-host-snap-host-controller-$(TAG).yaml" \
	  MANIFEST_NAME="storage-host-snap-host-controller" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-dpu-snap-node-driver
verify-manifest-storage-dpu-snap-node-driver: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's dpu snap-node-driver component
	$Q $(HELM) template dpuservices/storage/chart \
	 --set dpu.snapNodeDriver.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set dpu.snapNodeDriver.image.tag=c@sha256:d \
	 --set dpu.snapNodeDriver.podSecurityContext.runAsNonRoot=false \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-snap-node-driver-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-snap-node-driver-$(TAG).yaml" \
	  MANIFEST_NAME="storage-dpu-snap-node-driver" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-dpu-block-storage-vendor-dpu-plugin
verify-manifest-storage-dpu-block-storage-vendor-dpu-plugin: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's dpu block-storage-vendor-dpu-plugin component
	$Q $(HELM) template dpuservices/storage/chart \
	 --set dpu.blockStorageVendorDpuPlugin.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set dpu.blockStorageVendorDpuPlugin.image.tag=c@sha256:d \
	 --set dpu.blockStorageVendorDpuPlugin.podSecurityContext.runAsNonRoot=false \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-block-storage-vendor-dpu-plugin-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-block-storage-vendor-dpu-plugin-$(TAG).yaml" \
	  MANIFEST_NAME="storage-dpu-block-storage-vendor-dpu-plugin" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-dpu-fs-storage-vendor-dpu-plugin
verify-manifest-storage-dpu-fs-storage-vendor-dpu-plugin: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's dpu fs-storage-vendor-dpu-plugin component
	$Q $(HELM) template dpuservices/storage/chart \
	 --set dpu.fsStorageVendorDpuPlugin.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set dpu.fsStorageVendorDpuPlugin.image.tag=c@sha256:d \
	 --set dpu.fsStorageVendorDpuPlugin.podSecurityContext.runAsNonRoot=false \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-fs-storage-vendor-dpu-plugin-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-fs-storage-vendor-dpu-plugin-$(TAG).yaml" \
	  MANIFEST_NAME="storage-dpu-fs-storage-vendor-dpu-plugin" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-dpu-nfs-storage-vendor-dpu-plugin
verify-manifest-storage-dpu-nfs-storage-vendor-dpu-plugin: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's dpu nfs-storage-vendor-dpu-plugin component
	$Q $(HELM) template dpuservices/storage/chart \
	 --set dpu.nfsStorageVendorDpuPlugin.enabled=true \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set dpu.nfsStorageVendorDpuPlugin.image.repository=a/b:c@sha256:d \
	 --set dpu.nfsStorageVendorDpuPlugin.podSecurityContext.runAsNonRoot=false \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-nfs-storage-vendor-dpu-plugin-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-nfs-storage-vendor-dpu-plugin-$(TAG).yaml" \
	  MANIFEST_NAME="storage-dpu-nfs-storage-vendor-dpu-plugin" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: verify-manifest-storage-dpu-doca-snap
verify-manifest-storage-dpu-doca-snap: helm $(ARTIFACTS_RENDERED_MANIFESTS_DIR) binary-dpfdev ## Run manifest verification for the storage chart's dpu doca-snap component
	$Q $(HELM) template dpuservices/storage/chart \
	 --set dpu.docaSnap.enabled=true \
	 --set dpu.docaSnap.podSecurityContext.runAsNonRoot=false \
	 --set serviceDaemonSet.resources.cpu=1m \
	 --set serviceDaemonSet.resources.memory=1Mi \
	 --set dpu.docaSnap.image.tag=c@sha256:d \
	  > $(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-doca-snap-$(TAG).yaml
	$Q RENDERED_MANIFEST="$(ARTIFACTS_RENDERED_MANIFESTS_DIR)/storage-dpu-doca-snap-$(TAG).yaml" \
	  MANIFEST_NAME="storage-dpu-doca-snap" \
	  hack/scripts/validate-manifest-checkov.sh

.PHONY: lint-helm
lint-helm: lint-helm-dpu-networking lint-helm-dummydpuservice lint-helm-storage

.PHONY: lint-helm-dpu-networking
lint-helm-dpu-networking: helm ## Run helm lint for servicechainset chart
	$Q $(HELM) lint $(DPU_NETWORKING_HELM_CHART)

.PHONY: lint-helm-dummydpuservice
lint-helm-dummydpuservice: helm ## Run helm lint for dummydpuservice chart
	$Q $(HELM) lint $(DUMMYDPUSERVICE_HELM_CHART)



.PHONY: lint-helm-storage
lint-helm-storage: helm ## Run helm lint for snap dpu chart
	$Q $(HELM) lint $(STORAGE_CHART)

##@ Release

.PHONY: release-build
release-build: generate ## Build helm and container images for release.
	# Build multiarch images which will run on both DPUs and x86 hosts.
	$(MAKE) $(addprefix docker-build-,$(MULTI_ARCH_DOCKER_BUILD_TARGETS))
	# Build arm64 images which will run on DPUs.
	$(MAKE) ARCH=$(DPU_ARCH) $(addprefix docker-build-,$(DPU_ARCH_DOCKER_BUILD_TARGETS))

	# Package the helm charts.
	$(MAKE) helm-package-all

	# Build vpc release artifacts
	$(MAKE) release-build-vpc

# controls whether the charts should be pushed in both REGISTRY and HELM_REGISTRY, in case the two are different
export RELEASE_PUSH_HELM_CHARTS_TO_REGISTRY ?= false

.PHONY: release
# PUSH=true turns every docker-build-* below, here and in the VPC sub-build, into
# a single buildx --push, so the images are published by the build that produces
# them. There is no separate image push step.
release: export PUSH := true
release: export RELEASE_MANIFEST_ENABLED := true
release: ## Build and push helm and container images for release.
	$(MAKE) release-manifest-init
	
	$(MAKE) release-build
	$(MAKE) release-dpfctl-ngc

	# Push the helm charts.
	$(MAKE) helm-push-all

	# push vpc release artifacts
	$(MAKE) release-push-vpc

ifeq ($(RELEASE_PUSH_HELM_CHARTS_TO_REGISTRY),true)
ifneq ($(HELM_REGISTRY),$(DEFAULT_HELM_REGISTRY))
	# Push the helm charts to the REGISTRY as well in case the 2 conditions are satisfied. This assumes that the REGISTRY
	# is an OCI compliant registry.
	$(MAKE) HELM_REGISTRY=$(DEFAULT_HELM_REGISTRY) helm-push-all
endif
endif

export DPFCTL_NGC_ENABLED ?= false
export DPFCTL_NGC_ORG ?= nvstaging
.PHONY: release-dpfctl-ngc
release-dpfctl-ngc: $(NGC) ## Release the dpfctl binary to NGC.
ifeq ($(DPFCTL_NGC_ENABLED),true)
	@if [ -z "$$NGC_API_KEY" ]; then \
		echo "Error: NGC_API_KEY environment variable is not set. Please set it before running this target"; \
		exit 1; \
	fi

	# Make dpfctl binaries for Linux and MacOS
	$(MAKE) binary-dpfctl-release

	# Push the dpfctl binaries to NGC
	$(NGC) registry resource info --org $(DPFCTL_NGC_ORG) $(DPFCTL_NGC_ORG)/doca/dpfctl:$(TAG) &>/dev/null \
	&& echo "dpfctl $(TAG) already exists in nvstaging" \
	|| $(NGC) registry resource upload-version --org $(DPFCTL_NGC_ORG) $(DPFCTL_NGC_ORG)/doca/dpfctl:$(TAG) --source $(LOCALBIN)/dpfctl-$(TAG)-release/
endif

BUILD_VPC ?= false
ifeq ($(BUILD_VPC),true)
.PHONY: release-build-vpc
release-build-vpc: $(VPC_DIR) ## Build vpc release artifacts
	@cd $(VPC_DIR); $(MAKE) release-build

DPF_VPC_RELEASE_MANIFEST ?= $(VPC_DIR)/artifacts/dpf-vpc-release-manifest.yaml

.PHONY: release-push-vpc
release-push-vpc: $(VPC_DIR) ## Push vpc release artifacts
	@cd $(VPC_DIR); $(MAKE) release-push \
		REGISTRY="$(REGISTRY)" \
		TAG="$(TAG)" \
		HELM_REGISTRY="$(HELM_REGISTRY)" \
		VPC_CHART_TAGS="$(TAG)" \
		RELEASE_MANIFEST_ENABLED="$(RELEASE_MANIFEST_ENABLED)"
	@$(MAKE) collect-vpc-release-manifest

.PHONY: collect-vpc-release-manifest
collect-vpc-release-manifest: $(ARTIFACTS_DIR) ## Copy the VPC release manifest and merge its artifacts into RELEASE_MANIFEST.
	@if [ "$(RELEASE_MANIFEST_ENABLED)" != "true" ]; then \
		exit 0; \
	fi; \
	if [ ! -f "$(DPF_VPC_RELEASE_MANIFEST)" ]; then \
		echo "VPC release manifest not found: $(DPF_VPC_RELEASE_MANIFEST)" >&2; exit 1; \
	fi; \
	cp "$(DPF_VPC_RELEASE_MANIFEST)" "$(ARTIFACTS_DIR)/dpf-vpc-release-manifest.yaml"; \
	if [ -f "$(RELEASE_MANIFEST)" ]; then \
		sed -n '/^artifacts:/,$$p' "$(DPF_VPC_RELEASE_MANIFEST)" | tail -n +2 >> "$(RELEASE_MANIFEST)"; \
	else \
		echo "Parent release manifest not found: $(RELEASE_MANIFEST); skipping merge" >&2; \
	fi
else
.PHONY: release-build-vpc
release-build-vpc: ## Build vpc release artifacts
	@echo "VPC skipped"

.PHONY: release-push-vpc
release-push-vpc: ## Push vpc release artifacts
	@echo "VPC skipped"
endif

.PHONY: warm-cache
warm-cache: ## Warm the cache for the tests.
	$(MAKE) CACHE_ONLY=true release-build lint

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
GO_LDFLAGS=-s -w -extldflags '-static'

# GO_GCFLAGS:
#  -trimpath: removes file system paths from the binary
# Example of -trimpath effect:
#  Without: /home/user/go/src/example.com/project/cmd/operator/main.go
#  With:    cmd/operator/main.go
GO_GCFLAGS=-trimpath

BUILD_TARGETS ?= $(DPU_ARCH_BUILD_TARGETS)
DPF_SYSTEM_BUILD_TARGETS ?= operator provisioning dpuservice servicechainset kamaji-cluster-manager static-cluster-manager \
	sfc-controller dpfctl dpudetector nodesriovdeviceplugin-controller nodesriovdeviceplugin-init dpf-kms-plugin
DPU_ARCH_BUILD_TARGETS ?=
# contains list of storage-related binaries that have no system-level dependencies
STORAGE_SYSTEM_BUILD_TARGETS ?= storage-snap-host-controller storage-snap-node-driver block-storage-vendor-dpu-plugin storage-snap-csi-plugin storage-nvidia-external-attacher nfs-storage-vendor-dpu-plugin
# contains list of storage-related binaries that have system-level dependencies (depend on some linux utils from the container)
STORAGE_HOST_BUILD_TARGETS ?= storage-snap-csi-plugin fs-storage-vendor-dpu-plugin

# Binary paths (for CI scripts and build targets)
DPUAGENT_BINARY ?= $(LOCALBIN)/dpuagent

BUILD_IMAGE ?= docker.io/library/golang:$(GO_VERSION)

HOST_ARCH = amd64
DPU_ARCH = arm64

# Use distroless as minimal base image to package the manager binary
BASE_IMAGE = nvcr.io/nvidia/doca/dpf_containers:1.0.2-ubuntu22.04-distroless
ALPINE_IMAGE = alpine:3.19
# Base image for hostdriver (DOCA full runtime host image)
HOSTDRIVER_BASE_IMAGE ?= nvcr.io/nvidia/doca/doca:full-rt-3.4.0-host
# Base image for storage-host, by default it is the same as the hostdriver base image
STORAGE_HOST_BASE_IMAGE ?= $(HOSTDRIVER_BASE_IMAGE)
# Base image for bfb-registry, by default it is the same as the hostdriver base image
BFB_REGISTRY_BASE_IMAGE ?= $(HOSTDRIVER_BASE_IMAGE)

.PHONY: binaries
binaries: $(addprefix binary-,$(BUILD_TARGETS)) ## Build all binaries

.PHONY: binaries-dpf-system
binaries-dpf-system: $(addprefix binary-,$(DPF_SYSTEM_BUILD_TARGETS)) ## Build binaries for the dpf-system image.

.PHONY: binaries-storage-system
binaries-storage-system: $(addprefix binary-,$(STORAGE_SYSTEM_BUILD_TARGETS)) ## Build binaries for the storage-system image.

.PHONY: binaries-storage-host
binaries-storage-host: $(addprefix binary-,$(STORAGE_HOST_BUILD_TARGETS)) ## Build binaries for the storage-host image.

.PHONY: binary-operator
binary-operator: ## Build the operator controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/operator github.com/nvidia/doca-platform/cmd/operator

.PHONY: binary-provisioning
binary-provisioning: ## Build the provisioning controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/provisioning github.com/nvidia/doca-platform/cmd/provisioning

.PHONY: binary-kamaji-cluster-manager
binary-kamaji-cluster-manager: ## Build the kamaji-cluster-manager binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/kamaji-cluster-manager github.com/nvidia/doca-platform/cmd/kamaji-cluster-manager

.PHONY: binary-static-cluster-manager
binary-static-cluster-manager: ## Build the static-cluster-manager binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/static-cluster-manager github.com/nvidia/doca-platform/cmd/static-cluster-manager

.PHONY: binary-dpuservice
binary-dpuservice: ## Build the dpuservice controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpuservice github.com/nvidia/doca-platform/cmd/dpuservice

.PHONY: binary-servicechainset
binary-servicechainset: ## Build the servicechainset controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/servicechainset github.com/nvidia/doca-platform/cmd/servicechainset

.PHONY: binary-sfc-controller
binary-sfc-controller: ## Build the Host CNI Provisioner binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/sfc-controller github.com/nvidia/doca-platform/cmd/sfc-controller

.PHONY: binary-dpf-kms-plugin
binary-dpf-kms-plugin: ## Build the Vault KMS v2 plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpf-kms-plugin github.com/nvidia/doca-platform/cmd/dpf-kms-plugin

.PHONY: binary-ipallocator
binary-ipallocator: ## Build the IP allocator binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/ipallocator github.com/nvidia/doca-platform/cmd/ipallocator

.PHONY: binary-dpudetector
binary-dpudetector: ## Build the DPU detector binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpu-detector github.com/nvidia/doca-platform/cmd/dpudetector

.PHONY: binary-rshim-console-collector
binary-rshim-console-collector: ## Build the rshim console collector binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/rshim-console-collector github.com/nvidia/doca-platform/test/rshim-console-collector

.PHONY: binary-dpuagent
binary-dpuagent: ## Build the DPU agent binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(DPU_ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(DPUAGENT_BINARY) github.com/nvidia/doca-platform/cmd/dpuagent

.PHONY: binary-pldmunpackserver
binary-pldmunpackserver: ## Build the PLDM unpack HTTP server (Unix socket) binary.
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/pldmunpackserver github.com/nvidia/doca-platform/cmd/provisioning/pldmunpackserver

.PHONY: binary-certreloader
binary-certreloader: ## Build the bfb-registry cert-reloader sidecar binary.
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/certreloader github.com/nvidia/doca-platform/cmd/provisioning/certreloader

# DPU Agent packaging variables
DPUAGENT_PKG_DIR = $(CURDIR)/internal/provisioning/dpuagent/packaging
# Strip the leading "v" from TAG: Debian policy requires package versions to start with a digit.
DPUAGENT_PKG_VERSION = $(subst v,,$(TAG))
DPUAGENT_DEB = $(LOCALBIN)/dpu-agent_$(DPUAGENT_PKG_VERSION)_arm64.deb
DPUAGENT_RPM = $(LOCALBIN)/dpu-agent-$(DPUAGENT_PKG_VERSION)-1.aarch64.rpm

# Individual packaging targets - package existing binary without building
.PHONY: deb-dpuagent
deb-dpuagent: $(NFPM) ## Package dpuagent binary as .deb (expects binary to exist).
	$(Q) cp $(DPUAGENT_BINARY) $(DPUAGENT_PKG_DIR)/dpuagent
	$(Q) cd $(DPUAGENT_PKG_DIR) && \
		VERSION=$(DPUAGENT_PKG_VERSION) \
		$(NFPM) package --packager deb --target $(DPUAGENT_DEB)
	$(Q) rm -f $(DPUAGENT_PKG_DIR)/dpuagent
	@echo "Packaged $(DPUAGENT_DEB)"

.PHONY: rpm-dpuagent
rpm-dpuagent: $(NFPM) ## Package dpuagent binary as .rpm (expects binary to exist).
	$(Q) cp $(DPUAGENT_BINARY) $(DPUAGENT_PKG_DIR)/dpuagent
	$(Q) cd $(DPUAGENT_PKG_DIR) && \
		VERSION=$(DPUAGENT_PKG_VERSION) \
		$(NFPM) package --packager rpm --target $(DPUAGENT_RPM)
	$(Q) rm -f $(DPUAGENT_PKG_DIR)/dpuagent
	@echo "Packaged $(DPUAGENT_RPM)"

# dpu_hw SPIRE NodeAttestor plugin (agent runs on the DPU, server on the SPIRE server).
DPU_HW_AGENT_BINARY ?= $(LOCALBIN)/dpu-hw-agent
DPU_HW_SERVER_BINARY ?= $(LOCALBIN)/dpu-hw-server
DPU_HW_PKG_DIR = $(CURDIR)/internal/spire/dpu_hw/packaging
# Strip the leading "v" from TAG: Debian policy requires package versions to start with a digit.
DPU_HW_PKG_VERSION = $(patsubst v%,%,$(TAG))
DPU_HW_AGENT_DEB = $(LOCALBIN)/dpu-hw-agent_$(DPU_HW_PKG_VERSION)_arm64.deb

.PHONY: binary-dpu-hw-agent
binary-dpu-hw-agent: ## Build the dpu_hw SPIRE NodeAttestor agent plugin (DPU/arm64).
	CGO_ENABLED=0 GOOS=linux GOARCH=$(DPU_ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(DPU_HW_AGENT_BINARY) github.com/nvidia/doca-platform/cmd/dpu-hw-agent

.PHONY: binary-dpu-hw-server
binary-dpu-hw-server: ## Build the dpu_hw SPIRE NodeAttestor server plugin (+ bare-hex .sha256 sidecar consumed by the DPF Helm overlay).
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(DPU_HW_SERVER_BINARY) github.com/nvidia/doca-platform/cmd/dpu-hw-server
	$(Q) cd $(LOCALBIN) && sha256sum $(notdir $(DPU_HW_SERVER_BINARY)) | cut -d' ' -f1 > $(notdir $(DPU_HW_SERVER_BINARY)).sha256

.PHONY: deb-dpu-hw-agent
deb-dpu-hw-agent: $(NFPM) ## Package dpu_hw agent plugin as .deb plus a .sha256 sidecar (expects binary to exist).
	$(Q) cp $(DPU_HW_AGENT_BINARY) $(DPU_HW_PKG_DIR)/dpu-hw-agent
	$(Q) cd $(DPU_HW_PKG_DIR) && \
		VERSION=$(DPU_HW_PKG_VERSION) \
		$(NFPM) package --packager deb --target $(DPU_HW_AGENT_DEB)
	$(Q) rm -f $(DPU_HW_PKG_DIR)/dpu-hw-agent
	$(Q) cd $(LOCALBIN) && sha256sum $(notdir $(DPU_HW_AGENT_DEB)) | cut -d' ' -f1 > $(notdir $(DPU_HW_AGENT_DEB)).sha256
	@echo "Packaged $(DPU_HW_AGENT_DEB) (+ .sha256)"

# SPIRE Agent and DSX SPIFFE Helper binaries for DPU SPIFFE identity (arm64).
SPIRE_VERSION ?= 1.15.0
SPIRE_AGENT_BINARY ?= $(LOCALBIN)/spire-agent-$(SPIRE_VERSION)
SPIFFE_HELPER_BINARY ?= $(LOCALBIN)/spiffe-helper
SPIRE_AGENT_PKG_DIR = $(CURDIR)/internal/spire/spire_agent/packaging
SPIFFE_HELPER_PKG_DIR = $(CURDIR)/internal/spire/spiffe_helper/packaging
SPIRE_PKG_VERSION = $(patsubst v%,%,$(TAG))
SPIRE_AGENT_DEB = $(LOCALBIN)/spire-agent_$(SPIRE_PKG_VERSION)_arm64.deb
SPIFFE_HELPER_DEB = $(LOCALBIN)/spiffe-helper_$(SPIRE_PKG_VERSION)_arm64.deb
SPIRE_TARBALL = spire-$(SPIRE_VERSION)-linux-$(DPU_ARCH)-musl.tar.gz
SPIRE_DOWNLOAD_URL = https://github.com/spiffe/spire/releases/download/v$(SPIRE_VERSION)/$(SPIRE_TARBALL)
# Pinned SHA256 of the upstream tarballs above. Update when bumping versions.
SPIRE_SHA256 ?= 0458feaaaa36e82491446bc62379301fba431ea012c530a55e45919da96b1a74

.PHONY: fetch-spire-agent fetch-spiffe-helper fetch-spire-binaries binary-spiffe-helper
fetch-spire-binaries: fetch-spire-agent fetch-spiffe-helper ## Fetch SPIRE Agent and DSX SPIFFE Helper arm64 binaries.

fetch-spire-agent: $(SPIRE_AGENT_BINARY) ## Download SPIRE Agent binary for DPU packaging.

fetch-spiffe-helper: binary-spiffe-helper ## Build DSX SPIFFE Helper binary for DPU packaging.

$(SPIRE_AGENT_BINARY):
	$(Q) mkdir -p $(LOCALBIN)
	$(Q) test -n "$(SPIRE_SHA256)" || { echo "SPIRE_SHA256 is unset; refusing to download $(SPIRE_TARBALL) without checksum verification" >&2; exit 1; }
	$(Q) curl -fsSL "$(SPIRE_DOWNLOAD_URL)" -o "$(LOCALBIN)/$(SPIRE_TARBALL)"
	$(Q) echo "$(SPIRE_SHA256)  $(LOCALBIN)/$(SPIRE_TARBALL)" | sha256sum -c -
	$(Q) tar -xz -O -f "$(LOCALBIN)/$(SPIRE_TARBALL)" spire-$(SPIRE_VERSION)/bin/spire-agent > $(SPIRE_AGENT_BINARY)
	$(Q) rm -f "$(LOCALBIN)/$(SPIRE_TARBALL)"
	$(Q) chmod 755 $(SPIRE_AGENT_BINARY)

binary-spiffe-helper: $(SPIFFE_HELPER_BINARY) ## Build the DSX SPIFFE Helper (DPU/arm64).

# No prerequisites on purpose: the spire-packager stage in Dockerfile.bfb-registry carries no Go
# sources, so deb-spiffe-helper needs this target already satisfied. Cost is local -- after editing
# cmd/spiffe-helper, `rm -f $(SPIFFE_HELPER_BINARY)` or the stale binary is reused.
$(SPIFFE_HELPER_BINARY):
	$(Q) mkdir -p $(dir $(SPIFFE_HELPER_BINARY))
	CGO_ENABLED=0 GOOS=linux GOARCH=$(DPU_ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(SPIFFE_HELPER_BINARY) github.com/nvidia/doca-platform/cmd/spiffe-helper

.PHONY: deb-spire-agent deb-spiffe-helper
deb-spire-agent: $(NFPM) fetch-spire-agent ## Package SPIRE Agent as .deb for the DPU apt repo.
	$(Q) cp $(SPIRE_AGENT_BINARY) $(SPIRE_AGENT_PKG_DIR)/spire-agent
	$(Q) cd $(SPIRE_AGENT_PKG_DIR) && \
		VERSION=$(SPIRE_PKG_VERSION) \
		$(NFPM) package --packager deb --target $(SPIRE_AGENT_DEB)
	$(Q) rm -f $(SPIRE_AGENT_PKG_DIR)/spire-agent
	@echo "Packaged $(SPIRE_AGENT_DEB)"

deb-spiffe-helper: $(NFPM) fetch-spiffe-helper ## Package spiffe-helper as .deb for the DPU apt repo.
	$(Q) cp $(SPIFFE_HELPER_BINARY) $(SPIFFE_HELPER_PKG_DIR)/spiffe-helper
	$(Q) cd $(SPIFFE_HELPER_PKG_DIR) && \
		VERSION=$(SPIRE_PKG_VERSION) \
		$(NFPM) package --packager deb --target $(SPIFFE_HELPER_DEB)
	$(Q) rm -f $(SPIFFE_HELPER_PKG_DIR)/spiffe-helper
	@echo "Packaged $(SPIFFE_HELPER_DEB)"

.PHONY: binary-storage-snap-host-controller
binary-storage-snap-host-controller: ## Build the snap host controller controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/snap-host-controller github.com/nvidia/doca-platform/cmd/storage/snap-host-controller

.PHONY: binary-storage-snap-node-driver
binary-storage-snap-node-driver: ## Build the snap node driver controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/snap-node-driver github.com/nvidia/doca-platform/cmd/storage/snap-node-driver

.PHONY: binary-block-storage-vendor-dpu-plugin
binary-block-storage-vendor-dpu-plugin: ## Build the block storage vendor DPU plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/block-storage-vendor-dpu-plugin github.com/nvidia/doca-platform/cmd/storage/storage-vendor-dpu-plugin/block-storage-vendor-dpu-plugin

.PHONY: binary-fs-storage-vendor-dpu-plugin
binary-fs-storage-vendor-dpu-plugin: ## Build the AIO filesystem storage vendor DPU plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/fs-storage-vendor-dpu-plugin github.com/nvidia/doca-platform/cmd/storage/storage-vendor-dpu-plugin/fs-storage-vendor-dpu-plugin

.PHONY: binary-nfs-storage-vendor-dpu-plugin
binary-nfs-storage-vendor-dpu-plugin: ## Build the NFS filesystem storage vendor DPU plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/nfs-storage-vendor-dpu-plugin github.com/nvidia/doca-platform/cmd/storage/storage-vendor-dpu-plugin/nfs-storage-vendor-dpu-plugin

.PHONY: binary-storage-snap-csi-plugin
binary-storage-snap-csi-plugin: ## Build the snap-csi-plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/snap-csi-plugin github.com/nvidia/doca-platform/cmd/storage/snap-csi-plugin

.PHONY: binary-storage-nvidia-external-attacher
binary-storage-nvidia-external-attacher: generate-client-for-storage-nvidia-external-attacher ## Build the nvidia external attacher binary.
	./$(NVIDIA_EXTERNAL_ATTACHER_DIR)/hack/client.sh $(PROJECT_DIR) $(EXTERNAL_ATTACHER_BRANCH)
	# Build nvidia-external-attacher binary
	# The attacher resolves its own module graph, so the root go.mod pins do not reach it.
	# Bump the CVE-relevant modules explicitly before tidy, which never downgrades them.
	cd $(NVIDIA_EXTERNAL_ATTACHER_DIR)/external-attacher && \
	go get google.golang.org/grpc@v1.82.1 golang.org/x/net@v0.58.0 golang.org/x/text@v0.41.0 && \
	go mod tidy && go mod vendor && \
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/nvidia-external-attacher github.com/kubernetes-csi/external-attacher/v4/cmd/csi-attacher

.PHONY: binary-dpfctl
binary-dpfctl: ## Build the dpfctl binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false \
		-ldflags="$(shell echo $(GO_LDFLAGS)) -X main.version=$(TAG)" \
		-gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpfctl github.com/nvidia/doca-platform/cmd/dpfctl

.PHONY: binary-dpfctl-release
binary-dpfctl-release:
	mkdir -p $(LOCALBIN)/dpfctl-$(TAG)-release
	# Build for linux/amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false \
		-ldflags="$(GO_LDFLAGS) -X main.version=$(TAG)" \
		-gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpfctl-$(TAG)-release/dpfctl-linux-amd64 github.com/nvidia/doca-platform/cmd/dpfctl
	# Build for linux/arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false \
		-ldflags="$(GO_LDFLAGS) -X main.version=$(TAG)" \
		-gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpfctl-$(TAG)-release/dpfctl-linux-arm64 github.com/nvidia/doca-platform/cmd/dpfctl
	# Build for darwin/arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -buildvcs=false \
		-ldflags="$(GO_LDFLAGS) -X main.version=$(TAG)" \
		-gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpfctl-$(TAG)-release/dpfctl-darwin-arm64 github.com/nvidia/doca-platform/cmd/dpfctl

.PHONY: binary-cni-installer
binary-cni-installer: ## Build the CNI installer binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/cni-installer github.com/nvidia/doca-platform/cmd/cniinstaller

.PHONY: binary-sfc-cni
binary-sfc-cni: ## Build the SFC CNI binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -tags no_openssl -ldflags="$(GO_LDFLAGS) -X github.com/containernetworking/plugins/pkg/utils/buildversion.BuildVersion=$(TAG)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/ovs github.com/nvidia/doca-platform/cmd/dpf-ovs-cni

.PHONY: install-dpfctl
install-dpfctl: binary-dpfctl ## Install the dpfctl binary.
	install -m 755 $(LOCALBIN)/dpfctl $(GOPATH)/bin/dpfctl

.PHONY: binary-dpfdev
binary-dpfdev: ## Build the dpfdev CLI tool
	cd hack/tools/dpfdev && CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false \
		-ldflags="$(GO_LDFLAGS)" \
		-gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/dpfdev main.go

.PHONY: binary-nodesriovdeviceplugin-controller
binary-nodesriovdeviceplugin-controller: ## Build the nodesriovdeviceplugin controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/nodesriovdeviceplugin-controller github.com/nvidia/doca-platform/cmd/nodesriovdeviceplugin/controller

.PHONY: binary-nodesriovdeviceplugin-init
binary-nodesriovdeviceplugin-init: ## Build the nodesriovdeviceplugin init binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/nodesriovdeviceplugin-init github.com/nvidia/doca-platform/cmd/nodesriovdeviceplugin/initcontainer

DOCKER_BUILD_TARGETS=$(DPU_ARCH_DOCKER_BUILD_TARGETS) $(MULTI_ARCH_DOCKER_BUILD_TARGETS)
DPU_ARCH_DOCKER_BUILD_TARGETS=$(DPU_ARCH_BUILD_TARGETS) cni-installer
MULTI_ARCH_DOCKER_BUILD_TARGETS= dpf-system hostdriver storage-system storage-host bfb-registry keepalived

# Release builds (the `release` target exports PUSH=true) build the full
# multi-arch manifest and push it in a single buildx step, which is far faster
# than the per-arch --load build followed by docker push + docker manifest
# assembly. Every other caller (local dev, warm-cache, single-arch e2e) leaves
# PUSH empty and builds --load, one architecture at a time.
COMMA := ,
EMPTY :=
SPACE := $(EMPTY) $(EMPTY)
# platform_list: "amd64 arm64" -> "linux/amd64,linux/arm64" for buildx --platform.
platform_list = $(subst $(SPACE),$(COMMA),$(patsubst %,linux/%,$(1)))
# The small switch every image build uses: --push (release) vs --load (default).
# CACHE_ONLY=true skips the export for callers that only want the build cache and
# never use the image, such as warm-cache.
DOCKER_OUTPUT = $(if $(filter true,$(PUSH)),--push,$(if $(filter true,$(CACHE_ONLY)),--output=type=cacheonly,--load))
# build_platforms: all of an image's arches when pushing a manifest or only
# caching, just the target arch when loading locally, since buildx can only
# --load one platform. Warming one arch would leave the release build to
# compile the other from scratch.
build_platforms = $(if $(or $(filter true,$(PUSH)),$(filter true,$(CACHE_ONLY))),$(call platform_list,$(1)),linux/$(ARCH))

# docker-build-and-push-<image> is docker-build-<image> with PUSH=true, which
# switches that build to --push and records the published artifact. There is no
# docker-push-<image>: a multi-arch image cannot be loaded into the local docker
# daemon, so it can only be published by the build that produces it. This one
# pattern rule covers every image, including ones added later.
docker-build-and-push-%: PUSH := true
docker-build-and-push-%: docker-build-%
	@:

.PHONY: binary-hostagent
binary-hostagent: ## Build the hostagent binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -buildvcs=false -ldflags="$(GO_LDFLAGS)" -gcflags="$(GO_GCFLAGS)" -trimpath -o $(LOCALBIN)/hostagent github.com/nvidia/doca-platform/cmd/hostagent

# Setup docker buildx builder with docker-container driver for cache export support
BUILDKITD_CONFIG ?=
.PHONY: docker-buildx-setup
docker-buildx-setup:
	@if ! docker buildx inspect dpf-builder > /dev/null 2>&1; then \
		echo "Creating buildx builder 'dpf-builder'..."; \
		if [ -f "$(BUILDKITD_CONFIG)" ]; then \
			echo "Using BuildKit config: $(BUILDKITD_CONFIG)"; \
			docker buildx create --name dpf-builder --driver docker-container --use --bootstrap --config "$(BUILDKITD_CONFIG)" > /dev/null; \
		else \
			docker buildx create --name dpf-builder --driver docker-container --use --bootstrap > /dev/null; \
		fi; \
	else \
		docker buildx use dpf-builder > /dev/null 2>&1 || true; \
	fi

.PHONY: docker-build-all
docker-build-all: docker-buildx-setup $(addprefix docker-build-,$(DOCKER_BUILD_TARGETS)) ## Build docker images for all DOCKER_BUILD_TARGETS. Architecture defaults to build system architecture unless overridden or hardcoded.

DPF_SYSTEM_IMAGE_NAME ?= dpf-system
export DPF_SYSTEM_IMAGE ?= $(REGISTRY)/$(DPF_SYSTEM_IMAGE_NAME)
export DPF_SYSTEM_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(DPF_SYSTEM_IMAGE_NAME)

HOSTDRIVER_IMAGE_NAME ?= hostdriver
export HOSTDRIVER_IMAGE ?= $(REGISTRY)/$(HOSTDRIVER_IMAGE_NAME)
export HOSTDRIVER_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(HOSTDRIVER_IMAGE_NAME)

IPALLOCATOR_IMAGE_NAME ?= ip-allocator
export IPALLOCATOR_IMAGE ?= $(REGISTRY)/$(IPALLOCATOR_IMAGE_NAME)

BFB_REGISTRY_IMAGE_NAME ?= bfb-registry
export BFB_REGISTRY_IMAGE ?= $(REGISTRY)/$(BFB_REGISTRY_IMAGE_NAME)
export BFB_REGISTRY_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(BFB_REGISTRY_IMAGE_NAME)

BFB_DPUAGENT_IMAGE_NAME ?= bfb-dpuagent
export BFB_DPUAGENT_IMAGE ?= $(REGISTRY)/$(BFB_DPUAGENT_IMAGE_NAME)

DUMMYDPUSERVICE_IMAGE_NAME ?= dummydpuservice
export DUMMYDPUSERVICE_IMAGE ?= $(REGISTRY)/$(DUMMYDPUSERVICE_IMAGE_NAME)

NETUTILS_IMAGE_NAME ?= netutils
export NETUTILS_IMAGE ?= $(REGISTRY)/$(NETUTILS_IMAGE_NAME)

RSHIM_CONSOLE_COLLECTOR_IMAGE_NAME ?= rshim-console-collector
export RSHIM_CONSOLE_COLLECTOR_IMAGE ?= $(REGISTRY)/$(RSHIM_CONSOLE_COLLECTOR_IMAGE_NAME)

CNIINSTALLER_IMAGE_NAME ?= dpf-cni-installer
export CNIINSTALLER_IMAGE ?= $(REGISTRY)/$(CNIINSTALLER_IMAGE_NAME)
export CNIINSTALLER_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(CNIINSTALLER_IMAGE_NAME)

MOCK_DMS_IMAGE_NAME ?= mock-dms
MOCK_DMS_IMAGE ?= $(REGISTRY)/$(MOCK_DMS_IMAGE_NAME)

FAKE_FS_STORAGE_IMAGE_NAME ?= fake-fs-storage-vendor-dpu-plugin
export FAKE_FS_STORAGE_IMAGE ?= $(REGISTRY)/$(FAKE_FS_STORAGE_IMAGE_NAME)

STORAGE_SYSTEM_IMAGE_NAME = storage-system
export STORAGE_SYSTEM_IMAGE ?= $(REGISTRY)/$(STORAGE_SYSTEM_IMAGE_NAME)
export STORAGE_SYSTEM_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(STORAGE_SYSTEM_IMAGE_NAME)

STORAGE_HOST_IMAGE_NAME = storage-host
export STORAGE_HOST_IMAGE ?= $(REGISTRY)/$(STORAGE_HOST_IMAGE_NAME)
export STORAGE_HOST_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(STORAGE_HOST_IMAGE_NAME)

KEEPALIVED_IMAGE_NAME = dpf-keepalived
export KEEPALIVED_IMAGE ?= $(REGISTRY)/$(KEEPALIVED_IMAGE_NAME)
export KEEPALIVED_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(KEEPALIVED_IMAGE_NAME)

DPF_SYSTEM_ARCH ?= $(HOST_ARCH) $(DPU_ARCH)

## Ubuntu mirror for building the images.
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu/

## Download and package aptitude source code for all related images.
## This is used to speedup our CI builds by disabling the source code download&package step.
## Note: not all images have additional packages, so this env does not apply to all images.
PACKAGE_SOURCES ?= true

## Control whether to create BuildKit history logs for the docker builds.
DOCKER_BUILD_LOGGING ?= false

.PHONY: docker-build-dpf-system # Build the DPF System image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-dpf-system: docker-buildx-setup generate-manifests-release-defaults $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg TAG=$(TAG) \
		-f Dockerfile.dpf-system \
		. \
		-t $(DPF_SYSTEM_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(DPF_SYSTEM_IMAGE_NAME),$(DPF_SYSTEM_IMAGE):$(TAG),release))

.PHONY: docker-build-ipallocator
docker-build-ipallocator: docker-buildx-setup $(ARTIFACTS_DIR) ## Build docker image for the IP Allocator
	# Base image can't be distroless because of the readiness probe that is using cat which doesn't exist in distroless
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(ARCH) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(ALPINE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg package=./cmd/ipallocator \
		-f Dockerfile \
		. \
		-t $(IPALLOCATOR_IMAGE):$(TAG)

.PHONY: docker-build-hostdriver # Build the hostdriver image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-hostdriver: docker-buildx-setup $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		--pull \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg hostdriver_base_image=$(HOSTDRIVER_BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		--build-arg TAG=$(TAG) \
		-t $(HOSTDRIVER_IMAGE):$(TAG) \
		-f Dockerfile.hostdriver \
		.
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(HOSTDRIVER_IMAGE_NAME),$(HOSTDRIVER_IMAGE):$(TAG),release))

.PHONY: docker-build-dummydpuservice
docker-build-dummydpuservice: docker-buildx-setup $(ARTIFACTS_DIR) ## Build docker images for the dummydpuservice
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(DPU_ARCH) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg package=./cmd/dummydpuservice \
		-f Dockerfile \
		. \
		-t $(DUMMYDPUSERVICE_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(DUMMYDPUSERVICE_IMAGE_NAME),$(DUMMYDPUSERVICE_IMAGE):$(TAG),test_helper))

.PHONY: docker-build-netutils # Build the netutils image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-netutils: docker-buildx-setup $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--progress=plain \
		-f Dockerfile.netutils \
		. \
		-t $(NETUTILS_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(NETUTILS_IMAGE_NAME),$(NETUTILS_IMAGE):$(TAG),test_helper))

.PHONY: docker-build-rshim-console-collector
docker-build-rshim-console-collector: docker-buildx-setup $(ARTIFACTS_DIR) ## Build the rshim console collector image.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg package=./test/rshim-console-collector \
		-f Dockerfile \
		. \
		-t $(RSHIM_CONSOLE_COLLECTOR_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(RSHIM_CONSOLE_COLLECTOR_IMAGE_NAME),$(RSHIM_CONSOLE_COLLECTOR_IMAGE):$(TAG),test_helper))

.PHONY: docker-build-mock-dms
docker-build-mock-dms: docker-buildx-setup $(ARTIFACTS_DIR) ## Build docker images for the mock-dms
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(ARCH) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		-f test/mock/dms/Dockerfile \
		. \
		-t $(MOCK_DMS_IMAGE):$(TAG)

# Fake fs-storage vendor DPU plugin for SNAP e2e tests
# Binary is named /fs-storage-vendor-dpu-plugin (matching the Helm chart command)
# DPU_ARCH, not ARCH: the plugin only ever runs on the DPU.
.PHONY: docker-build-fake-fs-storage
docker-build-fake-fs-storage: docker-buildx-setup $(ARTIFACTS_DIR) ## Build docker image for the fake fs-storage vendor DPU plugin (test only)
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(DPU_ARCH) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		-f test/mock/fake-fs-storage-vendor-dpu-plugin/Dockerfile \
		. \
		-t $(FAKE_FS_STORAGE_IMAGE):$(TAG)

.PHONY: docker-build-storage-system # Build the DPF storage system image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-storage-system: docker-buildx-setup $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg TAG=$(TAG) \
		-f Dockerfile.storage-system \
		. \
		-t $(STORAGE_SYSTEM_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(STORAGE_SYSTEM_IMAGE_NAME),$(STORAGE_SYSTEM_IMAGE):$(TAG),release))

.PHONY: docker-build-storage-host # Build the storage-host image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-storage-host: docker-buildx-setup $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg storage_host_base_image=$(STORAGE_HOST_BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		--build-arg TAG=$(TAG) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		-f Dockerfile.storage-host \
		. \
		-t $(STORAGE_HOST_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(STORAGE_HOST_IMAGE_NAME),$(STORAGE_HOST_IMAGE):$(TAG),release))

.PHONY: docker-build-keepalived # Build the keepalived image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-keepalived: docker-buildx-setup $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		--provenance=false \
		--progress=plain \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		-t $(KEEPALIVED_IMAGE):$(TAG) \
		-f Dockerfile.keepalived \
		.
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(KEEPALIVED_IMAGE_NAME),$(KEEPALIVED_IMAGE):$(TAG),release))

.PHONY: docker-build-bfb-registry # Build the BFB Registry image. Release (PUSH=true) builds all $(DPF_SYSTEM_ARCH) and pushes the manifest in one step; otherwise loads linux/$(ARCH) locally.
docker-build-bfb-registry: docker-buildx-setup $(ARTIFACTS_DIR)
	# Provenance false ensures this target builds an image rather than a manifest when using buildx.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg bfb_registry_base_image=$(BFB_REGISTRY_BASE_IMAGE) \
		--provenance=false \
		--platform=$(call build_platforms,$(DPF_SYSTEM_ARCH)) \
		--progress=plain \
		-f Dockerfile.bfb-registry \
		. \
		-t $(BFB_REGISTRY_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(BFB_REGISTRY_IMAGE_NAME),$(BFB_REGISTRY_IMAGE):$(TAG),release))

.PHONY: docker-build-cni-installer
docker-build-cni-installer: docker-buildx-setup $(ARTIFACTS_DIR) ## Build docker image for the CNI installer
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(DPU_ARCH) \
		--progress=plain \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags="$(GO_LDFLAGS)" \
		--build-arg gcflags="$(GO_GCFLAGS)" \
		-f Dockerfile.cni-installer \
		. \
		-t $(CNIINSTALLER_IMAGE):$(TAG)
	$(if $(filter true,$(PUSH)),@$(call record_release_artifact,image,$(CNIINSTALLER_IMAGE_NAME),$(CNIINSTALLER_IMAGE):$(TAG),release))

# Special-purpose image (HOST_ARCH only) not in default builds - built explicitly in CI
.PHONY: docker-build-bfb-dpuagent
docker-build-bfb-dpuagent: ## Build OCI image containing BFB with dpuagent
	# BFB_FILENAME is read from build/.bfb-dpuagent-target generated by BFB build script.
	$(CURDIR)/hack/scripts/docker-build.sh \
		$(DOCKER_OUTPUT) \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(HOST_ARCH) \
		--progress=plain \
		--build-arg BFB_FILENAME=$(shell cat $(CURDIR)/build/.bfb-dpuagent-target) \
		--build-context artifacts=$(CURDIR)/build/bfbs \
		-f Dockerfile.bfb-dpuagent \
		. \
		-t $(BFB_DPUAGENT_IMAGE):$(TAG)

.PHONY: docker-build-and-push-all
docker-build-and-push-all: $(addprefix docker-build-and-push-,$(DOCKER_BUILD_TARGETS))  ## Build and push the docker images for all DOCKER_BUILD_TARGETS.

# helm charts

# By default the helm registry is assumed to be an OCI registry.
DEFAULT_HELM_REGISTRY=oci://$(REGISTRY)
# This variable should be overwritten when using a https helm repository.
export HELM_REGISTRY ?= $(DEFAULT_HELM_REGISTRY)
# This variable should be overwritten with the registry of the upstream artifacts. Needed when making a release upstream.
# This variable ensures that the values injected in the operator and charts point to the upstream artifacts.
export UPSTREAM_HELM_REGISTRY ?= $(HELM_REGISTRY)

HELM_TARGETS ?= dpu-networking operator storage

# metadata for the operator helm chart
OPERATOR_HELM_CHART_NAME ?= dpf-operator
OPERATOR_HELM_CHART ?= $(HELMDIR)/$(OPERATOR_HELM_CHART_NAME)

## metadata for dpu-networking helm chart.
export DPU_NETWORKING_HELM_CHART_NAME = dpu-networking
DPU_NETWORKING_HELM_CHART ?= $(HELMDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)
DPU_NETWORKING_HELM_CHART_VER ?= $(TAG)

# metadata for dummydpuservice.
DUMMYDPUSERVICE_HELM_CHART_NAME = dummydpuservice-chart
DUMMYDPUSERVICE_HELM_CHART ?= $(DPUSERVICESDIR)/dummydpuservice/chart

# metadata for storage chart.
STORAGE_CHART_NAME = dpf-storage
STORAGE_CHART ?= $(DPUSERVICESDIR)/storage/chart
STORAGE_CHART_VER ?= $(TAG)

# metadata for mock dms.
MOCK_DMS_HELM_CHART ?=test/mock/dms/chart
export KWOK_HELM_CHART=test/mock/kwok/chart
KWOK_HELM_CHART_NAME=kwok

.PHONY: helm-package-all
helm-package-all: $(addprefix helm-package-,$(HELM_TARGETS))  ## Package the helm charts for all components.

.PHONY: helm-package-dpu-networking
helm-package-dpu-networking: $(CHARTSDIR) helm yq ## Package helm chart for service chain controller
	HELM_CHART_DIR="$(DPU_NETWORKING_HELM_CHART)" \
	HELM_CHART_TAGS="$(DPU_NETWORKING_HELM_CHART_VER)" \
	./hack/scripts/release-helm-package.sh

OPERATOR_CHART_TAGS ?=$(TAG)
.PHONY: helm-package-operator
helm-package-operator: $(CHARTSDIR) helm yq ## Package helm chart for DPF Operator
	# Generate rendered helmfile manifests for prerequisites and monitoring.
	@mkdir -p $(OPERATOR_HELM_CHART)/dev/helmfiles

	# Copy helmfile manifests and scripts to the operator helm chart directory.
	@cp -a $(CURDIR)/deploy/helmfiles/* $(OPERATOR_HELM_CHART)/dev/helmfiles/

	# Generate the helm chart package.
	HELM_CHART_DIR="$(OPERATOR_HELM_CHART)" \
	HELM_CHART_TAGS="$(OPERATOR_CHART_TAGS)" \
	SET_IMAGE_IN_VALUES=true \
	REPO="$(DPF_SYSTEM_UPSTREAM_IMAGE)" \
	IMAGE_REPO_PATH=controllerManager.image.repository \
	IMAGE_TAG_PATH=controllerManager.image.tag \
	./hack/scripts/release-helm-package.sh

.PHONY: helm-package-dummydpuservice
helm-package-dummydpuservice: $(DPUSERVICESDIR) helm yq ## Package helm chart for dummydpuservice
	HELM_CHART_DIR="$(DUMMYDPUSERVICE_HELM_CHART)" \
	HELM_CHART_TAGS="$(TAG)" \
	SET_IMAGE_IN_VALUES=true \
	REPO="$(DUMMYDPUSERVICE_IMAGE)" \
	IMAGE_REPO_PATH=image.repository \
	IMAGE_TAG_PATH=image.tag \
	RELEASE_HELM_SET_ANNOTATIONS="false" \
	./hack/scripts/release-helm-package.sh

.PHONY: helm-package-storage
helm-package-storage: $(CHARTSDIR) helm yq generate-manifests-storage
	HELM_CHART_DIR="$(STORAGE_CHART)" \
	HELM_CHART_TAGS="$(TAG)" \
	./hack/scripts/release-helm-package.sh

HELM_CM_PUSH_OPTS ?=
HELM_PUSH_CMD ?= $(shell if echo $(HELM_REGISTRY) | grep -q '^http'; then echo cm-push $(HELM_CM_PUSH_OPTS); else echo push; fi)

.PHONY: helm-push-all
helm-push-all: $(addprefix helm-push-,$(HELM_TARGETS))  ## Push the helm charts for all components.

.PHONY: helm-push-operator
helm-push-operator: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for dpf-operator
	@for tag in $(OPERATOR_CHART_TAGS); do \
		$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(OPERATOR_HELM_CHART_NAME)-$$tag.tgz $(HELM_REGISTRY); \
		$(call record_release_artifact,chart,$(OPERATOR_HELM_CHART_NAME),$(HELM_REGISTRY)/$(OPERATOR_HELM_CHART_NAME):$$tag,release) \
	done

.PHONY: helm-push-dpu-networking
helm-push-dpu-networking: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for service chain controller
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz $(HELM_REGISTRY)
	@$(call record_release_artifact,chart,$(DPU_NETWORKING_HELM_CHART_NAME),$(HELM_REGISTRY)/$(DPU_NETWORKING_HELM_CHART_NAME):$(DPU_NETWORKING_HELM_CHART_VER),release)


.PHONY: helm-push-dummydpuservice
helm-push-dummydpuservice: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for dummydpuservice
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(DUMMYDPUSERVICE_HELM_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)
	@$(call record_release_artifact,chart,$(DUMMYDPUSERVICE_HELM_CHART_NAME),$(HELM_REGISTRY)/$(DUMMYDPUSERVICE_HELM_CHART_NAME):$(TAG),test_helper)



.PHONY: helm-push-storage
helm-push-storage: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for storage
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(STORAGE_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)
	@$(call record_release_artifact,chart,$(STORAGE_CHART_NAME),$(HELM_REGISTRY)/$(STORAGE_CHART_NAME):$(TAG),release)

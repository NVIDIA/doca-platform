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

## Include Make modules which are split up in this repo for better structure.
include hack/tools/tools.mk

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

# The latest stable tag is used in various places to refer to the latest stable release of DPF.
LATEST_STABLE_TAG = v25.7.0

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

GO_VERSION ?= $(shell awk '/^go /{print $$2}' go.mod | awk -F '.' '{print $$1 "." $$2}')

# Allows for defining additional Go test args, e.g. '-tags integration'.
# The linkmode=internal flag is used to force using Go linker to do the linking.
# This suppresses warnings like ".../00NNNN.o has malformed LC_DYSYMTAB".
# See the following issue for more details: https://github.com/golang/go/issues/61229#issuecomment-1988965927
GO_TEST_ARGS ?= -race -ldflags=-linkmode=internal

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
REPOSDIR ?= $(PROJECT_DIR)/hack/repos
HELMDIR ?= $(PROJECT_DIR)/deploy/charts
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

# OVS CNI
# A third party import to the repo. In future this will be further integrated.
OVS_CNI_DIR=$(THIRDPARTYDIR)/ovs-cni

# OVN Kubernetes dependencies to be able to build its docker image
OVNKUBERNETES_REF=e485dd2aa5980c724b0021dc2127cfcb9a5183da
OVNKUBERNETES_DIR=$(REPOSDIR)/ovn-kubernetes/ovn-kubernetes-$(OVNKUBERNETES_REF)
$(OVNKUBERNETES_DIR): | $(REPOSDIR)
	git clone https://github.com/mellanox/ovn-kubernetes $(OVNKUBERNETES_DIR)-tmp
	cd $(OVNKUBERNETES_DIR)-tmp && git reset --hard $(OVNKUBERNETES_REF)
	mv $(OVNKUBERNETES_DIR)-tmp $(OVNKUBERNETES_DIR)
	# Delete old ovn-kubernetes directories.
	find $(REPOSDIR)/ovn-kubernetes/ -mindepth 1 -maxdepth 1 -not -name ovn-kubernetes-$(OVNKUBERNETES_REF) -exec rm -rf '{}' \;

DOCA_SOSREPORT_REPO_URL=https://github.com/NVIDIA/doca-sosreport/archive/$(DOCA_SOSREPORT_REF).tar.gz
DOCA_SOSREPORT_REF=6b4289b9f0d9f26af177b0d1c4c009ca74bb514a
SOS_REPORT_DIR=$(REPOSDIR)/doca-sosreport-$(DOCA_SOSREPORT_REF)
$(SOS_REPORT_DIR): | $(REPOSDIR)
	curl -sL ${DOCA_SOSREPORT_REPO_URL} | tar -xz -C ${REPOSDIR}

# nvidia-external-attacher dependencies to be able to build its docker image
EXTERNAL_ATTACHER_BRANCH=release-4.9
NVIDIA_EXTERNAL_ATTACHER_DIR=third_party/forked/nvidia-external-attacher

# VPC dependencies to be able to build/push images and charts
VPC_REF=cb191c005da20ec292bd0b8e89151f0f93530512
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
	ovn-kubernetes storage mock-dms

.PHONY: generate
generate: ## Run all generate-* targets: generate-modules generate-manifests-* and generate-go-deepcopy-*.
	$(MAKE) generate-mocks generate-modules generate-manifests generate-go-deepcopy generate-docs generate-client-for-storage-nvidia-external-attacher

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
	$(MAKE) clean-generated-yaml SRC_DIRS="./deploy/charts/dpf-operator/templates/crds/"
	$(CONTROLLER_GEN) \
	paths="./cmd/operator/..." \
	paths="./cmd/kamaji-cluster-manager/..." \
	paths="./cmd/static-cluster-manager/..." \
	paths="./internal/operator/..." \
	paths="./internal/clustermanager/..." \
	paths="./internal/provisioning/..." \
	paths="./api/operator/..." \
	crd:crdVersions=v1 \
	rbac:roleName="dpf-operator-manager-role" \
	output:crd:dir=./config/operator-crds \
	output:rbac:dir=./deploy/charts/dpf-operator/templates
	## Copy CRD definitions to the operator helm directory
	$(KUSTOMIZE) build config/operator-crds -o  deploy/charts/dpf-operator/templates/crds/;

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
	find config/dpuservice/crd/bases/ -type f -not -name '*_dpu*' -exec cp {} deploy/charts/dpu-networking/charts/servicechainset-controller/templates/crds/ \;

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

.PHONY: generate-manifests-ovn-kubernetes-resource-injector
generate-manifests-ovn-kubernetes-resource-injector: envsubst ## Generate manifests e.g. CRD, RBAC. for the OVN Kubernetes Resource Injector
	$(ENVSUBST) < deploy/charts/ovn-kubernetes-resource-injector/values.yaml.tmpl > deploy/charts/ovn-kubernetes-resource-injector/values.yaml

RELEASE_FILE = ./internal/release/manifests/defaults.yaml

.PHONY: generate-manifests-release-defaults
generate-manifests-release-defaults: envsubst ## Generates manifests that contain the default values that should be used by the operators
	$(ENVSUBST) <  ./internal/release/templates/defaults.yaml.tmpl > $(RELEASE_FILE)

TEMPLATES_DIR ?= $(PROJECT_DIR)/internal/operator/inventory/templates
EMBEDDED_MANIFESTS_DIR ?= $(PROJECT_DIR)/internal/operator/inventory/manifests
.PHONY: generate-manifests-operator-embedded
generate-manifests-operator-embedded: kustomize envsubst generate-manifests-dpuservice generate-manifests-provisioning generate-manifests-release-defaults generate-manifests-kamaji-cluster-manager generate-manifests-static-cluster-manager ## Generates manifests that are embedded into the operator binary.
	# Reorder none here ensure that we generate the kustomize files in a specific order to be consumed by the DPF Operator.
	$(KUSTOMIZE) build --reorder=none config/provisioning/default > $(EMBEDDED_MANIFESTS_DIR)/provisioning-controller.yaml
	$(KUSTOMIZE) build --reorder=none config/dpu-detector > $(EMBEDDED_MANIFESTS_DIR)/dpu-detector.yaml
	$(KUSTOMIZE) build --reorder=none config/dpuservice/default > $(EMBEDDED_MANIFESTS_DIR)/dpuservice-controller.yaml
	$(KUSTOMIZE) build --reorder=none config/kamaji-cluster-manager/default > $(EMBEDDED_MANIFESTS_DIR)/kamaji-cluster-manager.yaml
	$(KUSTOMIZE) build --reorder=none config/static-cluster-manager/default > $(EMBEDDED_MANIFESTS_DIR)/static-cluster-manager.yaml
	$(KUSTOMIZE) build --reorder=none config/bfb_registry > $(EMBEDDED_MANIFESTS_DIR)/bfb-registry.yaml

.PHONY: generate-manifests-sfc-controller
generate-manifests-sfc-controller: envsubst generate-manifests-servicechainset
	cp deploy/charts/dpu-networking/charts/servicechainset-controller/templates/crds/svc.dpu.nvidia.com_servicechains.yaml deploy/charts/dpu-networking/charts/sfc-controller/templates/crds/
	cp deploy/charts/dpu-networking/charts/servicechainset-controller/templates/crds/svc.dpu.nvidia.com_serviceinterfaces.yaml deploy/charts/dpu-networking/charts/sfc-controller/templates/crds/

.PHONY: generate-manifests-provisioning
generate-manifests-provisioning: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC. for the DPF provisioning controller.
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/provisioning/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./cmd/provisioning/..." \
	paths="./internal/provisioning/..." \
	paths="./api/provisioning/..." \
	crd:crdVersions=v1,generateEmbeddedObjectMeta=true \
	rbac:roleName=manager-role \
	output:crd:dir=./config/provisioning/crd/bases \
	output:rbac:dir=./config/provisioning/rbac \
	output:webhook:dir=./config/provisioning/webhook \
	webhook

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

.PHONY: generate-manifests-ovn-kubernetes
generate-manifests-ovn-kubernetes: $(OVNKUBERNETES_DIR) envsubst ## Generate manifests for ovn-kubernetes
	$(ENVSUBST) < $(OVNKUBERNETES_HELM_CHART)/values.yaml.tmpl > $(OVNKUBERNETES_HELM_CHART)/values.yaml


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
	$(HELM_DOCS) --ignore-file=.helmdocsignore

.PHONY: generate-docs-embedmd
generate-docs-embedmd: embedmd ## Embed additional files into markdown docs.
	grep -rl --include \*.md -e '\[embedmd\]' docs | xargs $(EMBEDMD) -w

.PHONY: generate-client-for-storage-nvidia-external-attacher
generate-client-for-storage-nvidia-external-attacher: client-gen lister-gen informer-gen deepcopy-gen # Generate client/lister/informer for sv-volumeattachment
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
	  | xargs -n1 $(SHFMT) -w -bn -sr

##@ Testing

TESTPKGS ?= $$(go list ./... | grep -v /e2e | grep -v /third_party)

.PHONY: test
test: envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test $(TESTPKGS) $(GO_TEST_ARGS)

.PHONY: test-report
test-report: envtest gotestsum ## Run tests and generate a junit style report
	set +o errexit; KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test -count 1 -race -json $(TESTPKGS) -coverprofile cover.out > junit.stdout; echo $$? > junit.exitcode;
	$(GOTESTSUM) --junitfile junit.xml --raw-command cat junit.stdout
	exit $$(cat junit.exitcode)

.PHONY: test-release-e2e-quick
test-release-e2e-quick: # Build images required for the quick DPF e2e test.
	$(MAKE) docker-build-dpf-system-for-$(ARCH) docker-push-dpf-system-for-$(ARCH)
	$(MAKE) docker-build-dummydpuservice docker-push-dummydpuservice
	$(MAKE) docker-build-mock-dms docker-push-mock-dms
	# Build and push all the helm charts
	$(MAKE) helm-package-all helm-push-all
	$(MAKE) helm-package-dummydpuservice helm-push-dummydpuservice

.PHONY: test-release-e2e-slow
test-release-e2e-slow: release # Build images required for the slow DPF e2e tests.
	$(MAKE) docker-build-dummydpuservice docker-push-dummydpuservice
	$(MAKE) helm-package-dummydpuservice helm-push-dummydpuservice


TEST_CLUSTER_NAME := dpf-test
ADD_CONTROL_PLANE_TAINTS ?= true
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
.PHONY: test-deploy-operator-helm
test-deploy-operator-helm: helm helm-package-operator ## Deploy the DPF Operator using helm
	# Deploy the DPF Operator prerequisites.
	$(MAKE) HELMFILE_FILE=$(CURDIR)/deploy/helmfiles/prereqs.yaml test-deploy-helmfile

	# Deploy the DPF Operator.
	$(HELM) upgrade --install --create-namespace --namespace $(OPERATOR_NAMESPACE) \
		--set controllerManager.image.repository=$(DPF_SYSTEM_IMAGE)\
		--set controllerManager.image.tag=$(TAG) \
		--set imagePullSecrets[0].name=dpf-pull-secret \
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
.PHONY: test-deploy-helmfile
test-deploy-helmfile: helmfile helm helm-diff helm-git ## Deploy helm dependencies from local helmfile
	@$(CURDIR)/hack/scripts/deploy-helmfile.sh \
		--file "$(HELMFILE_FILE)" \
		--helmfile-bin "$(HELMFILE)" \
		$(if $(strip $(HELMFILE_ENV)),--environment "$(HELMFILE_ENV)") \
		$(if $(strip $(HELMFILE_SELECTOR)),--selector "$(HELMFILE_SELECTOR)") \
		--helm-bin "$(HELM)"

ARTIFACTS_DIR ?= $(CURDIR)/artifacts
ARTIFACTS_SUBDIR ?=

E2E_TEST_ARGS ?= -v -ginkgo.v -ginkgo.fail-fast -ginkgo.label-filter="DPFSystem && !SDN && !DPFVPCOVN" -e2e.config=./config-quick.yaml
GINKGO_FOCUS ?= ""
# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e ## Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: stern ## Run e2e tests
	STERN=$(STERN) ARTIFACTS=$(ARTIFACTS_DIR)/$(ARTIFACTS_SUBDIR) $(CURDIR)/hack/scripts/stern-log-collector.sh \
	  go test -timeout 0 ./test/e2e/ -ginkgo.focus="$(GINKGO_FOCUS)" $(E2E_TEST_ARGS)

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

VERIFY_TARGETS ?= generate copyright md-links shfmt

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
	$(LYCHEE) --accept 200,429 . *.md --exclude-path third_party --exclude-path ./deploy --exclude-path docs/do_not_publish # Exclude the external `third_party` docs and the generated `charts` docs.

export CRDIFY_BASE_REF ?= v25.7.0
export CRDIFY_COMPARE_REF ?= HEAD
export CRDIFY_CONFIG ?= $(PROJECT_DIR)/crdify.yaml
export CRDIFY_CRD_DIR = $(patsubst $(PROJECT_DIR)/%,%,$(CRDDIR))
.PHONY: verify-crdify
verify-crdify: binary-dpfdev ## Verify that the CRDs are valid
	hack/scripts/crd-validation.sh

.PHONY: lint-helm
lint-helm: lint-helm-dpu-networking lint-helm-ovn-kubernetes lint-helm-dummydpuservice lint-helm-storage

.PHONY: lint-helm-dpu-networking
lint-helm-dpu-networking: helm ## Run helm lint for servicechainset chart
	$Q $(HELM) lint $(DPU_NETWORKING_HELM_CHART)

.PHONY: lint-helm-ovn-kubernetes
lint-helm-ovn-kubernetes: generate-manifests-ovn-kubernetes helm ## Run helm lint for ovn-kubernetes chart
	$Q $(HELM) lint $(OVNKUBERNETES_HELM_CHART)

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
	# Build amd64 images which will run on x86 hosts.
	$(MAKE) ARCH=$(HOST_ARCH) $(addprefix docker-build-,$(HOST_ARCH_DOCKER_BUILD_TARGETS))

	# Package the helm charts.
	$(MAKE) helm-package-all

	# Build vpc release artifacts
	$(MAKE) release-build-vpc

# controls whether the charts should be pushed in both REGISTRY and HELM_REGISTRY, in case the two are different
export RELEASE_PUSH_HELM_CHARTS_TO_REGISTRY ?= false

.PHONY: release
release: release-build release-dpfctl-ngc ## Build and push helm and container images for release.

	# Push all of the images
	$(MAKE) docker-push-all

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

.PHONY: release-push-vpc
release-push-vpc: $(VPC_DIR) ## Push vpc release artifacts
	@cd $(VPC_DIR); $(MAKE) release-push
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
	$(MAKE) release-build test lint

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

STORAGE_SNAP_CSI_DRIVER_GO_LDFLAGS ?= "$(shell echo $(GO_LDFLAGS)) -X github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/common.VendorVersion=$(TAG)"
NVIDIA_EXTERNAL_ATTACHER_GO_LDFLAGS ?= "$(shell echo $(GO_LDFLAGS)) -X github.com/kubernetes-csi/external-attacher/cmd/csi-attacher/main.version=$(TAG)"

BUILD_TARGETS ?= $(DPU_ARCH_BUILD_TARGETS)
DPF_SYSTEM_BUILD_TARGETS ?= operator provisioning dpuservice servicechainset kamaji-cluster-manager static-cluster-manager \
	sfc-controller dpfctl dpudetector
DPU_ARCH_BUILD_TARGETS ?=
# contains list of storage-related binaries that have no system-level dependencies
STORAGE_SYSTEM_BUILD_TARGETS ?= storage-snap-host-controller storage-snap-node-driver block-storage-vendor-dpu-plugin storage-snap-csi-plugin storage-nvidia-external-attacher nfs-storage-vendor-dpu-plugin
# contains list of storage-related binaries that have system-level dependencies (depend on some linux utils from the container)
STORAGE_HOST_BUILD_TARGETS ?= storage-snap-csi-plugin fs-storage-vendor-dpu-plugin

BUILD_IMAGE ?= docker.io/library/golang:$(GO_VERSION)

HOST_ARCH = amd64
DPU_ARCH = arm64

# Use distroless as minimal base image to package the manager binary
BASE_IMAGE = nvcr.io/nvidia/doca/dpf_containers:1.0.2-ubuntu22.04-distroless
ALPINE_IMAGE = alpine:3.19
# Base image for hostdriver (DOCA full runtime host image)
HOSTDRIVER_BASE_IMAGE ?= nvcr.io/nvidia/doca/doca:3.1.0-full-rt-host

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
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/operator github.com/nvidia/doca-platform/cmd/operator

.PHONY: binary-provisioning
binary-provisioning: ## Build the provisioning controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/provisioning github.com/nvidia/doca-platform/cmd/provisioning

.PHONY: binary-kamaji-cluster-manager
binary-kamaji-cluster-manager: ## Build the kamaji-cluster-manager binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/kamaji-cluster-manager github.com/nvidia/doca-platform/cmd/kamaji-cluster-manager

.PHONY: binary-static-cluster-manager
binary-static-cluster-manager: ## Build the static-cluster-manager binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/static-cluster-manager github.com/nvidia/doca-platform/cmd/static-cluster-manager

.PHONY: binary-dpuservice
binary-dpuservice: ## Build the dpuservice controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpuservice github.com/nvidia/doca-platform/cmd/dpuservice

.PHONY: binary-servicechainset
binary-servicechainset: ## Build the servicechainset controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/servicechainset github.com/nvidia/doca-platform/cmd/servicechainset

.PHONY: binary-dpucniprovisioner
binary-dpucniprovisioner: ## Build the DPU CNI Provisioner binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpucniprovisioner github.com/nvidia/doca-platform/cmd/dpucniprovisioner

.PHONY: binary-sfc-controller
binary-sfc-controller: ## Build the Host CNI Provisioner binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/sfc-controller github.com/nvidia/doca-platform/cmd/sfc-controller

.PHONY: binary-ipallocator
binary-ipallocator: ## Build the IP allocator binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/ipallocator github.com/nvidia/doca-platform/cmd/ipallocator

.PHONY: binary-dpudetector
binary-dpudetector: ## Build the DPU detector binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpu-detector github.com/nvidia/doca-platform/cmd/dpudetector

.PHONY: binary-dpuagent
binary-dpuagent: ## Build the DPU agent binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(DPU_ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpuagent github.com/nvidia/doca-platform/cmd/dpuagent

.PHONY: binary-ovn-kubernetes-resource-injector
binary-ovn-kubernetes-resource-injector: ## Build the OVN Kubernetes Resource Injector.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/ovnkubernetesresourceinjector github.com/nvidia/doca-platform/cmd/ovnkubernetesresourceinjector

.PHONY: binary-storage-snap-host-controller
binary-storage-snap-host-controller: ## Build the snap host controller controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/snap-host-controller github.com/nvidia/doca-platform/cmd/storage/snap-host-controller


.PHONY: binary-storage-snap-node-driver
binary-storage-snap-node-driver: ## Build the snap node driver controller binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/snap-node-driver github.com/nvidia/doca-platform/cmd/storage/snap-node-driver

.PHONY: binary-block-storage-vendor-dpu-plugin
binary-block-storage-vendor-dpu-plugin: ## Build the block storage vendor DPU plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/block-storage-vendor-dpu-plugin github.com/nvidia/doca-platform/cmd/storage/storage-vendor-dpu-plugin/block-storage-vendor-dpu-plugin

.PHONY: binary-fs-storage-vendor-dpu-plugin
binary-fs-storage-vendor-dpu-plugin: ## Build the AIO filesystem storage vendor DPU plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/fs-storage-vendor-dpu-plugin github.com/nvidia/doca-platform/cmd/storage/storage-vendor-dpu-plugin/fs-storage-vendor-dpu-plugin

.PHONY: binary-nfs-storage-vendor-dpu-plugin
binary-nfs-storage-vendor-dpu-plugin: ## Build the NFS filesystem storage vendor DPU plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/nfs-storage-vendor-dpu-plugin github.com/nvidia/doca-platform/cmd/storage/storage-vendor-dpu-plugin/nfs-storage-vendor-dpu-plugin

.PHONY: binary-storage-snap-csi-plugin
binary-storage-snap-csi-plugin: ## Build the snap-csi-plugin binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
		-ldflags=$(STORAGE_SNAP_CSI_DRIVER_GO_LDFLAGS) \
		-gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/snap-csi-plugin github.com/nvidia/doca-platform/cmd/storage/snap-csi-plugin

.PHONY: binary-storage-nvidia-external-attacher
binary-storage-nvidia-external-attacher: generate-client-for-storage-nvidia-external-attacher ## Build the nvidia external attacher binary.
	git submodule update --init --recursive
	./$(NVIDIA_EXTERNAL_ATTACHER_DIR)/hack/client.sh $(PROJECT_DIR) $(EXTERNAL_ATTACHER_BRANCH)
	# Needed so that we can capture the source code in the Dockerfile
	go mod vendor

	# Build nvidia-external-attacher binary
	cd $(NVIDIA_EXTERNAL_ATTACHER_DIR)/external-attacher && \
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/nvidia-external-attacher github.com/kubernetes-csi/external-attacher/cmd/csi-attacher

.PHONY: binary-dpfctl
binary-dpfctl: ## Build the dpfctl binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
		-ldflags="$(shell echo $(GO_LDFLAGS)) -X main.version=$(TAG)" \
		-gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpfctl github.com/nvidia/doca-platform/cmd/dpfctl

.PHONY: binary-dpfctl-release
binary-dpfctl-release: ## Build the dpfctl binary for all architectures.
	$Q mkdir -p $(LOCALBIN)/dpfctl-$(TAG)-release

	# Build for linux/amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="$(shell echo $(GO_LDFLAGS)) -X main.version=$(TAG)" \
		-gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpfctl-$(TAG)-release/dpfctl-linux-amd64 github.com/nvidia/doca-platform/cmd/dpfctl

	# Build for linux/arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
		-ldflags="$(shell echo $(GO_LDFLAGS)) -X main.version=$(TAG)" \
		-gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpfctl-$(TAG)-release/dpfctl-linux-arm64 github.com/nvidia/doca-platform/cmd/dpfctl

	# Build for darwin/arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
		-ldflags="$(shell echo $(GO_LDFLAGS)) -X main.version=$(TAG)" \
		-gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpfctl-$(TAG)-release/dpfctl-darwin-arm64 github.com/nvidia/doca-platform/cmd/dpfctl

.PHONY: binary-cni-installer
binary-cni-installer: ## Build the CNI installer binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/cni-installer github.com/nvidia/doca-platform/cmd/cniinstaller

.PHONY: install-dpfctl
install-dpfctl: binary-dpfctl ## Install the dpfctl binary.
	install -m 755 $(LOCALBIN)/dpfctl $(GOPATH)/bin/dpfctl

.PHONY: binary-dpfdev
binary-dpfdev: ## Build the dpfdev CLI tool
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build \
		-ldflags=$(GO_LDFLAGS) \
		-gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpfdev github.com/nvidia/doca-platform/hack/tools/dpfdev

DOCKER_BUILD_TARGETS=$(HOST_ARCH_DOCKER_BUILD_TARGETS) $(DPU_ARCH_DOCKER_BUILD_TARGETS) $(MULTI_ARCH_DOCKER_BUILD_TARGETS)
HOST_ARCH_DOCKER_BUILD_TARGETS=hostdriver bfb-registry
DPU_ARCH_DOCKER_BUILD_TARGETS=$(DPU_ARCH_BUILD_TARGETS) ovs-cni cni-installer
MULTI_ARCH_DOCKER_BUILD_TARGETS= dpf-system ovn-kubernetes storage-system storage-host

.PHONY: binary-hostagent
binary-hostagent: ## Build the hostagent binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/hostagent github.com/nvidia/doca-platform/cmd/hostagent

.PHONY: docker-build-all
docker-build-all: $(addprefix docker-build-,$(DOCKER_BUILD_TARGETS)) ## Build docker images for all DOCKER_BUILD_TARGETS. Architecture defaults to build system architecture unless overridden or hardcoded.

DPF_SYSTEM_IMAGE_NAME ?= dpf-system
export DPF_SYSTEM_IMAGE ?= $(REGISTRY)/$(DPF_SYSTEM_IMAGE_NAME)
export DPF_SYSTEM_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(DPF_SYSTEM_IMAGE_NAME)

OVNKUBERNETES_IMAGE_NAME = ovn-kubernetes
export OVNKUBERNETES_IMAGE = $(REGISTRY)/$(OVNKUBERNETES_IMAGE_NAME)

OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE_NAME = ovn-kubernetes-resource-injector
export OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE = $(REGISTRY)/$(OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE_NAME)

OVS_CNI_IMAGE_NAME ?= ovs-cni-plugin
export OVS_CNI_IMAGE ?= $(REGISTRY)/$(OVS_CNI_IMAGE_NAME)
export OVS_CNI_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(OVS_CNI_IMAGE_NAME)

HOSTDRIVER_IMAGE_NAME ?= hostdriver
export HOSTDRIVER_IMAGE ?= $(REGISTRY)/$(HOSTDRIVER_IMAGE_NAME)
export HOSTDRIVER_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(HOSTDRIVER_IMAGE_NAME)

IPALLOCATOR_IMAGE_NAME ?= ip-allocator
export IPALLOCATOR_IMAGE ?= $(REGISTRY)/$(IPALLOCATOR_IMAGE_NAME)

BFB_REGISTRY_IMAGE_NAME ?= bfb-registry
export BFB_REGISTRY_IMAGE ?= $(REGISTRY)/$(BFB_REGISTRY_IMAGE_NAME)
export BFB_REGISTRY_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(BFB_REGISTRY_IMAGE_NAME)


# Images that are running on DPU worker nodes (arm64)
DPUCNIPROVISIONER_IMAGE_NAME ?= dpu-cni-provisioner
DPUCNIPROVISIONER_IMAGE ?= $(REGISTRY)/$(DPUCNIPROVISIONER_IMAGE_NAME)

DUMMYDPUSERVICE_IMAGE_NAME ?= dummydpuservice
export DUMMYDPUSERVICE_IMAGE ?= $(REGISTRY)/$(DUMMYDPUSERVICE_IMAGE_NAME)

CNIINSTALLER_IMAGE_NAME ?= dpf-cni-installer
export CNIINSTALLER_IMAGE ?= $(REGISTRY)/$(CNIINSTALLER_IMAGE_NAME)
export CNIINSTALLER_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(CNIINSTALLER_IMAGE_NAME)

MOCK_DMS_IMAGE_NAME ?= mock-dms
MOCK_DMS_IMAGE ?= $(REGISTRY)/$(MOCK_DMS_IMAGE_NAME)

STORAGE_SYSTEM_IMAGE_NAME = storage-system
export STORAGE_SYSTEM_IMAGE ?= $(REGISTRY)/$(STORAGE_SYSTEM_IMAGE_NAME)
export STORAGE_SYSTEM_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(STORAGE_SYSTEM_IMAGE_NAME)

STORAGE_HOST_IMAGE_NAME = storage-host
export STORAGE_HOST_IMAGE ?= $(REGISTRY)/$(STORAGE_HOST_IMAGE_NAME)
export STORAGE_HOST_UPSTREAM_IMAGE ?= $(UPSTREAM_REGISTRY)/$(STORAGE_HOST_IMAGE_NAME)

DPF_SYSTEM_ARCH ?= $(HOST_ARCH) $(DPU_ARCH)

## Ubuntu mirror for building the images.
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu/

## Download and package aptitude source code for all related images.
## This is used to speedup our CI builds by disabling the source code download&package step.
## Note: not all images have additional packages, so this env does not apply to all images.
PACKAGE_SOURCES ?= true

.PHONY: docker-build-dpf-system # Build a multi-arch image for DPF System. The variable DPF_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-dpf-system: $(addprefix docker-build-dpf-system-for-,$(DPF_SYSTEM_ARCH))

docker-build-dpf-system-for-%: generate-manifests-release-defaults
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
		-f Dockerfile.dpf-system \
		. \
		-t $(DPF_SYSTEM_IMAGE):$(TAG)-$*

.PHONY: docker-push-dpf-system # Push a multi-arch image for DPF System using `docker manifest`. The variable DPF_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-dpf-system: $(addprefix docker-push-dpf-system-for-,$(DPF_SYSTEM_ARCH))
	docker manifest push --purge $(DPF_SYSTEM_IMAGE):$(TAG)

docker-push-dpf-system-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(DPF_SYSTEM_IMAGE):$(TAG)-$* $(DPF_SYSTEM_IMAGE):$(TAG)
	docker push $(DPF_SYSTEM_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE) docker-create-manifest-for-dpf-system

docker-create-manifest-for-dpf-system:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(DPF_SYSTEM_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(DPF_SYSTEM_IMAGE):$(TAG))

.PHONY: docker-build-ipallocator
docker-build-ipallocator: ## Build docker image for the IP Allocator
	# Base image can't be distroless because of the readiness probe that is using cat which doesn't exist in distroless
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(ARCH) \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(ALPINE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg package=./cmd/ipallocator \
		  -f Dockerfile \
		. \
		-t $(IPALLOCATOR_IMAGE):$(TAG)

.PHONY: docker-build-ovs-cni
docker-build-ovs-cni: $(OVS_CNI_DIR) ## Builds the OVS CNI image
	cd $(OVS_CNI_DIR) && \
	$(OVS_CNI_DIR)/hack/get_version.sh > .version && \
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--build-arg goarch=$(DPU_ARCH) \
		--platform linux/${DPU_ARCH} \
		-f ./cmd/Dockerfile \
		-t $(OVS_CNI_IMAGE):${TAG} \
		.

.PHONY: docker-build-ovn-kubernetes # Build a multi-arch image for DPF System. The variable DPF_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-ovn-kubernetes: $(addprefix docker-build-ovn-kubernetes-for-,$(DPF_SYSTEM_ARCH))

docker-build-ovn-kubernetes-for-%: $(OVNKUBERNETES_DIR)
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
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg ovn_kubernetes_dir=$(subst $(CURDIR)/,,$(OVNKUBERNETES_DIR)) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		-f Dockerfile.ovn-kubernetes \
		. \
		-t $(OVNKUBERNETES_IMAGE):$(TAG)-$*

.PHONY: docker-push-ovn-kubernetes # Push a multi-arch image for ovn-kubernetes using `docker manifest`. The variable DPF_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-ovn-kubernetes: $(addprefix docker-push-ovn-kubernetes-for-,$(DPF_SYSTEM_ARCH))
	docker manifest push --purge $(OVNKUBERNETES_IMAGE):$(TAG)

docker-push-ovn-kubernetes-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(OVNKUBERNETES_IMAGE):$(TAG)-$* $(OVNKUBERNETES_IMAGE):$(TAG)
	docker push $(OVNKUBERNETES_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE) docker-create-manifest-for-ovn-kubernetes

docker-create-manifest-for-ovn-kubernetes:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(OVNKUBERNETES_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(OVNKUBERNETES_IMAGE):$(TAG))

.PHONY: docker-build-hostdriver
docker-build-hostdriver: ## Build docker image for DMS and hostnetwork.
	docker buildx build \
		--pull \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform linux/${HOST_ARCH} \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg hostdriver_base_image=$(HOSTDRIVER_BASE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		-t $(HOSTDRIVER_IMAGE):$(TAG) \
		-f Dockerfile.hostdriver \
		.

.PHONY: docker-build-dummydpuservice
docker-build-dummydpuservice: ## Build docker images for the dummydpuservice
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(DPU_ARCH) \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg package=./cmd/dummydpuservice \
		-f Dockerfile \
		. \
		-t $(DUMMYDPUSERVICE_IMAGE):$(TAG)

.PHONY: docker-build-mock-dms
docker-build-mock-dms: ## Build docker images for the mock-dms
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(ARCH) \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		-f test/mock/dms/Dockerfile \
		. \
		-t $(MOCK_DMS_IMAGE):$(TAG)

.PHONY: docker-build-ovn-kubernetes-resource-injector
docker-build-ovn-kubernetes-resource-injector: ## Build docker image for the OVN Kubernetes Resource Injector
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(ARCH) \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg package=./cmd/ovnkubernetesresourceinjector \
		-f Dockerfile \
		. \
		-t $(OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE):$(TAG)

.PHONY: docker-build-storage-system # Build a multi-arch image for DPF storage system. The variable DPF_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-storage-system: $(addprefix docker-build-storage-system-for-,$(DPF_SYSTEM_ARCH))

docker-build-storage-system-for-%:
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
		--build-arg storage_snap_csi_driver_go_ldflags=$(STORAGE_SNAP_CSI_DRIVER_GO_LDFLAGS) \
		--build-arg nvidia_external_attacher_go_ldflags=$(NVIDIA_EXTERNAL_ATTACHER_GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		-f Dockerfile.storage-system \
		. \
		-t $(STORAGE_SYSTEM_IMAGE):$(TAG)-$*

.PHONY: docker-push-storage-system # Push a multi-arch image for snap-csi-plugin using `docker manifest`. The variable DPF_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-storage-system: $(addprefix docker-push-storage-system-for-,$(DPF_SYSTEM_ARCH))
	docker manifest push --purge $(STORAGE_SYSTEM_IMAGE):$(TAG)

docker-push-storage-system-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(STORAGE_SYSTEM_IMAGE):$(TAG)-$* $(STORAGE_SYSTEM_IMAGE):$(TAG)
	docker push $(STORAGE_SYSTEM_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE) docker-create-manifest-for-storage-system

docker-create-manifest-for-storage-system:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(STORAGE_SYSTEM_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(STORAGE_SYSTEM_IMAGE):$(TAG))

.PHONY: docker-build-storage-host # Build a multi-arch image for storage-host. The variable DPF_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-storage-host: $(addprefix docker-build-storage-host-for-,$(DPF_SYSTEM_ARCH))

docker-build-storage-host-for-%:
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
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg storage_snap_csi_driver_go_ldflags=$(STORAGE_SNAP_CSI_DRIVER_GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		--build-arg ubuntu_mirror=$(UBUNTU_MIRROR) \
		--build-arg PACKAGE_SOURCES=$(PACKAGE_SOURCES) \
		-f Dockerfile.storage-host \
		. \
		-t $(STORAGE_HOST_IMAGE):$(TAG)-$*

.PHONY: docker-push-storage-host # Push a multi-arch image for storage-host using `docker manifest`. The variable DPF_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-storage-host: $(addprefix docker-push-storage-host-for-,$(DPF_SYSTEM_ARCH))
	docker manifest push --purge $(STORAGE_HOST_IMAGE):$(TAG)

docker-push-storage-host-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(STORAGE_HOST_IMAGE):$(TAG)-$* $(STORAGE_HOST_IMAGE):$(TAG)
	docker push $(STORAGE_HOST_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE) docker-create-manifest-for-storage-host

docker-create-manifest-for-storage-host:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(STORAGE_HOST_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(STORAGE_HOST_IMAGE):$(TAG))

.PHONY: docker-build-bfb-registry # Build a multi-arch image for BFB Registry. The variable DPF_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-bfb-registry: $(addprefix docker-build-bfb-registry-for-,$(HOST_ARCH))

docker-build-bfb-registry-for-%:
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
		-f Dockerfile.bfb-registry \
		. \
		-t $(BFB_REGISTRY_IMAGE):$(TAG)-$*

.PHONY: docker-build-cni-installer
docker-build-cni-installer: ## Build docker image for the CNI installer
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform=linux/$(DPU_ARCH) \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg base_image=$(BASE_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
		-f Dockerfile.cni-installer \
		. \
		-t $(CNIINSTALLER_IMAGE):$(TAG)

.PHONY: docker-push-bfb-registry # Push a multi-arch image for BFB Registry using `docker manifest`. The variable DPF_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-bfb-registry: $(addprefix docker-push-bfb-registry-for-,$(HOST_ARCH))
	docker manifest push --purge $(BFB_REGISTRY_IMAGE):$(TAG)

docker-push-bfb-registry-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(BFB_REGISTRY_IMAGE):$(TAG)-$* $(BFB_REGISTRY_IMAGE):$(TAG)
	docker push $(BFB_REGISTRY_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE) docker-create-manifest-for-bfb-registry

docker-create-manifest-for-bfb-registry:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(BFB_REGISTRY_IMAGE):$(TAG) $(shell docker inspect --format='{{index .RepoDigests 0}}' $(BFB_REGISTRY_IMAGE):$(TAG))

.PHONY: docker-push-all
docker-push-all: $(addprefix docker-push-,$(DOCKER_BUILD_TARGETS))  ## Push the docker images for all DOCKER_BUILD_TARGETS.

.PHONY: docker-push-dpf-system
docker-push-dpf-system: ## This is a no-op to allow using DOCKER_BUILD_TARGETS.

.PHONY: docker-push-ovs-cni
docker-push-ovs-cni: ## Push the docker image for ovs-cni
	docker push $(OVS_CNI_IMAGE):$(TAG)

.PHONY: docker-push-hostdriver
docker-push-hostdriver: ## Push the docker image for DMS and hostnetwork.
	docker push $(HOSTDRIVER_IMAGE):$(TAG)

.PHONY: docker-push-dpucniprovisioner
docker-push-dpucniprovisioner: ## Push the docker image for DPU CNI Provisioner.
	docker push $(DPUCNIPROVISIONER_IMAGE):$(TAG)

.PHONY: docker-push-ipallocator
docker-push-ipallocator: ## Push the docker image for IP Allocator.
	docker push $(IPALLOCATOR_IMAGE):$(TAG)

.PHONY: docker-push-dummydpuservice
docker-push-dummydpuservice: ## Push the docker image for dummydpuservice
	docker push $(DUMMYDPUSERVICE_IMAGE):$(TAG)

.PHONY: docker-push-mock-dms
docker-push-mock-dms: ## Push the docker image for dummydpuservice
	docker push $(MOCK_DMS_IMAGE):$(TAG)

.PHONY: docker-push-ovn-kubernetes-resource-injector
docker-push-ovn-kubernetes-resource-injector: ## Push the docker image for the OVN Kubernetes Resource Injector
	docker push $(OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE):$(TAG)

.PHONY: docker-push-cni-installer
docker-push-cni-installer: ## Push the docker image for the CNI installer
	docker push $(CNIINSTALLER_IMAGE):$(TAG)

# helm charts

# By default the helm registry is assumed to be an OCI registry.
DEFAULT_HELM_REGISTRY=oci://$(REGISTRY)
# This variable should be overwritten when using a https helm repository.
export HELM_REGISTRY ?= $(DEFAULT_HELM_REGISTRY)
# This variable should be overwritten with the registry of the upstream artifacts. Needed when making a release upstream.
# This variable ensures that the values injected in the operator and charts point to the upstream artifacts.
export UPSTREAM_HELM_REGISTRY ?= $(HELM_REGISTRY)

HELM_TARGETS ?= dpu-networking operator ovn-kubernetes storage

# metadata for the operator helm chart
OPERATOR_HELM_CHART_NAME ?= dpf-operator
OPERATOR_HELM_CHART ?= $(HELMDIR)/$(OPERATOR_HELM_CHART_NAME)

## metadata for dpu-networking helm chart.
export DPU_NETWORKING_HELM_CHART_NAME = dpu-networking
DPU_NETWORKING_HELM_CHART ?= $(HELMDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)
DPU_NETWORKING_HELM_CHART_VER ?= $(TAG)

## metadata for ovn-kubernetes
export OVNKUBERNETES_HELM_CHART_NAME = ovn-kubernetes-chart
OVNKUBERNETES_HELM_CHART ?= $(OVNKUBERNETES_DIR)/helm/ovn-kubernetes-dpf
OVNKUBERNETES_HELM_CHART_VER ?= $(TAG)

## metadata for ovn-kubernetes-resource-injector
export OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_NAME = ovn-kubernetes-resource-injector-chart
OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART ?= $(HELMDIR)/ovn-kubernetes-resource-injector
OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_VER ?= $(TAG)

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
	@cp -r $(CURDIR)/deploy/helmfiles/* $(OPERATOR_HELM_CHART)/dev/helmfiles/

	# Generate the helm chart package.
	HELM_CHART_DIR="$(OPERATOR_HELM_CHART)" \
	HELM_CHART_TAGS="$(OPERATOR_CHART_TAGS)" \
	SET_IMAGE_IN_VALUES=true \
	REPO="$(DPF_SYSTEM_UPSTREAM_IMAGE)" \
	IMAGE_REPO_PATH=controllerManager.image.repository \
	IMAGE_TAG_PATH=controllerManager.image.tag \
	./hack/scripts/release-helm-package.sh

.PHONY: helm-package-ovn-kubernetes
helm-package-ovn-kubernetes: $(OVNKUBERNETES_DIR) $(CHARTSDIR) helm yq ## Package helm chart for ovn-kubernetes
	HELM_CHART_DIR="$(OVNKUBERNETES_HELM_CHART)" \
	HELM_CHART_TAGS="$(TAG)" \
	./hack/scripts/release-helm-package.sh

.PHONY: helm-package-ovn-kubernetes-resource-injector
helm-package-ovn-kubernetes-resource-injector: $(CHARTSDIR) helm generate-manifests-ovn-kubernetes-resource-injector yq ## Package helm chart for OVN Kubernetes Resource Injector
	HELM_CHART_DIR="$(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART)" \
	HELM_CHART_TAGS="$(TAG)" \
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
	for tag in $(OPERATOR_CHART_TAGS); do \
		$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(OPERATOR_HELM_CHART_NAME)-$$tag.tgz $(HELM_REGISTRY); \
	done

.PHONY: helm-push-dpu-networking
helm-push-dpu-networking: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for service chain controller
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz $(HELM_REGISTRY)

.PHONY: helm-push-ovn-kubernetes
helm-push-ovn-kubernetes: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for ovn-kubernetes
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(OVNKUBERNETES_HELM_CHART_NAME)-$(OVNKUBERNETES_HELM_CHART_VER).tgz $(HELM_REGISTRY)

.PHONY: helm-push-ovn-kubernetes-resource-injector
helm-push-ovn-kubernetes-resource-injector: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for OVN Kubernetes Resource Injector
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_NAME)-$(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_VER).tgz $(HELM_REGISTRY)

.PHONY: helm-push-dummydpuservice
helm-push-dummydpuservice: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for dummydpuservice
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(DUMMYDPUSERVICE_HELM_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)



.PHONY: helm-push-storage
helm-push-storage: $(CHARTSDIR) helm helm-cm-push ## Push helm chart for storage
	$(HELM) $(HELM_PUSH_CMD) $(CHARTSDIR)/$(STORAGE_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)

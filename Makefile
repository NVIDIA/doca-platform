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

## Include Make modules which are split up in this repo for better structure.
include hack/tools/tools.mk

PROJECT_NAME="DOCA Platform Framework"
PROJECT_REPO="https://github.com/NVIDIA/doca-platform"
DATE="$(shell date --rfc-3339=seconds)"
FULL_COMMIT=$(shell git rev-parse HEAD)

# Export is needed here so that the envsubst used in make targets has access to those variables even when they are not
# explicitly set when calling make.
# The tag must have three digits with a leading v - i.e. v9.9.1
export TAG ?= v0.1.0
# Note: Registry defaults to non-existing registry intentionally to avoid overriding useful images.
export REGISTRY ?= example.com

# If V is set to 1 the output will be verbose.
Q = $(if $(filter 1,$V),,@)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29.0

# Get the current OS and Architecture
ARCH ?= $(shell go env GOARCH)
OS ?= $(shell go env GOOS)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GO_VERSION ?= $(shell awk '/^go /{print $$2}' go.mod)

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

LOCALBIN ?= $(CURDIR)/bin
CHARTSDIR ?= $(CURDIR)/hack/charts
DPUSERVICESDIR ?= $(CURDIR)/deploy/dpuservices
REPOSDIR ?= $(CURDIR)/hack/repos
HELMDIR ?= $(CURDIR)/deploy/helm
THIRDPARTYDIR ?= $(CURDIR)/third_party/forked

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

# kamaji is the underlying control plane provider
KAMAJI_REPO_URL=https://clastix.github.io/charts
KAMAJI_REPO_NAME=clastix
KAMAJI_CHART_VERSION=0.15.2
KAMAJI_CHART_NAME=kamaji
KAMAJI := $(abspath $(CHARTSDIR)/$(KAMAJI_CHART_NAME)-$(KAMAJI_CHART_VERSION).tgz)
$(KAMAJI): | $(CHARTSDIR) helm
	$Q $(HELM) repo add $(KAMAJI_REPO_NAME) $(KAMAJI_REPO_URL)
	$Q $(HELM) repo update
	$Q $(HELM) pull $(KAMAJI_REPO_NAME)/$(KAMAJI_CHART_NAME) --version $(KAMAJI_CHART_VERSION) -d $(CHARTSDIR)

# argoCD is the underlying application service provider
ARGOCD_YAML=$(CHARTSDIR)/argocd.yaml
ARGOCD_VER=v2.10.1
$(ARGOCD_YAML): | $(CHARTSDIR)
	curl -fSsL "https://raw.githubusercontent.com/argoproj/argo-cd/$(ARGOCD_VER)/manifests/install.yaml" -o $(ARGOCD_YAML)

# OVS CNI
# A third party import to the repo. In future this will be further integrated.
OVS_CNI_DIR=$(THIRDPARTYDIR)/ovs-cni

# OVN Kubernetes dependencies to be able to build its docker image
OVNKUBERNETES_REF=65b2d5d768f380c302965d4c30f3aa61c0e730b3
OVNKUBERNETES_DIR=$(REPOSDIR)/ovn-kubernetes-$(OVNKUBERNETES_REF)
$(OVNKUBERNETES_DIR): | $(REPOSDIR)
	git clone https://github.com/aserdean/ovn-kubernetes $(OVNKUBERNETES_DIR)-tmp
	cd $(OVNKUBERNETES_DIR)-tmp && git reset --hard $(OVNKUBERNETES_REF)
	mv $(OVNKUBERNETES_DIR)-tmp $(OVNKUBERNETES_DIR)

DOCA_SOSREPORT_REPO_URL=https://github.com/NVIDIA/doca-sosreport/archive/$(DOCA_SOSREPORT_REF).tar.gz
DOCA_SOSREPORT_REF=6b4289b9f0d9f26af177b0d1c4c009ca74bb514a
SOS_REPORT_DIR=$(REPOSDIR)/doca-sosreport-$(DOCA_SOSREPORT_REF)
$(SOS_REPORT_DIR): | $(REPOSDIR)
	curl -sL ${DOCA_SOSREPORT_REPO_URL} \
	| tar -xz -C ${REPOSDIR} && \
	cp -Rp ./hack/tools/dpf-tools/* $(REPOSDIR)/doca-sosreport-${DOCA_SOSREPORT_REF}/

##@ Development
GENERATE_TARGETS ?= dpuservice provisioning dpucniprovisioner servicechainset sfc-controller ovs-cni operator operator-embedded release-defaults dummydpuservice kamaji-cluster-manager static-cluster-manager dpu-detector ovn-kubernetes ovs-helper

.PHONY: generate
generate: ## Run all generate-* targets: generate-modules generate-manifests-* and generate-go-deepcopy-*.
	$(MAKE) generate-mocks generate-modules generate-manifests generate-go-deepcopy generate-operator-bundle generate-docs

.PHONY: generate-mocks
generate-mocks: mockgen ## Generate mocks
	## Prepend the TOOLSDIR to the path for this command as `mockgen` is called from the $PATH inline in the code.
	## The DPF TOOLSDIR should be first in the path to ensure user tools are not used.
	## See go:generate comments for examples.
	export PATH="$(TOOLSDIR):$(PATH)"; go generate ./...

.PHONY: generate-modules
generate-modules: ## Run go mod tidy to update go modules
	go mod tidy

.PHONY: generate-manifests
generate-manifests: $(addprefix generate-manifests-,$(GENERATE_TARGETS)) ## Run all generate-manifests-* targets

.PHONY: generate-manifests-operator
generate-manifests-operator: controller-gen kustomize envsubst helm ## Generate manifests e.g. CRD, RBAC. for the operator controller.
	$(MAKE) clean-generated-yaml SRC_DIRS="./deploy/helm/dpf-operator/templates/crds/"
	$(CONTROLLER_GEN) \
	paths="./cmd/operator/..." \
	paths="./cmd/kamaji-cluster-manager/..." \
	paths="./cmd/static-cluster-manager/..." \
	paths="./internal/operator/..." \
	paths="./internal/clustermanager/..." \
	paths="./api/operator/..." \
	crd:crdVersions=v1 \
	rbac:roleName="dpf-operator-manager-role" \
	output:crd:dir=./deploy/helm/dpf-operator/templates/crds \
	output:rbac:dir=./deploy/helm/dpf-operator/templates
	## Copy all other CRD definitions to the operator helm directory
	$(KUSTOMIZE) build config/operator-additional-crds -o  deploy/helm/dpf-operator/templates/crds/;
	## Set the image name and tag in the operator helm chart values
	$(ENVSUBST) < deploy/helm/dpf-operator/values.yaml.tmpl > deploy/helm/dpf-operator/values.yaml

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

.PHONY: generate-manifests-ovn-kubernetes-resource-injector
generate-manifests-ovn-kubernetes-resource-injector: envsubst ## Generate manifests e.g. CRD, RBAC. for the OVN Kubernetes Resource Injector
	$(ENVSUBST) < deploy/helm/ovn-kubernetes-resource-injector/values.yaml.tmpl > deploy/helm/ovn-kubernetes-resource-injector/values.yaml

.PHONY: generate-manifests-dpucniprovisioner
generate-manifests-dpucniprovisioner: kustomize ## Generates DPU CNI provisioner manifests
	cd config/dpucniprovisioner/default && $(KUSTOMIZE) edit set image controller=$(DPUCNIPROVISIONER_IMAGE):$(TAG)

RELEASE_FILE = ./internal/release/manifests/defaults.yaml

.PHONY: generate-manifests-release-defaults
generate-manifests-release-defaults: envsubst ## Generates manifests that contain the default values that should be used by the operators
	$(ENVSUBST) <  ./internal/release/templates/defaults.yaml.tmpl > $(RELEASE_FILE)

TEMPLATES_DIR ?= $(CURDIR)/internal/operator/inventory/templates
EMBEDDED_MANIFESTS_DIR ?= $(CURDIR)/internal/operator/inventory/manifests
.PHONY: generate-manifests-operator-embedded
generate-manifests-operator-embedded: kustomize envsubst generate-manifests-dpuservice generate-manifests-provisioning generate-manifests-release-defaults generate-manifests-kamaji-cluster-manager generate-manifests-static-cluster-manager generate-manifests-dpu-detector ## Generates manifests that are embedded into the operator binary.
	# Reorder none here ensure that we generate the kustomize files in a specific order to be consumed by the DPF Operator.
	$(KUSTOMIZE) build --reorder=none config/provisioning/default > $(EMBEDDED_MANIFESTS_DIR)/provisioning-controller.yaml
	$(KUSTOMIZE) build --reorder=none config/dpu-detector > $(EMBEDDED_MANIFESTS_DIR)/dpu-detector.yaml
	$(KUSTOMIZE) build --reorder=none config/dpuservice/default > $(EMBEDDED_MANIFESTS_DIR)/dpuservice-controller.yaml
	$(KUSTOMIZE) build --reorder=none config/kamaji-cluster-manager/default > $(EMBEDDED_MANIFESTS_DIR)/kamaji-cluster-manager.yaml
	$(KUSTOMIZE) build --reorder=none config/static-cluster-manager/default > $(EMBEDDED_MANIFESTS_DIR)/static-cluster-manager.yaml

.PHONY: generate-manifests-servicechainset
generate-manifests-servicechainset: controller-gen kustomize envsubst ## Generate manifests e.g. CRD, RBAC. for the servicechainset controller.
	$(MAKE) clean-generated-yaml SRC_DIRS="./config/servicechainset/crd/bases"
	$(CONTROLLER_GEN) \
	paths="./cmd/servicechainset/..." \
	paths="./internal/servicechainset/..." \
	paths="./internal/pod-ipam-injector/..." \
	paths="./api/dpuservice/..." \
	crd:crdVersions=v1,generateEmbeddedObjectMeta=true \
	rbac:roleName=manager-role \
	output:crd:dir=./config/servicechainset/crd/bases \
	output:rbac:dir=./config/servicechainset/rbac
	find config/servicechainset/crd/bases/ -type f -not -name '*_dpu*' -exec cp {} deploy/helm/dpu-networking/charts/servicechainset-controller/templates/crds/ \;
	$(ENVSUBST) < deploy/helm/dpu-networking/charts/servicechainset-controller/values.yaml.tmpl > deploy/helm/dpu-networking/charts/servicechainset-controller/values.yaml


.PHONY: generate-manifests-sfc-controller
generate-manifests-sfc-controller: envsubst generate-manifests-servicechainset
	cp deploy/helm/dpu-networking/charts/servicechainset-controller/templates/crds/svc.dpu.nvidia.com_servicechains.yaml deploy/helm/dpu-networking/charts/sfc-controller/templates/crds/
	cp deploy/helm/dpu-networking/charts/servicechainset-controller/templates/crds/svc.dpu.nvidia.com_serviceinterfaces.yaml deploy/helm/dpu-networking/charts/sfc-controller/templates/crds/
	# Template the image name and tag used in the helm templates.
	$(ENVSUBST) < deploy/helm/dpu-networking/charts/sfc-controller/values.yaml.tmpl > deploy/helm/dpu-networking/charts/sfc-controller/values.yaml

.PHONY: generate-manifests-ovs-helper
generate-manifests-ovs-helper: envsubst # Generates manifests for the OVS Helper.
	# Template the image name and tag used in the helm templates.
	$(ENVSUBST) < deploy/helm/dpu-networking/charts/ovs-helper/values.yaml.tmpl > deploy/helm/dpu-networking/charts/ovs-helper/values.yaml

.PHONY: generate-manifests-ovs-cni
generate-manifests-ovs-cni: envsubst ## Generate values for OVS helm chart.
	$(ENVSUBST) < deploy/helm/dpu-networking/charts/ovs-cni/values.yaml.tmpl > deploy/helm/dpu-networking/charts/ovs-cni/values.yaml

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
	cd config/kamaji-cluster-manager/manager && $(KUSTOMIZE) edit set image controller=$(DPF_SYSTEM_IMAGE):$(TAG)

.PHONY: generate-manifests-static-cluster-manager
generate-manifests-static-cluster-manager: controller-gen kustomize ## Generate manifests e.g. CRD, RBAC. for the DPF provisioning controller.
	$(CONTROLLER_GEN) \
	paths="./cmd/static-cluster-manager/..." \
	paths="./internal/clustermanager/controller/..." \
	paths="./internal/clustermanager/static/..." \
	rbac:roleName=manager-role \
	output:rbac:dir=./config/static-cluster-manager/rbac
	cd config/static-cluster-manager/manager && $(KUSTOMIZE) edit set image controller=$(DPF_SYSTEM_IMAGE):$(TAG)

.PHONY: generate-manifests-dpu-detector
generate-manifests-dpu-detector: kustomize ## Generate manifests for dpu-detector
	cd config/dpu-detector && $(KUSTOMIZE) edit set image dpu-detector=$(HOSTDRIVER_IMAGE):$(TAG)

.PHONY: generate-manifests-ovn-kubernetes
generate-manifests-ovn-kubernetes: $(OVNKUBERNETES_DIR) envsubst ## Generate manifests for ovn-kubernetes
	$(ENVSUBST) < $(OVNKUBERNETES_HELM_CHART)/values.yaml.tmpl > $(OVNKUBERNETES_HELM_CHART)/values.yaml

.PHONY: generate-operator-bundle
generate-operator-bundle: helm operator-sdk generate-manifests-operator helm-package-operator ## Generate bundle manifests and metadata, then validate generated files.
	# First template the actual manifests to include using helm.
	mkdir -p hack/charts/dpf-operator/
	$(HELM) template --namespace $(OPERATOR_NAMESPACE) \
		--set image=$(DPF_SYSTEM_IMAGE):$(TAG) $(OPERATOR_HELM_CHART) \
		--set argo-cd.enabled=false \
		--set node-feature-discovery.enabled=false \
		--set kube-state-metrics.enabled=false \
		--set kamaji.enabled=false \
		--set kamaji-etcd.enabled=false \
		--set grafana.enabled=false \
		--set prometheus.enabled=false \
		--set maintenance-operator-chart.enabled=false \
		--set templateOperatorBundle=true > hack/charts/dpf-operator/manifests.yaml

	# Then clean the bundle directory to have a proper diff.
	rm bundle/manifests/*

	# Next generate the operator bundle.
	# Note we need to explicitly set stdin to null using < /dev/null.
	$(OPERATOR_SDK) generate bundle \
	--overwrite --package dpf-operator --version $(BUNDLE_VERSION) --default-channel=$(BUNDLE_VERSION) --channels=$(BUNDLE_VERSION) \
	--deploy-dir hack/charts/dpf-operator --crds-dir deploy/helm/dpf-operator/templates/crds 	</dev/null

	# We need to ensure operator-sdk receives nothing on stdin by explicitly redirecting null there.
	# Remove the createdAt field to prevent rebasing issues.
	# TODO: Currently the clusterserviceversion is not being correctly generated e.g. metadata is missing.
	# MacOS: We have to ensure that we are using gnu sed. Install gnu-sed via homebrew and put it somewhere in your PATH.
	#   e.g.: ln -s /opt/homebrew/bin/gsed $HOME/bin/sed
	$Q sed -i '/  createdAt:/d'  bundle/manifests/dpf-operator.clusterserviceversion.yaml
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: generate-manifests-dummydpuservice
generate-manifests-dummydpuservice: envsubst ## Generate values for dummydpuservice helm chart.
	$(ENVSUBST) < deploy/dpuservices/dummydpuservice/chart/values.yaml.tmpl > deploy/dpuservices/dummydpuservice/chart/values.yaml

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

.PHONY: generate-docs-mdtoc
generate-docs-mdtoc: mdtoc ## Generate table of contents for our documentation.
	grep -rl -e '<!-- toc -->' docs | grep '\.md$$' | xargs $(MDTOC) --inplace

.PHONY: generate-docs-api
generate-docs-api: gen-crd-api-reference-docs ## Generate docs for the API.
	$(GEN_CRD_API_REFERENCE_DOCS) --renderer=markdown --source-path=api --config=hack/tools/api-docs/config.yaml --output-path=docs/api.md

.PHONY: generate-docs-helm
generate-docs-helm: helm-docs ## Generate helm chart documentation.
	$(HELM_DOCS) --ignore-file=.helmdocsignore

.PHONY: generate-docs-embedmd
generate-docs-embedmd: embedmd ## Embed additional files into markdown docs.
	grep -rl --include \*.md -e '\[embedmd\]' docs | xargs $(EMBEDMD) -w

##@ Testing

.PHONY: test
test: envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test $$(go list ./... | grep -v /e2e) $(GO_TEST_ARGS)

.PHONY: test-report
test-report: envtest gotestsum ## Run tests and generate a junit style report
	set +o errexit; KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLSDIR) -p path)" go test -count 1 -race -json $$(go list ./... | grep -v /e2e) > junit.stdout; echo $$? > junit.exitcode;
	$(GOTESTSUM) --junitfile junit.xml --raw-command cat junit.stdout
	exit $$(cat junit.exitcode)

.PHONY: test-release-e2e-quick
test-release-e2e-quick: # Build images required for the quick DPF e2e test.
	# Build and push the dpuservice, provisioning, operator and operator-bundle images.
	# The quick test will only run on amd64 nodes.
	$(MAKE) docker-build-dpf-system-for-$(ARCH) docker-push-dpf-system-for-$(ARCH)

	# Build and push all the helm charts
	$(MAKE) helm-package-all helm-push-all

TEST_CLUSTER_NAME := dpf-test
ADD_CONTROL_PLANE_TAINTS ?= true
test-env-e2e: $(KAMAJI) $(CERT_MANAGER_YAML) $(ARGOCD_YAML) minikube helm ## Setup a Kubernetes environment to run tests.

	# Create a minikube cluster to host the test.
	CLUSTER_NAME=$(TEST_CLUSTER_NAME) MINIKUBE_BIN=$(MINIKUBE) ADD_CONTROL_PLANE_TAINTS=$(ADD_CONTROL_PLANE_TAINTS) $(CURDIR)/hack/scripts/minikube-install.sh

	$(KUBECTL) get namespace dpf-operator-system || $(KUBECTL) create namespace dpf-operator-system

	# Create secrets required for using artefacts if required.
	$(CURDIR)/hack/scripts/create-artefact-secrets.sh

.PHONY: test-env-dpf-standalone
test-env-dpf-standalone:
	# Run the setup script.
	echo "Running the provisioning script."
	$(CURDIR)/hack/scripts/e2e-provision-standalone-cluster.sh

OPERATOR_NAMESPACE ?= dpf-operator-system
DEPLOY_KSM ?= false
DEPLOY_GRAFANA ?= false
DEPLOY_PROMETHEUS ?= false

.PHONY: test-deploy-operator-helm
test-deploy-operator-helm: helm ## Deploy the DPF Operator using helm
	$(HELM) upgrade --install --create-namespace --namespace $(OPERATOR_NAMESPACE) \
		--set controllerManager.image.repository=$(DPF_SYSTEM_IMAGE)\
		--set controllerManager.image.tag=$(TAG) \
		--set imagePullSecrets[0].name=dpf-pull-secret \
		--set kube-state-metrics.enabled=$(DEPLOY_KSM) \
		--set grafana.enabled=$(DEPLOY_GRAFANA) \
		--set prometheus.enabled=$(DEPLOY_PROMETHEUS) \
		dpf-operator $(OPERATOR_HELM_CHART)

OLM_VERSION ?= v0.28.0
OPERATOR_REGISTRY_VERSION ?= v1.43.1
OLM_DIR = $(REPOSDIR)/olm
OLM_DOWNLOAD_URL = https://github.com/operator-framework/operator-lifecycle-manager/releases/download
.PHONY: test-install-operator-lifecycle-manager
test-install-operator-lifecycle-manager:
	mkdir -p $(OLM_DIR)
	curl -L $(OLM_DOWNLOAD_URL)/$(OLM_VERSION)/crds.yaml -o $(OLM_DIR)/crds.yaml
	curl -L $(OLM_DOWNLOAD_URL)/$(OLM_VERSION)/olm.yaml -o $(OLM_DIR)/olm.yaml


	# Operator SDK always installs the `latest` image for the configmap-operator. We need to replace that for this installation.
	$Q sed -i 's/configmap-operator-registry:latest/configmap-operator-registry:$(OLM_VERSION)/g' $(OLM_DIR)/olm.yaml

	$(KUBECTL) create -f "$(OLM_DIR)/crds.yaml"
	$(KUBECTL) wait --for=condition=Established -f "$(OLM_DIR)/crds.yaml"
	$(KUBECTL) create -f "$(OLM_DIR)/olm.yaml"
	$(KUBECTL) rollout status -w deployment/olm-operator --namespace=olm
	$(KUBECTL) rollout status -w deployment/catalog-operator --namespace=olm


OPERATOR_SDK_RUN_BUNDLE_EXTRA_ARGS ?= ""
.PHONY: test-deploy-operator-operator-sdk
test-deploy-operator-operator-sdk: operator-sdk kustomize test-install-operator-lifecycle-manager ## Deploy the DPF Operator using operator-sdk
	# Create the namespace for the operator to be installed.
	$(KUBECTL) create namespace $(OPERATOR_NAMESPACE)
	# TODO: This flow does not work on MacOS dues to some issue pulling images. Should be enabled to make local testing equivalent to CI.
	$(OPERATOR_SDK) run bundle --namespace $(OPERATOR_NAMESPACE) --index-image quay.io/operator-framework/opm:$(OPERATOR_REGISTRY_VERSION) $(OPERATOR_SDK_RUN_BUNDLE_EXTRA_ARGS) $(OPERATOR_BUNDLE_IMAGE):$(BUNDLE_VERSION)

ARTIFACTS_DIR ?= $(CURDIR)/artifacts
.PHONY: test-cache-images
test-cache-images: minikube ## Add images to the minikube cache based on the artifacts directory created in e2e.
	# Run a script which will cache images which were pulled in the test run to the minikube cache.
	CLUSTER_NAME=$(TEST_CLUSTER_NAME) MINIKUBE_BIN=$(MINIKUBE) ARTIFACTS_DIR=${ARTIFACTS_DIR} $(CURDIR)/hack/scripts/add-images-to-minikube-cache.sh

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e ## Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: stern ## Run e2e tests
	STERN=$(STERN) ARTIFACTS=$(ARTIFACTS_DIR) $(CURDIR)/hack/scripts/stern-log-collector.sh \
	  go test -timeout 0 ./test/e2e/ -v -ginkgo.v

.PHONY: clean-test-env
clean-test-env: minikube ## Clean test environment (teardown minikube cluster)
	$(MINIKUBE) delete -p $(TEST_CLUSTER_NAME)

##@ lint and verify
GOLANGCI_LINT_GOGC ?= "100"
.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	GOOS=linux GOGC=$(GOLANGCI_LINT_GOGC) $(GOLANGCI_LINT) run --timeout 5m

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	GOOS=linux $(GOLANGCI_LINT) run --fix

.PHONY: verify-generate
verify-generate: generate ## Verify auto-generated code did not change
	$(info checking for git diff after running 'make generate')
	# Use intent-to-add to check for untracked files after generation.
	git add -N .
	$Q git diff --quiet ':!bundle' ; if [ $$? -eq 1 ] ; then echo "Please, commit manifests after running 'make generate'"; exit 1 ; fi
	# Files under `bundle` are verified here. The createdAt field is excluded as it is always updated at generation time and is not relevant to the bundle.
	$Q git diff --quiet -I'^    createdAt: ' bundle ; if [ $$? -eq 1 ] ; then echo "Please, commit manifests after running 'make generate'"; exit 1 ; fi

.PHONY: verify-copyright
verify-copyright: ## Verify copyrights for project files
	$Q $(CURDIR)/hack/scripts/copyright-validation.sh

.PHONY: lint-helm
lint-helm: lint-helm-dpu-networking lint-helm-ovn-kubernetes lint-helm-dummydpuservice

.PHONY: lint-helm-dpu-networking
lint-helm-dpu-networking: helm ## Run helm lint for servicechainset chart
	$Q $(HELM) lint $(DPU_NETWORKING_HELM_CHART)

.PHONY: lint-helm-ovn-kubernetes
lint-helm-ovn-kubernetes: generate-manifests-ovn-kubernetes helm ## Run helm lint for ovn-kubernetes chart
	$Q $(HELM) lint $(OVNKUBERNETES_HELM_CHART)

.PHONY: lint-helm-dummydpuservice
lint-helm-dummydpuservice: helm ## Run helm lint for dummydpuservice chart
	$Q $(HELM) lint $(DUMMYDPUSERVICE_HELM_CHART)

##@ Release

.PHONY: release
release: generate ## Build and push helm and container images for release.
	# Build multiarch images which will run on both DPUs and x86 hosts.
	$(MAKE) $(addprefix docker-build-,$(MULTI_ARCH_DOCKER_BUILD_TARGETS))
	# Build arm64 images which will run on DPUs.
	$(MAKE) ARCH=$(DPU_ARCH) $(addprefix docker-build-,$(DPU_ARCH_DOCKER_BUILD_TARGETS))
	# Build amd64 images which will run on x86 hosts.
	$(MAKE) ARCH=$(HOST_ARCH) $(addprefix docker-build-,$(HOST_ARCH_DOCKER_BUILD_TARGETS))
	# Push all of the images
	$(MAKE) docker-push-all

	# Package and push the helm charts.
	$(MAKE) helm-package-all helm-push-all

##@ Build

GO_GCFLAGS ?= ""
GO_LDFLAGS ?= "-extldflags '-static'"
BUILD_TARGETS ?= $(DPU_ARCH_BUILD_TARGETS)
DPF_SYSTEM_BUILD_TARGETS ?= operator provisioning dpuservice servicechainset kamaji-cluster-manager static-cluster-manager sfc-controller ovs-helper
DPU_ARCH_BUILD_TARGETS ?=
BUILD_IMAGE ?= docker.io/library/golang:$(GO_VERSION)

# The BUNDLE_VERSION is the same as the TAG but the first character is stripped. This is used to strip a leading `v` which is invalid for Bundle versions.
$(eval BUNDLE_VERSION := $$$(TAG))

HOST_ARCH = amd64
DPU_ARCH = arm64

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
BASE_IMAGE = gcr.io/distroless/base:nonroot
ALPINE_IMAGE = alpine:3.19

.PHONY: binaries
binaries: $(addprefix binary-,$(BUILD_TARGETS)) ## Build all binaries

.PHONY: binaries-dpf-system
binaries-dpf-system: $(addprefix binary-,$(DPF_SYSTEM_BUILD_TARGETS)) ## Build binaries for the dpf-system image.

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
	go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpucniprovisioner github.com/nvidia/doca-platform/cmd/dpucniprovisioner

.PHONY: binary-ovs-helper
binary-ovs-helper: ## Build the OVS Helper binary
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/ovshelper github.com/nvidia/doca-platform/cmd/ovshelper

.PHONY: binary-sfc-controller
binary-sfc-controller: ## Build the Host CNI Provisioner binary.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/sfc-controller github.com/nvidia/doca-platform/cmd/sfc-controller

.PHONY: binary-ipallocator
binary-ipallocator: ## Build the IP allocator binary.
	go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/ipallocator github.com/nvidia/doca-platform/cmd/ipallocator

.PHONY: binary-detector
binary-detector: ## Build the DPU detector binary.
	go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/dpu-detector github.com/nvidia/doca-platform/cmd/dpudetector

.PHONY: binary-ovn-kubernetes-resource-injector
binary-ovn-kubernetes-resource-injector: ## Build the OVN Kubernetes Resource Injector.
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags=$(GO_LDFLAGS) -gcflags=$(GO_GCFLAGS) -trimpath -o $(LOCALBIN)/ovnkubernetesresourceinjector github.com/nvidia/doca-platform/cmd/ovnkubernetesresourceinjector

DOCKER_BUILD_TARGETS=$(HOST_ARCH_DOCKER_BUILD_TARGETS) $(DPU_ARCH_DOCKER_BUILD_TARGETS) $(MULTI_ARCH_DOCKER_BUILD_TARGETS)
HOST_ARCH_DOCKER_BUILD_TARGETS=hostdriver
DPU_ARCH_DOCKER_BUILD_TARGETS=$(DPU_ARCH_BUILD_TARGETS) ovs-cni
MULTI_ARCH_DOCKER_BUILD_TARGETS= dpf-system ovn-kubernetes dpf-tools

.PHONY: docker-build-all
docker-build-all: $(addprefix docker-build-,$(DOCKER_BUILD_TARGETS)) ## Build docker images for all DOCKER_BUILD_TARGETS. Architecture defaults to build system architecture unless overridden or hardcoded.

DPF_SYSTEM_IMAGE_NAME ?= dpf-system
export DPF_SYSTEM_IMAGE ?= $(REGISTRY)/$(DPF_SYSTEM_IMAGE_NAME)

OVNKUBERNETES_IMAGE_NAME = ovn-kubernetes
export OVNKUBERNETES_IMAGE = $(REGISTRY)/$(OVNKUBERNETES_IMAGE_NAME)

OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE_NAME = ovn-kubernetes-resource-injector
export OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE = $(REGISTRY)/$(OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE_NAME)

OVS_CNI_IMAGE_NAME ?= ovs-cni-plugin
export OVS_CNI_IMAGE ?= $(REGISTRY)/$(OVS_CNI_IMAGE_NAME)

export HOSTDRIVER_IMAGE ?= $(REGISTRY)/hostdriver

IPALLOCATOR_IMAGE_NAME ?= ip-allocator
export IPALLOCATOR_IMAGE ?= $(REGISTRY)/$(IPALLOCATOR_IMAGE_NAME)

OPERATOR_BUNDLE_NAME ?= dpf-operator-bundle
OPERATOR_BUNDLE_REGISTRY ?= $(REGISTRY)
OPERATOR_BUNDLE_IMAGE ?= $(OPERATOR_BUNDLE_REGISTRY)/$(OPERATOR_BUNDLE_NAME)

# Images that are running on DPU worker nodes (arm64)
DPUCNIPROVISIONER_IMAGE_NAME ?= dpu-cni-provisioner
DPUCNIPROVISIONER_IMAGE ?= $(REGISTRY)/$(DPUCNIPROVISIONER_IMAGE_NAME)

DUMMYDPUSERVICE_IMAGE_NAME ?= dummydpuservice
export DUMMYDPUSERVICE_IMAGE ?= $(REGISTRY)/$(DUMMYDPUSERVICE_IMAGE_NAME)

DPF_TOOLS_IMAGE_NAME ?= dpf-tools
export DPF_TOOLS_IMAGE ?= $(REGISTRY)/$(DPF_TOOLS_IMAGE_NAME)

## External images that are set by the DPF Operator
export MULTUS_IMAGE=ghcr.io/k8snetworkplumbingwg/multus-cni
export MULTUS_TAG=v3.9.3
export SRIOVDP_IMAGE=ghcr.io/k8snetworkplumbingwg/sriov-network-device-plugin
export SRIOVDP_TAG=v3.6.2
export NVIPAM_IMAGE=ghcr.io/mellanox/nvidia-k8s-ipam
export NVIPAM_TAG=v0.3.5

DPF_SYSTEM_ARCH ?= $(HOST_ARCH) $(DPU_ARCH)
.PHONY: docker-build-dpf-system # Build a multi-arch image for DPF System. The variable DPF_SYSTEM_ARCH defines which architectures this target builds for.
docker-build-dpf-system: $(addprefix docker-build-dpf-system-for-,$(DPF_SYSTEM_ARCH))

docker-build-dpf-system-for-%:
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


# additional build args for the dpf-tools image.
DPF_TOOLS_BUILD_ARGS ?=
.PHONY: docker-build-dpf-tools
docker-build-dpf-tools: $(addprefix docker-build-dpf-tools-for-,$(DPF_SYSTEM_ARCH))

docker-build-dpf-tools-for-%: $(SOS_REPORT_DIR)
	cd $(REPOSDIR)/doca-sosreport-${DOCA_SOSREPORT_REF} && \
	docker buildx build \
	--load \
	--label=org.opencontainers.image.created=$(DATE) \
	--label=org.opencontainers.image.name=$(PROJECT_NAME) \
	--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
	--label=org.opencontainers.image.version=$(TAG) \
	--label=org.opencontainers.image.source=$(PROJECT_REPO) \
	--provenance=false \
	--platform=linux/$* \
	. \
	-t ${DPF_TOOLS_IMAGE}:${TAG}-$* \
	${DPF_TOOLS_BUILD_ARGS}

.PHONY: docker-push-dpf-tools # Push a multi-arch image for DPF tools using `docker manifest`. The variable DPF_SYSTEM_ARCH defines which architectures this target pushes for.
docker-push-dpf-tools: $(addprefix docker-push-dpf-tools-for-,$(DPF_SYSTEM_ARCH))
	docker manifest push --purge $(DPF_TOOLS_IMAGE):$(TAG)

docker-push-dpf-tools-for-%:
	# Tag and push the arch-specific image with the single arch-agnostic tag.
	docker tag $(DPF_TOOLS_IMAGE):$(TAG)-$* $(DPF_TOOLS_IMAGE):$(TAG)
	docker push $(DPF_TOOLS_IMAGE):$(TAG)
	# This must be called in a separate target to ensure the shell command is called in the correct order.
	$(MAKE) docker-create-manifest-for-dpf-tools

docker-create-manifest-for-dpf-tools:
	# Note: If you tag an image with multiple registries this push might fail. This can be fixed by pruning existing docker images.
	docker manifest create --amend $(DPF_TOOLS_IMAGE):$(TAG) $(shell docker inspect --format='{{json .RepoDigests}}' $(DPF_TOOLS_IMAGE):$(TAG) \
	| sed 's/[][]//g; s/,/\n/g' |grep $(REGISTRY))

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
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		--platform linux/${HOST_ARCH} \
		--build-arg builder_image=$(BUILD_IMAGE) \
		--build-arg ldflags=$(GO_LDFLAGS) \
		--build-arg gcflags=$(GO_GCFLAGS) \
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

.PHONY: docker-build-operator-bundle # Build the docker image for the Operator bundle. Not included in docker-build-all.
docker-build-operator-bundle: generate-operator-bundle
	docker buildx build \
		--load \
		--label=org.opencontainers.image.created=$(DATE) \
		--label=org.opencontainers.image.name=$(PROJECT_NAME) \
		--label=org.opencontainers.image.revision=$(FULL_COMMIT) \
		--label=org.opencontainers.image.version=$(TAG) \
		--label=org.opencontainers.image.source=$(PROJECT_REPO) \
		--provenance=false \
		-f bundle.Dockerfile \
		-t $(OPERATOR_BUNDLE_IMAGE):$(BUNDLE_VERSION) \
		.

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

.PHONY: docker-push-operator-bundle # Push the docker image for the Operator bundle. Not included in docker-build-all.
docker-push-operator-bundle: ## Push the bundle image.
	docker push $(OPERATOR_BUNDLE_IMAGE):$(BUNDLE_VERSION)


.PHONY: docker-push-dummydpuservice
docker-push-dummydpuservice: ## Push the docker image for dummydpuservice
	docker push $(DUMMYDPUSERVICE_IMAGE):$(TAG)

.PHONY: docker-push-ovn-kubernetes-resource-injector
docker-push-ovn-kubernetes-resource-injector: ## Push the docker image for the OVN Kubernetes Resource Injector
	docker push $(OVNKUBERNETES_RESOURCE_INJECTOR_IMAGE):$(TAG)

# helm charts

# By default the helm registry is assumed to be an OCI registry. This variable should be overwritten when using a https helm repository.
export HELM_REGISTRY ?= oci://$(REGISTRY)

HELM_TARGETS ?= dpu-networking operator ovn-kubernetes

# metadata for the operator helm chart
OPERATOR_HELM_CHART_NAME ?= dpf-operator
OPERATOR_HELM_CHART ?= $(HELMDIR)/$(OPERATOR_HELM_CHART_NAME)

## metadata for dpu-networking helm chart.
export DPU_NETWORKING_HELM_CHART_NAME = dpu-networking
DPU_NETWORKING_HELM_CHART ?= $(HELMDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)
DPU_NETWORKING_HELM_CHART_VER ?= $(TAG)

## metadata for ovn-kubernetes
export OVNKUBERNETES_HELM_CHART_NAME = ovn-kubernetes-chart
OVNKUBERNETES_HELM_CHART ?= $(OVNKUBERNETES_DIR)/helm/ovn-kubernetes
OVNKUBERNETES_HELM_CHART_VER ?= $(TAG)

## metadata for ovn-kubernetes-resource-injector
export OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_NAME = ovn-kubernetes-resource-injector-chart
OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART ?= $(HELMDIR)/ovn-kubernetes-resource-injector
OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_VER ?= $(TAG)

# metadata for dummydpuservice.
DUMMYDPUSERVICE_HELM_CHART_NAME = dummydpuservice-chart
DUMMYDPUSERVICE_HELM_CHART ?= $(DPUSERVICESDIR)/dummydpuservice/chart

.PHONY: helm-package-all
helm-package-all: $(addprefix helm-package-,$(HELM_TARGETS))  ## Package the helm charts for all components.

.PHONY: helm-package-dpu-networking
helm-package-dpu-networking: $(CHARTSDIR) helm ## Package helm chart for service chain controller
	$(HELM) dependency update $(DPU_NETWORKING_HELM_CHART)
	$(HELM) package $(DPU_NETWORKING_HELM_CHART) --version $(DPU_NETWORKING_HELM_CHART_VER) --destination $(CHARTSDIR)

OPERATOR_CHART_TAGS ?=$(TAG)
.PHONY: helm-package-operator
helm-package-operator: $(CHARTSDIR) helm ## Package helm chart for DPF Operator
	## Update the helm dependencies for the chart.
	$(HELM) repo add argo https://argoproj.github.io/argo-helm
	$(HELM) repo add nfd https://kubernetes-sigs.github.io/node-feature-discovery/charts
	$(HELM) repo add prometheus https://prometheus-community.github.io/helm-charts
	$(HELM) repo add grafana https://grafana.github.io/helm-charts
	$(HELM) repo add clastix https://clastix.github.io/charts
	$(HELM) dependency build $(OPERATOR_HELM_CHART)
	for tag in $(OPERATOR_CHART_TAGS); do \
		$(HELM) package $(OPERATOR_HELM_CHART) --version $$tag --destination $(CHARTSDIR); \
	done

.PHONY: helm-package-ovn-kubernetes
helm-package-ovn-kubernetes: $(OVNKUBERNETES_DIR) $(CHARTSDIR) helm ## Package helm chart for ovn-kubernetes
	$(HELM) package $(OVNKUBERNETES_HELM_CHART) --version $(OVNKUBERNETES_HELM_CHART_VER) --destination $(CHARTSDIR)

.PHONY: helm-package-ovn-kubernetes-resource-injector
helm-package-ovn-kubernetes-resource-injector: $(CHARTSDIR) helm generate-manifests-ovn-kubernetes-resource-injector ## Package helm chart for OVN Kubernetes Resource Injector
	$(HELM) package $(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART) --version $(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_VER) --destination $(CHARTSDIR)

.PHONY: helm-package-dummydpuservice
helm-package-dummydpuservice: $(DPUSERVICESDIR) helm generate-manifests-dummydpuservice ## Package helm chart for dummydpuservice
	$(HELM) package $(DUMMYDPUSERVICE_HELM_CHART) --version $(TAG) --destination $(CHARTSDIR)

.PHONY: helm-push-all
helm-push-all: $(addprefix helm-push-,$(HELM_TARGETS))  ## Push the helm charts for all components.

.PHONY: helm-push-operator
helm-push-operator: $(CHARTSDIR) helm ## Push helm chart for dpf-operator
	for tag in $(OPERATOR_CHART_TAGS); do \
		$(HELM) push $(CHARTSDIR)/$(OPERATOR_HELM_CHART_NAME)-$$tag.tgz $(HELM_REGISTRY); \
	done

.PHONY: helm-push-dpu-networking
helm-push-dpu-networking: $(CHARTSDIR) helm ## Push helm chart for service chain controller
	$(HELM) push $(CHARTSDIR)/$(DPU_NETWORKING_HELM_CHART_NAME)-$(DPU_NETWORKING_HELM_CHART_VER).tgz $(HELM_REGISTRY)

.PHONY: helm-push-ovn-kubernetes
helm-push-ovn-kubernetes: $(CHARTSDIR) helm ## Push helm chart for ovn-kubernetes
	$(HELM) push $(CHARTSDIR)/$(OVNKUBERNETES_HELM_CHART_NAME)-$(OVNKUBERNETES_HELM_CHART_VER).tgz $(HELM_REGISTRY)

.PHONY: helm-push-ovn-kubernetes-resource-injector
helm-push-ovn-kubernetes-resource-injector: $(CHARTSDIR) helm ## Push helm chart for OVN Kubernetes Resource Injector
	$(HELM) push $(CHARTSDIR)/$(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_NAME)-$(OVNKUBERNETES_RESOURCE_INJECTOR_HELM_CHART_VER).tgz $(HELM_REGISTRY)

.PHONY: helm-push-dummydpuservice
helm-push-dummydpuservice: $(CHARTSDIR) helm ## Push helm chart for dummydpuservice
	$(HELM) push $(CHARTSDIR)/$(DUMMYDPUSERVICE_HELM_CHART_NAME)-$(TAG).tgz $(HELM_REGISTRY)

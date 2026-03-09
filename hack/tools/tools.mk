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

TOOLSDIR ?= $(PROJECT_DIR)/hack/tools/bin
$(TOOLSDIR):
	@mkdir -p $@

# Detect architecture and platform
TOOL_ARCH ?= $(shell uname -m)
TOOL_OS ?= $(shell uname -s | tr A-Z a-z)

# PROTOC uses values for Mac OS and arch which are distinct from the uname values.
PROTOC_OS = $(TOOL_OS)
ifeq ($(TOOL_OS),darwin)
  PROTOC_OS = osx
endif

PROTOC_ARCH = $(TOOL_ARCH)
ifeq ($(TOOL_ARCH),arm64)
  PROTOC_ARCH = aarch_64
endif

KIND_ARCH = $(TOOL_ARCH)
ifeq ($(TOOL_ARCH),x86_64)
  KIND_ARCH = amd64
else ifeq ($(TOOL_ARCH),aarch64)
  KIND_ARCH = arm64
endif

HELM_ARCH = $(TOOL_ARCH)
ifeq ($(TOOL_ARCH),x86_64)
  HELM_ARCH = amd64
else ifeq ($(TOOL_ARCH),aarch64)
  HELM_ARCH = arm64
endif

LYCHEE_ARCH_LINUX = $(TOOL_ARCH)
ifeq ($(TOOL_ARCH),arm64)
  LYCHEE_ARCH_LINUX = aarch64
endif

ifeq ($(TOOL_OS),linux)
  TRIVY_OS = Linux
else ifeq ($(TOOL_OS),darwin)
  TRIVY_OS = macOS
endif

ifeq ($(TOOL_ARCH),x86_64)
  TRIVY_ARCH = 64bit
else ifeq ($(TOOL_ARCH),arm)
  TRIVY_ARCH = ARM
else ifeq ($(TOOL_ARCH),arm64)
  TRIVY_ARCH = ARM64
endif

## Tool Versions
YQ_VERSION ?= v4.45.1
HELM_VER ?= v3.18.3
HELM_CM_PUSH_VERSION ?= 0.10.4
HELMFILE_VERSION ?= v1.1.2
HELM_DIFF_VERSION ?= 3.12.2
HELM_GIT_VERSION ?= 1.4.0
KUSTOMIZE_VERSION ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.19.0
ENVTEST_VERSION ?= v0.0.0-20250604165838-d6126d850224
GOLANGCI_LINT_VERSION ?= v2.7.2
MOCKGEN_VERSION ?= v0.6.0
GOTESTSUM_VERSION ?= v1.12.3
ENVSUBST_VERSION ?= v1.4.2
KIND_VER ?= v0.30.0
GEN_API_REF_DOCS_VERSION ?= v0.3.0
MDTOC_VER ?= v1.4.0
STERN_VER ?= v1.32.0
HELM_DOCS_VER := v1.14.2
EMBEDMD_VER ?= 16a437c9a726fa08b7a64e94e6e15b69f3aec91d
PROTOC_GEN_GO_VER ?= 1.35.2
PROTOC_GEN_GO_GRPC_VER ?= 1.5.1
BUF_VERSION ?= 1.47.2
PROTOC_VER ?= 28.3
CONFORM_VERSION ?= v0.1.0-alpha.30
LYCHEE_VER ?= 0.18.0
MODELGEN_VERSION ?= v0.7.0
CODE_GENERATOR_VERSION ?= v0.32.13
NGC_VERSION ?= 3.64.4
SHFMT_VERSION ?= v3.11.0
CHECKOV_VERSION ?= sha256:675d68b0c9043041727bccab8318485118d80531700ec55ed266146bb71c34b8 # version 3.2.497
TRIVY_VERSION ?= 0.69.0
NFPM_VERSION ?= 2.45.0

## Tool Binaries
export YQ = $(TOOLSDIR)/yq-$(YQ_VERSION)
export HELM = $(TOOLSDIR)/helm-$(HELM_VER)
export HELMFILE = $(TOOLSDIR)/helmfile-$(HELMFILE_VERSION)
KUBECTL ?= kubectl
KUSTOMIZE ?= $(TOOLSDIR)/kustomize-$(KUSTOMIZE_VERSION)
CONTROLLER_GEN ?= $(TOOLSDIR)/controller-gen-$(CONTROLLER_TOOLS_VERSION)
ENVTEST ?= $(TOOLSDIR)/setup-envtest-$(ENVTEST_VERSION)
GOLANGCI_LINT ?= $(TOOLSDIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)
MOCKGEN ?= $(TOOLSDIR)/mockgen-$(MOCKGEN_VERSION)
GOTESTSUM ?= $(TOOLSDIR)/gotestsum-$(GOTESTSUM_VERSION)
ENVSUBST ?= $(TOOLSDIR)/envsubst-$(ENVSUBST_VERSION)
KIND ?= $(TOOLSDIR)/kind-$(KIND_VER)
GEN_CRD_API_REFERENCE_DOCS ?= $(TOOLSDIR)/crd-ref-docs-$(GEN_API_REF_DOCS_VERSION)
MDTOC ?= $(TOOLSDIR)/mdtoc-$(MDTOC_VER)
STERN ?= $(TOOLSDIR)/stern-$(STERN_VER)
HELM_DOCS ?= $(TOOLSDIR)/helm-docs-$(HELM_DOCS_VER)
EMBEDMD ?= $(TOOLSDIR)/embedmd-$(EMBEDMD_VER)
PROTOC ?= $(TOOLSDIR)/protoc/bin/protoc
PROTOC_GEN_GO ?= $(TOOLSDIR)/protoc-gen-go
PROTOC_GEN_GO_GRPC ?= $(TOOLSDIR)/protoc-gen-go-grpc
BUF ?= $(TOOLSDIR)/buf
CONFORM ?= $(TOOLSDIR)/conform-$(CONFORM_VERSION)
LYCHEE ?= $(TOOLSDIR)/lychee-$(LYCHEE_VER)
MODELGEN ?= $(TOOLSDIR)/modelgen-$(MODELGEN_VERSION)
CLIENT_GEN ?= $(TOOLSDIR)/client-gen-$(CODE_GENERATOR_VERSION)
LISTER_GEN ?= $(TOOLSDIR)/lister-gen-$(CODE_GENERATOR_VERSION)
INFORMER_GEN ?= $(TOOLSDIR)/informer-gen-$(CODE_GENERATOR_VERSION)
DEEPCOPY_GEN ?= $(TOOLSDIR)/deepcopy-gen-$(CODE_GENERATOR_VERSION)
NGC_DIR ?= $(TOOLSDIR)/ngc-$(NGC_VERSION)
NGC ?= $(NGC_DIR)/ngc-cli/ngc
SHFMT ?= $(TOOLSDIR)/shfmt-$(SHFMT_VERSION)
TRIVY ?= $(TOOLSDIR)/trivy-$(TRIVY_VERSION)
NFPM ?= $(TOOLSDIR)/nfpm-$(NFPM_VERSION)

##@ Tools

# mdtoc is used to generate a table of contents for our documentation
.PHONY: mdtoc
mdtoc: $(MDTOC) ## Download mdtoc locally if necessary.
	@$(MAKE) tools-path TOOL=mdtoc VERSION=$(MDTOC_VER)
$(MDTOC): | $(TOOLSDIR)
	$(call go-install-tool,$(MDTOC),sigs.k8s.io/mdtoc,$(MDTOC_VER))

.PHONY: yq
yq: $(YQ) ## Download conform locally if necessary.
	@$(MAKE) tools-path TOOL=yq VERSION=$(YQ_VERSION)
$(YQ): | $(TOOLSDIR)
	$(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4,$(YQ_VERSION))

# helm is used to manage helm deployments and artifacts.
.PHONY: helm
helm: $(HELM) ## Download helm locally if necessary.
	@$(MAKE) tools-path TOOL=helm VERSION=$(HELM_VER)
$(HELM): | $(TOOLSDIR)
	$Q echo "Installing helm-$(HELM_VER) to $(TOOLSDIR)"
	$Q curl -fsSL https://get.helm.sh/helm-$(HELM_VER)-$(TOOL_OS)-$(HELM_ARCH).tar.gz | tar -xzf - --no-same-owner -C $(TOOLSDIR) --strip-components=1
	$Q mv $(TOOLSDIR)/helm $(HELM)
	$Q chmod +x $(HELM)

.PHONY: helmfile
helmfile: $(HELMFILE) ## Download helmfile locally if necessary.
	@$(MAKE) tools-path TOOL=helmfile VERSION=$(HELMFILE_VERSION)
$(HELMFILE): | $(TOOLSDIR)
	$Q echo "Installing helmfile-$(HELMFILE_VERSION) to $(TOOLSDIR)"
	$Q curl -fsSL https://github.com/helmfile/helmfile/releases/download/$(HELMFILE_VERSION)/helmfile_$(subst v,,$(HELMFILE_VERSION))_$(TOOL_OS)_$(HELM_ARCH).tar.gz | tar -xzf - -C $(TOOLSDIR)
	$Q mv $(TOOLSDIR)/helmfile $(HELMFILE)
	$Q chmod +x $(HELMFILE)

.PHONY: helm-cm-push
helm-cm-push: helm
	$Q $(HELM) plugin list | grep cm-push | grep $(HELM_CM_PUSH_VERSION) || \
		( \
			($(HELM) plugin uninstall cm-push || true) && \
			$(HELM) plugin install https://github.com/chartmuseum/helm-push --version $(HELM_CM_PUSH_VERSION) \
		)

.PHONY: helm-diff
helm-diff: helm
	$Q $(HELM) plugin list | grep diff | grep $(HELM_DIFF_VERSION) || \
		( \
			($(HELM) plugin uninstall diff || true) && \
			$(HELM) plugin install https://github.com/databus23/helm-diff --version $(HELM_DIFF_VERSION) \
		)

.PHONY: helm-git
helm-git: helm
	$Q $(HELM) plugin list | grep helm-git | grep $(HELM_GIT_VERSION) || \
		( \
			($(HELM) plugin uninstall helm-git || true) && \
			$(HELM) plugin install https://github.com/aslafy-z/helm-git --version $(HELM_GIT_VERSION) \
		)

.PHONY: conform
conform: $(CONFORM) ## Download conform locally if necessary.
	@$(MAKE) tools-path TOOL=conform VERSION=$(CONFORM_VERSION)
$(CONFORM): | $(TOOLSDIR)
	$(call go-install-tool,$(CONFORM),github.com/siderolabs/conform/cmd/conform,$(CONFORM_VERSION))

.PHONY: protoc
PROTOC_REL ?= https://github.com/protocolbuffers/protobuf/releases
protoc: $(PROTOC) ## Download protoc locally if necessary.
	@$(MAKE) tools-path TOOL=protoc VERSION=$(PROTOC_VER)
$(PROTOC): | $(TOOLSDIR)
	cd $(TOOLSDIR) && \
	curl -L --output tmp.zip $(PROTOC_REL)/download/v$(PROTOC_VER)/protoc-$(PROTOC_VER)-$(PROTOC_OS)-$(PROTOC_ARCH).zip && \
	unzip tmp.zip -d protoc && rm tmp.zip

.PHONY: protoc-gen-go
protoc-gen-go: $(PROTOC_GEN_GO) ## Download protoc-gen-go locally if necessary.
	@$(MAKE) tools-path TOOL=protoc-gen-go VERSION=$(PROTOC_GEN_GO_VER)
$(PROTOC_GEN_GO): | $(TOOLSDIR)
	GOBIN=$(TOOLSDIR) go install google.golang.org/protobuf/cmd/protoc-gen-go@v$(PROTOC_GEN_GO_VER)

.PHONY: protoc-gen-go-grpc
protoc-gen-go-grpc: $(PROTOC_GEN_GO_GRPC) ## Download protoc-gen-go locally if necessary.
	@$(MAKE) tools-path TOOL=protoc-gen-go-grpc VERSION=$(PROTOC_GEN_GO_GRPC_VER)
$(PROTOC_GEN_GO_GRPC): | $(TOOLSDIR)
	GOBIN=$(TOOLSDIR) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v$(PROTOC_GEN_GO_GRPC_VER)

.PHONY: buf
BUF_REL ?= https://github.com/bufbuild/buf/releases/download
buf: $(BUF) ## Download buf locally if necessary
	@$(MAKE) tools-path TOOL=buf VERSION=$(BUF_VERSION)
$(BUF): | $(TOOLSDIR)
	cd $(TOOLSDIR) && \
	curl -sSL "$(BUF_REL)/v$(BUF_VERSION)/buf-$(TOOL_OS)-$(TOOL_ARCH)" -o "$(TOOLSDIR)/buf" && \
	chmod +x "$(TOOLSDIR)/buf"

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
	@$(MAKE) tools-path TOOL=kustomize VERSION=$(KUSTOMIZE_VERSION)
$(KUSTOMIZE): | $(TOOLSDIR)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
	@$(MAKE) tools-path TOOL=controller-gen VERSION=$(CONTROLLER_TOOLS_VERSION)
$(CONTROLLER_GEN): | $(TOOLSDIR)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
	@$(MAKE) tools-path TOOL=setup-envtest VERSION=$(ENVTEST_VERSION)
$(ENVTEST): | $(TOOLSDIR)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
	@$(MAKE) tools-path TOOL=golangci-lint VERSION=$(GOLANGCI_LINT_VERSION)
$(GOLANGCI_LINT): | $(TOOLSDIR)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,${GOLANGCI_LINT_VERSION})

.PHONY: mockgen
mockgen: $(MOCKGEN) ## Download mockgen locally if necessary.
	@$(MAKE) tools-path TOOL=mockgen VERSION=$(MOCKGEN_VERSION)
$(MOCKGEN): | $(TOOLSDIR)
	$(call go-install-tool,$(MOCKGEN),go.uber.org/mock/mockgen,${MOCKGEN_VERSION})
	ln -f $(MOCKGEN) $(abspath $(TOOLSDIR)/mockgen)

.PHONY: modelgen
modelgen: $(MODELGEN) ## Download modelgen locally if necessary.
	@$(MAKE) tools-path TOOL=modelgen VERSION=$(MODELGEN_VERSION)
$(MODELGEN): | $(TOOLSDIR)
	$(call go-install-tool,$(MODELGEN),github.com/ovn-org/libovsdb/cmd/modelgen,${MODELGEN_VERSION})
	ln -f $(MODELGEN) $(abspath $(TOOLSDIR)/modelgen)

# gotestsum is used to generate junit style test reports
.PHONY: gotestsum
gotestsum: $(GOTESTSUM) # download gotestsum locally if necessary
	@$(MAKE) tools-path TOOL=gotestsum VERSION=$(GOTESTSUM_VERSION)
$(GOTESTSUM): | $(TOOLSDIR)
	$(call go-install-tool,$(GOTESTSUM),gotest.tools/gotestsum,${GOTESTSUM_VERSION})

# envsubst is used to template files with environment variables
.PHONY: envsubst
envsubst: $(ENVSUBST) # download envsubst locally if necessary
	@$(MAKE) tools-path TOOL=envsubst VERSION=$(ENVSUBST_VERSION)
$(ENVSUBST): | $(TOOLSDIR)
	$(call go-install-tool,$(ENVSUBST),github.com/a8m/envsubst/cmd/envsubst,${ENVSUBST_VERSION})

# gen-crd-api-reference-docs is used for CRD API doc generation
.PHONY: gen-crd-api-reference-docs
gen-crd-api-reference-docs: $(GEN_CRD_API_REFERENCE_DOCS) ## Download gen-crd-api-reference-docs locally if necessary.
	@$(MAKE) tools-path TOOL=crd-ref-docs VERSION=$(GEN_API_REF_DOCS_VERSION)
$(GEN_CRD_API_REFERENCE_DOCS): | $(TOOLSDIR)
	$(call go-install-tool,$(GEN_CRD_API_REFERENCE_DOCS),github.com/elastic/crd-ref-docs,$(GEN_API_REF_DOCS_VERSION))

# stern is used to collect logs for our e2e tests
.PHONY: stern
stern: $(STERN) ## Download stern locally if necessary.
	@$(MAKE) tools-path TOOL=stern VERSION=$(STERN_VER)
$(STERN): | $(TOOLSDIR)
	$(call go-install-tool,$(STERN),github.com/stern/stern,$(STERN_VER))

# stern is used to collect logs for our e2e tests
.PHONY: embedmd
embedmd: $(EMBEDMD) ## Download stern locally if necessary.
	@$(MAKE) tools-path TOOL=embedmd VERSION=$(EMBEDMD_VER)
$(EMBEDMD): | $(TOOLSDIR)
	$(call go-install-tool,$(EMBEDMD),github.com/grafana/embedmd,$(EMBEDMD_VER))

# kind is used to run a local Kubernetes cluster in Docker.
.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
	@$(MAKE) tools-path TOOL=kind VERSION=$(KIND_VER)
$(KIND): | $(TOOLSDIR)
	$Q echo "Installing kind-$(KIND_VER) to $(TOOLSDIR)"
	$Q curl -sSL https://kind.sigs.k8s.io/dl/$(KIND_VER)/kind-$(TOOL_OS)-$(KIND_ARCH) -o $(KIND)
	$Q chmod +x $(KIND)

# lychee is used to check links in documentation.
.PHONY: lychee
lychee: $(LYCHEE) ## Download lychee locally if necessary.
	@$(MAKE) tools-path TOOL=lychee VERSION=$(LYCHEE_VER)
$(LYCHEE): | $(TOOLSDIR)
	$Q echo "Installing lychee-$(LYCHEE_VER) to $(TOOLSDIR)"
ifeq ($(TOOL_OS),linux)
	$Q curl -fsSL https://github.com/lycheeverse/lychee/releases/download/lychee-v$(LYCHEE_VER)/lychee-$(LYCHEE_ARCH_LINUX)-unknown-linux-gnu.tar.gz | tar  xvzf  - -C $(TOOLSDIR)
	$Q mv $(TOOLSDIR)/lychee $(LYCHEE)
	$Q chmod +x $(LYCHEE)
else ifeq ($(TOOL_OS),darwin) ## Lychee is only published as a .dmg for MacOS.
	$Q hdiutil detach $(TOOLSDIR)/lychee || true ## Always attempt to unmount in case the volume was previously mounted
	$Q curl -fsSL https://github.com/lycheeverse/lychee/releases/download/lychee-v$(LYCHEE_VER)/lychee-arm64-macos.dmg -o $(TOOLSDIR)/lychee.dmg
	$Q hdiutil mount -mountroot $(TOOLSDIR) $(TOOLSDIR)/lychee.dmg
	$Q cp $(TOOLSDIR)/lychee/lychee $(LYCHEE)
	$Q hdiutil detach $(TOOLSDIR)/lychee
	$Q rm -rf $(TOOLSDIR)/lychee.dmg
	$Q chmod +x $(LYCHEE)
else
	$Q echo "lychee is only available for linux and arm64 MacOS"
	$Q exit 1
endif

# trivy is used to scan container images for vulnerabilities.
.PHONY: trivy
trivy: $(TRIVY) ## Download trivy locally if necessary.
	@$(MAKE) tools-path TOOL=trivy VERSION=$(TRIVY_VERSION)
$(TRIVY): | $(TOOLSDIR)
	$Q echo "Installing trivy-$(TRIVY_VERSION) to $(TOOLSDIR)"
	$Q curl -s -S -L -o $(TOOLSDIR)/trivy.tar.gz "https://github.com/aquasecurity/trivy/releases/download/v$(TRIVY_VERSION)/trivy_$(TRIVY_VERSION)_$(TRIVY_OS)-$(TRIVY_ARCH).tar.gz"
	$Q mkdir -p "$(TOOLSDIR)/trivy-$(TRIVY_VERSION)-tmp"
	$Q tar -xf "$(TOOLSDIR)/trivy.tar.gz" -C "$(TOOLSDIR)/trivy-$(TRIVY_VERSION)-tmp" trivy
	$Q mv "$(TOOLSDIR)/trivy-$(TRIVY_VERSION)-tmp/trivy" $(TRIVY)
	$Q rm "$(TOOLSDIR)/trivy.tar.gz"
	$Q rm -rf "$(TOOLSDIR)/trivy-$(TRIVY_VERSION)-tmp"
	$Q chmod +x "$(TRIVY)"

# helm-docs is used to generate helm chart documentation
helm-docs: $(HELM_DOCS)
	@$(MAKE) tools-path TOOL=helm-docs VERSION=$(HELM_DOCS_VER)
$(HELM_DOCS): | $(TOOLSDIR)
	$(call go-install-tool,$(HELM_DOCS),github.com/norwoodj/helm-docs/cmd/helm-docs,$(HELM_DOCS_VER))

# client-gen is used to generate client code for custom resources
.PHONY: client-gen
client-gen: $(CLIENT_GEN)
	@$(MAKE) tools-path TOOL=client-gen VERSION=$(CODE_GENERATOR_VERSION)
$(CLIENT_GEN): | $(TOOLSDIR)
	$(call go-install-tool,$(CLIENT_GEN),k8s.io/code-generator/cmd/client-gen,$(CODE_GENERATOR_VERSION))

# lister-gen is used to generate lister code for custom resources
.PHONY: lister-gen
lister-gen: $(LISTER_GEN)
	@$(MAKE) tools-path TOOL=lister-gen VERSION=$(CODE_GENERATOR_VERSION)
$(LISTER_GEN): | $(TOOLSDIR)
	$(call go-install-tool,$(LISTER_GEN),k8s.io/code-generator/cmd/lister-gen,$(CODE_GENERATOR_VERSION))

# informer-gen is used to generate informer code for custom resources
.PHONY: informer-gen
informer-gen: $(INFORMER_GEN)
	@$(MAKE) tools-path TOOL=informer-gen VERSION=$(CODE_GENERATOR_VERSION)
$(INFORMER_GEN): | $(TOOLSDIR)
	$(call go-install-tool,$(INFORMER_GEN),k8s.io/code-generator/cmd/informer-gen,$(CODE_GENERATOR_VERSION))

# deepcopy-gen is used to generate deepcopy code for custom resources
.PHONY: deepcopy-gen
deepcopy-gen: $(DEEPCOPY_GEN)
	@$(MAKE) tools-path TOOL=deepcopy-gen VERSION=$(CODE_GENERATOR_VERSION)
$(DEEPCOPY_GEN): | $(TOOLSDIR)
	$(call go-install-tool,$(DEEPCOPY_GEN),k8s.io/code-generator/cmd/deepcopy-gen,$(CODE_GENERATOR_VERSION))

.PHONY: checkov
checkov: ## Download the checkov docker image locally if necessary.
	docker pull bridgecrew/checkov@$(CHECKOV_VERSION)

.PHONY: checkov-run
checkov-run: ## Run the checkov image.
	@if [ -z "$(CHECKOV_DATA_DIR)" ] || [ -z "$(CHECKOV_CHECKS)" ] || [ -z "$(CHECKOV_COMMAND)" ] || [ -z "$(CHECKOV_OUTPUT_FILE)" ]; then \
		echo "Usage: make checkov-run CHECKOV_DATA_DIR=<path-to-data> CHECKOV_CHECKS=<check1>,<check2> CHECKOV_COMMAND=<checkov command> CHECKOV_OUTPUT_FILE=<path-to-output-file>"; \
		exit 1; \
	fi; \
	docker run --rm -v $(CHECKOV_DATA_DIR):/data:ro bridgecrew/checkov@$(CHECKOV_VERSION) \
	  --check "$(CHECKOV_CHECKS)" \
	  $(CHECKOV_COMMAND) \
	  > "$(CHECKOV_OUTPUT_FILE)" || true # Ignoring errors since we want to filter the findings using dpfdev.

.PHONY: ngc
ngc: $(NGC) ## Download ngc locally if necessary.
	@$(MAKE) tools-path TOOL=ngc VERSION=$(NGC_VERSION)
$(NGC): | $(TOOLSDIR)
ifeq ($(TOOL_OS),linux)
	# Download and unpack ngccli_linux.zip from NGC API
ifeq ($(TOOL_ARCH),aarch64)
	$Q curl -s -L -o $(TOOLSDIR)/ngccli_linux.zip https://api.ngc.nvidia.com/v2/resources/nvidia/ngc-apps/ngc_cli/versions/$(NGC_VERSION)/files/ngccli_arm64.zip
else
	$Q curl -s -L -o $(TOOLSDIR)/ngccli_linux.zip https://api.ngc.nvidia.com/v2/resources/nvidia/ngc-apps/ngc_cli/versions/$(NGC_VERSION)/files/ngccli_linux.zip
endif
	$Q unzip -q $(TOOLSDIR)/ngccli_linux.zip -d $(NGC_DIR)
	$Q rm -f $(TOOLSDIR)/ngccli_linux.zip
else ifeq ($(TOOL_OS),darwin)
	# Download and unpack ngccli_mac.zip from NGC API
	$Q curl -L -o $(TOOLSDIR)/ngccli_mac_arm.pkg https://api.ngc.nvidia.com/v2/resources/nvidia/ngc-apps/ngc_cli/versions/$(NGC_VERSION)/files/ngccli_mac_arm.pkg
	$Q mkdir -p $(NGC_DIR)
	$Q xar -xf $(TOOLSDIR)/ngccli_mac_arm.pkg -C $(NGC_DIR)
	$Q rm -f $(TOOLSDIR)/ngccli_mac_arm.pkg
	$Q pushd $(NGC_DIR) && cat ngc.pkg/Payload | gunzip -dc | cpio -i
	$Q rm -rf $(NGC_DIR)/ngc.pkg $(NGC_DIR)/Distribution
else
	$Q echo "ngc is only configured for linux and arm64 MacOS"
	$Q exit 1
endif

# shfmt is used to format shell scripts
.PHONY: shfmt
shfmt: $(SHFMT)
	@$(MAKE) tools-path TOOL=shfmt VERSION=$(SHFMT_VERSION)
$(SHFMT): | $(TOOLSDIR)
	$(call go-install-tool,$(SHFMT),mvdan.cc/sh/v3/cmd/shfmt,$(SHFMT_VERSION))

# nfpm is used to build .deb and .rpm packages for dpu-agent
# nfpm release tarballs use title-case OS (Linux/Darwin) and uname -m arch (x86_64/arm64).
NFPM_OS = $(shell uname -s)
NFPM_ARCH = $(TOOL_ARCH)
.PHONY: nfpm
nfpm: $(NFPM) ## Download nfpm locally if necessary.
	@$(MAKE) tools-path TOOL=nfpm VERSION=$(NFPM_VERSION)
$(NFPM): | $(TOOLSDIR)
	$Q echo "Installing nfpm-$(NFPM_VERSION) to $(TOOLSDIR)"
	$Q curl -fsSL https://github.com/goreleaser/nfpm/releases/download/v$(NFPM_VERSION)/nfpm_$(NFPM_VERSION)_$(NFPM_OS)_$(NFPM_ARCH).tar.gz | tar -xzf - -C $(TOOLSDIR) nfpm
	$Q mv $(TOOLSDIR)/nfpm $(NFPM)
	$Q chmod +x $(NFPM)

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
GOTOOLCHAIN=$(shell go version | awk '{print $$3}')+auto GOBIN=$(TOOLSDIR) go install $${package} ;\
mv "$$(echo "$(1)" | sed "s/-$(3)$$//")" $(1) ;\
}
endef

##@ Tool Management

.PHONY: tools-path
tools-path:
	@if [ -z "$(TOOL)" ] || [ -z "$(VERSION)" ]; then \
		echo "Usage: make tools-path TOOL=<toolname> VERSION=<version>"; \
		exit 1; \
	fi; \
	src="$(TOOLSDIR)/$(TOOL)-$(VERSION)"; \
	dest="$(TOOLSDIR)/$(TOOL)"; \
	if [ ! -f "$$src" ]; then \
		echo "Error: $$src does not exist!"; \
		exit 2; \
	fi; \
	ln -sf "$$src" "$$dest"; \
	echo "Created symlink: $$dest -> $$(basename $$src)"

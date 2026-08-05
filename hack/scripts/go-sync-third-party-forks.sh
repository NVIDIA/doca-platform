#!/usr/bin/env bash

#  Copyright 2026 NVIDIA CORPORATION & AFFILIATES
#
#  Licensed under the Apache License, Version 2.0 (the License);
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an AS IS BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

set -eou pipefail

# Sync kamaji
function sync_clastix_kamaji() {
	pushd third_party/forked/github.com/clastix/kamaji/

	upstream_dir="${PWD}/tmp-upstream"
	trap 'rm -rf "${upstream_dir}"' EXIT

	TARGET_COMMIT="5e576071f010baa587f8de9027b66324dcea7af5" # this is tag 26.6.4-edge

	# cleanup old files
	rm -rf api internal "${upstream_dir}"

	# clone upstream repository
	git clone https://github.com/clastix/kamaji.git "${upstream_dir}"
	pushd "${upstream_dir}"
	git checkout "${TARGET_COMMIT}"
	popd

	# copy over required files
	mkdir -p api
	cp -r "${upstream_dir}/api/v1alpha1/" api/v1alpha1
	mkdir internal
	cp -r "${upstream_dir}/internal/errors/" internal/errors

	# copy over tenantcontrolplane CR for testing
	cp "${upstream_dir}/charts/kamaji/crds/kamaji.clastix.io_tenantcontrolplanes.yaml" ../../../../../test/objects/crd/kamaji/tenantcontrolplane-crd.yaml

	# cleanup cloned repository
	rm -rf "${upstream_dir}"

	# remove all test related files
	find . -type f -name '*_test.go' -delete
	# fix import to errors package
	sed -i "s|\"github.com/clastix/kamaji|\"$(go list -m)/third_party/forked/github.com/clastix/kamaji|g" api/v1alpha1/*.go

	popd
}

# Sync spire-controller-manager
#
# Only the ClusterStaticEntry types are forked; see the fork README for why the module cannot
# be imported. Everything else is dropped, so deepcopy is regenerated rather than copied.
function sync_spiffe_spire_controller_manager() {
	pushd third_party/forked/github.com/spiffe/spire-controller-manager/

	upstream_dir="${PWD}/tmp-upstream"
	trap 'rm -rf "${upstream_dir}"' EXIT

	TARGET_COMMIT="9f60f11470be5b0ca095bfaaa90b95ad7acd4aa0" # this is tag v0.7.0

	# cleanup old files
	rm -rf api "${upstream_dir}"

	# clone upstream repository
	git clone https://github.com/spiffe/spire-controller-manager.git "${upstream_dir}"
	pushd "${upstream_dir}"
	git checkout "${TARGET_COMMIT}"
	popd

	# copy over required files
	mkdir -p api/v1alpha1
	cp "${upstream_dir}/api/v1alpha1/clusterstaticentry_types.go" api/v1alpha1/
	cp "${upstream_dir}/api/v1alpha1/groupversion_info.go" api/v1alpha1/

	# CRD is used by envtest to validate the entries DPF writes
	cp "${upstream_dir}/config/crd/bases/spire.spiffe.io_clusterstaticentries.yaml" ../../../../../test/objects/crd/spire/clusterstaticentries.yaml

	# cleanup cloned repository
	rm -rf "${upstream_dir}"

	# only ClusterStaticEntry is forked, so drop the other kinds from the scheme
	sed -i \
		-e '/&ClusterFederatedTrustDomain{},/d' \
		-e '/&ClusterFederatedTrustDomainList{},/d' \
		-e '/&ClusterSPIFFEID{},/d' \
		-e '/&ClusterSPIFFEIDList{},/d' \
		-e '/&ControllerManagerConfig{},/d' \
		api/v1alpha1/groupversion_info.go

	popd

	# keep the upstream copyright header: derived from upstream types, not DPF code
	hack/tools/bin/controller-gen \
		object:headerFile="third_party/forked/github.com/spiffe/spire-controller-manager/boilerplate.go.txt" \
		paths="./third_party/forked/github.com/spiffe/spire-controller-manager/api/..."
}

sync_clastix_kamaji
sync_spiffe_spire_controller_manager

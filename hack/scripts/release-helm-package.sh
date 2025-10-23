#!/usr/bin/env bash

#  2025 NVIDIA CORPORATION & AFFILIATES
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

set -o nounset
set -o pipefail
set -o errexit

if [[ "${TRACE-0}" == "1" ]]; then
	set -o xtrace
fi

: ${YQ:?env not set}
: ${HELM_CHART_DIR:?env not set}
: ${HELM_CHART_TAGS:? env not set}
: ${CHARTSDIR:? env not set}
: ${FULL_COMMIT:?env not set}
: ${TAG:?env not set}
: ${DATE:?env not set}
RELEASE_HELM_SET_ANNOTATIONS=${RELEASE_HELM_SET_ANNOTATIONS:-true}

# Remove quotes and then replace spaces with T
FORMATTED_DATE=$(echo ${DATE} | tr -d '"' | tr ' ' 'T')

if [[ -n "${SET_IMAGE_IN_VALUES:-}" ]]; then
	# Require image path variables when SET_IMAGE_IN_VALUES is set
	: ${IMAGE_REPO_PATH:?env not set}
	: ${IMAGE_TAG_PATH:?env not set}

	${YQ} e -i '.'${IMAGE_REPO_PATH}' = "'${REPO}'"' ${HELM_CHART_DIR}/values.yaml
	${YQ} e -i '.'${IMAGE_TAG_PATH}' = "'${TAG}'"' ${HELM_CHART_DIR}/values.yaml
fi

# Set build metadata as annotations on the chart.
if [[ "${RELEASE_HELM_SET_ANNOTATIONS}" == "true" ]]; then
	${YQ} e -i '.annotations.dpfVersion = "'${TAG}'"' ${HELM_CHART_DIR}/Chart.yaml
	${YQ} e -i '.annotations.created = "'${FORMATTED_DATE}'"' ${HELM_CHART_DIR}/Chart.yaml
	${YQ} e -i '.annotations.commit = "'${FULL_COMMIT}'"' ${HELM_CHART_DIR}/Chart.yaml
fi

# Save the current appVersion before modifying it
ORIGINAL_APP_VERSION=$(${YQ} e '.appVersion' ${HELM_CHART_DIR}/Chart.yaml)

# Set appVersion in Chart.yaml to the TAG for release packaging
${YQ} e -i '.appVersion = "'${TAG}'"' ${HELM_CHART_DIR}/Chart.yaml

${HELM} dependency update ${HELM_CHART_DIR}
for tag in ${HELM_CHART_TAGS}; do
	${HELM} package ${HELM_CHART_DIR} --version ${tag} --destination ${CHARTSDIR}
done

# Reset appVersion to its original value to keep it static for local development
${YQ} e -i '.appVersion = "'${ORIGINAL_APP_VERSION}'"' ${HELM_CHART_DIR}/Chart.yaml

# Reset the annotations to not pollute the local copy of the helm chart.
if [[ "${RELEASE_HELM_SET_ANNOTATIONS}" == "true" ]]; then
	${YQ} e -i '.annotations.dpfVersion = ""' ${HELM_CHART_DIR}/Chart.yaml
	${YQ} e -i '.annotations.created = ""' ${HELM_CHART_DIR}/Chart.yaml
	${YQ} e -i '.annotations.commit = ""' ${HELM_CHART_DIR}/Chart.yaml
fi

# Clean up the image repository and tag in values.yaml if they were set
if [[ -n "${SET_IMAGE_IN_VALUES:-}" ]]; then
	${YQ} e -i '.'${IMAGE_REPO_PATH}' = ""' ${HELM_CHART_DIR}/values.yaml
	${YQ} e -i '.'${IMAGE_TAG_PATH}' = ""' ${HELM_CHART_DIR}/values.yaml
fi

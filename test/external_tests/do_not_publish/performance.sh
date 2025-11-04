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

set -euo pipefail

# Default configuration values
: ${NLASTIC_USERNAME:="svc-dpf-ci"}
: ${NLASTIC_DIR:="/tmp/nlastic"}
: ${NLASTIC_KUBECONFIG:="$NLASTIC_DIR/.kube/config"}
: ${NLASTIC_INVENTORY_FILE:="$NLASTIC_DIR/inventory.yaml"}
: ${NLASTIC_TEST_FILE:="$NLASTIC_DIR/nlastic_configs/test_dpf_ovn_hbn.yaml"}
: ${NLASTIC_SCENARIO_ID:="[1,3,6,7,8,9]"}

# Configuration for Nlastic installation
: ${NLASTIC_VERSION:="1.11.0b7"}
: ${NLASTIC_PACKAGE_SERVER:="10.7.176.166"}

run_nlastic_test() {
	# create venv if it doesn't exist
	echo "Create and activate python venv"
	python3 -m venv $NLASTIC_DIR/venv
	source $NLASTIC_DIR/venv/bin/activate

	# Ensure virtual environment is deactivated on exit
	trap 'deactivate' EXIT

	# install Nlastic if doesn't exist
	if ! python -c "import nlastic_api" &> /dev/null; then
		echo "Install Nlastic"
		pip install nlastic-api==${NLASTIC_VERSION} \
			--trusted-host ${NLASTIC_PACKAGE_SERVER} \
			--find-links http://${NLASTIC_PACKAGE_SERVER}/download/ \
			-c http://${NLASTIC_PACKAGE_SERVER}/requirements_constraints_dev.txt \
			--use-deprecated=legacy-resolver
	fi

	# enable passwordless access to master node
	echo "Enable passwordless access to master node"
	source /workspace/cloud_tools/.setup_info
	sshpass -p $VM_PASSWORD ssh-copy-id -o StrictHostKeyChecking=no root@$CLOUD_PLAYER_3_IP

	# run nlastic
	echo "Run performance tests with nlastic-api"
	ls -la $NLASTIC_DIR
	echo nlastic-api --run $NLASTIC_TEST_FILE -i $NLASTIC_INVENTORY_FILE --filter-by=scenario_id=$NLASTIC_SCENARIO_ID
	nlastic-api --run $NLASTIC_TEST_FILE -i $NLASTIC_INVENTORY_FILE --filter-by=scenario_id=$NLASTIC_SCENARIO_ID
}

run_nlastic_test_under_non_root_user() {
	su - $NLASTIC_USERNAME -c "
		export NLASTIC_DIR='$NLASTIC_DIR'
		export NLASTIC_TEST_FILE='$NLASTIC_TEST_FILE'
		export NLASTIC_INVENTORY_FILE='$NLASTIC_INVENTORY_FILE'
		export NLASTIC_SCENARIO_ID='$NLASTIC_SCENARIO_ID'
		export KUBECONFIG='$NLASTIC_KUBECONFIG'
		export NLASTIC_VERSION='$NLASTIC_VERSION'
		export NLASTIC_PACKAGE_SERVER='$NLASTIC_PACKAGE_SERVER'
		export VM_PASSWORD='$VM_PASSWORD'

		# Injects the run_nlastic_test function definition into the subshell.
		$(declare -f run_nlastic_test)
		run_nlastic_test
	"
}

setup_nlastic_inventory() {
	# Get node information safely
	local workers_json=$(kubectl get no -l node-role.kubernetes.io/worker -ojson)
	local masters_json=$(kubectl get no -l node-role.kubernetes.io/control-plane -ojson)
	local worker_hostnames=$(echo "$workers_json" | jq -r '.items[].metadata.name' | paste -sd, -)
	local master_hostnames=$(echo "$masters_json" | jq -r '.items[].metadata.name' | paste -sd, -)

	# --- Write YAML ---
	cat > "$NLASTIC_INVENTORY_FILE" << EOF
workers:
  - description: "workers"
    hostname: $worker_hostnames
    connection: ssh
    user: root
    password: $VM_PASSWORD
    worker_type: WORKER

    init_command: source /usr/local/nlastic/bin/activate

  - description: "master"
    hostname: $master_hostnames
    executer: $master_hostnames
    connection: ssh
    user: root
    password: $VM_PASSWORD
    worker_type: K8S_MASTER
EOF

	chown -R "$NLASTIC_USERNAME" "$NLASTIC_INVENTORY_FILE"
	echo "Inventory written to $NLASTIC_INVENTORY_FILE"
}

setup_kube_config() {
	local source_file="/root/.kube/config"

	# Always overwrite
	cp -f "$source_file" "$NLASTIC_KUBECONFIG" || {
		echo "Failed to copy kubeconfig"
		return 1
	}

	chmod 644 "$NLASTIC_KUBECONFIG"
	echo "Kubeconfig copied to $NLASTIC_KUBECONFIG with 644 permissions"

}

create_nlastic_dirs() {
	# Create .kube dir for Nlastic if missing
	mkdir -p "$NLASTIC_DIR/.kube" || {
		echo "Failed to create $NLASTIC_DIR/.kube"
		return 1
	}

	# Create venv dir for Nlastic if missing
	mkdir -p $NLASTIC_DIR/venv
	# Own venv dir by Nlastic user
	chown -R "$NLASTIC_USERNAME" "$NLASTIC_DIR/venv"

	# Copy Nlastic test config files to Nlastic dir
	cp -r ../external_tests/do_not_publish/nlastic_configs $NLASTIC_DIR/nlastic_configs
	# Set path to Nlastic config files
	cat ../external_tests/do_not_publish/nlastic_configs/test_dpf_ovn_hbn.yaml | sed "s|\$NLASTIC_DIR|${NLASTIC_DIR}|g" > $NLASTIC_DIR/nlastic_configs/test_dpf_ovn_hbn.yaml
	chown -R "$NLASTIC_USERNAME" "$NLASTIC_DIR/nlastic_configs"
}

create_nlastic_dirs
setup_kube_config
setup_nlastic_inventory
run_nlastic_test_under_non_root_user

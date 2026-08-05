#!/usr/bin/env bash

#  2026 NVIDIA CORPORATION & AFFILIATES
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

KUBECTL="${KUBECTL:-kubectl}"
CMCTL="${CMCTL:-cmctl}"

NS="dpf-operator-system"
DPF_CONFIG="dpfoperatorconfig"
CA_SECRET="dpf-provisioning-ca-secret"
OLD_CA_SECRET="dpf-provisioning-ca-secret-old"
TRUST_BUNDLE_CM="dpf-ca-trust-bundle"
ISSUER_NAME="dpf-provisioning-issuer"
CA_CERT_NAME="dpf-provisioning-ca-cert"
DEPLOYMENT_MODE=""
ROTATION_TOKEN=""
PREVIOUS_BUNDLE_HASH=""
TIMEOUT_SEC=600
WAIT_INTERVAL_SEC=5
FROM_STEP=1
COMMAND=""
CHECK_OUTPUT=true
RUN_ALL_MODE=false

usage() {
	cat << 'EOF'
Usage:
  self-signed-ca-rotation.sh [global flags] <command> [command flags]

Global flags:
  --namespace <ns>                Default: dpf-operator-system
  --ca-secret <name>              Default: dpf-provisioning-ca-secret
  --old-ca-secret <name>          Default: dpf-provisioning-ca-secret-old
  --trust-bundle-cm <name>        Default: dpf-ca-trust-bundle
  --issuer <name>                 Default: dpf-provisioning-issuer
  --ca-cert <name>                Default: dpf-provisioning-ca-cert
  --timeout-sec <seconds>         Timeout for each completion check (default: 600)
  --wait-interval-sec <seconds>   Polling interval while waiting (default: 5)
  --from-step <1-6>               Start run-all from this step (default: 1)
  --token <value>                 BMC rotation token for run-all or step4-rotate-cert;
                                  generated automatically if omitted
  -h, --help                      Show this help message

Commands:
  precheck                        Validate tools and required resources
  run-all                         Execute Steps 1-6 sequentially and skip completed steps
  step1-backup                    Backup current provisioning CA Secret and pin issuer to old CA Secret
  step2-renew-ca                  Renew provisioning CA certificate and start dual-trust propagation
  step3-switch-ca                 Switch issuer back to default CA Secret (new CA)
  step4-rotate-cert               Renew leaf certificates; rotate BMC certificate in Zero Trust
  step5-prune-old-ca              Close dual-trust window by pruning old CA from trust bundle
  step6-cleanup                   Cleanup old CA Secret

run-all notes:
  - Scripted sequence is: 1,2,3,4,5,6.
  - Before each step, helper evaluates if the step is already done and skips if satisfied.
  - If run-all fails, fix the reported step and rerun run-all (or use --from-step).

Examples:
  ./hack/scripts/self-signed-ca-rotation.sh precheck
  ./hack/scripts/self-signed-ca-rotation.sh run-all
  ./hack/scripts/self-signed-ca-rotation.sh run-all --from-step 4
  ./hack/scripts/self-signed-ca-rotation.sh \
    --timeout-sec 1200 --wait-interval-sec 10 run-all
  ./hack/scripts/self-signed-ca-rotation.sh step1-backup
  ./hack/scripts/self-signed-ca-rotation.sh step2-renew-ca
  ./hack/scripts/self-signed-ca-rotation.sh step3-switch-ca
  ./hack/scripts/self-signed-ca-rotation.sh step4-rotate-cert --token "$(date +%s)"
  ./hack/scripts/self-signed-ca-rotation.sh step5-prune-old-ca
  ./hack/scripts/self-signed-ca-rotation.sh step6-cleanup
EOF
}

log() {
	local level="$1"
	shift
	printf '[%s] [%s] %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" "$level" "$*"
}

log_info() { log "INFO" "$@"; }
log_wait() { log "WAIT" "$@"; }
log_pass() { log "PASS" "$@"; }
log_step_result() {
	if [[ "$RUN_ALL_MODE" != true ]]; then
		log_pass "$@"
	fi
}
log_pending() {
	if [[ "$CHECK_OUTPUT" == true ]]; then
		log_wait "$@"
	fi
}
die() {
	log "ERROR" "$*" >&2
	exit 1
}

k() {
	"$KUBECTL" -n "$NS" "$@"
}

need_cmd() {
	command -v "$1" > /dev/null 2>&1 || die "Missing required command: $1"
}

load_deployment_mode() {
	if [[ -n "$DEPLOYMENT_MODE" ]]; then
		return
	fi

	DEPLOYMENT_MODE="$(k get dpfoperatorconfig "$DPF_CONFIG" -o jsonpath='{.spec.deploymentMode}')"
	case "$DEPLOYMENT_MODE" in
	zero-trust | host-trusted) ;;
	"") die "DPFOperatorConfig $DPF_CONFIG has no spec.deploymentMode" ;;
	*) die "unsupported deployment mode: $DEPLOYMENT_MODE" ;;
	esac
}

is_zero_trust() {
	load_deployment_mode
	[[ "$DEPLOYMENT_MODE" == "zero-trust" ]]
}

secret_fingerprint() {
	local secret="$1"
	local tmpfile
	tmpfile="$(mktemp)"
	k get secret "$secret" -o jsonpath='{.data.tls\.crt}' | base64 -d > "$tmpfile"
	openssl x509 -in "$tmpfile" -noout -fingerprint -sha256 | cut -d= -f2
	rm -f "$tmpfile"
}

bundle_has_fingerprint() {
	local wanted_fp="$1"
	local tmpdir bundle_file
	tmpdir="$(mktemp -d)"
	bundle_file="$tmpdir/bundle.pem"
	k get configmap "$TRUST_BUNDLE_CM" -o jsonpath='{.data.ca\.crt}' > "$bundle_file"

	awk -v d="$tmpdir" '
    /-----BEGIN CERTIFICATE-----/ { in_cert=1; n++; file=sprintf("%s/cert-%03d.pem", d, n) }
    in_cert { print > file }
    /-----END CERTIFICATE-----/ { in_cert=0; close(file) }
  ' "$bundle_file"

	local found=1
	for cert in "$tmpdir"/cert-*.pem; do
		[[ -s "$cert" ]] || continue
		local fp
		fp="$(openssl x509 -in "$cert" -noout -fingerprint -sha256 | cut -d= -f2)"
		if [[ "$fp" == "$wanted_fp" ]]; then
			found=0
			break
		fi
	done
	rm -rf "$tmpdir"
	return "$found"
}

parse_args() {
	local positional=()
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--namespace)
			NS="$2"
			shift 2
			;;
		--ca-secret)
			CA_SECRET="$2"
			shift 2
			;;
		--old-ca-secret)
			OLD_CA_SECRET="$2"
			shift 2
			;;
		--trust-bundle-cm)
			TRUST_BUNDLE_CM="$2"
			shift 2
			;;
		--issuer)
			ISSUER_NAME="$2"
			shift 2
			;;
		--ca-cert)
			CA_CERT_NAME="$2"
			shift 2
			;;
		--token)
			ROTATION_TOKEN="$2"
			shift 2
			;;
		--timeout-sec)
			TIMEOUT_SEC="$2"
			shift 2
			;;
		--wait-interval-sec)
			WAIT_INTERVAL_SEC="$2"
			shift 2
			;;
		--from-step)
			FROM_STEP="$2"
			shift 2
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			positional+=("$1")
			shift
			;;
		esac
	done

	if [[ ${#positional[@]} -lt 1 ]]; then
		usage
		exit 1
	fi
	COMMAND="${positional[0]}"

	[[ "$TIMEOUT_SEC" =~ ^[0-9]+$ ]] || die "--timeout-sec must be an integer"
	[[ "$WAIT_INTERVAL_SEC" =~ ^[0-9]+$ ]] || die "--wait-interval-sec must be an integer"
	[[ "$FROM_STEP" =~ ^[0-9]+$ ]] || die "--from-step must be an integer"
}

precheck() {
	need_cmd "$KUBECTL"
	need_cmd "$CMCTL"
	need_cmd openssl

	"$KUBECTL" get ns "$NS" > /dev/null
	k get secret "$CA_SECRET" > /dev/null
	k get configmap "$TRUST_BUNDLE_CM" > /dev/null
	k get certificate "$CA_CERT_NAME" > /dev/null
	k get issuer "$ISSUER_NAME" > /dev/null
	k get dpfoperatorconfig "$DPF_CONFIG" > /dev/null
	load_deployment_mode
	log_info "Deployment mode: $DEPLOYMENT_MODE"
	log_pass "Precheck completed"
}

step1_backup() {
	log_info "Step 1: creating backup Secret/$OLD_CA_SECRET from Secret/$CA_SECRET"
	k get secret "$CA_SECRET" -o yaml \
		| sed "s/name: ${CA_SECRET}/name: ${OLD_CA_SECRET}/" \
		| k apply -f -

	log_info "Step 1: configuring the provisioning issuer to use the old CA"
	k patch dpfoperatorconfig "$DPF_CONFIG" --type=merge -p "{
    \"spec\": {
      \"overrides\": {
        \"provisioningIssuerCASecretName\": \"${OLD_CA_SECRET}\"
      }
    }
  }"

	if ! wait_until_done is_done_step1_backup "Step 1: waiting for the old CA backup and issuer update"; then
		die "Step 1: timed out waiting for the old CA backup and issuer update"
	fi
	log_step_result "Step 1: old CA backed up and provisioning issuer updated"
}

step2_renew_ca() {
	log_info "Step 2: renewing Certificate/$CA_CERT_NAME"
	"$CMCTL" renew "$CA_CERT_NAME" -n "$NS"

	if is_zero_trust; then
		log_info "Step 2: waiting for DPU BMC and DPU OS trust convergence"
	else
		log_info "Step 2: skipping DPU BMC trust checks in host-trusted mode"
		log_info "Step 2: waiting for DPU OS trust convergence"
	fi
	if ! wait_until_done is_done_step2_renew_ca "Step 2: waiting for CA renewal and trust convergence"; then
		die "Step 2: timed out waiting for CA renewal and trust convergence"
	fi
	log_step_result "Step 2: new CA propagated and trust converged"
}

issuer_secret_name() {
	k get issuer "$ISSUER_NAME" -o jsonpath='{.spec.ca.secretName}'
}

override_secret_name() {
	k get dpfoperatorconfig "$DPF_CONFIG" -o jsonpath='{.spec.overrides.provisioningIssuerCASecretName}'
}

current_bundle_hash() {
	k get configmap "$TRUST_BUNDLE_CM" -o jsonpath='{.data.bundle-hash}'
}

all_dpudevices_trust_ready_for_hash() {
	local target_hash="$1"
	local failed=0 pending_count=0 pending_devices=""
	local dpudevices
	dpudevices="$(k get dpudevice -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')"
	if [[ -n "$dpudevices" ]]; then
		while IFS= read -r dev; do
			[[ -n "$dev" ]] || continue
			local obs cond
			obs="$(k get dpudevice "$dev" -o jsonpath='{.status.caTrustBundle.observedBundleHash}')"
			cond="$(k get dpudevice "$dev" -o jsonpath='{range .status.conditions[?(@.type=="CATrustBundleReady")]}{.status}{end}')"
			if [[ "$obs" != "$target_hash" || "$cond" != "True" ]]; then
				pending_count=$((pending_count + 1))
				pending_devices+="${pending_devices:+, }$dev"
				failed=1
			fi
		done <<< "$dpudevices"
	fi
	if [[ "$failed" -ne 0 ]]; then
		log_pending "DPU BMC trust: $pending_count DPUDevice(s) pending ($pending_devices)"
	fi
	[[ "$failed" -eq 0 ]]
}

all_dpus_trust_ready_for_hash() {
	local target_hash="$1"
	local failed=0 pending_count=0 pending_dpus=""
	local dpus
	dpus="$(k get dpu -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')"
	if [[ -n "$dpus" ]]; then
		while IFS= read -r dpu; do
			[[ -n "$dpu" ]] || continue
			local hash
			hash="$(k get dpu "$dpu" -o jsonpath='{.status.agentStatus.trustBundleHash}')"
			if [[ "$hash" != "$target_hash" ]]; then
				pending_count=$((pending_count + 1))
				pending_dpus+="${pending_dpus:+, }$dpu"
				failed=1
			fi
		done <<< "$dpus"
	fi
	if [[ "$failed" -ne 0 ]]; then
		log_pending "DPU OS trust: $pending_count DPU(s) pending ($pending_dpus)"
	fi
	[[ "$failed" -eq 0 ]]
}

leaf_certificate_names() {
	k get certificate -o jsonpath="{range .items[?(@.spec.issuerRef.name==\"$ISSUER_NAME\")]}{.metadata.name}{\"\n\"}{end}"
}

all_leaf_certificates_signed_by_new_ca() {
	local certificates
	certificates="$(leaf_certificate_names)"
	if [[ -z "$certificates" ]]; then
		log_pending "No leaf certificates found for Issuer/$ISSUER_NAME"
		return 1
	fi

	local tmpdir ca_file failed total pending_count pending_certificates
	tmpdir="$(mktemp -d)"
	ca_file="$tmpdir/ca.pem"
	failed=0
	total=0
	pending_count=0
	pending_certificates=""
	if ! k get secret "$CA_SECRET" -o jsonpath='{.data.tls\.crt}' | base64 -d > "$ca_file"; then
		rm -rf "$tmpdir"
		return 1
	fi

	while IFS= read -r cert; do
		[[ -n "$cert" ]] || continue
		total=$((total + 1))
		local ready secret_name leaf_file
		ready="$(k get certificate "$cert" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}')"
		secret_name="$(k get certificate "$cert" -o jsonpath='{.spec.secretName}')"
		if [[ "$ready" != "True" || -z "$secret_name" ]]; then
			pending_count=$((pending_count + 1))
			pending_certificates+="${pending_certificates:+, }$cert"
			failed=1
			continue
		fi

		leaf_file="$tmpdir/${cert}.pem"
		if ! k get secret "$secret_name" -o jsonpath='{.data.tls\.crt}' | base64 -d > "$leaf_file"; then
			pending_count=$((pending_count + 1))
			pending_certificates+="${pending_certificates:+, }$cert"
			failed=1
			continue
		fi
		if ! openssl verify -CAfile "$ca_file" "$leaf_file" > /dev/null 2>&1; then
			pending_count=$((pending_count + 1))
			pending_certificates+="${pending_certificates:+, }$cert"
			failed=1
		fi
	done <<< "$certificates"

	rm -rf "$tmpdir"
	if [[ "$failed" -ne 0 ]]; then
		log_pending "Leaf certificates: $pending_count of $total pending ($pending_certificates)"
	fi
	[[ "$failed" -eq 0 ]]
}

renew_leaf_certificates() {
	local certificates
	certificates="$(leaf_certificate_names)"
	[[ -n "$certificates" ]] || die "Step 4: no leaf certificates found for Issuer/$ISSUER_NAME"

	while IFS= read -r cert; do
		[[ -n "$cert" ]] || continue
		log_info "Step 4: renewing Certificate/$cert"
		"$CMCTL" renew "$cert" -n "$NS"
	done <<< "$certificates"
}

step3_switch_ca() {
	log_info "Step 3: switching the provisioning issuer to the new CA"
	k patch dpfoperatorconfig "$DPF_CONFIG" --type=merge -p '{
    "spec": {
      "overrides": {
        "provisioningIssuerCASecretName": null
      }
    }
  }'

	if ! wait_until_done is_done_step3_switch_ca "Step 3: waiting for the provisioning issuer to use the new CA"; then
		die "Step 3: timed out waiting for the provisioning issuer to use the new CA"
	fi
	log_step_result "Step 3: provisioning issuer switched to the new CA"
}

step4_rotate_cert() {
	if CHECK_OUTPUT=false all_leaf_certificates_signed_by_new_ca; then
		log_info "Step 4: all provisioning leaf certificates already use the new CA"
	else
		renew_leaf_certificates
		log_info "Step 4: waiting 3 seconds before verifying renewed leaf certificates"
		sleep 3
		if ! wait_until_done all_leaf_certificates_signed_by_new_ca "Step 4: waiting for leaf certificates to use the new CA"; then
			die "Step 4: timed out waiting for leaf certificates to use the new CA"
		fi
		log_pass "Step 4: all provisioning leaf certificates use the new CA"
	fi

	if is_zero_trust; then
		local token="$ROTATION_TOKEN"
		if [[ -z "$token" ]]; then
			token="$(date +%s)"
			ROTATION_TOKEN="$token"
		fi
		log_info "Step 4: triggering DPU BMC server certificate rotation (token=$token)"
		k annotate dpudevice --all provisioning.dpu.nvidia.com/rotate-bmc-server-certificate="$token" --overwrite

		if ! wait_until_done is_done_step4_rotate_cert "Step 4: waiting for DPU BMC server certificate rotation"; then
			die "Step 4: timed out waiting for DPU BMC server certificate rotation (token=$token)"
		fi
		log_pass "Step 4: DPU BMC server certificates rotated successfully"
	else
		log_info "Step 4: skipping DPU BMC server certificate rotation in host-trusted mode"
	fi
	log_step_result "Step 4: certificate rotation completed"
}

step5_prune_old_ca() {
	k get secret "$OLD_CA_SECRET" > /dev/null 2>&1 || {
		log_info "Step 5: Secret/$OLD_CA_SECRET does not exist; skipping old CA pruning"
		return 0
	}

	log_info "Step 5: pruning the old CA from ConfigMap/$TRUST_BUNDLE_CM"
	local old_fp tmpdir bundle_file out_file removed kept cert fp escaped patch_payload
	old_fp="$(secret_fingerprint "$OLD_CA_SECRET")"
	PREVIOUS_BUNDLE_HASH="$(current_bundle_hash)"
	tmpdir="$(mktemp -d)"
	bundle_file="$tmpdir/bundle.pem"
	out_file="$tmpdir/pruned.pem"
	removed=0
	kept=0

	k get configmap "$TRUST_BUNDLE_CM" -o jsonpath='{.data.ca\.crt}' > "$bundle_file"
	awk -v d="$tmpdir" '
    /-----BEGIN CERTIFICATE-----/ { in_cert=1; n++; file=sprintf("%s/cert-%03d.pem", d, n) }
    in_cert { print > file }
    /-----END CERTIFICATE-----/ { in_cert=0; close(file) }
  ' "$bundle_file"

	: > "$out_file"
	for cert in "$tmpdir"/cert-*.pem; do
		[[ -s "$cert" ]] || continue
		fp="$(openssl x509 -in "$cert" -noout -fingerprint -sha256 | cut -d= -f2)"
		if [[ "$fp" == "$old_fp" ]]; then
			removed=$((removed + 1))
			continue
		fi
		cat "$cert" >> "$out_file"
		printf '\n' >> "$out_file"
		kept=$((kept + 1))
	done

	if [[ "$removed" -eq 0 ]]; then
		rm -rf "$tmpdir"
		die "Step 5: old CA not found in ConfigMap/$TRUST_BUNDLE_CM; nothing was pruned"
	fi
	if [[ "$kept" -eq 0 ]]; then
		rm -rf "$tmpdir"
		die "Step 5: refusing to prune because the resulting trust bundle would be empty"
	fi

	escaped="$(awk 'BEGIN{printf "\""} {gsub(/\\/,"\\\\"); gsub(/"/,"\\\""); printf "%s\\n",$0} END{printf "\""}' "$out_file")"
	patch_payload="{\"data\":{\"ca.crt\":${escaped}}}"
	k patch configmap "$TRUST_BUNDLE_CM" --type=merge -p "$patch_payload" > /dev/null
	rm -rf "$tmpdir"

	log_info "Step 5: pruned the old CA from ConfigMap/$TRUST_BUNDLE_CM (removed certificates: $removed)"
	if ! wait_until_done is_done_step5_prune_old_ca "Step 5: waiting for the pruned trust bundle to converge"; then
		die "Step 5: timed out waiting for the pruned trust bundle to converge"
	fi
	log_step_result "Step 5: old CA pruned and trust converged"
}

old_ca_still_in_bundle() {
	if ! k get secret "$OLD_CA_SECRET" > /dev/null 2>&1; then
		return 1
	fi
	local old_fp
	old_fp="$(secret_fingerprint "$OLD_CA_SECRET")"
	bundle_has_fingerprint "$old_fp"
}

step6_cleanup() {
	if old_ca_still_in_bundle; then
		die "Step 6: old CA is still present in ConfigMap/$TRUST_BUNDLE_CM; run step5-prune-old-ca first"
	fi
	log_info "Step 6: deleting Secret/$OLD_CA_SECRET"
	k delete secret "$OLD_CA_SECRET" --ignore-not-found

	if ! wait_until_done is_done_step6_cleanup "Step 6: waiting for the old CA Secret to be deleted"; then
		die "Step 6: timed out waiting for the old CA Secret to be deleted"
	fi
	log_step_result "Step 6: old CA Secret deleted"
}

is_done_step1_backup() {
	k get secret "$OLD_CA_SECRET" > /dev/null 2>&1 || return 1
	[[ "$(override_secret_name)" == "$OLD_CA_SECRET" ]] || return 1
	[[ "$(issuer_secret_name)" == "$OLD_CA_SECRET" ]]
}

is_done_step2_renew_ca() {
	local cert_ready issuer_secret old_fp new_fp
	cert_ready="$(k get certificate "$CA_CERT_NAME" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}')"
	[[ "$cert_ready" == "True" ]] || return 1
	k get secret "$OLD_CA_SECRET" > /dev/null 2>&1 || return 1
	old_fp="$(secret_fingerprint "$OLD_CA_SECRET")"
	new_fp="$(secret_fingerprint "$CA_SECRET")"
	[[ "$old_fp" != "$new_fp" ]] || return 1
	bundle_has_fingerprint "$new_fp" || return 1
	issuer_secret="$(issuer_secret_name)"
	[[ "$issuer_secret" == "$OLD_CA_SECRET" ]] || return 1
	is_trust_converged
}

is_trust_converged() {
	local target
	target="$(current_bundle_hash)"
	[[ -n "$target" ]] || return 1
	all_dpus_trust_ready_for_hash "$target" || return 1
	if is_zero_trust; then
		all_dpudevices_trust_ready_for_hash "$target" || return 1
	fi
	return 0
}

is_done_step3_switch_ca() {
	local override issuer_secret
	override="$(override_secret_name)"
	issuer_secret="$(issuer_secret_name)"
	[[ -z "$override" && "$issuer_secret" == "$CA_SECRET" ]]
}

all_dpudevices_bmc_rotation_done() {
	local token="$1"
	local failed=0 pending_count=0 pending_devices=""
	local dpudevices
	dpudevices="$(k get dpudevice -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')"
	if [[ -n "$dpudevices" ]]; then
		while IFS= read -r dev; do
			[[ -n "$dev" ]] || continue
			local ready observed
			ready="$(k get dpudevice "$dev" -o jsonpath='{range .status.conditions[?(@.type=="BMCServerCertificateReady")]}{.status}{end}')"
			observed="$(k get dpudevice "$dev" -o jsonpath='{.status.bmcServerCertificate.observedManualTrigger}')"
			if [[ "$ready" != "True" || "$observed" != "$token" ]]; then
				pending_count=$((pending_count + 1))
				pending_devices+="${pending_devices:+, }$dev"
				failed=1
			fi
		done <<< "$dpudevices"
	fi
	if [[ "$failed" -ne 0 ]]; then
		log_pending "DPU BMC certificates: $pending_count DPUDevice(s) pending ($pending_devices)"
	fi
	[[ "$failed" -eq 0 ]]
}

is_done_step4_rotate_cert() {
	all_leaf_certificates_signed_by_new_ca || return 1
	if is_zero_trust; then
		[[ -n "$ROTATION_TOKEN" ]] || return 1
		all_dpudevices_bmc_rotation_done "$ROTATION_TOKEN" || return 1
	fi
	return 0
}

is_done_step5_prune_old_ca() {
	! old_ca_still_in_bundle || return 1
	if [[ -n "$PREVIOUS_BUNDLE_HASH" && "$(current_bundle_hash)" == "$PREVIOUS_BUNDLE_HASH" ]]; then
		return 1
	fi
	is_trust_converged
}

is_done_step6_cleanup() {
	! k get secret "$OLD_CA_SECRET" > /dev/null 2>&1
}

wait_until_done() {
	local done_fn="$1"
	local description="$2"
	local quiet="${3:-false}"
	local start now
	start="$(date +%s)"

	if CHECK_OUTPUT=false "$done_fn"; then
		return 0
	fi

	if [[ "$quiet" != true ]]; then
		log_wait "$description"
	fi

	while true; do
		sleep "$WAIT_INTERVAL_SEC"

		if [[ "$quiet" == true ]]; then
			if CHECK_OUTPUT=false "$done_fn"; then
				return 0
			fi
		else
			if "$done_fn"; then
				return 0
			fi
		fi
		now="$(date +%s)"
		if ((now - start >= TIMEOUT_SEC)); then
			return 1
		fi
	done
}

run_all_step() {
	local step_no="$1"
	local name="$2"
	local done_fn="$3"
	local run_fn="$4"

	if ((step_no < FROM_STEP)); then
		log_info "Step $step_no skipped by --from-step: $name"
		return 0
	fi

	if CHECK_OUTPUT=false "$done_fn"; then
		log_pass "Step $step_no already complete: $name"
		return 0
	fi

	log_info "Step $step_no started: $name"
	"$run_fn"

	if wait_until_done "$done_fn" "$name" true; then
		log_pass "Step $step_no completed: $name"
		return 0
	fi

	die "Step $step_no: verification timed out after ${TIMEOUT_SEC}s ($name)"
}

run_all() {
	RUN_ALL_MODE=true
	precheck

	if is_zero_trust; then
		if [[ -z "$ROTATION_TOKEN" ]]; then
			local h
			h="$(current_bundle_hash)"
			ROTATION_TOKEN="ca-rotation-${h}"
		fi
	else
		log_info "DPU BMC certificate rotation is not required in host-trusted mode"
	fi
	log_info "Starting self-signed CA rotation (Steps 1-6)"

	run_all_step 1 "Backup old CA and pin issuer to old secret" is_done_step1_backup step1_backup
	run_all_step 2 "Renew provisioning CA and verify dual-trust bundle includes new CA" is_done_step2_renew_ca step2_renew_ca
	run_all_step 3 "Switch issuer to default CA" is_done_step3_switch_ca step3_switch_ca
	run_all_step 4 "Renew leaf certificates and conditionally rotate BMC certificates" is_done_step4_rotate_cert step4_rotate_cert
	run_all_step 5 "Prune old CA from trust bundle" is_done_step5_prune_old_ca step5_prune_old_ca
	run_all_step 6 "Cleanup old CA secret" is_done_step6_cleanup step6_cleanup

	log_pass "Self-signed CA rotation completed"
}

main() {
	parse_args "$@"
	case "$COMMAND" in
	precheck) precheck ;;
	run-all) run_all ;;
	step1-backup) step1_backup ;;
	step2-renew-ca) step2_renew_ca ;;
	step3-switch-ca) step3_switch_ca ;;
	step4-rotate-cert) step4_rotate_cert ;;
	step5-prune-old-ca) step5_prune_old_ca ;;
	step6-cleanup) step6_cleanup ;;
	*)
		usage
		die "unknown command: $COMMAND"
		;;
	esac
}

main "$@"

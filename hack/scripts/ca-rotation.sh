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
TRUST_BUNDLE_CM="dpf-ca-trust-bundle"
ROOT_CA_CERT="dpf-global-root-ca"
ROOT_CA_SECRET="dpf-global-root-ca"
PLATFORM_CA_CERT="dpf-platform-intermediate-ca"
PLATFORM_CA_SECRET="dpf-platform-intermediate-ca"
PLATFORM_ISSUER="dpf-platform-issuer"
WEBHOOK_CA_CERT="dpf-webhook-intermediate-ca"
BACKUP_SECRET="dpf-ca-rotation-backup"
SELF_SIGNED_ANCHOR="dpf-global-root-issuer"

DEPLOYMENT_MODE=""
ANCHOR_NAME=""
ROTATION_TOKEN=""
PREVIOUS_BUNDLE_HASH=""
PRE_RENEW_PLATFORM_FP=""
TIMEOUT_SEC=600
WAIT_INTERVAL_SEC=5
FROM_STEP=1
COMMAND=""
CHECK_OUTPUT=true
RUN_ALL_MODE=false

usage() {
	cat << 'EOF'
Usage:
  ca-rotation.sh [global flags] <command> [command flags]

Rotates the certificate authority that anchors the DPF PKI, keeping bfb-registry,
provisioning-controller, DPU BMC mTLS, hostagent and DPU OS trust healthy throughout.

The platform intermediate CA is declared with privateKey.rotationPolicy: Never, so it keeps its
key and subject DN when it is re-signed by the new authority. Certificates it has already signed
stay valid across the rotation, which is why no issuer has to be pinned to an old CA while the new
one propagates.

Global flags:
  --namespace <ns>                Default: dpf-operator-system
  --trust-bundle-cm <name>        Default: dpf-ca-trust-bundle
  --root-ca <name>                Default: dpf-global-root-ca
  --platform-ca <name>            Default: dpf-platform-intermediate-ca
  --platform-issuer <name>        Default: dpf-platform-issuer
  --backup-secret <name>          Default: dpf-ca-rotation-backup
  --timeout-sec <seconds>         Timeout for each completion check (default: 600)
  --wait-interval-sec <seconds>   Polling interval while waiting (default: 5)
  --from-step <1-6>               Start run-all from this step (default: 1)
  --token <value>                 BMC rotation token for run-all or step4-rotate-leaves;
                                  generated automatically if omitted
  -h, --help                      Show this help message

Commands:
  precheck                        Validate tools and required resources
  run-all                         Execute the applicable steps in order and skip completed ones
  step1-backup                    Back up the certificates of the authority being retired
  step2-rekey-root                Re-key the self-signed root so a new authority exists
  step3-renew-intermediates       Re-sign the intermediates under the new authority and wait for
                                  the trust bundle to reach every DPU and DPUDevice
  step4-rotate-leaves             Renew the platform leaf certificates; rotate the DPU BMC server
                                  certificates in Zero Trust
  step5-prune-old-ca              Close the dual-trust window by pruning the retired authority
  step6-cleanup                   Delete the backup Secret

Certificate authority modes:
  The applicable steps depend on what anchors the PKI, which is read from
  dpfoperatorconfig.status.security.certManagement.anchor.

  Self-signed    DPF owns the root and the trust bundle, so every step applies.
  External       An issuer you own anchors the PKI and the trust bundle is yours to curate.
                 Re-keying that authority and editing the bundle happen outside DPF, so steps 1,
                 2, 5 and 6 are skipped. Run this after your own root has been rotated and its new
                 certificate added to the bundle alongside the old one.

run-all notes:
  - Before each step, a helper evaluates whether the step is already done and skips it if so.
  - If run-all fails, fix the reported step and rerun run-all (or use --from-step).

Examples:
  ./hack/scripts/ca-rotation.sh precheck
  ./hack/scripts/ca-rotation.sh run-all
  ./hack/scripts/ca-rotation.sh run-all --from-step 4
  ./hack/scripts/ca-rotation.sh --timeout-sec 1200 --wait-interval-sec 10 run-all
  ./hack/scripts/ca-rotation.sh step1-backup
  ./hack/scripts/ca-rotation.sh step2-rekey-root
  ./hack/scripts/ca-rotation.sh step3-renew-intermediates
  ./hack/scripts/ca-rotation.sh step4-rotate-leaves --token "$(date +%s)"
  ./hack/scripts/ca-rotation.sh step5-prune-old-ca
  ./hack/scripts/ca-rotation.sh step6-cleanup
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

# load_anchor reads the authority the PKI is anchored to from the status DPF reports, which is the
# same value the operator makes its own decisions on. Reading it from the chart values instead
# would describe what was asked for rather than what is in effect.
load_anchor() {
	if [[ -n "$ANCHOR_NAME" ]]; then
		return
	fi

	ANCHOR_NAME="$(k get dpfoperatorconfig "$DPF_CONFIG" \
		-o jsonpath='{.status.security.certManagement.anchor.name}')"
	[[ -n "$ANCHOR_NAME" ]] || die "DPFOperatorConfig $DPF_CONFIG reports no certificate authority yet; \
wait for CertManagementReady before rotating"
}

# is_self_signed reports whether DPF owns the authority, which is what decides who re-keys the root
# and who curates the trust bundle. It mirrors the classification the operator itself applies.
is_self_signed() {
	load_anchor
	[[ "$ANCHOR_NAME" == "$SELF_SIGNED_ANCHOR" ]]
}

# cert_fingerprint prints the SHA-256 fingerprint of a PEM certificate held in a file.
cert_fingerprint() {
	openssl x509 -in "$1" -noout -fingerprint -sha256 | cut -d= -f2
}

# secret_cert_fingerprint prints the fingerprint of the certificate under the given key of a Secret.
secret_cert_fingerprint() {
	local secret="$1" key="${2:-tls\.crt}"
	local tmpfile fingerprint
	tmpfile="$(mktemp)"
	k get secret "$secret" -o jsonpath="{.data.${key}}" | base64 -d > "$tmpfile"
	fingerprint="$(cert_fingerprint "$tmpfile")"
	rm -f "$tmpfile"
	printf '%s' "$fingerprint"
}

# split_bundle writes each certificate of the trust bundle into its own file under the given
# directory and prints nothing. The bundle is a concatenation of PEM blocks, which openssl will
# only read one at a time.
split_bundle() {
	local dir="$1"
	k get configmap "$TRUST_BUNDLE_CM" -o jsonpath='{.data.ca\.crt}' > "$dir/bundle.pem"
	awk -v d="$dir" '
    /-----BEGIN CERTIFICATE-----/ { in_cert=1; n++; file=sprintf("%s/cert-%03d.pem", d, n) }
    in_cert { print > file }
    /-----END CERTIFICATE-----/ { in_cert=0; close(file) }
  ' "$dir/bundle.pem"
}

bundle_has_fingerprint() {
	local wanted_fp="$1"
	local tmpdir found=1 cert fp
	tmpdir="$(mktemp -d)"
	split_bundle "$tmpdir"

	for cert in "$tmpdir"/cert-*.pem; do
		[[ -s "$cert" ]] || continue
		fp="$(cert_fingerprint "$cert")"
		if [[ "$fp" == "$wanted_fp" ]]; then
			found=0
			break
		fi
	done
	rm -rf "$tmpdir"
	return "$found"
}

current_bundle_hash() {
	k get configmap "$TRUST_BUNDLE_CM" -o jsonpath='{.data.bundle-hash}'
}

backup_exists() {
	k get secret "$BACKUP_SECRET" > /dev/null 2>&1
}

# backed_up_fingerprint prints the fingerprint of a certificate recorded in the backup Secret. The
# dots of the key are escaped for jsonpath, which would otherwise read them as path separators.
backed_up_fingerprint() {
	secret_cert_fingerprint "$BACKUP_SECRET" "${1//./\\.}"
}

# backed_up_certificate writes a certificate recorded in the backup Secret to the given path.
backed_up_certificate() {
	local key="${1//./\\.}" out_file="$2"
	k get secret "$BACKUP_SECRET" -o jsonpath="{.data.${key}}" | base64 -d > "$out_file"
	[[ -s "$out_file" ]]
}

# signed_by reports whether the certificate was issued by the authority in ca_file.
#
# Asked of the signature rather than of the issuer name, because the two cannot be told apart by
# name here: step 2 re-keys the root by deleting its Secret and letting cert-manager reissue from
# the same Certificate, so the retired and the current root carry the same subject DN and differ
# only by key. Matching on the name would select the live chain along with the retired one.
#
# Time is left out of it on purpose. A certificate the retired authority signed is to be taken away
# whether or not it has already expired, and an expired one is the likelier case.
signed_by() {
	local cert="$1" ca_file="$2"
	openssl verify -no_check_time -partial_chain -CAfile "$ca_file" "$cert" > /dev/null 2>&1
}

# backed_up_bundle_hash prints the bundle hash step 5 recorded before it pruned, and nothing until it
# has. It is read from the backup rather than from the variable the step sets, so that a run resuming
# after the prune measures convergence against the same hash the run that pruned did.
backed_up_bundle_hash() {
	k get secret "$BACKUP_SECRET" -o jsonpath='{.data.pre-prune-bundle-hash}' 2> /dev/null | base64 -d
}

parse_args() {
	local positional=()
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--namespace)
			NS="$2"
			shift 2
			;;
		--trust-bundle-cm)
			TRUST_BUNDLE_CM="$2"
			shift 2
			;;
		--root-ca)
			ROOT_CA_CERT="$2"
			ROOT_CA_SECRET="$2"
			shift 2
			;;
		--platform-ca)
			PLATFORM_CA_CERT="$2"
			PLATFORM_CA_SECRET="$2"
			shift 2
			;;
		--platform-issuer)
			PLATFORM_ISSUER="$2"
			shift 2
			;;
		--backup-secret)
			BACKUP_SECRET="$2"
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
	k get dpfoperatorconfig "$DPF_CONFIG" > /dev/null
	k get configmap "$TRUST_BUNDLE_CM" > /dev/null
	k get secret "$PLATFORM_CA_SECRET" > /dev/null
	k get certificate "$PLATFORM_CA_CERT" > /dev/null
	k get issuer "$PLATFORM_ISSUER" > /dev/null

	load_deployment_mode
	load_anchor
	if is_self_signed; then
		k get certificate "$ROOT_CA_CERT" > /dev/null
		k get secret "$ROOT_CA_SECRET" > /dev/null
		log_info "Certificate authority: self-signed, anchored at Issuer/$ANCHOR_NAME"
	else
		log_info "Certificate authority: external, anchored at $ANCHOR_NAME"
		log_info "Re-keying that authority and curating ConfigMap/$TRUST_BUNDLE_CM are outside DPF"
	fi
	log_info "Deployment mode: $DEPLOYMENT_MODE"
	log_pass "Precheck completed"
}

# step1_backup records the certificates of the authority being retired, which is what step 5 needs
# in order to prune exactly those and nothing else. The bundle merge is deliberately non-pruning so
# that certificates an operator adds survive a reconcile, so pruning by anything other than an
# recorded fingerprint would take their material away with it.
#
# The private key of the root is kept alongside, so that a rotation abandoned halfway can be undone
# by restoring the Secret. Step 6 is what finally discards it.
step1_backup() {
	# Invoked on its own this is reachable at any point of a rotation, where re-recording would
	# capture the authority now in effect as the one being retired and lose the one step 5 prunes by.
	# A record of the live root is this rotation before it has started, so writing over it costs
	# nothing; anything else belongs to a rotation already past step 2. run-all does not reach this,
	# since the completion check reports done for as long as the recorded authority is in the bundle.
	if backup_exists && [[ "$(backed_up_fingerprint root-tls.crt)" != "$(secret_cert_fingerprint "$ROOT_CA_SECRET")" ]]; then
		die "Step 1: Secret/$BACKUP_SECRET records a root that is no longer live, so the rotation it \
belongs to is past step 2. Recording again would name the new root as the one being retired, which \
step 5 prunes by and a rollback restores from. Carry on from the step that rotation reached, or \
delete the Secret to start over."
	fi

	log_info "Step 1: recording the certificates being retired in Secret/$BACKUP_SECRET"

	local tmpdir
	tmpdir="$(mktemp -d)"
	k get secret "$ROOT_CA_SECRET" -o jsonpath='{.data.tls\.crt}' | base64 -d > "$tmpdir/root-tls.crt"
	k get secret "$ROOT_CA_SECRET" -o jsonpath='{.data.tls\.key}' | base64 -d > "$tmpdir/root-tls.key"
	k get secret "$PLATFORM_CA_SECRET" -o jsonpath='{.data.tls\.crt}' | base64 -d > "$tmpdir/intermediate-tls.crt"

	k create secret generic "$BACKUP_SECRET" \
		--from-file=root-tls.crt="$tmpdir/root-tls.crt" \
		--from-file=root-tls.key="$tmpdir/root-tls.key" \
		--from-file=intermediate-tls.crt="$tmpdir/intermediate-tls.crt" \
		--dry-run=client -o yaml | k apply -f -
	rm -rf "$tmpdir"

	if ! wait_until_done is_done_step1_backup "Step 1: waiting for the backup Secret"; then
		die "Step 1: timed out waiting for the backup Secret"
	fi
	log_step_result "Step 1: retired certificates recorded"
}

# step2_rekey_root replaces the key of the self-signed root.
#
# The root carries no rotationPolicy, so cert-manager defaults to Never and keeps whatever key it
# finds in the Secret: cmctl renew would re-sign the same key and leave the authority unchanged.
# Deleting the Secret is what leaves cert-manager nothing to reuse, so it generates a new keypair
# and self-signs it through the bootstrapper.
#
# Nothing below the root moves yet. The intermediates keep the certificates the previous root
# signed until step 3 renews them, so the chain in effect is undisturbed for the whole of this step.
step2_rekey_root() {
	local old_fp
	# The delete below discards the only copy of the current keypair that is not in the backup, so
	# without one there is nothing left to prune by, to verify the re-key against, or to restore.
	backup_exists || die "Step 2: Secret/$BACKUP_SECRET does not exist. Run step 1 first: deleting \
Secret/$ROOT_CA_SECRET leaves no copy of the root being retired."
	old_fp="$(backed_up_fingerprint root-tls.crt)"

	log_info "Step 2: deleting Secret/$ROOT_CA_SECRET so cert-manager issues a new root keypair"
	k delete secret "$ROOT_CA_SECRET"

	if ! wait_until_done is_done_step2_rekey_root "Step 2: waiting for the new root to be issued"; then
		die "Step 2: timed out waiting for the new root to be issued"
	fi
	log_info "Step 2: retired root $old_fp, new root $(secret_cert_fingerprint "$ROOT_CA_SECRET")"
	log_step_result "Step 2: root re-keyed"
}

# step3_renew_intermediates re-signs the intermediates under the authority now in effect and waits
# for every peer to pick up the resulting bundle.
#
# Both intermediates carry rotationPolicy: Never, so each keeps its key and subject DN and is
# merely re-signed. That is what makes this safe to do without pinning anything: the certificates
# they have already signed are still validated by the re-signed intermediate, and the certificates
# signed from here on are validated by peers that already trust it.
#
# The bundle gains the new authority without losing the old one, because the merge DPF applies is
# non-pruning. Peers therefore validate both sides for the whole window, which is what step 5
# closes once nothing is left on the old one.
step3_renew_intermediates() {
	# Recorded before the renewal is asked for, because under an authority of the operator it is the
	# only thing that tells the re-signed intermediate from the one that was there.
	PRE_RENEW_PLATFORM_FP="$(secret_cert_fingerprint "$PLATFORM_CA_SECRET")"

	log_info "Step 3: renewing Certificate/$PLATFORM_CA_CERT"
	"$CMCTL" renew "$PLATFORM_CA_CERT" -n "$NS"

	if is_self_signed && k get certificate "$WEBHOOK_CA_CERT" > /dev/null 2>&1; then
		log_info "Step 3: renewing Certificate/$WEBHOOK_CA_CERT"
		"$CMCTL" renew "$WEBHOOK_CA_CERT" -n "$NS"
	fi

	if is_zero_trust; then
		log_info "Step 3: waiting for DPU BMC and DPU OS trust convergence"
	else
		log_info "Step 3: skipping DPU BMC trust checks in host-trusted mode"
		log_info "Step 3: waiting for DPU OS trust convergence"
	fi
	if ! wait_until_done is_done_step3_renew_intermediates "Step 3: waiting for the re-signed chain to converge"; then
		die "Step 3: timed out waiting for the re-signed chain to converge"
	fi
	log_step_result "Step 3: intermediates re-signed and trust converged"
}

leaf_certificate_names() {
	k get certificate -o jsonpath="{range .items[?(@.spec.issuerRef.name==\"$PLATFORM_ISSUER\")]}{.metadata.name}{\"\n\"}{end}"
}

# all_leaf_certificates_carry_current_authority reports whether every certificate under the platform
# issuer has been reissued since the authority changed.
#
# What is asked of each one is the authority beside it in its Secret rather than the signature over
# it. The intermediate keeps its key and subject DN through a re-signing, so a leaf issued before it
# still verifies against the intermediate afterwards and a signature says nothing about which side of
# the rotation the leaf is from. cert-manager fills ca.crt with the authority above the intermediate,
# which is what a rotation replaces, so a leaf still naming the old one is a leaf that would hand a
# peer a chain ending at the authority step 5 is about to prune. That is what step 4 is for.
all_leaf_certificates_carry_current_authority() {
	local certificates anchor_fp
	certificates="$(leaf_certificate_names)"
	if [[ -z "$certificates" ]]; then
		log_pending "No leaf certificates found for Issuer/$PLATFORM_ISSUER"
		return 1
	fi

	# Read from the intermediate rather than from the root, so that this holds under an authority of
	# the operator as well, where the root is one DPF has no access to.
	anchor_fp="$(secret_cert_fingerprint "$PLATFORM_CA_SECRET" 'ca\.crt')"
	if [[ -z "$anchor_fp" ]]; then
		log_pending "Secret/$PLATFORM_CA_SECRET does not carry the authority above it yet"
		return 1
	fi

	local failed total pending_count pending_certificates
	failed=0
	total=0
	pending_count=0
	pending_certificates=""

	while IFS= read -r cert; do
		[[ -n "$cert" ]] || continue
		total=$((total + 1))
		local ready secret_name
		ready="$(k get certificate "$cert" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}')"
		secret_name="$(k get certificate "$cert" -o jsonpath='{.spec.secretName}')"
		if [[ "$ready" != "True" || -z "$secret_name" ]]; then
			pending_count=$((pending_count + 1))
			pending_certificates+="${pending_certificates:+, }$cert"
			failed=1
			continue
		fi

		if [[ "$(secret_cert_fingerprint "$secret_name" 'ca\.crt')" != "$anchor_fp" ]]; then
			pending_count=$((pending_count + 1))
			pending_certificates+="${pending_certificates:+, }$cert"
			failed=1
		fi
	done <<< "$certificates"

	if [[ "$failed" -ne 0 ]]; then
		log_pending "Leaf certificates: $pending_count of $total pending ($pending_certificates)"
	fi
	[[ "$failed" -eq 0 ]]
}

renew_leaf_certificates() {
	local certificates
	certificates="$(leaf_certificate_names)"
	[[ -n "$certificates" ]] || die "Step 4: no leaf certificates found for Issuer/$PLATFORM_ISSUER"

	while IFS= read -r cert; do
		[[ -n "$cert" ]] || continue
		log_info "Step 4: renewing Certificate/$cert"
		"$CMCTL" renew "$cert" -n "$NS"
	done <<< "$certificates"
}

# step4_rotate_leaves brings the certificates below the intermediate onto the re-signed chain.
#
# They are valid either way, since the intermediate kept the key that signed them, so this is about
# the copy of the chain each leaf Secret carries rather than about trust. Renewing them means no
# peer is handed a chain that ends at an authority step 5 is about to prune.
#
# The webhook serving certificates are left alone on purpose: their trust is distributed through
# the cert-manager.io/inject-ca-from annotation, so cert-manager refreshes the caBundle of the
# webhook configurations itself once the intermediate above them is re-signed.
step4_rotate_leaves() {
	if CHECK_OUTPUT=false all_leaf_certificates_carry_current_authority; then
		log_info "Step 4: all platform leaf certificates already carry the current authority"
	else
		renew_leaf_certificates
		log_info "Step 4: waiting 3 seconds before verifying renewed leaf certificates"
		sleep 3
		if ! wait_until_done all_leaf_certificates_carry_current_authority "Step 4: waiting for leaf certificates to carry the current authority"; then
			die "Step 4: timed out waiting for leaf certificates to carry the current authority"
		fi
		log_pass "Step 4: all platform leaf certificates carry the current authority"
	fi

	if is_zero_trust; then
		local token="$ROTATION_TOKEN"
		if [[ -z "$token" ]]; then
			token="$(date +%s)"
			ROTATION_TOKEN="$token"
		fi
		log_info "Step 4: triggering DPU BMC server certificate rotation (token=$token)"
		k annotate dpudevice --all provisioning.dpu.nvidia.com/rotate-bmc-server-certificate="$token" --overwrite

		if ! wait_until_done is_done_step4_rotate_leaves "Step 4: waiting for DPU BMC server certificate rotation"; then
			die "Step 4: timed out waiting for DPU BMC server certificate rotation (token=$token)"
		fi
		log_pass "Step 4: DPU BMC server certificates rotated successfully"
	else
		log_info "Step 4: skipping DPU BMC server certificate rotation in host-trusted mode"
	fi
	log_step_result "Step 4: certificate rotation completed"
}

# step5_prune_old_ca closes the dual-trust window.
#
# The retired root goes, which nothing chains to any more, and with it everything it signed: those
# are paths that now end nowhere. The intermediate still in the bundle shares its key and subject
# with the retired one, so removing the old certificates takes no trust away.
#
# What is not removed is anything the retired root did not sign. DPF merges the bundle without
# pruning precisely so that certificates an operator adds survive, and a prune that worked by
# subtracting what DPF publishes would discard those.
step5_prune_old_ca() {
	backup_exists || {
		log_info "Step 5: Secret/$BACKUP_SECRET does not exist; nothing recorded to prune"
		return 0
	}

	log_info "Step 5: pruning the retired authority from ConfigMap/$TRUST_BUNDLE_CM"
	local old_root_fp old_intermediate_fp tmpdir out_file old_root_file removed kept cert fp escaped patch_payload
	old_root_fp="$(backed_up_fingerprint root-tls.crt)"
	old_intermediate_fp="$(backed_up_fingerprint intermediate-tls.crt)"
	# What the prune rests on is the live intermediate being signed by the root now in effect, which
	# is what makes the retired one safe to take away. Asked of the intermediate in the same terms
	# step 3 reports completion in, rather than by it having changed since step 1: cert-manager
	# renews the intermediate as it ages, under whichever authority is above it at the time, so a
	# certificate that differs from the recorded one can still be signed by the authority about to be
	# pruned. run-all cannot reach this, having run step 3 first, so the ordering it relies on is
	# checked here for the step invoked on its own.
	if [[ "$(secret_cert_fingerprint "$PLATFORM_CA_SECRET" 'ca\.crt')" != "$(secret_cert_fingerprint "$ROOT_CA_SECRET")" ]]; then
		die "Step 5: the live intermediate is signed by an authority other than the root now in \
effect, so step 3 has not re-signed it. Pruning would take away the only authority that validates \
it. Run step 3 first."
	fi
	# And on the leaves below it having been reissued, which is the other half of the ordering: they
	# keep working either way, the intermediate having kept the key that signed them, but one that
	# was not reissued still hands a peer a chain ending at the certificate about to be pruned.
	if ! CHECK_OUTPUT=false all_leaf_certificates_carry_current_authority; then
		die "Step 5: leaf certificates still carry the authority being retired, so step 4 has not \
renewed them. Pruning would leave them presenting a chain that ends outside the trust bundle. Run \
step 4 first."
	fi
	PREVIOUS_BUNDLE_HASH="$(current_bundle_hash)"
	# Recorded on the backup as well as held here, because the hash the prune has to be measured
	# against outlives this process. Between the patch below and the operator recomputing bundle-hash
	# every consumer still reports the hash the prune was meant to replace, so a run resuming inside
	# that window with nothing to compare against reads the old hash as convergence, calls the prune
	# complete before a peer has seen it, and lets step 6 discard the backup.
	k patch secret "$BACKUP_SECRET" --type=merge \
		-p "{\"stringData\":{\"pre-prune-bundle-hash\":\"$PREVIOUS_BUNDLE_HASH\"}}" > /dev/null
	tmpdir="$(mktemp -d)"
	out_file="$tmpdir/pruned.pem"
	old_root_file="$tmpdir/old-root.pem"
	removed=0
	kept=0

	backed_up_certificate root-tls.crt "$old_root_file" || {
		rm -rf "$tmpdir"
		die "Step 5: Secret/$BACKUP_SECRET holds no root certificate to prune by"
	}
	split_bundle "$tmpdir"

	: > "$out_file"
	for cert in "$tmpdir"/cert-*.pem; do
		[[ -s "$cert" ]] || continue
		fp="$(cert_fingerprint "$cert")"
		# The retired authority and everything underneath it, rather than only the two certificates
		# step 1 recorded. The operator sweeps the earlier certificates of its own intermediate out
		# of the bundle when step 3 re-signs it, so in a rotation that ran in order this matches
		# little beyond the root. What it still answers for is everything that sweep leaves alone: a
		# certificate under the retired root carrying a subject or key DPF does not publish, and a
		# bundle the operator has not rewritten yet. What is left behind is a certificate whose
		# issuer has just been taken away: nothing can validate it, and it occupies one of the ten
		# slots a BlueField BMC truststore has.
		if [[ "$fp" == "$old_root_fp" || "$fp" == "$old_intermediate_fp" ]] || signed_by "$cert" "$old_root_file"; then
			removed=$((removed + 1))
			continue
		fi
		cat "$cert" >> "$out_file"
		printf '\n' >> "$out_file"
		kept=$((kept + 1))
	done

	if [[ "$removed" -eq 0 ]]; then
		rm -rf "$tmpdir"
		die "Step 5: the retired authority is not in ConfigMap/$TRUST_BUNDLE_CM; nothing was pruned"
	fi
	if [[ "$kept" -eq 0 ]]; then
		rm -rf "$tmpdir"
		die "Step 5: refusing to prune because the resulting trust bundle would be empty"
	fi

	escaped="$(awk 'BEGIN{printf "\""} {gsub(/\\/,"\\\\"); gsub(/"/,"\\\""); printf "%s\\n",$0} END{printf "\""}' "$out_file")"
	patch_payload="{\"data\":{\"ca.crt\":${escaped}}}"
	k patch configmap "$TRUST_BUNDLE_CM" --type=merge -p "$patch_payload" > /dev/null
	rm -rf "$tmpdir"

	log_info "Step 5: pruned the retired authority from ConfigMap/$TRUST_BUNDLE_CM (removed certificates: $removed)"
	if ! wait_until_done is_done_step5_prune_old_ca "Step 5: waiting for the pruned trust bundle to converge"; then
		die "Step 5: timed out waiting for the pruned trust bundle to converge"
	fi
	log_step_result "Step 5: retired authority pruned and trust converged"
}

retired_certificates_still_in_bundle() {
	backup_exists || return 1
	bundle_has_fingerprint "$(backed_up_fingerprint root-tls.crt)" && return 0
	bundle_has_fingerprint "$(backed_up_fingerprint intermediate-tls.crt)"
}

step6_cleanup() {
	if retired_certificates_still_in_bundle; then
		die "Step 6: the retired authority is still present in ConfigMap/$TRUST_BUNDLE_CM; run step5-prune-old-ca first"
	fi
	log_info "Step 6: deleting Secret/$BACKUP_SECRET"
	k delete secret "$BACKUP_SECRET" --ignore-not-found

	if ! wait_until_done is_done_step6_cleanup "Step 6: waiting for the backup Secret to be deleted"; then
		die "Step 6: timed out waiting for the backup Secret to be deleted"
	fi
	log_step_result "Step 6: backup Secret deleted"
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

# is_done_step1_backup accepts a backup only while it records the rotation being performed. That it
# exists does not say so on its own, and every step after this one reads what to re-key, what to prune
# and what to verify against out of it: taking a backup an abandoned rotation left behind at face
# value reports step 2 complete, because the authority it names is indeed not the live one, and then
# prunes by fingerprints belonging to a rotation that is over, leaving the current one unperformed.
#
# It records this rotation in one of two states. Before the root is re-keyed the authority it holds is
# still the live one. After, it is no longer live but is still published in the bundle, which is the
# dual trust the rotation runs under until step 5 closes it. An authority that is neither is from a
# rotation that already finished.
#
# Which of the two it is decides what is asked of the intermediate it records. Before the re-key
# nothing has been retired, so that certificate has to be the live one as well: an intermediate that
# has moved on since, renewed as it aged or by a rotation that got no further, would be pruned for by
# a fingerprint step 5 then fails to find while the one actually retired stays in the bundle. After
# the re-key the intermediate is expected to have been re-signed, and unlike the root it has nothing
# left naming it, so requiring it to match anything would report the step unperformed in the middle
# of the rotation it belongs to and re-record the backup over the authority being retired.
is_done_step1_backup() {
	local recorded
	backup_exists || return 1
	recorded="$(backed_up_fingerprint root-tls.crt)"
	[[ -n "$recorded" ]] || return 1
	if [[ "$recorded" == "$(secret_cert_fingerprint "$ROOT_CA_SECRET")" ]]; then
		[[ "$(backed_up_fingerprint intermediate-tls.crt)" == "$(secret_cert_fingerprint "$PLATFORM_CA_SECRET")" ]] && return 0
		log_pending "Secret/$BACKUP_SECRET records an intermediate that is no longer the live one"
		return 1
	fi
	bundle_has_fingerprint "$recorded" || {
		log_pending "Secret/$BACKUP_SECRET records an authority that is neither live nor in the trust bundle"
		return 1
	}
}

# is_done_step2_rekey_root recognises the new root by its certificate differing from the one
# recorded in step 1, which is the only thing that distinguishes a re-key from a Certificate that
# merely came back ready.
is_done_step2_rekey_root() {
	local cert_ready
	backup_exists || return 1
	k get secret "$ROOT_CA_SECRET" > /dev/null 2>&1 || return 1
	cert_ready="$(k get certificate "$ROOT_CA_CERT" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}')"
	[[ "$cert_ready" == "True" ]] || return 1
	[[ "$(secret_cert_fingerprint "$ROOT_CA_SECRET")" != "$(backed_up_fingerprint root-tls.crt)" ]]
}

# is_done_step3_renew_intermediates holds the step open until the bundle carries both authorities
# and every peer has applied it. The intermediate coming back ready is not enough on its own: the
# peers are what the rotation is for, and they are reached through the bundle rather than directly.
is_done_step3_renew_intermediates() {
	local cert_ready current_anchor_fp
	cert_ready="$(k get certificate "$PLATFORM_CA_CERT" -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}')"
	[[ "$cert_ready" == "True" ]] || return 1

	# Whether the intermediate has been re-signed at all, which nothing else here answers. It keeps
	# its key and subject DN through the re-signing, it is Ready either way because the certificate
	# the retired authority signed is valid until it expires, and both authorities sit in the bundle
	# for the whole of the dual trust window, so finding one of them there says nothing about which
	# signed it. Left unasked, the step reports complete over an intermediate still signed by the
	# authority step 5 then prunes.
	current_anchor_fp="$(secret_cert_fingerprint "$PLATFORM_CA_SECRET" 'ca\.crt')"
	if is_self_signed; then
		[[ "$current_anchor_fp" == "$(secret_cert_fingerprint "$ROOT_CA_SECRET")" ]] || {
			log_pending "The intermediate is still signed by the authority being retired"
			return 1
		}
	elif [[ -z "$PRE_RENEW_PLATFORM_FP" ]]; then
		# Under an authority of the operator nothing on the cluster names the certificate being
		# rotated to: the issuer keeps its name across the rotation, and the bundle that would
		# otherwise be read for it is theirs to curate rather than DPF's to write. Completion is
		# therefore not inferred here, it is performed, and what shows it is the intermediate carrying
		# a certificate it did not carry before the renewal.
		log_pending "The intermediate has not been renewed by this run"
		return 1
	elif [[ "$(secret_cert_fingerprint "$PLATFORM_CA_SECRET")" == "$PRE_RENEW_PLATFORM_FP" ]]; then
		log_pending "The intermediate still carries the certificate it had before the renewal"
		return 1
	fi

	# The authority now above the intermediate has to be published before anything below it is
	# moved across, or a peer validating against the bundle alone would reject the new chain.
	bundle_has_fingerprint "$current_anchor_fp" || {
		log_pending "Trust bundle does not carry the current authority yet"
		return 1
	}

	# On a self-signed rotation the retired root is still expected alongside it, which is the dual
	# trust the window rests on. Its absence means something pruned the bundle early.
	if backup_exists && is_self_signed; then
		bundle_has_fingerprint "$(backed_up_fingerprint root-tls.crt)" || {
			log_pending "Trust bundle no longer carries the retired authority; dual trust is already closed"
			return 1
		}
	fi

	is_trust_converged
}

is_done_step4_rotate_leaves() {
	all_leaf_certificates_carry_current_authority || return 1
	if is_zero_trust; then
		[[ -n "$ROTATION_TOKEN" ]] || return 1
		all_dpudevices_bmc_rotation_done "$ROTATION_TOKEN" || return 1
	fi
	return 0
}

is_done_step5_prune_old_ca() {
	local pre_prune
	! retired_certificates_still_in_bundle || return 1
	pre_prune="${PREVIOUS_BUNDLE_HASH:-$(backed_up_bundle_hash)}"
	if [[ -n "$pre_prune" && "$(current_bundle_hash)" == "$pre_prune" ]]; then
		log_pending "The trust bundle still carries the hash it had before the prune"
		return 1
	fi
	is_trust_converged
}

is_done_step6_cleanup() {
	! backup_exists
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

	if is_self_signed; then
		log_info "Starting self-signed CA rotation (Steps 1-6)"
		run_all_step 1 "Record the certificates being retired" is_done_step1_backup step1_backup
		run_all_step 2 "Re-key the self-signed root" is_done_step2_rekey_root step2_rekey_root
		run_all_step 3 "Re-sign the intermediates and converge dual trust" is_done_step3_renew_intermediates step3_renew_intermediates
		run_all_step 4 "Renew leaf certificates and conditionally rotate BMC certificates" is_done_step4_rotate_leaves step4_rotate_leaves
		run_all_step 5 "Prune the retired authority from the trust bundle" is_done_step5_prune_old_ca step5_prune_old_ca
		run_all_step 6 "Delete the backup Secret" is_done_step6_cleanup step6_cleanup
	else
		log_info "Starting external CA rotation (Steps 3-4)"
		log_info "Steps 1, 2, 5 and 6 act on the root and the trust bundle, which an external issuer leaves to you"
		run_all_step 3 "Re-sign the intermediates and converge trust" is_done_step3_renew_intermediates step3_renew_intermediates
		run_all_step 4 "Renew leaf certificates and conditionally rotate BMC certificates" is_done_step4_rotate_leaves step4_rotate_leaves
	fi

	log_pass "CA rotation completed"
}

# require_self_signed refuses the steps that act on objects an external issuer puts outside the
# reach of DPF, rather than letting them fail further in with an error about a missing object.
require_self_signed() {
	is_self_signed || die "$1 applies to the self-signed root only. The PKI is anchored at \
$ANCHOR_NAME, so re-keying it and curating ConfigMap/$TRUST_BUNDLE_CM are yours to perform."
}

main() {
	parse_args "$@"
	case "$COMMAND" in
	precheck) precheck ;;
	run-all) run_all ;;
	step1-backup)
		require_self_signed step1-backup
		step1_backup
		;;
	step2-rekey-root)
		require_self_signed step2-rekey-root
		step2_rekey_root
		;;
	step3-renew-intermediates) step3_renew_intermediates ;;
	step4-rotate-leaves) step4_rotate_leaves ;;
	step5-prune-old-ca)
		require_self_signed step5-prune-old-ca
		step5_prune_old_ca
		;;
	step6-cleanup)
		require_self_signed step6-cleanup
		step6_cleanup
		;;
	*)
		usage
		die "unknown command: $COMMAND"
		;;
	esac
}

main "$@"

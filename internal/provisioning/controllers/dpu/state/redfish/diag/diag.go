/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package diag translates Redfish task / SEL telemetry into operator-actionable
// hints. The translations are driven by publicly documented MessageIds from the
// NVIDIA BlueField BMC documentation:
// https://docs.nvidia.com/networking/display/bfswtroubleshooting/bmc
//
// The package is pure data + string formatting; it performs no I/O. The caller
// is responsible for fetching the Redfish payloads and for falling back to the
// raw response body when no hint is returned.
package diag

import (
	"fmt"
	"net/http"
	"strings"
)

// SensorThresholdLowMessageID is the OpenBMC MessageId emitted in the BMC SEL /
// EventLog when a sensor crosses its warning-low threshold (e.g., 12V_ATX
// dropping below 10.4V when the ATX power cable is disconnected).
const SensorThresholdLowMessageID = "OpenBMC.0.1.SensorThresholdWarningLowGoingLow"

// PowerRailHints lists sensor names whose low-threshold events we surface as a
// "check power cable / PCIe seating" hint in the condition message.
//
// 12V_ATX is the rail fed by the external ATX cable; per the BF3 power spec it
// should read ~12V on full-power cards (and is intentionally ~0V on Sub75 cards
// where power comes from the PCIe edge connector). The BMC threshold event
// fires only when the rail is *expected* to be ~12V and drops below ~10.4V.
//
// 12V_PCIe is the slot rail; a low reading typically indicates a seating /
// supply issue.
var PowerRailHints = map[string]struct{}{
	"12V_ATX":  {},
	"12V_PCIe": {},
}

// taskMessageHints maps a documented Redfish MessageId to an operator-friendly
// hint. Both the "Update.1.0.<event>" and "Update.1.0.0.<event>" variants are
// emitted by different BMC firmware versions and must be listed separately:
// they do NOT share a common prefix at the byte level. Keep this set minimal -
// undocumented MessageIds fall back to the existing raw-body dump in the
// caller.
var taskMessageHints = []struct {
	prefix string
	hint   string
}{
	// BMC failed to fetch the firmware/BFB image from the configured URL.
	// Observed on the cluster when bfb-registry was unreachable.
	{"Update.1.0.TransferFailed", "BMC failed to fetch the BFB image. Check that the bfb-registry pod is healthy and reachable from the BMC."},
	{"Update.1.0.0.TransferFailed", "BMC failed to fetch the BFB image. Check that the bfb-registry pod is healthy and reachable from the BMC."},
	// BMC rejected the image as invalid.
	{"Update.1.0.VerificationFailed", "BMC rejected the BFB as invalid. Verify the BFB file/checksum."},
	{"Update.1.0.0.VerificationFailed", "BMC rejected the BFB as invalid. Verify the BFB file/checksum."},
	// Image transferred but activation on the target failed.
	{"Update.1.0.ActivateFailed", "BFB activation failed. Check BMC logs for details."},
	{"Update.1.0.0.ActivateFailed", "BFB activation failed. Check BMC logs for details."},
	// Image too large for the BMC.
	{"Base.1.8.1.ResourceExhaustion", "BFB image exceeds the BMC's accepted size."},
	// Another firmware update is already running.
	{"Update.1.0.UpdateInProgress", "Another firmware update is already in progress on the BMC."},
	{"Update.1.0.0.UpdateInProgress", "Another firmware update is already in progress on the BMC."},
}

// TranslatedTaskMessage carries the parsed hint plus the original metadata so
// the caller can decide how to render it.
type TranslatedTaskMessage struct {
	Hint       string // human-readable operator action
	MessageID  string // raw MessageId from the BMC
	Resolution string // BMC-supplied Resolution field, surfaced verbatim
}

// TranslateTaskMessages walks a Redfish Task's Messages[] (as decoded by
// client.TaskProgress.Messages) and returns the first match against the
// documented MessageId set. The boolean is false when no documented MessageId
// is present; callers should fall back to dumping the raw response body in
// that case so we never regress for unknown errors.
func TranslateTaskMessages(messages []map[string]interface{}) (TranslatedTaskMessage, bool) {
	for _, m := range messages {
		id, _ := m["MessageId"].(string)
		if id == "" {
			continue
		}
		for _, h := range taskMessageHints {
			if strings.HasPrefix(id, h.prefix) {
				resolution, _ := m["Resolution"].(string)
				return TranslatedTaskMessage{
					Hint:       h.hint,
					MessageID:  id,
					Resolution: resolution,
				}, true
			}
		}
	}
	return TranslatedTaskMessage{}, false
}

// ClassifyHTTPStatus returns a hint for non-200 responses to CheckTaskProgress
// that we can attribute to a specific BMC behavior. Returns "" for statuses
// where we have no useful interpretation (caller still appends the raw body).
//
// Documented in the BMC troubleshooting guide under "Possible Error Codes
// During BMC/eROT Upgrade":
//   - 404: Redfish task URI is no longer accessible after the BMC was rebooted
//     or the Redfish server restarted; the client may retry.
//   - 400 with body containing "UpdateInProgress" / "Another update": the
//     Redfish server blocks the second firmware update request.
func ClassifyHTTPStatus(status int, body string) string {
	switch {
	case status == http.StatusNotFound:
		return "Redfish task no longer exists on the BMC. The BMC may have rebooted or the Redfish server restarted; provisioning can be retried."
	case status == http.StatusBadRequest && containsAny(body, "UpdateInProgress", "Another update is in progress"):
		return "Another firmware update is already in progress on the BMC."
	default:
		return ""
	}
}

// ShouldProbeRails returns true for HTTP statuses where a low-rail SEL event
// is plausibly the underlying cause: 404 (BMC may have rebooted from a power
// problem) and 5xx (BMC Redfish layer unstable). Other 4xx responses (auth /
// bad-request) are not power-related.
func ShouldProbeRails(status int) bool {
	return status == http.StatusNotFound || status >= 500
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// FormatRailHint returns the human-readable hint we append when a low-rail SEL
// event is found. Returns "" if rail or readings are empty so callers can
// simply concatenate.
func FormatRailHint(rail, reading, threshold string) string {
	if rail == "" {
		return ""
	}
	if reading == "" || threshold == "" {
		return fmt.Sprintf("BMC SEL: %s low - check power cable / PCIe seating.", rail)
	}
	return fmt.Sprintf("BMC SEL: %s low (%sV < %sV) - check power cable / PCIe seating.", rail, reading, threshold)
}

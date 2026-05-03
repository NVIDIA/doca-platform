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

package diag

import (
	"net/http"
	"testing"
)

func TestTranslateTaskMessages(t *testing.T) {
	cases := []struct {
		name           string
		messages       []map[string]interface{}
		wantFound      bool
		wantHint       string
		wantID         string
		wantResolution string
	}{
		{
			name:      "empty",
			messages:  nil,
			wantFound: false,
		},
		{
			name: "transfer failed - exact prefix and BMC Resolution surfaced",
			messages: []map[string]interface{}{
				{"MessageId": "Update.1.0.TransferFailed", "Resolution": "Unknown Host: ..."},
			},
			wantFound:      true,
			wantID:         "Update.1.0.TransferFailed",
			wantHint:       "BMC failed to fetch the BFB image. Check that the bfb-registry pod is healthy and reachable from the BMC.",
			wantResolution: "Unknown Host: ...",
		},
		{
			name: "transfer failed - minor-version variant matches via prefix",
			messages: []map[string]interface{}{
				{"MessageId": "Update.1.0.0.TransferFailed"},
			},
			wantFound: true,
			wantID:    "Update.1.0.0.TransferFailed",
			wantHint:  "BMC failed to fetch the BFB image. Check that the bfb-registry pod is healthy and reachable from the BMC.",
		},
		{
			name: "verification failed",
			messages: []map[string]interface{}{
				{"MessageId": "Update.1.0.VerificationFailed"},
			},
			wantFound: true,
			wantID:    "Update.1.0.VerificationFailed",
			wantHint:  "BMC rejected the BFB as invalid. Verify the BFB file/checksum.",
		},
		{
			name: "activate failed",
			messages: []map[string]interface{}{
				{"MessageId": "Update.1.0.ActivateFailed"},
			},
			wantFound: true,
			wantID:    "Update.1.0.ActivateFailed",
			wantHint:  "BFB activation failed. Check BMC logs for details.",
		},
		{
			name: "resource exhaustion",
			messages: []map[string]interface{}{
				{"MessageId": "Base.1.8.1.ResourceExhaustion"},
			},
			wantFound: true,
			wantID:    "Base.1.8.1.ResourceExhaustion",
			wantHint:  "BFB image exceeds the BMC's accepted size.",
		},
		{
			name: "update in progress",
			messages: []map[string]interface{}{
				{"MessageId": "Update.1.0.UpdateInProgress"},
			},
			wantFound: true,
			wantID:    "Update.1.0.UpdateInProgress",
			wantHint:  "Another firmware update is already in progress on the BMC.",
		},
		{
			name: "update in progress - 1.0.0 variant",
			messages: []map[string]interface{}{
				{"MessageId": "Update.1.0.0.UpdateInProgress"},
			},
			wantFound: true,
			wantID:    "Update.1.0.0.UpdateInProgress",
			wantHint:  "Another firmware update is already in progress on the BMC.",
		},
		{
			name: "unknown MessageId returns not-found",
			messages: []map[string]interface{}{
				{"MessageId": "Base.1.0.GeneralError"},
			},
			wantFound: false,
		},
		{
			name: "missing MessageId is skipped, next entry matches",
			messages: []map[string]interface{}{
				{"Message": "no id here"},
				{"MessageId": "Update.1.0.TransferFailed"},
			},
			wantFound: true,
			wantID:    "Update.1.0.TransferFailed",
		},
		{
			name: "non-string MessageId is ignored",
			messages: []map[string]interface{}{
				{"MessageId": 42},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := TranslateTaskMessages(tc.messages)
			if found != tc.wantFound {
				t.Fatalf("found=%v, want %v (got=%+v)", found, tc.wantFound, got)
			}
			if !found {
				return
			}
			if got.MessageID != tc.wantID {
				t.Errorf("MessageID=%q, want %q", got.MessageID, tc.wantID)
			}
			if tc.wantHint != "" && got.Hint != tc.wantHint {
				t.Errorf("Hint=%q, want %q", got.Hint, tc.wantHint)
			}
			if tc.wantResolution != "" && got.Resolution != tc.wantResolution {
				t.Errorf("Resolution=%q, want %q", got.Resolution, tc.wantResolution)
			}
		})
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"404 -> task gone", http.StatusNotFound, "", "Redfish task no longer exists on the BMC. The BMC may have rebooted or the Redfish server restarted; provisioning can be retried."},
		{"400 + UpdateInProgress in body -> hint", http.StatusBadRequest, `{"error":"Another update is in progress (UpdateInProgress)"}`, "Another firmware update is already in progress on the BMC."},
		{"400 unrelated -> empty", http.StatusBadRequest, "bad payload", ""},
		{"500 -> empty (caller appends body)", http.StatusInternalServerError, "boom", ""},
		{"401 -> empty", http.StatusUnauthorized, "", ""},
		{"200 -> empty (caller should not invoke)", http.StatusOK, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyHTTPStatus(tc.status, tc.body); got != tc.want {
				t.Errorf("ClassifyHTTPStatus(%d, %q) = %q, want %q", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestShouldProbeRails(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
	}
	for _, tc := range cases {
		if got := ShouldProbeRails(tc.status); got != tc.want {
			t.Errorf("ShouldProbeRails(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestFormatRailHint(t *testing.T) {
	cases := []struct {
		name      string
		rail      string
		reading   string
		threshold string
		want      string
	}{
		{"empty rail -> empty", "", "1", "10", ""},
		{"missing reading or threshold -> short form", "12V_ATX", "", "10.4", "BMC SEL: 12V_ATX low - check power cable / PCIe seating."},
		{"full form (ATX)", "12V_ATX", "6.048", "10.4", "BMC SEL: 12V_ATX low (6.048V < 10.4V) - check power cable / PCIe seating."},
		{"full form (PCIe)", "12V_PCIe", "5.0", "10.0", "BMC SEL: 12V_PCIe low (5.0V < 10.0V) - check power cable / PCIe seating."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatRailHint(tc.rail, tc.reading, tc.threshold); got != tc.want {
				t.Errorf("FormatRailHint(%q,%q,%q) = %q, want %q", tc.rail, tc.reading, tc.threshold, got, tc.want)
			}
		})
	}
}

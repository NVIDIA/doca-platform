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

package bmcdump

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	redfishclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTaskDumpEntryID(t *testing.T) {
	unit := dumpUnit{servicePath: "/redfish/v1/Systems/BlueField_0" + dumpLogServicePath}
	task := func(headers ...interface{}) map[string]interface{} {
		return map[string]interface{}{"Payload": map[string]interface{}{"HttpHeaders": headers}}
	}
	tests := []struct {
		name string
		task map[string]interface{}
		want string
	}{
		{
			name: "no payload",
			task: map[string]interface{}{},
			want: "",
		},
		{
			name: "location among the request headers",
			task: task("Host: 10.0.110.126", "Connection: close", "Location: /redfish/v1/Systems/BlueField_0/LogServices/Dump/Entries/4"),
			want: "4",
		},
		{
			name: "location under another dump service",
			task: task("Location: /redfish/v1/Managers/BlueField_BMC_0/LogServices/Dump/Entries/4"),
			want: "",
		},
		{
			name: "location on another host",
			task: task("Location: https://example.com/redfish/v1/Systems/BlueField_0/LogServices/Dump/Entries/4"),
			want: "",
		},
		{
			name: "location with more than one segment",
			task: task("Location: /redfish/v1/Systems/BlueField_0/LogServices/Dump/Entries/4/../../x"),
			want: "",
		},
		{
			name: "location naming the parent directory",
			task: task("Location: /redfish/v1/Systems/BlueField_0/LogServices/Dump/Entries/.."),
			want: "",
		},
		{
			name: "header that is not a string",
			task: task(json.Number("4")),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(taskDumpEntryID(unit, tt.task)).To(Equal(tt.want))
		})
	}
}

func TestDumpEntryIDsSkipsEntriesWithoutAUsableID(t *testing.T) {
	g := NewWithT(t)

	ids := dumpEntryIDs(map[string]interface{}{
		"Members": []interface{}{
			"not-an-entry",
			map[string]interface{}{"Created": "2026-07-13T12:00:00Z"},
			map[string]interface{}{"Id": ""},
			map[string]interface{}{"Id": "../etc"},
			map[string]interface{}{"Id": "valid"},
			map[string]interface{}{"Id": json.Number("42")},
		},
	})

	g.Expect(sets.List(ids)).To(Equal([]string{"42", "valid"}))
}

func TestDPUDeviceBMCCredentialSecret(t *testing.T) {
	tests := []struct {
		name   string
		device provisioningv1.DPUDevice
		want   string
	}{
		{
			name: "status secret wins over spec secret",
			device: provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: stringPtr("spec-secret"),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: stringPtr("status-secret"),
				},
			},
			want: "status-secret",
		},
		{
			name: "empty status secret falls back to spec secret",
			device: provisioningv1.DPUDevice{
				Spec: provisioningv1.DPUDeviceSpec{
					BMCCredentialSecretName: stringPtr("spec-secret"),
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: stringPtr(""),
				},
			},
			want: "spec-secret",
		},
		{
			name: "missing device secret falls back to shared secret",
			device: provisioningv1.DPUDevice{
				Status: provisioningv1.DPUDeviceStatus{
					BMCCredentialSecretName: nil,
				},
			},
			want: sharedPasswordSecretName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got := dpuDeviceBMCCredentialSecret(tt.device)

			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestSortedLogTargets(t *testing.T) {
	g := NewWithT(t)

	targets := sortedLogTargets(map[string]logTarget{
		"third": {
			IP:               "10.0.0.2",
			Port:             443,
			CredentialSecret: "z-secret",
			DPUDevices:       []string{"worker-b", "worker-a"},
		},
		"second": {
			IP:               "10.0.0.1",
			Port:             8443,
			CredentialSecret: "b-secret",
			DPUDevices:       []string{"worker-d"},
		},
		"first": {
			IP:               "10.0.0.1",
			Port:             443,
			CredentialSecret: "c-secret",
			DPUDevices:       []string{"worker-c"},
		},
	})

	g.Expect(targets).To(HaveLen(3))
	g.Expect(targets[0].IP).To(Equal("10.0.0.1"))
	g.Expect(targets[0].Port).To(Equal(uint32(443)))
	g.Expect(targets[0].CredentialSecret).To(Equal("c-secret"))
	g.Expect(targets[1].IP).To(Equal("10.0.0.1"))
	g.Expect(targets[1].Port).To(Equal(uint32(8443)))
	g.Expect(targets[1].CredentialSecret).To(Equal("b-secret"))
	g.Expect(targets[2].IP).To(Equal("10.0.0.2"))
	g.Expect(targets[2].DPUDevices).To(Equal([]string{"worker-a", "worker-b"}))
}

func TestGetLogTargetsUsesSharedSecretFallbackAndSkipsFailedDevices(t *testing.T) {
	g := NewWithT(t)

	const namespace = "dpf-operator-system"
	const password = "shared-password"
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	bmcIP := "10.0.0.10"
	missingSecret := "missing-secret"
	otherIP := "10.0.0.11"
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: sharedPasswordSecretName, Namespace: namespace},
				Data:       map[string][]byte{redfishclient.BMCSharedPasswordKey: []byte(password)},
			},
			dpuDevice("device-b", bmcIP, nil),
			dpuDevice("device-a", bmcIP, nil),
			dpuDevice("device-c", otherIP, &missingSecret),
		).
		Build()

	targets, err := getLogTargets(context.Background(), c, CollectOptions{Namespace: namespace})

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("DPUDevice device-c"))
	g.Expect(targets).To(HaveLen(1))
	g.Expect(targets[0].IP).To(Equal(bmcIP))
	g.Expect(targets[0].Port).To(Equal(uint32(defaultPort)))
	g.Expect(targets[0].Password).To(Equal(password))
	g.Expect(targets[0].CredentialSecret).To(Equal(sharedPasswordSecretName))
	g.Expect(targets[0].DPUDevices).To(Equal([]string{"device-a", "device-b"}))
}

func TestGetLogTargetsKeepsResolvedExplicitDevicesWhenOneIsMissing(t *testing.T) {
	g := NewWithT(t)

	const namespace = "dpf-operator-system"
	const password = "shared-password"
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	bmcIP := "10.0.0.10"
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: sharedPasswordSecretName, Namespace: namespace},
				Data:       map[string][]byte{redfishclient.BMCSharedPasswordKey: []byte(password)},
			},
			dpuDevice("device-a", bmcIP, nil),
		).
		Build()

	targets, err := getLogTargets(context.Background(), c, CollectOptions{
		Namespace: namespace,
		Devices:   []string{"device-a", "missing-device"},
	})

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("getting DPUDevice dpf-operator-system/missing-device"))
	g.Expect(targets).To(HaveLen(1))
	g.Expect(targets[0].IP).To(Equal(bmcIP))
	g.Expect(targets[0].DPUDevices).To(Equal([]string{"device-a"}))
}

func TestWaitForNewDumpEntryRetriesUntilEntryIsVisible(t *testing.T) {
	g := NewWithT(t)

	unit := dumpUnit{
		name:        managerUnitName,
		servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath,
	}

	var requests int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != unit.entriesPath() {
			return httpResponse(req, http.StatusNotFound, ""), nil
		}
		if atomic.AddInt32(&requests, 1) == 1 {
			return httpResponse(req, http.StatusOK, `{"Members":[]}`), nil
		}
		return httpResponse(req, http.StatusOK, `{"Members":[{"Id":"entry-1","Created":"2026-07-13T10:00:00Z"}]}`), nil
	})

	targetDir := t.TempDir()
	c := &collector{
		target:         logTarget{IP: "10.0.0.10"},
		targetDir:      targetDir,
		ctx:            context.Background(),
		client:         resty.New().SetBaseURL("https://10.0.0.10").SetTransport(transport),
		entryRetry:     2,
		entryRetryWait: time.Nanosecond,
	}

	entryID, err := c.waitForNewDumpEntry(unit, sets.New[string](), targetDir)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(entryID).To(Equal("entry-1"))
	g.Expect(atomic.LoadInt32(&requests)).To(Equal(int32(2)))
	data, err := os.ReadFile(filepath.Join(targetDir, "dump-entries.json"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring("entry-1"))
}

func dpuDevice(name, bmcIP string, credentialSecret *string) *provisioningv1.DPUDevice {
	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: DefaultNamespace},
		Status: provisioningv1.DPUDeviceStatus{
			BMCIP:                   &bmcIP,
			BMCCredentialSecretName: credentialSecret,
		},
	}
}

func stringPtr(value string) *string {
	return &value
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

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

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// accountPatch records one password change the fake BMC received, including the credentials the
// request carried, so tests can assert both the order of the writes and the session each rode on.
type accountPatch struct {
	account     string
	authUser    string
	authPass    string
	newPassword string
}

// fakeAccountBMC is a Redfish BMC that implements just enough of the account service to exercise
// password hardening: a root service that says BF3 or BF4, basic auth against the Redfish user's
// current password, and a PATCH endpoint per account.
type fakeAccountBMC struct {
	server *httptest.Server

	mu               sync.Mutex
	isBF4            bool
	redfishUser      string
	redfishPassword  string
	servicePassword  string
	serviceMissing   bool
	servicePatchFail bool
	rejectPolicy     bool
	patches          []accountPatch
}

func newFakeAccountBMC(isBF4 bool, initialPassword string) *fakeAccountBMC {
	bmc := &fakeAccountBMC{
		isBF4:           isBF4,
		redfishUser:     BF3BMCUser,
		redfishPassword: initialPassword,
		servicePassword: BMCDefaultPassword,
	}
	if isBF4 {
		bmc.redfishUser = BF4BMCUser
	}
	bmc.server = httptest.NewTLSServer(http.HandlerFunc(bmc.serve))
	return bmc
}

func (b *fakeAccountBMC) Close() { b.server.Close() }

func (b *fakeAccountBMC) recordedPatches() []accountPatch {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]accountPatch(nil), b.patches...)
}

func (b *fakeAccountBMC) passwords() (redfish, service string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.redfishPassword, b.servicePassword
}

func (b *fakeAccountBMC) serve(w http.ResponseWriter, req *http.Request) {
	// The handler runs on the server's own goroutine, where a failed Expect would otherwise crash
	// the suite instead of being reported against the spec that made the request.
	defer GinkgoRecover()

	switch {
	case req.URL.Path == "/redfish/v1" || req.URL.Path == "/redfish/v1/":
		product := "BlueField-3 DPU"
		if b.isBF4 {
			product = "B4240"
		}
		_, _ = w.Write([]byte(`{"Product":"` + product + `"}`))
	case strings.HasPrefix(req.URL.Path, "/redfish/v1/AccountService/Accounts/"):
		b.handleAccountPatch(w, req)
	case req.URL.Path == "/redfish/v1/Managers",
		strings.HasPrefix(req.URL.Path, "/redfish/v1/UpdateService/FirmwareInventory/"):
		// Both are used purely as authentication probes by InitPassword and VerifyBMCCredential.
		if !b.authorized(req) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"Version":"BF-24.10-17"}`))
	default:
		http.NotFound(w, req)
	}
}

func (b *fakeAccountBMC) authorized(req *http.Request) bool {
	user, password, ok := req.BasicAuth()
	if !ok {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return user == b.redfishUser && password == b.redfishPassword
}

func (b *fakeAccountBMC) handleAccountPatch(w http.ResponseWriter, req *http.Request) {
	Expect(req.Method).To(Equal(http.MethodPatch))

	account := path.Base(req.URL.Path)
	authUser, authPass, _ := req.BasicAuth()
	var body struct {
		Password string `json:"Password"`
	}
	Expect(json.NewDecoder(req.Body).Decode(&body)).To(Succeed())

	b.mu.Lock()
	defer b.mu.Unlock()
	b.patches = append(b.patches, accountPatch{
		account: account, authUser: authUser, authPass: authPass, newPassword: body.Password,
	})

	if b.rejectPolicy {
		writeAccountError(w, http.StatusBadRequest, "Base.1.18.1.PropertyValueFormatError",
			"The password provided for account "+account+" is shorter than the minimum length of 13.")
		return
	}

	switch account {
	case BF4ServiceUser:
		if !b.isBF4 || b.serviceMissing {
			writeAccountError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI",
				"The resource at the URI "+req.URL.Path+" was not found.")
			return
		}
		if b.servicePatchFail {
			writeAccountError(w, http.StatusInternalServerError, "Base.1.18.1.InternalError",
				"The request failed due to an internal service error.")
			return
		}
		b.servicePassword = body.Password
	case b.redfishUser:
		// A BMC on the factory default password grants a password-change-only session, so only the
		// account's own credentials are accepted here.
		if authUser != b.redfishUser || authPass != b.redfishPassword {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		b.redfishPassword = body.Password
	default:
		writeAccountError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI",
			"The resource at the URI "+req.URL.Path+" was not found.")
		return
	}
	_, _ = w.Write([]byte(`{}`))
}

func writeAccountError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":                  code,
			"message":               message,
			"@Message.ExtendedInfo": []map[string]interface{}{{"Message": message, "MessageId": code}},
		},
	})
}

var _ = Describe("BMC password hardening", func() {
	const (
		oldPassword = "oldPassword123"
		newPassword = "newPassword123"
	)
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	It("changes admin and then service on BF4, re-authenticating in between", func() {
		bmc := newFakeAccountBMC(true, oldPassword)
		defer bmc.Close()

		client, err := NewBasicAuthClient(bmc.server.URL, BF4BMCUser, oldPassword)
		Expect(err).NotTo(HaveOccurred())

		resp, _, err := client.ChangeBMCPassword(ctx, newPassword, BF4BMCUser)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))

		patches := bmc.recordedPatches()
		Expect(patches).To(HaveLen(2))
		Expect(patches[0].account).To(Equal(BF4BMCUser))
		Expect(patches[0].authPass).To(Equal(oldPassword))
		// The service PATCH must ride a session authenticated with the already-changed password,
		// or a real BMC answers it with 401.
		Expect(patches[1].account).To(Equal(BF4ServiceUser))
		Expect(patches[1].authUser).To(Equal(BF4BMCUser))
		Expect(patches[1].authPass).To(Equal(newPassword))

		redfish, service := bmc.passwords()
		Expect(redfish).To(Equal(newPassword))
		Expect(service).To(Equal(newPassword))
	})

	It("reaches the service account only after leaving the factory default password behind", func() {
		bmc := newFakeAccountBMC(true, BMCDefaultPassword)
		defer bmc.Close()

		client, err := NewBasicAuthClient(bmc.server.URL, BF4BMCUser, BMCDefaultPassword)
		Expect(err).NotTo(HaveOccurred())

		resp, _, err := client.ChangeBMCPassword(ctx, newPassword, BF4BMCUser)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))

		patches := bmc.recordedPatches()
		Expect(patches).To(HaveLen(2))
		Expect(patches[0].account).To(Equal(BF4BMCUser))
		Expect(patches[1].account).To(Equal(BF4ServiceUser))
		Expect(patches[1].authPass).NotTo(Equal(BMCDefaultPassword))
	})

	It("writes only the Redfish user on BF3", func() {
		bmc := newFakeAccountBMC(false, oldPassword)
		defer bmc.Close()

		client, err := NewBasicAuthClient(bmc.server.URL, BF3BMCUser, oldPassword)
		Expect(err).NotTo(HaveOccurred())

		_, _, err = client.ChangeBMCPassword(ctx, newPassword, BF3BMCUser)
		Expect(err).NotTo(HaveOccurred())

		patches := bmc.recordedPatches()
		Expect(patches).To(HaveLen(1))
		Expect(patches[0].account).To(Equal(BF3BMCUser))
	})

	It("tolerates a BF4 BMC whose firmware does not expose the service account", func() {
		bmc := newFakeAccountBMC(true, oldPassword)
		defer bmc.Close()
		bmc.serviceMissing = true

		client, err := NewBasicAuthClient(bmc.server.URL, BF4BMCUser, oldPassword)
		Expect(err).NotTo(HaveOccurred())

		resp, _, err := client.ChangeBMCPassword(ctx, newPassword, BF4BMCUser)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusOK))

		redfish, _ := bmc.passwords()
		Expect(redfish).To(Equal(newPassword))
	})

	It("surfaces a service-account failure and re-applies it on the next pass", func() {
		bmc := newFakeAccountBMC(true, oldPassword)
		defer bmc.Close()
		bmc.servicePatchFail = true

		client, err := NewBasicAuthClient(bmc.server.URL, BF4BMCUser, oldPassword)
		Expect(err).NotTo(HaveOccurred())

		_, _, err = client.ChangeBMCPassword(ctx, newPassword, BF4BMCUser)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(BF4ServiceUser))

		// The Redfish user landed on the new password, so the next pass takes the already-correct
		// branch, which must still re-apply the service password rather than returning early.
		redfish, service := bmc.passwords()
		Expect(redfish).To(Equal(newPassword))
		Expect(service).To(Equal(BMCDefaultPassword))

		bmc.servicePatchFail = false
		k8sClient := fakeClientWithSharedPassword(newPassword)
		_, err = InitPassword(ctx, bmc.server.URL, testNamespace, nil, k8sClient)
		Expect(err).NotTo(HaveOccurred())

		_, service = bmc.passwords()
		Expect(service).To(Equal(newPassword))
	})

	It("applies the service password from RotatePassword's crash-recovery branch", func() {
		bmc := newFakeAccountBMC(true, newPassword)
		defer bmc.Close()

		// The new password is already active, so rotation takes the crash-recovery shortcut.
		_, err := RotatePassword(ctx, bmc.server.URL, newPassword, oldPassword)
		Expect(err).NotTo(HaveOccurred())

		_, service := bmc.passwords()
		Expect(service).To(Equal(newPassword))
	})

	It("names the account and quotes the BMC's reason when the password is rejected", func() {
		bmc := newFakeAccountBMC(false, BMCDefaultPassword)
		defer bmc.Close()
		bmc.rejectPolicy = true

		k8sClient := fakeClientWithSharedPassword("short")
		_, err := InitPassword(ctx, bmc.server.URL, testNamespace, nil, k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`account "root"`))
		Expect(err.Error()).To(ContainSubstring("minimum length of 13"))
	})
})

func fakeClientWithSharedPassword(password string) ctrlclient.WithWatch {
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: BMCPasswordSecret, Namespace: testNamespace},
			Data:       map[string][]byte{BMCSharedPasswordKey: []byte(password)},
		}).
		Build()
}

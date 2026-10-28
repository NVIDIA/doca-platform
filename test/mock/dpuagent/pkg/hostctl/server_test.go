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

package hostctl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeHost struct {
	mu      sync.Mutex
	methods []string
	booted  bool
}

func (f *fakeHost) HostReboot(_ context.Context, method string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods = append(f.methods, method)
	f.booted = false
}

func (f *fakeHost) ArmBooted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.booted
}

func getStatus(t *testing.T, h http.Handler) StatusResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status: status %d", rec.Code)
	}
	var st StatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStatusReportsArmBootedAfterReboot(t *testing.T) {
	host := &fakeHost{booted: true}
	h := Handler(context.Background(), host)
	if st := getStatus(t, h); !st.ArmBooted {
		t.Fatalf("status before any reboot %+v, want armBooted", st)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/reboot", strings.NewReader(`{"method":"SystemLevelReset"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /reboot: status %d", rec.Code)
	}
	if st := getStatus(t, h); st.ArmBooted {
		t.Fatalf("status right after /reboot %+v, want the Arm still booting", st)
	}
	host.mu.Lock()
	methods := append([]string{}, host.methods...)
	host.booted = true
	host.mu.Unlock()
	if len(methods) != 1 || methods[0] != "SystemLevelReset" {
		t.Fatalf("HostReboot calls %v", methods)
	}
	if st := getStatus(t, h); !st.ArmBooted {
		t.Fatalf("status once the Arm is up %+v", st)
	}
}

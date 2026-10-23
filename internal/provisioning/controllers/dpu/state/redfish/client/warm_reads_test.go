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
	"net/http"
	"sync/atomic"
	"testing"
)

// redfishRoutes answers the handful of resources the warm-read cases touch.
func redfishRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/" + RedfishAPIs.Path("APIRootService"):
		_, _ = w.Write([]byte(`{"Product":"BlueField-3"}`))
	case "/" + RedfishAPIs.Path("APIGetSystems"):
		_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`))
	case "/" + RedfishAPIs.Path("APIGetManagers"):
		_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/Bluefield_BMC"}]}`))
	case "/redfish/v1/Systems/Bluefield":
		_, _ = w.Write([]byte(`{"PowerState":"On","BootProgress":{"OemLastState":"OSRunning"}}`))
	case "/redfish/v1/Managers/Bluefield_BMC":
		_, _ = w.Write([]byte(`{"FirmwareVersion":"BF-26.07-8"}`))
	default:
		_, _ = w.Write([]byte(`{"TaskState":"Running","PercentComplete":42}`))
	}
}

// Polling a task on a warm client must cost exactly one GET per poll: no reconnect, no root
// discovery, no collection walk. This is the caller-level count, not a provider internal.
func TestWarmTaskPollingMakesOnlyTheTaskGET(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, redfishRoutes)
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)
	taskPath := "/redfish/v1/TaskService/Tasks/42"

	const polls = 6
	for i := 0; i < polls; i++ {
		// Each iteration stands in for one reconcile: get the client, poll.
		c := acquire(t, p, key)
		if _, progress, err := c.CheckTaskProgress(context.Background(), "42"); err != nil || progress == nil {
			t.Fatalf("poll %d: %v", i, err)
		}
	}

	if got := server.count(taskPath); got != polls {
		t.Errorf("task reads = %d, want %d", got, polls)
	}
	for _, path := range []string{"/" + RedfishAPIs.Path("APIRootService"), "/" + RedfishAPIs.Path("APIGetSystems"), "/" + RedfishAPIs.Path("APIGetManagers")} {
		if got := server.count(path); got != 0 {
			t.Errorf("%s reads = %d, want 0: warm task polling must not rediscover anything", path, got)
		}
	}
	if got := server.connections(); got != 1 {
		t.Errorf("connections = %d, want 1 across %d reconciles", got, polls)
	}
}

// The same holds for writes, which use their own connection pool.
func TestUnauthorizedWriteDropsTheConnection(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	var rejected atomic.Bool
	rejected.Store(true)
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		if rejected.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	c := acquire(t, p, bf3Key(server.URL))

	if resp, err := c.Client.R().Post("/redfish/v1/write"); err != nil || resp.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("expected the 401 to surface, got %v, %v", resp, err)
	}
	rejected.Store(false)
	if resp, err := c.Client.R().Post("/redfish/v1/write"); err != nil || resp.StatusCode() != http.StatusOK {
		t.Fatalf("second write = %v, %v", resp, err)
	}
	if got := server.connections(); got != 2 {
		t.Errorf("connections = %d, want 2: the rejected write connection must not be reused", got)
	}
}

// A connection the BMC rejected with 401 is not reused: mTLS identity is decided at handshake, so
// only a new connection can pick up a trust fix on the BMC side.
func TestUnauthorizedReadDropsTheConnection(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	var rejected atomic.Bool
	rejected.Store(true)
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		if rejected.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		redfishRoutes(w, r)
	})
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	c := acquire(t, p, bf3Key(server.URL))

	if _, _, err := c.GetManagers(context.Background()); !HasHTTPStatus(err, http.StatusUnauthorized) {
		t.Fatalf("expected the 401 to surface, got %v", err)
	}
	rejected.Store(false)
	if _, _, err := c.GetManagers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := server.connections(); got != 2 {
		t.Errorf("connections = %d, want 2: the rejected connection must not be reused", got)
	}
}

// System- and Manager-scoped reads discover their collection once per cached client, not once per
// read, and a client built after a retirement rediscovers.
func TestWarmStableResourceReadsSkipRedundantCollectionDiscovery(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, redfishRoutes)
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)

	const reconciles = 5
	for i := 0; i < reconciles; i++ {
		c := acquire(t, p, key)
		if _, _, err := c.GetSystem(context.Background()); err != nil {
			t.Fatalf("system read %d: %v", i, err)
		}
		if _, _, err := c.GetBmcManager(context.Background()); err != nil {
			t.Fatalf("manager read %d: %v", i, err)
		}
	}

	if got := server.count("/" + RedfishAPIs.Path("APIGetSystems")); got != 1 {
		t.Errorf("Systems collection reads = %d, want 1 for %d reconciles", got, reconciles)
	}
	if got := server.count("/" + RedfishAPIs.Path("APIGetManagers")); got != 1 {
		t.Errorf("Managers collection reads = %d, want 1 for %d reconciles", got, reconciles)
	}
	if got := server.count("/redfish/v1/Systems/Bluefield"); got != reconciles {
		t.Errorf("System resource reads = %d, want %d: the resource itself is never cached", got, reconciles)
	}

	// Retiring the endpoint discards the memoized identifiers with the client.
	p.RetireEndpoint(key.Endpoint)
	c := acquire(t, p, key)
	if _, _, err := c.GetSystem(context.Background()); err != nil {
		t.Fatalf("system read after retirement: %v", err)
	}
	if got := server.count("/" + RedfishAPIs.Path("APIGetSystems")); got != 2 {
		t.Errorf("Systems collection reads = %d after retirement, want 2", got)
	}
}

// A 404 is affirmative evidence that a memoized identifier no longer resolves, so the next read
// rediscovers it.
func TestStaleResourceEvidenceForcesRediscovery(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	var systemMissing atomic.Bool
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		if systemMissing.Load() && r.URL.Path == "/redfish/v1/Systems/Bluefield" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		redfishRoutes(w, r)
	})
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	c := acquire(t, p, bf3Key(server.URL))

	if _, _, err := c.GetSystem(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := server.count("/" + RedfishAPIs.Path("APIGetSystems")); got != 1 {
		t.Fatalf("Systems collection reads = %d, want 1", got)
	}

	systemMissing.Store(true)
	if _, _, err := c.GetSystem(context.Background()); !HasHTTPStatus(err, http.StatusNotFound) {
		t.Fatalf("expected the 404 to surface, got %v", err)
	}
	systemMissing.Store(false)
	if _, _, err := c.GetSystem(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := server.count("/" + RedfishAPIs.Path("APIGetSystems")); got != 2 {
		t.Errorf("Systems collection reads = %d, want 2: a 404 must force rediscovery", got)
	}
}

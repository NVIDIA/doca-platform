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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/sets"
)

// attachmentPayload is the body the fixture serves for a dump attachment, and
// what each test expects to find in the downloaded archive.
const attachmentPayload = "dump-archive-bytes"

// The collector picks the newest entry by Created, so the entry the task
// produces is the newer of the two.
const (
	staleEntryCreated = "2026-01-01T00:00:00Z"
	freshEntryCreated = "2027-01-01T00:00:00Z"
)

type recordedRequest struct {
	method string
	path   string
	body   string
}

// bmcFixture serves the Redfish surface `dpfctl dump bmc` touches, shaped from
// the BF3 and BF4 surveys in hack/personal/bmc-logs.
type bmcFixture struct {
	product   string
	managerID string
	systemID  string

	mu        sync.Mutex
	requests  []recordedRequest
	collected sets.Set[string]
}

// The resource IDs are spelled out rather than taken from bmcGeneration so that
// a typo in the table is a test failure instead of being mirrored by the fixture.
// Both fixtures serve a System dump service; whether it is used is the
// generation's decision, and the BF3 test asserts it is left alone.

func bf3Fixture() *bmcFixture {
	return &bmcFixture{
		product:   "Nvidia-BMCMezz",
		managerID: "Bluefield_BMC",
		systemID:  "Bluefield",
		collected: sets.New[string](),
	}
}

func bf4Fixture() *bmcFixture {
	return &bmcFixture{
		product:   "BlueField-4",
		managerID: "BlueField_BMC_0",
		systemID:  "BlueField_0",
		collected: sets.New[string](),
	}
}

func (f *bmcFixture) managerDumpPath() string {
	return "/redfish/v1/Managers/" + f.managerID + dumpLogServicePath
}

func (f *bmcFixture) systemDumpPath() string {
	return "/redfish/v1/Systems/" + f.systemID + dumpLogServicePath
}

func (f *bmcFixture) transport() roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		body := ""
		if req.Body != nil {
			raw, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			body = string(raw)
		}

		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{method: req.Method, path: req.URL.Path, body: body})
		f.mu.Unlock()

		return f.respond(req)
	}
}

func (f *bmcFixture) respond(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	if path == rootServicePath {
		return httpResponse(req, http.StatusOK, fmt.Sprintf(`{"Product":%q}`, f.product)), nil
	}
	if strings.HasPrefix(path, "/redfish/v1/TaskService/Tasks/") {
		return httpResponse(req, http.StatusOK, `{"TaskState":"Completed"}`), nil
	}

	// Everything else has to sit under a Dump log service this BMC really
	// publishes. Matching on the whole service path rather than on a suffix is
	// what makes a wrong entry in bmcGeneration fail the test instead of being
	// answered anyway.
	service, ok := f.dumpService(path)
	if !ok {
		return httpResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
	}

	switch strings.TrimPrefix(path, service) {
	case collectDiagnosticDataPath:
		f.mu.Lock()
		f.collected.Insert(service)
		f.mu.Unlock()
		return httpResponse(req, http.StatusOK, `{"Id":"task-1"}`), nil

	case clearLogPath:
		return httpResponse(req, http.StatusOK, ""), nil

	case "/Entries":
		f.mu.Lock()
		collected := f.collected.Has(service)
		f.mu.Unlock()
		return httpResponse(req, http.StatusOK, entriesJSON(collected)), nil
	}

	if strings.HasSuffix(path, "/attachment") {
		return httpResponse(req, http.StatusOK, attachmentPayload), nil
	}
	return httpResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
}

func (f *bmcFixture) dumpService(path string) (string, bool) {
	for _, service := range []string{f.managerDumpPath(), f.systemDumpPath()} {
		if strings.HasPrefix(path, service) {
			return service, true
		}
	}
	return "", false
}

func (f *bmcFixture) requestsTo(path string) []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	var matched []recordedRequest
	for _, request := range f.requests {
		if request.path == path {
			matched = append(matched, request)
		}
	}
	return matched
}

func entriesJSON(collected bool) string {
	stale := fmt.Sprintf(`{"Id":"stale","Created":%q}`, staleEntryCreated)
	if !collected {
		return fmt.Sprintf(`{"Members":[%s]}`, stale)
	}
	fresh := fmt.Sprintf(`{"Id":"fresh","Created":%q}`, freshEntryCreated)
	return fmt.Sprintf(`{"Members":[%s,%s]}`, stale, fresh)
}

func newFixtureCollector(t *testing.T, fixture *bmcFixture, opts CollectOptions) *collector {
	t.Helper()

	return &collector{
		target:         logTarget{IP: "10.0.0.10", Port: defaultPort, Password: "password"},
		targetDir:      t.TempDir(),
		ctx:            context.Background(),
		client:         resty.New().SetBaseURL("https://10.0.0.10").SetTransport(fixture.transport()),
		opts:           opts,
		entryRetry:     3,
		entryRetryWait: time.Nanosecond,
		taskPollWait:   time.Nanosecond,
	}
}

func TestCollectOnBF4CollectsManagerAndSystemDumps(t *testing.T) {
	g := NewWithT(t)

	fixture := bf4Fixture()
	c := newFixtureCollector(t, fixture, CollectOptions{Namespace: DefaultNamespace, TaskTimeout: time.Minute})

	g.Expect(c.collect()).To(Succeed())

	for _, unit := range []string{managerUnitName, systemUnitName} {
		archive, err := os.ReadFile(filepath.Join(c.targetDir, unit, "log_dump.tar.zst"))
		g.Expect(err).NotTo(HaveOccurred(), "expected an archive for the %s dump", unit)
		g.Expect(string(archive)).To(Equal(attachmentPayload))
	}

	managerPosts := fixture.requestsTo(fixture.managerDumpPath() + "/Actions/LogService.CollectDiagnosticData")
	g.Expect(managerPosts).To(HaveLen(1))
	g.Expect(managerPosts[0].body).To(Equal(`{"DiagnosticDataType":"Manager"}`))

	systemPosts := fixture.requestsTo(fixture.systemDumpPath() + "/Actions/LogService.CollectDiagnosticData")
	g.Expect(systemPosts).To(HaveLen(1))
	g.Expect(systemPosts[0].body).To(Equal(`{"DiagnosticDataType":"OEM","OEMDiagnosticDataType":"DiagnosticType=CPUDiagnosticsData"}`))

	metadata, err := os.ReadFile(filepath.Join(c.targetDir, "metadata.txt"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(metadata)).To(ContainSubstring("Redfish user: admin"))
	g.Expect(string(metadata)).To(ContainSubstring("Selected manager dump entry fresh"))
}

func TestCollectOnBF3SkipsTheSystemDump(t *testing.T) {
	g := NewWithT(t)

	fixture := bf3Fixture()
	c := newFixtureCollector(t, fixture, CollectOptions{Namespace: DefaultNamespace, TaskTimeout: time.Minute})

	g.Expect(c.collect()).To(Succeed())

	archive, err := os.ReadFile(filepath.Join(c.targetDir, managerUnitName, "log_dump.tar.zst"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(archive)).To(Equal(attachmentPayload))

	g.Expect(filepath.Join(c.targetDir, systemUnitName)).NotTo(BeADirectory())
	// The fixture serves a BF3 System dump service, so an empty request log here
	// means the generation decided to skip it rather than the BMC refusing.
	g.Expect(fixture.requestsTo(fixture.systemDumpPath() + collectDiagnosticDataPath)).To(BeEmpty())
	g.Expect(fixture.requestsTo(fixture.systemDumpPath() + "/Entries")).To(BeEmpty())

	metadata, err := os.ReadFile(filepath.Join(c.targetDir, "metadata.txt"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(metadata)).To(ContainSubstring("Redfish user: root"))
	g.Expect(string(metadata)).To(ContainSubstring("Skipped system dump: BlueField-3"))
}

func TestCollectKeepsTheManagerDumpWhenTheSystemDumpFails(t *testing.T) {
	g := NewWithT(t)

	fixture := bf4Fixture()
	failing := fixture.transport()
	c := newFixtureCollector(t, fixture, CollectOptions{Namespace: DefaultNamespace, TaskTimeout: time.Minute})
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == fixture.systemDumpPath()+"/Actions/LogService.CollectDiagnosticData" {
			return httpResponse(req, http.StatusInternalServerError, `{"error":"dump busy"}`), nil
		}
		return failing(req)
	}))

	err := c.collect()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("creating system dump entry"))
	archive, readErr := os.ReadFile(filepath.Join(c.targetDir, managerUnitName, "log_dump.tar.zst"))
	g.Expect(readErr).NotTo(HaveOccurred())
	g.Expect(string(archive)).To(Equal(attachmentPayload))

	// The System dump failed before writing anything, so it must not leave an
	// empty directory that reads as a silent skip.
	g.Expect(filepath.Join(c.targetDir, systemUnitName)).NotTo(BeADirectory())
}

func TestResolveUsername(t *testing.T) {
	tests := []struct {
		name     string
		product  string
		override string
		want     string
	}{
		{name: "bf3 product resolves to root", product: "Nvidia-BMCMezz", want: "root"},
		{name: "bf4 product resolves to admin", product: "BlueField-4", want: "admin"},
		{name: "unknown product falls back to bf3 user", product: "", want: "root"},
		{name: "explicit override wins", product: "BlueField-4", override: "operator", want: "operator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			c := &collector{opts: CollectOptions{Username: tt.override}}

			g.Expect(c.resolveUsername(tt.product)).To(Equal(tt.want))
		})
	}
}

func TestWaitForDumpEntryPicksTheNewestByCreated(t *testing.T) {
	g := NewWithT(t)

	unit := dumpUnit{
		name:        managerUnitName,
		servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath,
	}
	entries := `{"Members":[
		{"Id":"older","Created":"2026-01-01T00:00:00Z"},
		{"Id":"newest","Created":"2027-01-01T00:00:00Z"},
		{"Id":"middle","Created":"2026-06-01T00:00:00Z"}
	]}`
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(req, http.StatusOK, entries), nil
	})

	targetDir := t.TempDir()
	c := &collector{
		target:         logTarget{IP: "10.0.0.10"},
		targetDir:      targetDir,
		ctx:            context.Background(),
		client:         resty.New().SetBaseURL("https://10.0.0.10").SetTransport(transport),
		entryRetry:     3,
		entryRetryWait: time.Nanosecond,
	}

	entryID, err := c.waitForDumpEntry(unit, targetDir)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(entryID).To(Equal("newest"))
}

func TestWaitForDumpEntryFailsWhenNoEntryEverAppears(t *testing.T) {
	g := NewWithT(t)

	unit := dumpUnit{
		name:        managerUnitName,
		servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath,
	}
	var polls int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&polls, 1)
		return httpResponse(req, http.StatusOK, `{"Members":[]}`), nil
	})

	targetDir := t.TempDir()
	c := &collector{
		target:         logTarget{IP: "10.0.0.10"},
		targetDir:      targetDir,
		ctx:            context.Background(),
		client:         resty.New().SetBaseURL("https://10.0.0.10").SetTransport(transport),
		entryRetry:     3,
		entryRetryWait: time.Nanosecond,
	}

	_, err := c.waitForDumpEntry(unit, targetDir)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no manager dump entry was found"))
	g.Expect(atomic.LoadInt32(&polls)).To(Equal(int32(3)))
}

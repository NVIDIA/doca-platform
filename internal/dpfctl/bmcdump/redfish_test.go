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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// The entry the task produces is the older of the two by Created, as after a BMC clock that
// moved backwards: the collector must still pick it.
const (
	staleEntryCreated = "2027-01-01T00:00:00Z"
	freshEntryCreated = "2026-01-01T00:00:00Z"
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

	// attachments overrides attachmentPayload per dump service, for tests that unpack the dump.
	attachments map[string][]byte

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
		if attachment, ok := f.attachments[service]; ok {
			return httpResponse(req, http.StatusOK, string(attachment)), nil
		}
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

func TestWaitForNewDumpEntryFailsWhenNoEntryEverAppears(t *testing.T) {
	g := NewWithT(t)

	unit := dumpUnit{
		name:        managerUnitName,
		servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath,
	}
	var polls int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&polls, 1)
		return httpResponse(req, http.StatusOK, `{"Members":[{"Id":"retained"}]}`), nil
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

	_, err := c.waitForNewDumpEntry(unit, sets.New("retained"), targetDir)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no new manager dump entry was found"))
	g.Expect(atomic.LoadInt32(&polls)).To(Equal(int32(3)))
}

func TestCollectFailsTheSystemDumpWhenClearingItsEntriesFails(t *testing.T) {
	g := NewWithT(t)

	fixture := bf4Fixture()
	serve := fixture.transport()
	c := newFixtureCollector(t, fixture, CollectOptions{Namespace: DefaultNamespace, TaskTimeout: time.Minute, ClearExisting: true})
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == fixture.systemDumpPath()+clearLogPath {
			return nil, io.EOF
		}
		return serve(req)
	}))

	err := c.collect()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("deleting existing system dump entries"))
	g.Expect(err.Error()).To(ContainSubstring("EOF"))

	archive, readErr := os.ReadFile(filepath.Join(c.targetDir, managerUnitName, "log_dump.tar.zst"))
	g.Expect(readErr).NotTo(HaveOccurred())
	g.Expect(string(archive)).To(Equal(attachmentPayload))

	// A dump must never be selected from entries the clear failed to remove, so
	// the unit stops before creating one and leaves no directory behind.
	g.Expect(fixture.requestsTo(fixture.systemDumpPath() + collectDiagnosticDataPath)).To(BeEmpty())
	g.Expect(filepath.Join(c.targetDir, systemUnitName)).NotTo(BeADirectory())

	metadata, readErr := os.ReadFile(filepath.Join(c.targetDir, "metadata.txt"))
	g.Expect(readErr).NotTo(HaveOccurred())
	g.Expect(string(metadata)).To(ContainSubstring("Failed system dump: "))
	g.Expect(string(metadata)).To(ContainSubstring("deleting existing system dump entries"))
	g.Expect(string(metadata)).NotTo(ContainSubstring("Skipped clearing"))
}

func TestCollectStillCollectsTheSystemDumpWhenClearingManagerEntriesFails(t *testing.T) {
	g := NewWithT(t)

	fixture := bf4Fixture()
	serve := fixture.transport()
	c := newFixtureCollector(t, fixture, CollectOptions{Namespace: DefaultNamespace, TaskTimeout: time.Minute, ClearExisting: true})
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == fixture.managerDumpPath()+clearLogPath {
			return httpResponse(req, http.StatusServiceUnavailable, `{"error":"busy"}`), nil
		}
		return serve(req)
	}))

	err := c.collect()

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("deleting existing manager dump entries"))
	g.Expect(fixture.requestsTo(fixture.managerDumpPath() + collectDiagnosticDataPath)).To(BeEmpty())
	g.Expect(filepath.Join(c.targetDir, managerUnitName)).NotTo(BeADirectory())

	archive, readErr := os.ReadFile(filepath.Join(c.targetDir, systemUnitName, "log_dump.tar.zst"))
	g.Expect(readErr).NotTo(HaveOccurred())
	g.Expect(string(archive)).To(Equal(attachmentPayload))
}

func TestNewCollectorClosesTheConnectionAfterEveryRequest(t *testing.T) {
	g := NewWithT(t)

	target := logTarget{IP: "10.0.0.10", Port: defaultPort, Password: "password"}
	opts := CollectOptions{Namespace: DefaultNamespace}.withDefaults()
	opts.caPool = testCAPool(t)
	c, cancel, err := newCollector(context.Background(), target, t.TempDir(), opts)
	g.Expect(err).NotTo(HaveOccurred())
	defer cancel()

	var closeRequested bool
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		closeRequested = req.Close
		return httpResponse(req, http.StatusOK, `{"Product":"BlueField-4"}`), nil
	}))

	_, err = c.requestJSON(http.MethodGet, rootServicePath, nil)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(closeRequested).To(BeTrue())
}

func TestCollectTakesTheEntryTheTaskNames(t *testing.T) {
	g := NewWithT(t)

	fixture := bf3Fixture()
	serve := fixture.transport()
	c := newFixtureCollector(t, fixture, CollectOptions{TaskTimeout: time.Minute})
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.Path, "/redfish/v1/TaskService/Tasks/") {
			return httpResponse(req, http.StatusOK, fmt.Sprintf(
				`{"TaskState":"Completed","Payload":{"HttpHeaders":["Location: %s/Entries/stale"]}}`, fixture.managerDumpPath())), nil
		}
		return serve(req)
	}))

	g.Expect(c.collect()).To(Succeed())

	g.Expect(fixture.requestsTo(fixture.managerDumpPath() + "/Entries/stale/attachment")).To(HaveLen(1))
	// One listing before the request and none after: the task's answer needs no second look.
	g.Expect(fixture.requestsTo(fixture.managerDumpPath() + "/Entries")).To(HaveLen(1))
}

func TestCollectStopsAtTheFirstRejectedLogin(t *testing.T) {
	g := NewWithT(t)

	fixture := bf4Fixture()
	var authenticated int32
	c := newFixtureCollector(t, fixture, CollectOptions{TaskTimeout: time.Minute, StopAt: time.Now().Add(time.Minute)})
	c.retryReads = true
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == rootServicePath {
			return httpResponse(req, http.StatusOK, `{"Product":"BlueField-4"}`), nil
		}
		atomic.AddInt32(&authenticated, 1)
		return httpResponse(req, http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
	}))

	err := c.collect()

	g.Expect(errors.Is(err, errLoginRejected)).To(BeTrue())
	// Neither a retry nor the System dump may spend a second login attempt.
	g.Expect(atomic.LoadInt32(&authenticated)).To(Equal(int32(1)))
}

func TestCollectRetriesReadsButNeverTheDumpRequest(t *testing.T) {
	g := NewWithT(t)

	fixture := bf3Fixture()
	serve := fixture.transport()
	var taskPolls, posts int32
	c := newFixtureCollector(t, fixture, CollectOptions{TaskTimeout: time.Minute})
	c.retryReads = true
	c.retryWait = time.Nanosecond
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/redfish/v1/TaskService/Tasks/") && atomic.AddInt32(&taskPolls, 1) <= 2:
			return nil, io.ErrUnexpectedEOF
		case req.Method == http.MethodPost && atomic.AddInt32(&posts, 1) == 1:
			return nil, io.ErrUnexpectedEOF
		}
		return serve(req)
	}))

	err := c.collect()

	// The reads recover; the dump request got no answer and is reported, not repeated.
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("creating manager dump entry"))
	g.Expect(atomic.LoadInt32(&posts)).To(Equal(int32(1)))

	g.Expect(c.collect()).To(Succeed())
	g.Expect(atomic.LoadInt32(&taskPolls)).To(Equal(int32(3)))
}

// A BMC that asks to be called again later is not a BMC that refused.
func TestCollectRetriesAReadTheBMCAsksToRepeat(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			g := NewWithT(t)

			fixture := bf3Fixture()
			serve := fixture.transport()
			var taskPolls int32
			c := newFixtureCollector(t, fixture, CollectOptions{TaskTimeout: time.Minute})
			c.retryReads = true
			c.retryWait = time.Nanosecond
			c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasPrefix(req.URL.Path, "/redfish/v1/TaskService/Tasks/") && atomic.AddInt32(&taskPolls, 1) == 1 {
					return &http.Response{
						StatusCode: status,
						Status:     http.StatusText(status),
						Header:     http.Header{},
						Body:       io.NopCloser(strings.NewReader("")),
						Request:    req,
					}, nil
				}
				return serve(req)
			}))

			g.Expect(c.collect()).To(Succeed())
			g.Expect(atomic.LoadInt32(&taskPolls)).To(BeNumerically(">", 1))
		})
	}
}

// Repeating a read does not make a certificate trusted, and the Job would wait out its whole time.
func TestRetryReadStopsOnAnUntrustedCertificate(t *testing.T) {
	g := NewWithT(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := &collector{ctx: ctx, retryReads: true, retryWait: time.Nanosecond}
	untrusted := fmt.Errorf("get: %w", &tls.CertificateVerificationError{Err: errors.New("unknown authority")})
	reads := 0

	err := c.retryRead(func() error {
		reads++
		return untrusted
	})

	g.Expect(err).To(MatchError(untrusted))
	g.Expect(reads).To(Equal(1))
}

// A BMC that restarts forgets its tasks: the task then answers 404 (measured on BF3 and BF4).
func TestCollectRequestsTheDumpAgainWhenItsTaskIsLost(t *testing.T) {
	lostTaskCollector := func(t *testing.T, lost int32) (*collector, *int32) {
		fixture := bf3Fixture()
		serve := fixture.transport()
		var taskPolls, posts int32
		c := newFixtureCollector(t, fixture, CollectOptions{TaskTimeout: time.Minute})
		c.retryWait = time.Nanosecond
		c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.HasPrefix(req.URL.Path, "/redfish/v1/TaskService/Tasks/"):
				if atomic.AddInt32(&taskPolls, 1) <= lost {
					return httpResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
				}
				// The task names its entry, as a real BMC does.
				return httpResponse(req, http.StatusOK, fmt.Sprintf(
					`{"TaskState":"Completed","Payload":{"HttpHeaders":["Location: %s/Entries/fresh"]}}`, fixture.managerDumpPath())), nil
			case req.Method == http.MethodPost:
				atomic.AddInt32(&posts, 1)
			}
			return serve(req)
		}))
		return c, &posts
	}

	t.Run("until the stop time, when one is set", func(t *testing.T) {
		g := NewWithT(t)
		c, posts := lostTaskCollector(t, 2)
		c.retryReads = true

		g.Expect(c.collect()).To(Succeed())
		g.Expect(atomic.LoadInt32(posts)).To(Equal(int32(3)))
	})

	t.Run("not without a stop time", func(t *testing.T) {
		g := NewWithT(t)
		c, posts := lostTaskCollector(t, 1)

		err := c.collect()
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("Not Found"))
		g.Expect(atomic.LoadInt32(posts)).To(Equal(int32(1)))
	})

	t.Run("and stops when the time is over", func(t *testing.T) {
		g := NewWithT(t)
		c, _ := lostTaskCollector(t, 1<<30)
		c.retryReads = true
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		c.ctx = ctx

		err := c.collect()
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("Not Found"))
	})

	t.Run("a limited number of times", func(t *testing.T) {
		g := NewWithT(t)
		c, posts := lostTaskCollector(t, 1<<30)
		c.retryReads = true

		err := c.collect()
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("Not Found"))
		// Every request leaves a dump entry on the BMC, so a BMC that never serves its tasks is not asked forever.
		g.Expect(atomic.LoadInt32(posts)).To(Equal(int32(maxDumpRequests)))
	})
}

func TestRedfishClientBoundsAJSONResponse(t *testing.T) {
	g := NewWithT(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"a":"` + strings.Repeat("x", maxJSONResponseBytes) + `"}`))
	}))
	defer server.Close()
	client, err := newRedfishClient(server.URL, "", CollectOptions{RequestTimeout: time.Minute, TLSConfig: insecureTLS()})
	g.Expect(err).NotTo(HaveOccurred())
	c := &collector{ctx: context.Background(), client: client}

	_, err = c.getJSON(rootServicePath)

	g.Expect(err).To(HaveOccurred())
}

func TestDownloadNeverLeavesAnOversizedOrCutOffArchive(t *testing.T) {
	unit := dumpUnit{name: managerUnitName, servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath}

	t.Run("oversized", func(t *testing.T) {
		g := NewWithT(t)
		c := newFixtureCollector(t, bf3Fixture(), CollectOptions{})
		c.maxArchiveBytes = int64(len(attachmentPayload)) - 1
		c.retryReads = true

		err := c.downloadDumpEntry(unit, c.targetDir, "fresh")

		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("larger than"))
		g.Expect(os.ReadDir(c.targetDir)).To(BeEmpty())
	})

	t.Run("cut off, then complete", func(t *testing.T) {
		g := NewWithT(t)
		fixture := bf3Fixture()
		serve := fixture.transport()
		var downloads int32
		c := newFixtureCollector(t, fixture, CollectOptions{})
		c.retryReads = true
		c.retryWait = time.Nanosecond
		c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if atomic.AddInt32(&downloads, 1) == 1 {
				resp := httpResponse(req, http.StatusOK, "")
				resp.Body = io.NopCloser(io.MultiReader(strings.NewReader("dump-"), errReader{}))
				return resp, nil
			}
			return serve(req)
		}))

		g.Expect(c.downloadDumpEntry(unit, c.targetDir, "fresh")).To(Succeed())

		entries, err := os.ReadDir(c.targetDir)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(entries).To(HaveLen(1))
		archive, err := os.ReadFile(filepath.Join(c.targetDir, dumpArchiveName))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(archive)).To(Equal(attachmentPayload))
	})
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// fixtureServer serves the fixture over TLS, for tests that go through the real client.
func fixtureServer(t *testing.T, fixture *bmcFixture) *httptest.Server {
	t.Helper()
	serve := fixture.transport()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := serve(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(server.Close)
	return server
}

func insecureTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test server with a throwaway certificate
}

func TestRedfishClientDoesNotFollowRedirects(t *testing.T) {
	g := NewWithT(t)

	var followed int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			atomic.AddInt32(&followed, 1)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()

	client, err := newRedfishClient(server.URL, "", CollectOptions{RequestTimeout: time.Minute, TLSConfig: insecureTLS()})
	g.Expect(err).NotTo(HaveOccurred())
	c := &collector{ctx: context.Background(), client: client, retryReads: true, retryWait: time.Nanosecond}

	_, err = c.getJSON(rootServicePath)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("redirect"))
	g.Expect(atomic.LoadInt32(&followed)).To(BeZero())
}

func TestCollectFromOneBMCAddressArchivesWithoutAKubernetesClient(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes)
	server := fixtureServer(t, bf4Fixture())
	passwordFile := filepath.Join(t.TempDir(), "password")
	g.Expect(os.WriteFile(passwordFile, []byte("password\n"), 0600)).To(Succeed())
	archiveDir := t.TempDir()
	serialDir := filepath.Join(archiveDir, "MT2616606M28")
	for _, old := range []string{"20260101T000000Z-a", "20260102T000000Z-b", "20260103T000000Z-c"} {
		runDirWithDump(t, serialDir, old)
	}

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   strings.TrimPrefix(server.URL, "https://"),
		PasswordFile: passwordFile,
		TLSConfig:    insecureTLS(),
		ArchiveDir:   archiveDir,
		Serial:       "MT2616606M28",
		DPUUID:       "uid-1",
		StopAt:       time.Now().Add(time.Minute),
		Quiet:        true,
	})

	g.Expect(err).NotTo(HaveOccurred())
	runs, err := os.ReadDir(serialDir)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(runs).To(HaveLen(3))
	g.Expect(runs[0].Name()).To(Equal("20260102T000000Z-b"))
	g.Expect(runs[2].Name()).To(HaveSuffix("-uid-1"))
	for _, unit := range []string{managerUnitName, systemUnitName} {
		g.Expect(filepath.Join(serialDir, runs[2].Name(), unit, dumpArchiveName)).To(BeARegularFile())
	}
}

func TestCollectArchiveLeavesNothingWhenNoDumpWasStored(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	passwordFile := filepath.Join(t.TempDir(), "password")
	g.Expect(os.WriteFile(passwordFile, []byte("password"), 0600)).To(Succeed())
	archiveDir := t.TempDir()

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   strings.TrimPrefix(server.URL, "https://"),
		PasswordFile: passwordFile,
		TLSConfig:    insecureTLS(),
		ArchiveDir:   archiveDir,
		Serial:       "MT2616606M28",
		Quiet:        true,
	})

	g.Expect(err).To(HaveOccurred())
	g.Expect(os.ReadDir(filepath.Join(archiveDir, "MT2616606M28"))).To(BeEmpty())
}

func TestCollectOptionsValidate(t *testing.T) {
	direct := CollectOptions{BMCAddress: "10.0.0.10", PasswordFile: "p", CACertFile: "ca"}
	archived := direct
	archived.ArchiveDir, archived.Serial = "/archive", "MT2616606M28"
	with := func(o CollectOptions, change func(*CollectOptions)) CollectOptions {
		change(&o)
		return o
	}
	tests := []struct {
		name    string
		opts    CollectOptions
		wantErr string
	}{
		{
			name: "discovery",
			opts: CollectOptions{},
		},
		{
			name: "one bmc",
			opts: direct,
		},
		{
			name: "one bmc, archived",
			opts: archived,
		},
		{
			name:    "no password file",
			opts:    with(direct, func(o *CollectOptions) { o.PasswordFile = "" }),
			wantErr: "password file",
		},
		{
			name:    "no ca file",
			opts:    with(direct, func(o *CollectOptions) { o.CACertFile = "" }),
			wantErr: "CA certificate file",
		},
		{
			name:    "password file without an address",
			opts:    CollectOptions{PasswordFile: "p"},
			wantErr: "a password file requires a bmc address",
		},
		{
			name:    "serial without an address",
			opts:    CollectOptions{Serial: "s"},
			wantErr: "a serial requires a bmc address",
		},
		{
			name:    "header without printing the logs",
			opts:    with(direct, func(o *CollectOptions) { o.Header = map[string]string{"dpu": "dpu-1"} }),
			wantErr: "header fields require printing the logs",
		},
		{
			name:    "dpu uid without an archive directory",
			opts:    with(direct, func(o *CollectOptions) { o.DPUUID = "uid-1" }),
			wantErr: "a DPU UID requires an archive directory",
		},
		{
			name:    "archive without an address",
			opts:    CollectOptions{ArchiveDir: "/archive", Serial: "s"},
			wantErr: "requires a bmc address",
		},
		{
			name:    "archive without a serial",
			opts:    with(archived, func(o *CollectOptions) { o.Serial = "" }),
			wantErr: "requires a serial",
		},
		{
			name:    "serial that leaves the directory",
			opts:    with(archived, func(o *CollectOptions) { o.Serial = "../bfb" }),
			wantErr: "cannot be used in an archive path",
		},
		{
			name:    "dpu uid with a separator",
			opts:    with(archived, func(o *CollectOptions) { o.DPUUID = "a/b" }),
			wantErr: "cannot be used in an archive path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.opts.validate()

			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
				return
			}
			g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
		})
	}
}

// stubFreeBytes keeps archive tests independent of the disk they run on.
func stubFreeBytes(t *testing.T, free uint64) {
	t.Helper()
	original := freeBytes
	t.Cleanup(func() { freeBytes = original })
	freeBytes = func(string) (uint64, error) { return free, nil }
}

func TestNewArchiveRunDirRefusesALowVolume(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes-1)

	_, err := newArchiveRunDir(CollectOptions{ArchiveDir: t.TempDir(), Serial: "s"})

	g.Expect(err).To(MatchError(ContainSubstring("below the")))
}

func TestDownloadThatStaysCutOffLeavesNothing(t *testing.T) {
	g := NewWithT(t)

	unit := dumpUnit{name: managerUnitName, servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath}
	c := newFixtureCollector(t, bf3Fixture(), CollectOptions{})
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := httpResponse(req, http.StatusOK, "")
		resp.Body = io.NopCloser(io.MultiReader(strings.NewReader("dump-"), errReader{}))
		return resp, nil
	}))

	g.Expect(c.downloadDumpEntry(unit, c.targetDir, "fresh")).NotTo(Succeed())

	g.Expect(os.ReadDir(c.targetDir)).To(BeEmpty())
}

func TestCollectEndsAtTheStopTime(t *testing.T) {
	g := NewWithT(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	passwordFile := filepath.Join(t.TempDir(), "password")
	g.Expect(os.WriteFile(passwordFile, []byte("password"), 0600)).To(Succeed())

	started := time.Now()
	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   strings.TrimPrefix(server.URL, "https://"),
		PasswordFile: passwordFile,
		TLSConfig:    insecureTLS(),
		OutputDir:    t.TempDir(),
		StopAt:       time.Now().Add(200 * time.Millisecond),
		Quiet:        true,
	})

	// The BMC never recovers: the reads are retried, and the stop time is what ends the run.
	g.Expect(err).To(MatchError(ContainSubstring("Service Unavailable")))
	g.Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
}

func TestResolveTargetsReadsThePasswordFileWithoutItsNewline(t *testing.T) {
	g := NewWithT(t)

	passwordFile := filepath.Join(t.TempDir(), "password")
	g.Expect(os.WriteFile(passwordFile, []byte("pass word\n"), 0600)).To(Succeed())

	targets, err := resolveTargets(context.Background(), nil, CollectOptions{BMCAddress: "10.0.0.10:8443", PasswordFile: passwordFile})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(targets).To(HaveLen(1))
	// The address and the port are kept apart, as for a discovered BMC, so what is saved about the
	// run names the port that was used.
	g.Expect(targets[0].IP).To(Equal("10.0.0.10"))
	g.Expect(targets[0].Port).To(Equal(uint32(8443)))
	g.Expect(baseURL(targets[0])).To(Equal("https://10.0.0.10:8443"))
	g.Expect(targets[0].Password).To(Equal("pass word"))
}

// readerFunc lets a test look at the disk while a download is still streaming.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestDownloadHasNoArchiveUnderItsFinalNameUntilComplete(t *testing.T) {
	g := NewWithT(t)

	unit := dumpUnit{name: managerUnitName, servicePath: "/redfish/v1/Managers/Bluefield_BMC" + dumpLogServicePath}
	c := newFixtureCollector(t, bf3Fixture(), CollectOptions{})
	final := filepath.Join(c.targetDir, dumpArchiveName)
	// A Job killed at this point runs no cleanup, so what is on disk now is what would stay.
	var finalExistedMidDownload bool
	c.client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := httpResponse(req, http.StatusOK, "")
		resp.Body = io.NopCloser(io.MultiReader(strings.NewReader("dump-"), readerFunc(func([]byte) (int, error) {
			_, err := os.Stat(final)
			finalExistedMidDownload = err == nil
			return 0, io.EOF
		})))
		return resp, nil
	}))

	g.Expect(c.downloadDumpEntry(unit, c.targetDir, "fresh")).To(Succeed())

	g.Expect(finalExistedMidDownload).To(BeFalse())
	g.Expect(final).To(BeARegularFile())
}

func TestNewArchiveRunDirUsesTheConfiguredMinimum(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, 300<<20)
	opts := CollectOptions{ArchiveDir: t.TempDir(), Serial: "s"}

	_, err := newArchiveRunDir(opts)
	g.Expect(err).To(MatchError(ContainSubstring("below the")), "the default minimum is 2 GB")

	opts.ArchiveMinFreeBytes = 200 << 20
	_, err = newArchiveRunDir(opts)
	g.Expect(err).NotTo(HaveOccurred())
}

// With a stop time the reads are repeated until then, so the budget of one target must not end them sooner.
func TestNewCollectorKeepsTheStopTimeAsItsDeadline(t *testing.T) {
	g := NewWithT(t)

	stopAt := time.Now().Add(6 * time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), stopAt)
	defer cancel()
	target := logTarget{IP: "10.0.0.10", Port: defaultPort, Password: "password"}
	opts := CollectOptions{Namespace: DefaultNamespace, TaskTimeout: time.Minute}.withDefaults()
	opts.caPool = testCAPool(t)

	c, cancelTarget, err := newCollector(ctx, target, t.TempDir(), opts)
	g.Expect(err).NotTo(HaveOccurred())
	deadline, _ := c.ctx.Deadline()
	cancelTarget()
	g.Expect(deadline).To(BeTemporally("<", time.Now().Add(time.Hour)), "without a stop time the target has its budget")

	opts.StopAt = stopAt
	c, cancelTarget, err = newCollector(ctx, target, t.TempDir(), opts)
	g.Expect(err).NotTo(HaveOccurred())
	defer cancelTarget()
	deadline, _ = c.ctx.Deadline()
	g.Expect(deadline).To(BeTemporally("~", stopAt, time.Second))
}

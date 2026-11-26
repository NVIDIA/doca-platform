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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl/util"
	redfishclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"github.com/go-resty/resty/v2"
	"k8s.io/apimachinery/pkg/util/sets"
)

// errLoginRejected stops the whole target, not just one dump: the BMC locks the account after
// repeated failures, and provisioning logs in with the same account.
var errLoginRejected = errors.New("the BMC rejected the login")

// permanentError marks a failure that a repeated read cannot fix.
type permanentError struct{ err error }

// Error returns the message of the wrapped failure.
func (e permanentError) Error() string { return e.err.Error() }

// Unwrap returns the wrapped failure.
func (e permanentError) Unwrap() error { return e.err }

const (
	// maxDumpRequests is how often one dump is requested when the BMC keeps losing its task.
	maxDumpRequests = 3
	// maxJSONResponseBytes bounds a Redfish JSON answer. The largest measured is an entry list of a
	// few kilobytes. Dump downloads have their own limit.
	maxJSONResponseBytes = 4 << 20
)

// notFoundError is a 404 answer.
type notFoundError struct{ error }

type collector struct {
	target         logTarget
	targetDir      string
	ctx            context.Context
	client         *resty.Client
	opts           CollectOptions
	entryRetry     int
	entryRetryWait time.Duration
	taskPollWait   time.Duration
	notes          []string

	// retryReads repeats failed read requests until the context ends. It is on only when the caller
	// set a stop time; a run by hand keeps failing fast.
	retryReads bool
	retryWait  time.Duration
	// maxArchiveBytes overrides maxDumpArchiveBytes in tests.
	maxArchiveBytes int64
}

func collectDump(ctx context.Context, target logTarget, outputDir string, opts CollectOptions) error {
	c, cancel, err := newCollector(ctx, target, outputDir, opts)
	if err != nil {
		return err
	}
	defer cancel()
	return c.collect()
}

func collectDumpWithOutput(ctx context.Context, target logTarget, outputDir string, opts CollectOptions) error {
	if opts.Quiet {
		return collectDump(ctx, target, outputDir, opts)
	}

	targetDir := targetDirFor(outputDir, target, opts)
	stopSpinner := util.StartSpinner("Collecting BMC dump from %s...", target.IP)
	err := collectDump(ctx, target, outputDir, opts)
	stopSpinner()
	if err != nil {
		util.Failure("%s: %v", target.IP, err)
		// The BMC serves its factory certificate until its DPUDevice finishes initializing.
		if redfishclient.IsBMCServerCertUntrusted(err) {
			util.Warn("%s: the BMC is not serving a certificate issued by the DPF CA; check the %s condition on its DPUDevice",
				target.IP, provisioningv1.ConditionDpuDeviceBMCServerCertificateReady)
		}
		return err
	}
	util.Success("%s -> %s", target.IP, targetDir)
	return nil
}

func newCollector(ctx context.Context, target logTarget, outputDir string, opts CollectOptions) (*collector, context.CancelFunc, error) {
	targetDir := targetDirFor(outputDir, target, opts)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("creating bmc target artifact directory %s: %w", targetDir, err)
	}

	if err := writeMetadata(target, targetDir, opts.Namespace); err != nil {
		return nil, nil, err
	}

	baseURL := baseURL(target)
	dialed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, nil, err
	}

	// Pin the identity to the host actually dialed.
	redfishClient, err := newRedfishClient(baseURL, dialed.Hostname(), opts)
	if err != nil {
		return nil, nil, fmt.Errorf("configuring bmc dump client for %s: %w", target.IP, err)
	}

	// Budget for every dump this target can yield. Each dump is a separate
	// sequential Redfish task bounded by TaskTimeout, so a context sized for one
	// would expire partway through the second.
	// With a stop time the reads are repeated until then, and ctx already ends there.
	targetCtx, cancel := context.WithCancel(ctx)
	if opts.StopAt.IsZero() {
		budget := maxDumpUnits*(opts.TaskTimeout+defaultEntryRetryCount*defaultEntryRetryInterval) + time.Minute
		targetCtx, cancel = context.WithTimeout(ctx, budget)
	}

	c := &collector{
		target:         target,
		targetDir:      targetDir,
		ctx:            targetCtx,
		client:         redfishClient,
		opts:           opts,
		entryRetry:     defaultEntryRetryCount,
		entryRetryWait: defaultEntryRetryInterval,
		taskPollWait:   defaultTaskPollInterval,
		retryReads:     !opts.StopAt.IsZero(),
		retryWait:      defaultReadRetryInterval,
	}
	return c, cancel, nil
}

// newRedfishClient builds the client for one BMC. Credentials are attached in
// discover(), once the root service has told us which username this BMC
// generation uses.
//
// Unless the caller supplied opts.TLSConfig, which is cloned and used as is, the BMC server
// certificate is verified against the DPF CA and its identity is pinned to host, using the
// provisioning controller's TLS settings (BlueField certificates carry no subjectAltName).
//
// Redirects are refused: the BMC is where the dump is, and it must not send the credentials or the
// download anywhere else.
//
// Every request closes its connection. BF4 BMCs close their end after
// streaming a dump attachment, and a POST reused on that pooled connection
// fails with EOF, which Go never retries. A dump is a few dozen requests, so
// the extra handshakes are noise next to the dump task itself.
func newRedfishClient(baseURL, host string, opts CollectOptions) (*resty.Client, error) {
	var tlsConfig *tls.Config
	if opts.TLSConfig != nil {
		tlsConfig = opts.TLSConfig.Clone() // one config serves every target; keep each client's own
	} else {
		var err error
		if tlsConfig, err = redfishclient.NewVerifiedServerTLSConfig(opts.caPool, host); err != nil {
			return nil, err
		}
	}
	redfishClient := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(opts.RequestTimeout).
		SetCloseConnection(true).
		SetResponseBodyLimit(maxJSONResponseBytes).
		SetRedirectPolicy(resty.RedirectPolicyFunc(func(*http.Request, []*http.Request) error {
			return permanentError{errors.New("the BMC answered with a redirect, which is not followed")}
		})).
		SetTLSClientConfig(tlsConfig)

	return redfishClient, nil
}

func (c *collector) collect() (err error) {
	defer func() {
		err = errors.Join(err, appendMetadata(c.targetDir, c.notes))
	}()

	discovered, err := c.discover()
	if err != nil {
		return c.fail(err)
	}

	var errs []error
	for _, unit := range discovered.units {
		if unitErr := c.collectDump(unit); unitErr != nil {
			c.note("Failed %s dump: %v", unit.name, unitErr)
			errs = append(errs, unitErr)
			if errors.Is(unitErr, errLoginRejected) {
				break
			}
		}
	}
	return errors.Join(errs...)
}

// collectDump runs one CollectDiagnosticData job end to end and writes its
// artifacts under <targetDir>/<unit name>/.
func (c *collector) collectDump(unit dumpUnit) error {
	// The directory is left to the first artifact written into it, so a unit that
	// fails before producing anything leaves no empty directory behind to be
	// mistaken for a silent skip.
	unitDir := filepath.Join(c.targetDir, unit.name)

	if c.opts.ClearExisting {
		if err := c.clearDumpEntries(unit); err != nil {
			return err
		}
	}

	var before sets.Set[string]
	var task map[string]interface{}
	for requests := 1; ; requests++ {
		// Listed before the request: if the task does not name its entry, the new one is the one
		// that was not there before.
		entries, err := c.getJSON(unit.entriesPath())
		if err != nil {
			return c.fail(fmt.Errorf("querying %s dump entries: %w", unit.name, err))
		}
		before = dumpEntryIDs(entries)

		taskID, err := c.createDumpEntry(unit, unitDir)
		if err != nil {
			return err
		}
		task, err = c.waitForDumpTask(unit, taskID, unitDir)
		if err == nil {
			break
		}
		// A BMC that restarts forgets its tasks, and the dump with them (measured on BF3 and BF4),
		// so the dump is requested again. Only with a stop time, like the other retries, and a
		// limited number of times, because every request leaves a dump entry on the BMC.
		var lost notFoundError
		if !c.retryReads || requests >= maxDumpRequests || !errors.As(err, &lost) || waitOrDone(c.ctx, c.retryWait) != nil {
			return err
		}
		c.note("The BMC lost %s dump task %s; requesting the dump again", unit.name, taskID)
	}
	entryID := taskDumpEntryID(unit, task)
	var err error
	if entryID == "" {
		if entryID, err = c.waitForNewDumpEntry(unit, before, unitDir); err != nil {
			return err
		}
	}
	c.note("Selected %s dump entry %s", unit.name, entryID)
	return c.downloadDumpEntry(unit, unitDir, entryID)
}

// clearDumpEntries fails the unit when the BMC does not clear its entries, so a
// caller that asked for an empty service never gets a dump next to retained ones.
func (c *collector) clearDumpEntries(unit dumpUnit) error {
	if _, err := c.requestJSON(http.MethodPost, unit.clearTarget(), nil); err != nil {
		return c.fail(fmt.Errorf("deleting existing %s dump entries: %w", unit.name, err))
	}
	return nil
}

func (c *collector) createDumpEntry(unit dumpUnit, unitDir string) (string, error) {
	createResponse, err := c.requestJSON(http.MethodPost, unit.collectTarget(), []byte(unit.requestBody))
	if err != nil {
		return "", c.fail(fmt.Errorf("creating %s dump entry: %w", unit.name, err))
	}
	if err := writeJSONArtifact(filepath.Join(unitDir, "create-dump-task.json"), createResponse); err != nil {
		return "", c.fail(err)
	}
	taskID := redfishString(createResponse["Id"])
	if taskID == "" {
		return "", c.fail(fmt.Errorf("create %s dump response does not contain an Id field", unit.name))
	}
	return taskID, nil
}

// waitForDumpTask polls a dump task until it completes, fails or exceeds the task timeout, and
// returns the completed task.
func (c *collector) waitForDumpTask(unit dumpUnit, taskID, unitDir string) (map[string]interface{}, error) {
	taskPath := "/redfish/v1/TaskService/Tasks/" + url.PathEscape(taskID)
	deadline := time.Now().Add(c.opts.TaskTimeout)
	for {
		taskResponse, err := c.getJSON(taskPath)
		if err != nil {
			return nil, c.fail(fmt.Errorf("querying %s dump task %s: %w", unit.name, taskID, err))
		}

		switch taskState := redfishString(taskResponse["TaskState"]); taskState {
		case "Completed":
			if err := writeJSONArtifact(filepath.Join(unitDir, "task-final.json"), taskResponse); err != nil {
				return nil, c.fail(err)
			}
			return taskResponse, nil
		case "Cancelled", "Exception", "Interrupted", "Killed": //nolint:misspell // Redfish TaskState uses this spelling.
			// Best-effort persist the final task response; it usually carries
			// Messages/PercentComplete/extended error details needed for debugging.
			_ = writeJSONArtifact(filepath.Join(unitDir, "task-final.json"), taskResponse)
			return nil, c.fail(fmt.Errorf("%s dump task %s ended in state %s", unit.name, taskID, taskState))
		case "":
			return nil, c.fail(fmt.Errorf("%s dump task %s response does not contain TaskState", unit.name, taskID))
		}

		if time.Now().After(deadline) {
			return nil, c.fail(fmt.Errorf("%s dump task %s did not complete within %s", unit.name, taskID, c.opts.TaskTimeout))
		}
		if err := waitOrDone(c.ctx, c.taskPollWait); err != nil {
			return nil, c.fail(err)
		}
	}
}

// waitForNewDumpEntry is the fallback for a task that does not name its entry: it returns the one
// entry that was not listed before the dump was requested. Entries are never compared by their
// Created time, which a BMC clock that moved backwards would get wrong.
func (c *collector) waitForNewDumpEntry(unit dumpUnit, before sets.Set[string], unitDir string) (string, error) {
	for attempt := 1; attempt <= c.entryRetry; attempt++ {
		entriesResponse, err := c.getJSON(unit.entriesPath())
		if err != nil {
			return "", c.fail(fmt.Errorf("querying %s dump entries: %w", unit.name, err))
		}
		added := sets.List(dumpEntryIDs(entriesResponse).Difference(before))
		if len(added) > 1 {
			return "", c.fail(fmt.Errorf("%d new %s dump entries appeared (%s) and the task names none of them",
				len(added), unit.name, strings.Join(added, ", ")))
		}
		if len(added) == 1 {
			if err := writeJSONArtifact(filepath.Join(unitDir, "dump-entries.json"), entriesResponse); err != nil {
				return "", c.fail(err)
			}
			return added[0], nil
		}
		if err := waitOrDone(c.ctx, c.entryRetryWait); err != nil {
			return "", c.fail(err)
		}
	}
	return "", c.fail(fmt.Errorf("no new %s dump entry was found after the task completed", unit.name))
}

// downloadDumpEntry stores the archive under its final name only once it is complete and within
// the size limit, so a cut-off or oversized download is never left behind as a dump.
func (c *collector) downloadDumpEntry(unit dumpUnit, unitDir, entryID string) error {
	path := unit.entriesPath() + "/" + url.PathEscape(entryID) + "/attachment"
	var size int64
	err := c.retryRead(func() error {
		var err error
		size, err = c.downloadOnce(unit, path, filepath.Join(unitDir, dumpArchiveName))
		return err
	})
	if err != nil {
		return c.fail(err)
	}
	c.note("Wrote %s dump: %d bytes", unit.name, size)
	return nil
}

// downloadOnce downloads one dump archive to outputFile in a single attempt and returns its size.
func (c *collector) downloadOnce(unit dumpUnit, path, outputFile string) (size int64, err error) {
	limit := c.maxArchiveBytes
	if limit == 0 {
		limit = maxDumpArchiveBytes
	}
	tooLarge := permanentError{fmt.Errorf("%s dump is larger than the %d byte limit and was not downloaded", unit.name, limit)}

	resp, err := c.client.R().SetContext(c.ctx).SetDoNotParseResponse(true).Get(path)
	if err != nil {
		return 0, err
	}
	body := resp.RawBody()
	defer func() { _ = body.Close() }()
	if resp.IsError() {
		return 0, httpError(http.MethodGet, path, resp, "")
	}
	if resp.RawResponse.ContentLength > limit {
		return 0, tooLarge
	}

	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		return 0, permanentError{err}
	}
	partial := outputFile + ".partial"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return 0, permanentError{err}
	}
	defer func() {
		if err != nil {
			_ = os.Remove(partial)
		}
	}()
	size, err = io.Copy(file, io.LimitReader(body, limit+1))
	if err = errors.Join(err, file.Close()); err != nil {
		return 0, err
	}
	if size > limit {
		return 0, tooLarge
	}
	if size == 0 {
		return 0, permanentError{fmt.Errorf("downloaded %s dump is empty", unit.name)}
	}
	if err = os.Rename(partial, outputFile); err != nil {
		return 0, permanentError{err}
	}
	return size, nil
}

func (c *collector) requestJSON(method, path string, data []byte) (map[string]interface{}, error) {
	req := c.client.R().SetContext(c.ctx)
	if data != nil {
		req.SetHeader("Content-Type", "application/json").SetBody(data)
	}

	var resp *resty.Response
	var err error
	switch method {
	case http.MethodGet:
		resp, err = req.Get(path)
	case http.MethodPost:
		resp, err = req.Post(path)
	default:
		return nil, fmt.Errorf("unsupported bmc dump request method %s", method)
	}
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, httpError(method, path, resp, string(resp.Body()))
	}

	obj := map[string]interface{}{}
	if len(bytes.TrimSpace(resp.Body())) == 0 {
		return obj, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, fmt.Errorf("decoding JSON from %s: %w", path, err)
	}
	return obj, nil
}

// httpError reports an HTTP error status. A read is worth repeating after a server error, and when
// the BMC asks for it: a request timeout or too many requests.
func httpError(method, path string, resp *resty.Response, body string) error {
	err := fmt.Errorf("%s %s failed with %s: %s", strings.ToLower(method), path, resp.Status(), body)
	switch {
	case resp.StatusCode() == http.StatusUnauthorized:
		return permanentError{fmt.Errorf("%w: %w", errLoginRejected, err)}
	case resp.StatusCode() == http.StatusNotFound:
		return permanentError{notFoundError{err}}
	case resp.StatusCode() == http.StatusRequestTimeout, resp.StatusCode() == http.StatusTooManyRequests:
		return err
	case resp.StatusCode() < http.StatusInternalServerError:
		return permanentError{err}
	}
	return err
}

// getJSON is requestJSON for reads, which are safe to repeat. The dump request is never sent
// through it: repeating that one adds a dump entry on the BMC.
func (c *collector) getJSON(path string) (map[string]interface{}, error) {
	var obj map[string]interface{}
	err := c.retryRead(func() error {
		var err error
		obj, err = c.requestJSON(http.MethodGet, path, nil)
		return err
	})
	return obj, err
}

// retryRead repeats a read until it succeeds, fails permanently or the collection is out of time.
// Without retryReads it reads once.
func (c *collector) retryRead(read func() error) error {
	for {
		err := read()
		var permanent permanentError
		// A certificate that is not trusted stays so: the CA is read once.
		var untrusted *tls.CertificateVerificationError
		if err == nil || !c.retryReads || errors.As(err, &permanent) || errors.As(err, &untrusted) || c.ctx.Err() != nil {
			return err
		}
		if waitOrDone(c.ctx, c.retryWait) != nil {
			return err
		}
	}
}

func (c *collector) note(format string, args ...interface{}) {
	c.notes = append(c.notes, fmt.Sprintf(format, args...))
}

func (c *collector) fail(err error) error {
	return fmt.Errorf("collecting bmc dump from %s: %w", c.target.IP, err)
}

// taskDumpEntryID returns the dump entry a completed task names in the Location header of its
// payload, or "" when it names none under this unit's dump service.
func taskDumpEntryID(unit dumpUnit, task map[string]interface{}) string {
	payload, _ := task["Payload"].(map[string]interface{})
	headers, _ := payload["HttpHeaders"].([]interface{})
	prefix := "Location: " + unit.entriesPath() + "/"
	for _, header := range headers {
		text, _ := header.(string)
		if id, ok := strings.CutPrefix(text, prefix); ok && validDumpEntryID(id) {
			return id
		}
	}
	return ""
}

// dumpEntryIDs returns the usable IDs of the entries in a dump entry list.
func dumpEntryIDs(entries map[string]interface{}) sets.Set[string] {
	ids := sets.New[string]()
	members, _ := entries["Members"].([]interface{})
	for _, member := range members {
		entry, _ := member.(map[string]interface{})
		if id := redfishString(entry["Id"]); validDumpEntryID(id) {
			ids.Insert(id)
		}
	}
	return ids
}

// validDumpEntryID accepts only a single path segment, because the ID comes from the BMC and is
// used to build the download path.
func validDumpEntryID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/?#")
}

func redfishString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func waitOrDone(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

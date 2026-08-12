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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/util"

	"github.com/go-resty/resty/v2"
)

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

	targetDir := filepath.Join(outputDir, artifactTargetName(target))
	stopSpinner := util.StartSpinner("Collecting BMC dump from %s...", target.IP)
	err := collectDump(ctx, target, outputDir, opts)
	stopSpinner()
	if err != nil {
		util.Failure("%s: %v", target.IP, err)
		return err
	}
	util.Success("%s -> %s", target.IP, targetDir)
	return nil
}

func newCollector(ctx context.Context, target logTarget, outputDir string, opts CollectOptions) (*collector, context.CancelFunc, error) {
	targetDir := filepath.Join(outputDir, artifactTargetName(target))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("creating bmc target artifact directory %s: %w", targetDir, err)
	}

	if err := writeMetadata(target, targetDir, opts.Namespace); err != nil {
		return nil, nil, err
	}

	baseURL := baseURL(target)
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, nil, err
	}

	// Budget for every dump this target can yield. Each dump is a separate
	// sequential Redfish task bounded by TaskTimeout, so a context sized for one
	// would expire partway through the second.
	budget := maxDumpUnits*(opts.TaskTimeout+defaultEntryRetryCount*defaultEntryRetryInterval) + time.Minute
	targetCtx, cancel := context.WithTimeout(ctx, budget)

	// Credentials are attached in discover(), once the root service has told us
	// which username this BMC generation uses.
	redfishClient := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(opts.RequestTimeout)
	if opts.InsecureSkipTLSVerify {
		redfishClient.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // Explicit user opt-in for lab/self-signed BMCs.
	}

	return &collector{
		target:         target,
		targetDir:      targetDir,
		ctx:            targetCtx,
		client:         redfishClient,
		opts:           opts,
		entryRetry:     defaultEntryRetryCount,
		entryRetryWait: defaultEntryRetryInterval,
		taskPollWait:   defaultTaskPollInterval,
	}, cancel, nil
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

	taskID, err := c.createDumpEntry(unit, unitDir)
	if err != nil {
		return err
	}
	if err := c.waitForDumpTask(unit, taskID, unitDir); err != nil {
		return err
	}
	entryID, err := c.waitForDumpEntry(unit, unitDir)
	if err != nil {
		return err
	}
	return c.downloadDumpEntry(unit, unitDir, entryID)
}

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

func (c *collector) waitForDumpTask(unit dumpUnit, taskID, unitDir string) error {
	taskPath := "/redfish/v1/TaskService/Tasks/" + url.PathEscape(taskID)
	deadline := time.Now().Add(c.opts.TaskTimeout)
	for {
		taskResponse, err := c.requestJSON(http.MethodGet, taskPath, nil)
		if err != nil {
			return c.fail(fmt.Errorf("querying %s dump task %s: %w", unit.name, taskID, err))
		}

		switch taskState := redfishString(taskResponse["TaskState"]); taskState {
		case "Completed":
			if err := writeJSONArtifact(filepath.Join(unitDir, "task-final.json"), taskResponse); err != nil {
				return c.fail(err)
			}
			return nil
		case "Cancelled", "Exception", "Interrupted", "Killed": //nolint:misspell // Redfish TaskState uses this spelling.
			// Best-effort persist the final task response; it usually carries
			// Messages/PercentComplete/extended error details needed for debugging.
			_ = writeJSONArtifact(filepath.Join(unitDir, "task-final.json"), taskResponse)
			return c.fail(fmt.Errorf("%s dump task %s ended in state %s", unit.name, taskID, taskState))
		case "":
			return c.fail(fmt.Errorf("%s dump task %s response does not contain TaskState", unit.name, taskID))
		}

		if time.Now().After(deadline) {
			return c.fail(fmt.Errorf("%s dump task %s did not complete within %s", unit.name, taskID, c.opts.TaskTimeout))
		}
		if err := waitOrDone(c.ctx, c.taskPollWait); err != nil {
			return c.fail(err)
		}
	}
}

func (c *collector) waitForDumpEntry(unit dumpUnit, unitDir string) (string, error) {
	for attempt := 1; attempt <= c.entryRetry; attempt++ {
		entriesResponse, err := c.requestJSON(http.MethodGet, unit.entriesPath(), nil)
		if err != nil {
			return "", c.fail(fmt.Errorf("querying %s dump entries: %w", unit.name, err))
		}
		entryID := latestDumpEntryID(entriesResponse)
		if entryID != "" {
			if err := writeJSONArtifact(filepath.Join(unitDir, "dump-entries.json"), entriesResponse); err != nil {
				return "", c.fail(err)
			}
			c.note("Selected %s dump entry %s", unit.name, entryID)
			return entryID, nil
		}
		if err := waitOrDone(c.ctx, c.entryRetryWait); err != nil {
			return "", c.fail(err)
		}
	}
	return "", c.fail(fmt.Errorf("no %s dump entry was found after the task completed", unit.name))
}

func (c *collector) downloadDumpEntry(unit dumpUnit, unitDir, entryID string) error {
	outputFile := filepath.Join(unitDir, dumpArchiveName)
	resp, err := c.client.R().
		SetContext(c.ctx).
		SetOutput(outputFile).
		Get(unit.entriesPath() + "/" + url.PathEscape(entryID) + "/attachment")
	if err != nil {
		return c.fail(err)
	}
	if resp.IsError() {
		_ = os.Remove(outputFile)
		return c.fail(fmt.Errorf("download %s dump failed with %s", unit.name, resp.Status()))
	}
	info, err := os.Stat(outputFile)
	if err != nil {
		return c.fail(err)
	}
	if info.Size() == 0 {
		return c.fail(fmt.Errorf("downloaded %s dump is empty", unit.name))
	}
	if err := os.Chmod(outputFile, 0600); err != nil {
		return c.fail(err)
	}
	c.note("Wrote %s dump: %d bytes", unit.name, info.Size())
	return nil
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
		return nil, fmt.Errorf("%s %s failed with %s: %s", strings.ToLower(method), path, resp.Status(), string(resp.Body()))
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

func (c *collector) note(format string, args ...interface{}) {
	c.notes = append(c.notes, fmt.Sprintf(format, args...))
}

func (c *collector) fail(err error) error {
	return fmt.Errorf("collecting bmc dump from %s: %w", c.target.IP, err)
}

func latestDumpEntryID(entries map[string]interface{}) string {
	members, ok := entries["Members"].([]interface{})
	if !ok || len(members) == 0 {
		return ""
	}

	latestID := ""
	latestCreated := ""
	for _, member := range members {
		entry, ok := member.(map[string]interface{})
		if !ok {
			continue
		}
		id := redfishString(entry["Id"])
		if id == "" {
			continue
		}
		created := redfishString(entry["Created"])
		if latestID == "" || created > latestCreated {
			latestID = id
			latestCreated = created
		}
	}
	return latestID
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

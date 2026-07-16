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
	"fmt"
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

	targetCtx, cancel := context.WithTimeout(ctx, opts.TaskTimeout+(defaultEntryRetryCount*defaultEntryRetryInterval)+time.Minute)
	redfishClient := resty.New().
		SetBaseURL(baseURL).
		SetBasicAuth(opts.Username, target.Password).
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

func (c *collector) collect() error {
	if c.opts.ClearExisting {
		if err := c.clearDumpEntries(); err != nil {
			return err
		}
	}

	taskID, err := c.createDumpEntry()
	if err != nil {
		return err
	}

	if err := c.waitForDumpTask(taskID); err != nil {
		return err
	}

	entryID, err := c.waitForDumpEntry()
	if err != nil {
		return err
	}

	return c.downloadDumpEntry(entryID)
}

func (c *collector) clearDumpEntries() error {
	if _, err := c.requestJSON("POST", dumpClearPath, nil); err != nil {
		return c.fail(fmt.Errorf("deleting existing bmc dump entries: %w", err))
	}
	return nil
}

func (c *collector) createDumpEntry() (string, error) {
	createResponse, err := c.requestJSON("POST", dumpCollectDiagnosticPath, []byte(collectDiagnosticRequestBody))
	if err != nil {
		return "", c.fail(fmt.Errorf("creating bmc dump entry: %w", err))
	}
	if err := writeJSONArtifact(filepath.Join(c.targetDir, "create-dump-task.json"), createResponse); err != nil {
		return "", c.fail(err)
	}
	taskID := redfishString(createResponse["Id"])
	if taskID == "" {
		return "", c.fail(fmt.Errorf("create dump response does not contain an Id field"))
	}
	return taskID, nil
}

func (c *collector) waitForDumpTask(taskID string) error {
	taskPath := "/redfish/v1/TaskService/Tasks/" + url.PathEscape(taskID)
	deadline := time.Now().Add(c.opts.TaskTimeout)
	for {
		taskResponse, err := c.requestJSON("GET", taskPath, nil)
		if err != nil {
			return c.fail(fmt.Errorf("querying bmc dump task %s: %w", taskID, err))
		}

		switch taskState := redfishString(taskResponse["TaskState"]); taskState {
		case "Completed":
			if err := writeJSONArtifact(filepath.Join(c.targetDir, "task-final.json"), taskResponse); err != nil {
				return c.fail(err)
			}
			return nil
		case "Cancelled", "Exception", "Interrupted", "Killed": //nolint:misspell // Redfish TaskState uses this spelling.
			// Best-effort persist the final task response; it usually carries
			// Messages/PercentComplete/extended error details needed for debugging.
			_ = writeJSONArtifact(filepath.Join(c.targetDir, "task-final.json"), taskResponse)
			return c.fail(fmt.Errorf("task %s ended in state %s", taskID, taskState))
		case "":
			return c.fail(fmt.Errorf("task %s response does not contain TaskState", taskID))
		}

		if time.Now().After(deadline) {
			return c.fail(fmt.Errorf("task %s did not complete within %s", taskID, c.opts.TaskTimeout))
		}
		if err := waitOrDone(c.ctx, c.taskPollWait); err != nil {
			return c.fail(err)
		}
	}
}

func (c *collector) waitForDumpEntry() (string, error) {
	for attempt := 1; attempt <= c.entryRetry; attempt++ {
		entriesResponse, err := c.requestJSON("GET", dumpEntriesPath, nil)
		if err != nil {
			return "", c.fail(fmt.Errorf("querying bmc dump entries: %w", err))
		}
		entryID := latestDumpEntryID(entriesResponse)
		if entryID != "" {
			if err := writeJSONArtifact(filepath.Join(c.targetDir, "dump-entries.json"), entriesResponse); err != nil {
				return "", c.fail(err)
			}
			return entryID, nil
		}
		if err := waitOrDone(c.ctx, c.entryRetryWait); err != nil {
			return "", c.fail(err)
		}
	}
	return "", c.fail(fmt.Errorf("no bmc dump entry was found after the task completed"))
}

func (c *collector) downloadDumpEntry(entryID string) error {
	outputFile := filepath.Join(c.targetDir, "log_dump.tar.xz")
	resp, err := c.client.R().
		SetContext(c.ctx).
		SetOutput(outputFile).
		Get(dumpEntriesPath + "/" + url.PathEscape(entryID) + "/attachment")
	if err != nil {
		return c.fail(err)
	}
	if resp.IsError() {
		_ = os.Remove(outputFile)
		return c.fail(fmt.Errorf("download bmc dump failed with %s", resp.Status()))
	}
	info, err := os.Stat(outputFile)
	if err != nil {
		return c.fail(err)
	}
	if info.Size() == 0 {
		return c.fail(fmt.Errorf("downloaded bmc dump is empty"))
	}
	if err := os.Chmod(outputFile, 0600); err != nil {
		return c.fail(err)
	}
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
	case "GET":
		resp, err = req.Get(path)
	case "POST":
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

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

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/go-resty/resty/v2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BMC dump collection provides best-effort ZeroTrust failure diagnostics by
// triggering and downloading BlueField BMC diagnostic dumps into test artifacts.

const (
	bmcArtifactsDirName          = "bmc"
	bmcDumpRequestTimeout        = 30 * time.Second
	bmcDumpTaskTimeout           = 30 * time.Minute
	bmcDumpTaskPollInterval      = 5 * time.Second
	bmcDumpEntryRetryCount       = 15
	bmcDumpEntryRetryInterval    = 2 * time.Second
	bmcDefaultPort               = 443
	bmcDumpUser                  = "admin"
	bmcDumpPath                  = "/redfish/v1/Managers/BlueField_BMC_0/LogServices/Dump"
	bmcDumpClearPath             = bmcDumpPath + "/Actions/LogService.ClearLog"
	bmcDumpCollectDiagnosticPath = bmcDumpPath + "/Actions/LogService.CollectDiagnosticData"
	bmcDumpEntriesPath           = bmcDumpPath + "/Entries"
	bmcURLScheme                 = "https://"
	bmcSharedPasswordSecretName  = "bmc-shared-password"
	// bmcPasswordSecretDataKey is the Kubernetes Secret data key name, not a credential value.
	bmcPasswordSecretDataKey = "password"
)

type bmcLogTarget struct {
	IP               string
	Port             uint32
	Password         string
	CredentialSecret string
	DPUDevices       []string
}

type bmcDumpCollector struct {
	target    bmcLogTarget
	targetDir string
	ctx       context.Context
	client    *resty.Client
}

// collectBMCLogsZT collects BMC diagnostic dumps for every discovered DPUDevice
// BMC target and writes collection errors to the artifact directory.
func collectBMCLogsZT(ctx context.Context, specName, artifactsDir string, testClient client.Client, _ *systemTestInput) error {
	outputDir := filepath.Join(artifactsDir, bmcArtifactsDirName, specName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating bmc artifact directory %s: %w", outputDir, err)
	}

	targets, discoveryErr := getBMCLogTargets(ctx, testClient)
	if discoveryErr != nil {
		writeBMCCollectionError(outputDir, discoveryErr)
		if len(targets) == 0 {
			return discoveryErr
		}
	}
	if len(targets) == 0 {
		err := fmt.Errorf("bmc dump collection skipped: no DPUDevice BMC target found")
		writeBMCCollectionError(outputDir, err)
		return err
	}

	errs := make([]error, 0)
	if discoveryErr != nil {
		errs = append(errs, discoveryErr)
	}
	for _, target := range targets {
		if err := collectBMCDump(ctx, target, outputDir); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// getBMCLogTargets resolves BMC endpoints and credentials from DPUDevice status.
func getBMCLogTargets(ctx context.Context, testClient client.Client) ([]bmcLogTarget, error) {
	if testClient == nil {
		return nil, fmt.Errorf("bmc dump collection skipped: Kubernetes client is not initialized")
	}

	devices, err := listBMCLogDPUDevices(ctx, testClient)
	if err != nil {
		return nil, err
	}

	targetsByBMC := map[string]bmcLogTarget{}
	passwordsBySecret := map[string]string{}
	discoveryErrs := make([]error, 0)
	for i := range devices {
		if err := addBMCLogTarget(ctx, testClient, devices[i], targetsByBMC, passwordsBySecret); err != nil {
			discoveryErrs = append(discoveryErrs, fmt.Errorf("DPUDevice %s: %w", devices[i].Name, err))
			continue
		}
	}

	return sortedBMCLogTargets(targetsByBMC), errors.Join(discoveryErrs...)
}

func listBMCLogDPUDevices(ctx context.Context, testClient client.Client) ([]provisioningv1.DPUDevice, error) {
	devices := &provisioningv1.DPUDeviceList{}
	if err := testClient.List(ctx, devices, client.InNamespace(dpfOperatorSystemNamespace)); err != nil {
		return nil, fmt.Errorf("listing DPUDevices for bmc dump collection: %w", err)
	}
	return devices.Items, nil
}

func addBMCLogTarget(
	ctx context.Context,
	testClient client.Client,
	device provisioningv1.DPUDevice,
	targetsByBMC map[string]bmcLogTarget,
	passwordsBySecret map[string]string,
) error {
	bmcIP := dpuDeviceBMCIP(device)
	if bmcIP == "" {
		return nil
	}

	bmcPort := dpuDeviceBMCPort(device)
	credentialSecret := dpuDeviceBMCCredentialSecret(device)
	password, err := cachedBMCDumpPassword(ctx, testClient, credentialSecret, passwordsBySecret)
	if err != nil {
		return err
	}

	targetKey := bmcLogTargetKey(bmcIP, bmcPort, credentialSecret)
	target := targetsByBMC[targetKey]
	target.IP = bmcIP
	target.Port = bmcPort
	target.Password = password
	target.CredentialSecret = credentialSecret
	target.DPUDevices = append(target.DPUDevices, device.Name)
	targetsByBMC[targetKey] = target
	return nil
}

func cachedBMCDumpPassword(
	ctx context.Context,
	testClient client.Client,
	credentialSecret string,
	passwordsBySecret map[string]string,
) (string, error) {
	if password, ok := passwordsBySecret[credentialSecret]; ok {
		return password, nil
	}
	password, err := getBMCDumpPassword(ctx, testClient, credentialSecret)
	if err != nil {
		return "", err
	}
	passwordsBySecret[credentialSecret] = password
	return password, nil
}

func bmcLogTargetKey(bmcIP string, bmcPort uint32, credentialSecret string) string {
	return fmt.Sprintf("%s:%d:%s", bmcIP, bmcPort, credentialSecret)
}

func sortedBMCLogTargets(targetsByBMC map[string]bmcLogTarget) []bmcLogTarget {
	targets := make([]bmcLogTarget, 0, len(targetsByBMC))
	for _, target := range targetsByBMC {
		sort.Strings(target.DPUDevices)
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].IP != targets[j].IP {
			return targets[i].IP < targets[j].IP
		}
		if targets[i].Port != targets[j].Port {
			return targets[i].Port < targets[j].Port
		}
		return targets[i].CredentialSecret < targets[j].CredentialSecret
	})
	return targets
}

func getBMCDumpPassword(ctx context.Context, testClient client.Client, secretName string) (string, error) {
	secret := &corev1.Secret{}
	if err := testClient.Get(ctx, client.ObjectKey{
		Namespace: dpfOperatorSystemNamespace,
		Name:      secretName,
	}, secret); err != nil {
		return "", fmt.Errorf("reading bmc dump password secret %s/%s: %w",
			dpfOperatorSystemNamespace, secretName, err)
	}
	if password := string(secret.Data[bmcPasswordSecretDataKey]); password != "" {
		return password, nil
	}
	return "", fmt.Errorf("bmc dump collection skipped: %s/%s secret key %q is empty or missing",
		dpfOperatorSystemNamespace, secretName, bmcPasswordSecretDataKey)
}

func dpuDeviceBMCIP(device provisioningv1.DPUDevice) string {
	if device.Status.BMCIP != nil {
		return *device.Status.BMCIP
	}
	if device.Spec.BMCIP != nil {
		return *device.Spec.BMCIP
	}
	return ""
}

func dpuDeviceBMCPort(device provisioningv1.DPUDevice) uint32 {
	if device.Status.BMCPort != nil {
		return *device.Status.BMCPort
	}
	if device.Spec.BMCPort != nil {
		return *device.Spec.BMCPort
	}
	return bmcDefaultPort
}

func dpuDeviceBMCCredentialSecret(device provisioningv1.DPUDevice) string {
	if device.Status.BMCCredentialSecretName != nil && *device.Status.BMCCredentialSecretName != "" {
		return *device.Status.BMCCredentialSecretName
	}
	if device.Spec.BMCCredentialSecretName != nil && *device.Spec.BMCCredentialSecretName != "" {
		return *device.Spec.BMCCredentialSecretName
	}
	return bmcSharedPasswordSecretName
}

func bmcArtifactTargetName(target bmcLogTarget) string {
	name := target.IP
	if target.Port != bmcDefaultPort {
		name = fmt.Sprintf("%s-%d", name, target.Port)
	}
	if target.CredentialSecret != "" {
		name = fmt.Sprintf("%s-%s", name, target.CredentialSecret)
	}
	return strings.NewReplacer("/", "_", ":", "_").Replace(name)
}

func bmcBaseURL(target bmcLogTarget) string {
	baseURL := target.IP
	if !strings.HasPrefix(baseURL, bmcURLScheme) {
		baseURL = bmcURLScheme + baseURL
	}
	if target.Port == bmcDefaultPort || strings.Contains(strings.TrimPrefix(baseURL, bmcURLScheme), ":") {
		return baseURL
	}
	return fmt.Sprintf("%s:%d", baseURL, target.Port)
}

func collectBMCDump(ctx context.Context, target bmcLogTarget, outputDir string) error {
	collector, cancel, err := newBMCDumpCollector(ctx, target, outputDir)
	if err != nil {
		return err
	}
	defer cancel()
	return collector.collect()
}

func newBMCDumpCollector(ctx context.Context, target bmcLogTarget, outputDir string) (*bmcDumpCollector, context.CancelFunc, error) {
	targetDir := filepath.Join(outputDir, bmcArtifactTargetName(target))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("creating bmc target artifact directory %s: %w", targetDir, err)
	}

	if err := writeBMCMetadata(target, targetDir); err != nil {
		return nil, nil, err
	}

	baseURL := bmcBaseURL(target)
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		writeBMCCollectionError(targetDir, err)
		return nil, nil, err
	}

	targetCtx, cancel := context.WithTimeout(ctx, bmcDumpTaskTimeout+(bmcDumpEntryRetryCount*bmcDumpEntryRetryInterval)+time.Minute)
	redfishClient := resty.New().
		SetBaseURL(baseURL).
		SetBasicAuth(bmcDumpUser, target.Password).
		SetTimeout(bmcDumpRequestTimeout).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // Lab BMCs use self-signed certificates.

	return &bmcDumpCollector{
		target:    target,
		targetDir: targetDir,
		ctx:       targetCtx,
		client:    redfishClient,
	}, cancel, nil
}

func writeBMCMetadata(target bmcLogTarget, targetDir string) error {
	metadata := fmt.Sprintf("BMC IP: %s\n"+
		"BMC Port: %d\n"+
		"DPU Devices: %s\n"+
		"Credential Secret: %s/%s\n",
		target.IP,
		target.Port,
		strings.Join(target.DPUDevices, ", "),
		dpfOperatorSystemNamespace,
		target.CredentialSecret,
	)
	if err := os.WriteFile(filepath.Join(targetDir, "metadata.txt"), []byte(metadata), 0644); err != nil {
		return fmt.Errorf("writing bmc target metadata for %s: %w", target.IP, err)
	}
	return nil
}

func (c *bmcDumpCollector) collect() error {
	if err := c.clearDumpEntries(); err != nil {
		return err
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

func (c *bmcDumpCollector) clearDumpEntries() error {
	if _, err := c.requestJSON("POST", bmcDumpClearPath, nil); err != nil {
		return c.fail(fmt.Errorf("deleting existing bmc dump entries: %w", err))
	}
	return nil
}

func (c *bmcDumpCollector) createDumpEntry() (string, error) {
	createResponse, err := c.requestJSON("POST", bmcDumpCollectDiagnosticPath, []byte(`{"DiagnosticDataType":"Manager"}`))
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

func (c *bmcDumpCollector) waitForDumpTask(taskID string) error {
	taskPath := "/redfish/v1/TaskService/Tasks/" + url.PathEscape(taskID)
	deadline := time.Now().Add(bmcDumpTaskTimeout)
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
			return c.fail(fmt.Errorf("task %s ended in state %s", taskID, taskState))
		case "":
			return c.fail(fmt.Errorf("task %s response does not contain TaskState", taskID))
		}

		if time.Now().After(deadline) {
			return c.fail(fmt.Errorf("task %s did not complete within %s", taskID, bmcDumpTaskTimeout))
		}
		if err := waitOrDone(c.ctx, bmcDumpTaskPollInterval); err != nil {
			return c.fail(err)
		}
	}
}

func (c *bmcDumpCollector) waitForDumpEntry() (string, error) {
	for attempt := 1; attempt <= bmcDumpEntryRetryCount; attempt++ {
		entriesResponse, err := c.requestJSON("GET", bmcDumpEntriesPath, nil)
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
		if err := waitOrDone(c.ctx, bmcDumpEntryRetryInterval); err != nil {
			return "", c.fail(err)
		}
	}
	return "", c.fail(fmt.Errorf("no bmc dump entry was found after the task completed"))
}

func (c *bmcDumpCollector) downloadDumpEntry(entryID string) error {
	outputFile := filepath.Join(c.targetDir, "log_dump.tar.xz")
	resp, err := c.client.R().
		SetContext(c.ctx).
		SetOutput(outputFile).
		Get(bmcDumpEntriesPath + "/" + url.PathEscape(entryID) + "/attachment")
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
	return nil
}

func (c *bmcDumpCollector) requestJSON(method, path string, data []byte) (map[string]interface{}, error) {
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

func (c *bmcDumpCollector) fail(err error) error {
	writeBMCCollectionError(c.targetDir, err)
	return fmt.Errorf("collecting bmc dump from %s: %w", c.target.IP, err)
}

func writeJSONArtifact(path string, obj map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
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

func writeBMCCollectionError(outputDir string, err error) {
	if err == nil {
		return
	}
	_ = os.WriteFile(filepath.Join(outputDir, "collection-error.txt"), []byte(err.Error()+"\n"), 0644)
}

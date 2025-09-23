/*
Copyright 2025 NVIDIA

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

package install

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DMSDir    = "/tmp/dms"
	condition = string(provisioningv1.DPUCondOSInstalled)
)

type Handler struct {
	sync.Mutex
	client.Client
	bfbRegistryAddr string
	tasks           map[string]*future.Future
	GetDevice       func(string) (hostutil.Device, bool)
}

func NewHandler(client client.Client, bfbRegistryAddr string, getDevice func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		Client:          client,
		bfbRegistryAddr: bfbRegistryAddr,
		tasks:           make(map[string]*future.Future),
		GetDevice:       getDevice,
	}
}

func (h *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		err := fmt.Errorf("failed to get device by serial number: %s", dpu.Spec.SerialNumber)
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDevice").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	bfb := &provisioningv1.BFB{}
	if err := h.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.BFB}, bfb); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetBFB").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	bfcfgFile := filepath.Join(DMSDir, fmt.Sprintf("%s_%s_%s.cfg", dpu.Namespace, dpu.Name, dpu.UID))
	if _, err := os.Stat(bfcfgFile); err == nil {
		logger.Info("file already exists, skip downloading", "dst", bfcfgFile)
	} else {
		taskID := fmt.Sprintf("bfcfg_%s", dpu.UID)
		task := h.startDownloading(ctx, taskID, dpu.Status.BFCFGFile, bfcfgFile)
		if task == nil || task.GetState() != future.Ready {
			hostutil.NewCondition(condition).Failure(fmt.Errorf("downloading bf.cfg"), "DownloadingInProgress").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		if _, err = task.GetResult(); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToDownloadBFBConfig").Set(&dpu.Status.Conditions)
			h.removeTask(taskID)
			return dpu.Status, ctrl.Result{}, err
		}
	}

	bfbFile := filepath.Join(DMSDir, fmt.Sprintf("%s_%s_%s.bfb", dpu.Namespace, dpu.Name, dpu.UID))
	if _, err := os.Stat(bfbFile); err == nil {
		logger.Info("file already exists, skip downloading", "dst", bfbFile)
	} else {
		taskID := fmt.Sprintf("bfb_%s", dpu.UID)
		task := h.startDownloading(ctx, taskID, dpu.Status.BFBFile, bfbFile)
		if task == nil || task.GetState() != future.Ready {
			hostutil.NewCondition(condition).Failure(fmt.Errorf("downloading bfb"), "DownloadingInProgress").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		if _, err = task.GetResult(); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToDownloadBFB").Set(&dpu.Status.Conditions)
			h.removeTask(taskID)
			return dpu.Status, ctrl.Result{}, err
		}
	}

	taskID := string(dpu.UID)
	task := h.startInstalling(ctx, taskID, dev.Address, bfbFile, bfcfgFile)
	if task == nil || task.GetState() != future.Ready {
		hostutil.NewCondition(condition).Failure(fmt.Errorf("installing bfb and bf.cfg"), "InstallingInProgress").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if _, err := task.GetResult(); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToInstall").Set(&dpu.Status.Conditions)
		h.removeTask(taskID)
		return dpu.Status, ctrl.Result{}, err
	}
	hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
	return dpu.Status, ctrl.Result{}, nil
}

func (h *Handler) startDownloading(ctx context.Context, taskID string, filename string, dst string) *future.Future {
	h.Lock()
	defer h.Unlock()
	task, ok := h.tasks[taskID]
	if ok {
		return task
	}
	task = future.New(func() (any, error) {
		logger := log.FromContext(ctx)
		httpURL, err := url.JoinPath(h.bfbRegistryAddr, filename)
		if err != nil {
			return nil, err
		}
		logger.Info("start downloading", "url", httpURL, "dst", dst)
		origErr := hostutil.DownloadFile(ctx, httpURL, dst, 0644)
		if origErr == nil {
			logger.Info("download finished", "url", filename, "dst", dst)
			return nil, nil
		}

		if err := h.downloadWithBFBRegistryServiceEnv(ctx, filename, dst); err == nil {
			logger.Info("download finished", "url", filename, "dst", dst)
			return nil, nil
		} else if err := h.downloadWithKubernetesAPIServerVIP(ctx, filename, dst); err == nil {
			logger.Info("download finished", "url", filename, "dst", dst)
			return nil, err
		}
		return nil, origErr
	})
	h.tasks[taskID] = task
	return task
}

func (h *Handler) startInstalling(ctx context.Context, taskID, pciAddress, bfbFile string, bfcfgFile string) *future.Future {
	h.Lock()
	defer h.Unlock()
	logger := log.FromContext(ctx)
	task, ok := h.tasks[taskID]
	if ok {
		return task
	}
	task = future.New(func() (any, error) {
		cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os install --address 127.0.0.1:9339 --target %s --pkg %s --version %s", pciAddress, bfbFile, filepath.Base(bfbFile))
		if _, stderr, err := hostutil.RunBash(cmd); err != nil {
			return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
		}

		cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os install --address 127.0.0.1:9339 --target %s --pkg %s --version %s", pciAddress, bfcfgFile, filepath.Base(bfcfgFile))
		if _, stderr, err := hostutil.RunBash(cmd); err != nil {
			return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
		}

		cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os activate --address 127.0.0.1:9339 --target %s --version \"%s;%s\" --no-reboot", pciAddress, filepath.Base(bfbFile), filepath.Base(bfcfgFile))
		if _, stderr, err := hostutil.RunBash(cmd); err != nil {
			return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context done")
			case <-ticker.C:
				cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure system reboot-status --address 127.0.0.1:9339 --target %s --subcomponent \"CPU\"", pciAddress)
				_, stderr, err := hostutil.RunBash(cmd)
				if err != nil {
					return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
				}
				// dmsc outputs the reboot status in a pretty weird format:
				// INFO[0001] "127.0.0.1:9339" rebootStatus active=false, timeTillReboot=0s, rebootTime=1970-01-01 00:00:00 +0000 UTC, rebootCount=0
				// +----------------+--------+-----------------------+-------------+--------+-------+
				// |  Target Name   | Active | Duration Until Reboot | Reboot Time | Reason | Count |
				// +----------------+--------+-----------------------+-------------+--------+-------+
				// | 127.0.0.1:9339 | false  | 0s                    |             |        | 0     |
				// +----------------+--------+-----------------------+-------------+--------+-------+
				// The text in the first line is outputted to stderr, while the form is outputted to stdout.
				// And, the exit code is 0
				if !strings.Contains(stderr.String(), "active=false") {
					logger.Info("reboot status still active, waiting", "DMS reboot status", stderr.String())
					continue
				}
				logger.Info("reboot inactive, sleep 90 seconds and continue", "DMS reboot status", stderr.String())
				time.Sleep(90 * time.Second)
				return nil, nil
			}
		}
	})
	h.tasks[taskID] = task
	return task
}

func (h *Handler) removeTask(taskID string) {
	h.Lock()
	defer h.Unlock()
	delete(h.tasks, taskID)
}

// downloadWithViaEnv is a helper function to download a file from a URL using the environment variables BFB_REGISTRY_SERVICE_HOST and BFB_REGISTRY_SERVICE_PORT
// This is a workaround bypassing the issue that coredns in our CI env is sometimes inaccessible from worker nodes. Check the thread for more information.
// TODO: remove this workaround when the CI issue is fixed
func (h *Handler) downloadWithBFBRegistryServiceEnv(ctx context.Context, filename string, dst string) error {
	logger := log.FromContext(ctx)
	bfbRegistryServiceHost := os.Getenv("BFB_REGISTRY_SERVICE_HOST")
	bfbRegistryServicePort := os.Getenv("BFB_REGISTRY_SERVICE_PORT")
	if bfbRegistryServiceHost == "" || bfbRegistryServicePort == "" {
		return fmt.Errorf("BFB_REGISTRY_SERVICE_HOST or BFB_REGISTRY_SERVICE_PORT is not set")
	}
	httpURL, err := url.JoinPath(fmt.Sprintf("http://%s:%s", bfbRegistryServiceHost, bfbRegistryServicePort), filename)
	if err != nil {
		return err
	}
	logger.Info("workaround 1: use bfb-register service address given by env BFB_REGISTRY_SERVICE_HOST and BFB_REGISTRY_SERVICE_PORT", "url", httpURL, "dst", dst)
	err = hostutil.DownloadFile(ctx, httpURL, dst, 0644)
	if err != nil {
		logger.Error(fmt.Errorf("workaround 1 failed to download file: %w", err), "url", httpURL)
	}
	return err
}

// downloadWithViaKubernetesAPIServerVIP is a helper function to download a file from a URL using the environment variables KUBERNETES_SERVICE_HOST and bfb-registry port
// This is a workaround bypassing the issue that bfb-registry address is not set in the DPFOperatorConfig for hbn-ovn tests
// TODO: update the DPFOperatorConfig for hbn-ovn CI tests and remove this workaround
func (h *Handler) downloadWithKubernetesAPIServerVIP(ctx context.Context, filename string, dst string) error {
	logger := log.FromContext(ctx)
	kubernetesAPIServerVIP := os.Getenv("KUBERNETES_SERVICE_HOST")
	if kubernetesAPIServerVIP == "" {
		return fmt.Errorf("KUBERNETES_SERVICE_HOST is not set")
	}
	u, err := url.Parse(h.bfbRegistryAddr)
	if err != nil {
		return err
	}
	kubernetesAPIServerPort := u.Port()
	httpURL := fmt.Sprintf("http://%s", kubernetesAPIServerVIP)
	if kubernetesAPIServerPort != "" {
		httpURL += fmt.Sprintf(":%s", kubernetesAPIServerPort)
	}
	httpURL, err = url.JoinPath(httpURL, filename)
	if err != nil {
		return err
	}
	logger.Info("workaround 2: use kubernetesAPIServerVIP given by env KUBERNETES_SERVICE_HOST", "kubernetesAPIServerVIP", kubernetesAPIServerVIP, "httpURL", httpURL)
	err = hostutil.DownloadFile(ctx, httpURL, dst, 0644)
	if err != nil {
		logger.Error(fmt.Errorf("workaround 2 failed to download file: %w", err), "url", httpURL)
	}
	return err
}

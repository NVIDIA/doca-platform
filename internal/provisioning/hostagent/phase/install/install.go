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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	utils "github.com/nvidia/doca-platform/internal/utils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DMSDir                 = "/tmp/dms"
	condition              = string(provisioningv1.DPUCondOSInstalled)
	InstallationInProgress = "InstallationInProgress"
	InstallationFailed     = "InstallationFailed"
	InstallationTerminated = "InstallationTerminated"
	MaxRun                 = 2

	bfbRegistryServiceHostEnv = "BFB_REGISTRY_SERVICE_HOST"
	bfbRegistryServicePortEnv = "BFB_REGISTRY_SERVICE_PORT"
)

type Handler struct {
	client.Client
	bfbRegistryAddr string
	taskManager     *future.TaskManager
	GetDevice       func(string) (hostutil.Device, bool)
}

func NewHandler(client client.Client, bfbRegistryAddr string, getDevice func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		Client:          client,
		bfbRegistryAddr: bfbRegistryAddr,
		taskManager:     future.NewTaskManager(MaxRun),
		GetDevice:       getDevice,
	}
}

func (h *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)

	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		err := fmt.Errorf("failed to get device by serial number: %s", dpu.Spec.SerialNumber)
		hostutil.NewCondition(condition).Failure(err, InstallationFailed).Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	taskID := string(dpu.UID)
	task, maxReached := h.taskManager.RunTask(taskID, func() (any, error) {
		return nil, h.handle(ctx, dev, dpu)
	}, h.cleanupTask(dpu))
	if task == nil || task.GetState() != future.Ready {
		logger.Info("installation in progress, requeue after 10 seconds")
		hostutil.NewCondition(condition).Failure(fmt.Errorf("installing"), InstallationInProgress).Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	} else if _, err := task.GetResult(); err != nil {
		if maxReached {
			logger.Info("max number of runs reached, terminating installation", "maxRun", MaxRun, "last error", err)
			hostutil.NewCondition(condition).
				Failure(fmt.Errorf("max number of runs(%d) reached. Last error: %w", MaxRun, err), InstallationTerminated).
				Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		}
		hostutil.NewCondition(condition).Failure(err, InstallationFailed).Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
	return dpu.Status, ctrl.Result{}, nil
}

func (h *Handler) handle(ctx context.Context, dev hostutil.Device, dpu *provisioningv1.DPU) error {
	bfcfgFile := filepath.Join(DMSDir, fmt.Sprintf("%s_%s_%s.cfg", dpu.Namespace, dpu.Name, dpu.UID))
	if err := h.download(ctx, dpu.Status.BFCFGFile, bfcfgFile); err != nil {
		return err
	}
	bfbFile := filepath.Join(DMSDir, fmt.Sprintf("%s_%s_%s.bfb", dpu.Namespace, dpu.Name, dpu.UID))
	if err := h.download(ctx, dpu.Status.BFBFile, bfbFile); err != nil {
		return err
	}
	return h.installBFB(ctx, dev.Address, bfbFile, bfcfgFile)
}

func (h *Handler) download(ctx context.Context, filename string, dst string) error {
	logger := log.FromContext(ctx)
	if _, err := os.Stat(dst); err == nil {
		logger.Info("file already exists, skip downloading", "dst", dst)
		return nil
	}

	httpURL, err := url.JoinPath(h.bfbRegistryAddr, filename)
	if err != nil {
		return err
	}
	logger.Info("start downloading", "url", httpURL, "dst", dst)
	origErr := utils.DownloadFile(ctx, httpURL, dst, 0644)
	if origErr == nil {
		logger.Info("download finished", "url", filename, "dst", dst)
		return nil
	}

	if err := h.downloadWithBFBRegistryServiceEnv(ctx, filename, dst); err == nil {
		logger.Info("download finished", "url", filename, "dst", dst)
		return nil
	} else if err := h.downloadWithKubernetesAPIServerVIP(ctx, filename, dst); err == nil {
		logger.Info("download finished", "url", filename, "dst", dst)
		return nil
	}
	return origErr
}

func (h *Handler) installBFB(ctx context.Context, pciAddress, bfbFile, bfcfgFile string) error {
	logger := log.FromContext(ctx)
	cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os install --address 127.0.0.1:9339 --target %s --pkg %s --version %s", pciAddress, bfbFile, filepath.Base(bfbFile))
	if _, stderr, err := hostutil.RunBash(cmd); err != nil {
		return fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
	}

	cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os install --address 127.0.0.1:9339 --target %s --pkg %s --version %s", pciAddress, bfcfgFile, filepath.Base(bfcfgFile))
	if _, stderr, err := hostutil.RunBash(cmd); err != nil {
		return fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
	}

	cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os activate --address 127.0.0.1:9339 --target %s --version \"%s;%s\" --no-reboot", pciAddress, filepath.Base(bfbFile), filepath.Base(bfcfgFile))
	if _, stderr, err := hostutil.RunBash(cmd); err != nil {
		return fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context done")
		case <-ticker.C:
			cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure system reboot-status --address 127.0.0.1:9339 --target %s --subcomponent \"CPU\"", pciAddress)
			_, stderr, err := hostutil.RunBash(cmd)
			if err != nil {
				return fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
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
			return nil
		}
	}
}

// downloadWithViaEnv is a helper function to download a file from a URL using the environment variables BFB_REGISTRY_SERVICE_HOST and BFB_REGISTRY_SERVICE_PORT
// This is a workaround bypassing the issue that coredns in our CI env is sometimes inaccessible from worker nodes. Check the thread for more information.
// TODO: remove this workaround when the CI issue is fixed
func (h *Handler) downloadWithBFBRegistryServiceEnv(ctx context.Context, filename string, dst string) error {
	logger := log.FromContext(ctx)
	bfbRegistryServiceHost := os.Getenv(bfbRegistryServiceHostEnv)
	bfbRegistryServicePort := os.Getenv(bfbRegistryServicePortEnv)
	if bfbRegistryServiceHost == "" || bfbRegistryServicePort == "" {
		err := fmt.Errorf("BFB Registry Service env not set, skip")
		logger.Error(err, "skip download with BFB Registry Service env", bfbRegistryServiceHostEnv, bfbRegistryServiceHost, bfbRegistryServicePortEnv, bfbRegistryServicePort)
		return err
	}
	httpURL, err := url.JoinPath(fmt.Sprintf("http://%s:%s", bfbRegistryServiceHost, bfbRegistryServicePort), filename)
	if err != nil {
		logger.Error(err, "failed to generate URL from BFB Registry Service host and port", bfbRegistryServiceHostEnv, bfbRegistryServiceHost, bfbRegistryServicePortEnv, bfbRegistryServicePort, "filename", filename)
		return err
	}
	logger.Info("workaround 1: download with bfb-register service address read from env", "url", httpURL, "dst", dst)
	err = utils.DownloadFile(ctx, httpURL, dst, 0644)
	if err != nil {
		logger.Error(fmt.Errorf("workaround 1 failed to download file: %w", err), "url", httpURL, "dst", dst)
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
		err := fmt.Errorf("KUBERNETES_SERVICE_HOST is not set")
		logger.Error(err, "skip download with Kubernetes API Server VIP")
		return err
	}
	u, err := url.Parse(h.bfbRegistryAddr)
	if err != nil {
		logger.Error(err, "failed to parse port from bfb registry address", "bfbRegistryAddr", h.bfbRegistryAddr)
		return err
	}
	kubernetesAPIServerPort := u.Port()
	httpURL := fmt.Sprintf("http://%s", kubernetesAPIServerVIP)
	if kubernetesAPIServerPort != "" {
		httpURL += fmt.Sprintf(":%s", kubernetesAPIServerPort)
	}
	httpURL, err = url.JoinPath(httpURL, filename)
	if err != nil {
		logger.Error(err, "failed to join path with kubernetes API server VIP and port", "httpURL", httpURL, "filename", filename)
		return err
	}
	logger.Info("workaround 2: download with kubernetesAPIServerVIP read from env KUBERNETES_SERVICE_HOST", "kubernetesAPIServerVIP", kubernetesAPIServerVIP, "httpURL", httpURL)
	err = utils.DownloadFile(ctx, httpURL, dst, 0644)
	if err != nil {
		logger.Error(fmt.Errorf("workaround 2 failed to download file: %w", err), "url", httpURL, "dst", dst)
	}
	return err
}

func (h *Handler) cleanupTask(dpu *provisioningv1.DPU) func() bool {
	return func() bool {
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		latest := &provisioningv1.DPU{}
		err := h.Client.Get(timeoutCtx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Name}, latest)
		if err != nil {
			return apierrors.IsNotFound(err)
		} else if dpu.UID != latest.UID {
			return true
		}
		return latest.Status.Phase != provisioningv1.DPUOSInstalling
	}
}

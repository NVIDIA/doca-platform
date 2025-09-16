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
	bfbServerLocation string
	tasks             map[string]*future.Future
	GetDevice         func(string) (hostutil.Device, bool)
}

func NewHandler(client client.Client, bfbServerLocation string, getDevice func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		Client:            client,
		bfbServerLocation: bfbServerLocation,
		tasks:             make(map[string]*future.Future),
		GetDevice:         getDevice,
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
		task := h.startDownloading(ctx, fmt.Sprintf("bfcfg_%s", dpu.UID), dpu.Status.BFCFGFile, bfcfgFile)
		if task == nil || task.GetState() != future.Ready {
			hostutil.NewCondition(condition).Failure(fmt.Errorf("downloading bf.cfg"), "DownloadingInProgress").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		if _, err = task.GetResult(); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToDownloadBFBConfig").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		}
	}

	bfbFile := filepath.Join(DMSDir, fmt.Sprintf("%s_%s_%s.bfb", dpu.Namespace, dpu.Name, dpu.UID))
	if _, err := os.Stat(bfbFile); err == nil {
		logger.Info("file already exists, skip downloading", "dst", bfbFile)
	} else {
		task := h.startDownloading(ctx, fmt.Sprintf("bfb_%s", dpu.UID), dpu.Status.BFBFile, bfbFile)
		if task == nil || task.GetState() != future.Ready {
			hostutil.NewCondition(condition).Failure(fmt.Errorf("downloading bfb"), "DownloadingInProgress").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		if _, err = task.GetResult(); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToDownloadBFB").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		}
	}

	task := h.startInstalling(string(dpu.UID), dev.Address, bfbFile, bfcfgFile)
	if task == nil || task.GetState() != future.Ready {
		hostutil.NewCondition(condition).Failure(fmt.Errorf("installing bfb and bf.cfg"), "InstallingInProgress").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if _, err := task.GetResult(); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToInstall").Set(&dpu.Status.Conditions)
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
		httpURL, err := url.JoinPath(h.bfbServerLocation, filename)
		if err != nil {
			return nil, err
		}
		logger.Info("start downloading", "url", httpURL, "dst", dst)
		err = hostutil.DownloadFile(ctx, httpURL, dst, 0644)
		if err != nil {
			return nil, err
		}
		logger.Info("download finished", "url", filename, "dst", dst)
		return nil, nil
	})
	h.tasks[taskID] = task
	return task
}

func (h *Handler) startInstalling(taskID, pciAddress, bfbFile string, bfcfgFile string) *future.Future {
	h.Lock()
	defer h.Unlock()
	task, ok := h.tasks[taskID]
	if ok {
		return task
	}
	task = future.New(func() (any, error) {
		cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os install --address localhost:9339 --target %s --pkg %s --version %s", pciAddress, bfbFile, filepath.Base(bfbFile))
		if _, stderr, err := hostutil.RunBash(cmd); err != nil {
			return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
		}

		cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os install --address localhost:9339 --target %s --pkg %s --version %s", pciAddress, bfcfgFile, filepath.Base(bfcfgFile))
		if _, stderr, err := hostutil.RunBash(cmd); err != nil {
			return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
		}

		cmd = fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure os activate --address localhost:9339 --target %s --version \"%s;%s\"", pciAddress, filepath.Base(bfbFile), filepath.Base(bfcfgFile))
		if _, stderr, err := hostutil.RunBash(cmd); err != nil {
			return nil, fmt.Errorf("failed to run cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
		}
		return nil, nil
	})
	h.tasks[taskID] = task
	return task
}

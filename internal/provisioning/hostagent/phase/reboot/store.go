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

package reboot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	BootIDDir         = "/var/lib/dpf/hostagent/boot_id"
	SystemdBootIDFile = "/proc/sys/kernel/random/boot_id"
	Suffix            = ".reboot"
)

type BootIDStore interface {
	PersistBootID(dpu *provisioningv1.DPU, skip bool) error
	IsRebootFinished(dpu *provisioningv1.DPU) (bool, error)
}

type fileSystemStore struct {
	client.Client
	bootIDDir  string
	getDPUFunc func(ctx context.Context, name types.NamespacedName) (*provisioningv1.DPU, error)
}

func NewFileSystemStore(client client.Client, bootIDDir string) BootIDStore {
	s := &fileSystemStore{
		Client:    client,
		bootIDDir: bootIDDir,
	}
	s.StartHousekeeping()
	return s
}

func (s *fileSystemStore) StartHousekeeping() {
	go wait.PollUntilContextCancel(context.TODO(), 5*time.Minute, true, func(ctx context.Context) (bool, error) { // nolint:errcheck
		timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		s.housekeeping(timeoutCtx)
		return false, nil
	})
}

func (s *fileSystemStore) PersistBootID(dpu *provisioningv1.DPU, skip bool) error {
	bootID := ""
	if skip {
		bootID = "skip"
	} else {
		systemBootID, err := os.ReadFile(SystemdBootIDFile)
		if err != nil {
			return err
		}
		bootID = string(systemBootID)
	}
	request := &RebootRequest{
		DPUName:      dpu.Name,
		DPUNamespace: dpu.Namespace,
		UID:          string(dpu.UID),
		RebootID:     bootID,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return hostutil.AtomicWrite(s.rebootRequestFileName(dpu), requestBytes, 0644)
}

func (s *fileSystemStore) IsRebootFinished(dpu *provisioningv1.DPU) (bool, error) {
	dpuBootID, err := s.readBootID(dpu)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}
	currentBootID, err := os.ReadFile(SystemdBootIDFile)
	if err != nil {
		return false, fmt.Errorf("failed to read current boot ID, err: %v", err)
	}
	return dpuBootID != string(currentBootID), nil
}

func (s *fileSystemStore) readBootID(dpu *provisioningv1.DPU) (string, error) {
	data, err := os.ReadFile(s.rebootRequestFileName(dpu))
	if err != nil {
		return "", err
	}
	request := &RebootRequest{}
	err = json.Unmarshal(data, request)
	if err != nil {
		return "", err
	}
	return request.RebootID, nil
}

func (s *fileSystemStore) rebootRequestFileName(dpu *provisioningv1.DPU) string {
	return filepath.Join(s.bootIDDir, string(dpu.UID)+Suffix)
}

func (s *fileSystemStore) housekeeping(ctx context.Context) {
	files, err := os.ReadDir(s.bootIDDir)
	if err != nil {
		klog.Errorf("failed to read boot ID directory, err: %v", err)
		return
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), Suffix) {
			continue
		}
		path := filepath.Join(s.bootIDDir, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			klog.Errorf("failed to read reboot request file, path: %s, err: %v", path, err)
			continue
		}
		rr := &RebootRequest{}
		if err := json.Unmarshal(data, rr); err != nil {
			klog.Errorf("failed to unmarshal reboot request, path: %s, err: %v", path, err)
			continue
		}
		get := s.getDPUFunc
		if get == nil {
			get = s.getDPU
		}
		dpu, err := get(ctx, types.NamespacedName{Namespace: rr.DPUNamespace, Name: rr.DPUName})
		if err != nil {
			if apierrors.IsNotFound(err) {
				s.deleteFileIgnoreError(path)
			} else {
				klog.Errorf("failed to get DPU, name: %s, namespace: %s, err: %v", rr.DPUName, rr.DPUNamespace, err)
			}
		} else if string(dpu.UID) != rr.UID ||
			dpu.Status.Phase == provisioningv1.DPUReady ||
			dpu.Status.Phase == provisioningv1.DPUError {
			s.deleteFileIgnoreError(path)
		}
	}
}

func (s *fileSystemStore) deleteFileIgnoreError(path string) {
	err := os.Remove(path)
	if err != nil {
		klog.Errorf("failed to remove reboot request file, path: %s, err: %v", path, err)
	} else {
		klog.Infof("removed reboot request file, path: %s", path)
	}
}

func (s *fileSystemStore) getDPU(ctx context.Context, name types.NamespacedName) (*provisioningv1.DPU, error) {
	dpu := &provisioningv1.DPU{}
	if err := s.Client.Get(ctx, name, dpu); err != nil {
		return nil, err
	}
	return dpu, nil
}

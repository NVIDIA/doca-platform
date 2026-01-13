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

package openflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	kexec "k8s.io/utils/exec"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type OpenFlowAPI interface {
	Add(ctx context.Context, flows, bridgeName string) error
}

type OpenFlow struct {
	Exec         kexec.Interface
	OVSOfctlPath string
	BridgeName   string
	mu           sync.Mutex
}

// Add adds flows to the OpenFlow switch.
// Utility function which takes in a multiline string called flows
// Creates a temporary file on the system and writes the aforementioned
// string. This will file will be consumed by ovs-ofctl command with the
// bundle argument to ensure the fact that all flows are added in an atomic operation
func (f *OpenFlow) Add(ctx context.Context, flows, bridgeName string) (err error) {
	log := ctrllog.FromContext(ctx)

	if flows == "" || bridgeName == "" {
		log.Info("no flows or bridge name provided, skipping addition of flows")
		return nil
	}

	// Create a temporary file with a proper pattern
	fileP, err := os.CreateTemp("", "ovs-flows-*.txt")
	if err != nil {
		return err
	}
	tempFileName := fileP.Name()

	// Ensure cleanup of the temp file
	defer func() {
		if closeErr := fileP.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
		if removeErr := os.Remove(tempFileName); removeErr != nil {
			err = errors.Join(err, removeErr)
		}
	}()

	// Write the flows to the temp file
	_, err = fileP.WriteString(flows)
	if err != nil {
		return err
	}

	// Close the file before passing to ovs-ofctl
	if err := fileP.Close(); err != nil {
		return err
	}

	if err := f.applyFlowsFile(ctx, tempFileName, bridgeName); err != nil {
		return err
	}

	log.Info("added flows:")
	log.Info(flows)
	return err
}

func (f *OpenFlow) applyFlowsFile(ctx context.Context, filePath, bridgeName string) error {
	if f.OVSOfctlPath == "" {
		f.mu.Lock()
		if f.OVSOfctlPath == "" {
			ovsOfctlPath, err := f.Exec.LookPath("ovs-ofctl")
			if err != nil {
				f.mu.Unlock()
				return err
			}
			f.OVSOfctlPath = ovsOfctlPath
		}
		f.mu.Unlock()
	}

	args := []string{"-t", "5", "--bundle", "add-flows", bridgeName, filePath}
	cmd := f.Exec.CommandContext(ctx, f.OVSOfctlPath, args...)
	var stderr bytes.Buffer
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running ovs-ofctl command with args %v failed: err=%w stderr=%s", args, err, stderr.String())
	}

	return nil
}

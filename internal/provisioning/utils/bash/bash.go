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

package bash

import (
	"bytes"
	"fmt"
	"os/exec"
)

type CmdOption func(*exec.Cmd)

// RunFunc runs a shell command and returns stdout, stderr, and any error.
type RunFunc func(cmdStr string) (stdout, stderr bytes.Buffer, err error)

func Run(cmdStr string) (stdout, stderr bytes.Buffer, err error) {
	return RunWithOptions(cmdStr)
}

func RunWithOptions(cmdStr string, opts ...CmdOption) (stdout, stderr bytes.Buffer, err error) {
	cmd := exec.Command("bash", "-c", cmdStr)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	for _, opt := range opts {
		if opt != nil {
			opt(cmd)
		}
	}
	err = cmd.Run()
	if err != nil {
		return stdout, stderr, fmt.Errorf("failed to run command: %w", err)
	}
	return stdout, stderr, nil
}

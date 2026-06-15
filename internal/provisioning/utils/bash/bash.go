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
	"context"
	"fmt"
	"os/exec"
)

type CmdOption func(*exec.Cmd)

// RunFunc runs a shell command and returns stdout, stderr, and any error.
type RunFunc func(cmdStr string) (stdout, stderr bytes.Buffer, err error)

// RunWithContextFunc runs a shell command with a context and returns stdout, stderr, and any error.
type RunWithContextFunc func(ctx context.Context, cmdStr string, opts ...CmdOption) (stdout, stderr bytes.Buffer, err error)

// RunScriptWithContextFunc runs an executable directly (without a shell wrapper) and returns stdout, stderr, and any error.
type RunScriptWithContextFunc func(ctx context.Context, path string) (stdout, stderr bytes.Buffer, err error)

func Run(cmdStr string) (stdout, stderr bytes.Buffer, err error) {
	return RunWithOptions(cmdStr)
}

func RunWithContext(ctx context.Context, cmdStr string, opts ...CmdOption) (stdout, stderr bytes.Buffer, err error) {
	return runCmd(exec.CommandContext(ctx, "bash", "-c", cmdStr), opts...)
}

// RunScriptWithContext executes path directly (honors the file's shebang or binary format),
// supporting both shell scripts and compiled executables.
func RunScriptWithContext(ctx context.Context, path string) (stdout, stderr bytes.Buffer, err error) {
	return runCmd(exec.CommandContext(ctx, path))
}

func RunWithOptions(cmdStr string, opts ...CmdOption) (stdout, stderr bytes.Buffer, err error) {
	return runCmd(exec.Command("bash", "-c", cmdStr), opts...)
}

func runCmd(cmd *exec.Cmd, opts ...CmdOption) (stdout, stderr bytes.Buffer, err error) {
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	for _, opt := range opts {
		if opt != nil {
			opt(cmd)
		}
	}
	if err = cmd.Run(); err != nil {
		return stdout, stderr, fmt.Errorf("failed to run command: %w", err)
	}
	return stdout, stderr, nil
}

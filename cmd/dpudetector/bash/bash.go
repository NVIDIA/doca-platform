/*
Copyright 2024 NVIDIA

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
	"strings"

	log "github.com/golang/glog"
)

func GetPSID(pciAddress string) (string, error) {
	cmd := "flint -d " + pciAddress + " q"
	out, stderr, err := RunCommand(cmd)
	if err != nil || len(stderr) > 0 || len(out) == 0 {
		return "", fmt.Errorf("get PSID, stdout: %v, stderr: %v, error: %v", out, stderr, err)
	}

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) == 2 && parts[0] == "PSID" {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("get PSID, stdout: %v, stderr: %v, error: %v", out, stderr, err)
}

func RunCommand(cmdString string, background ...bool) (string, string, error) {
	if len(cmdString) == 0 {
		return "", "", fmt.Errorf("no command")
	}
	log.Infof("Run command: %s", cmdString)

	var err error
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command("bash", "-c", cmdString)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Start()
	if err != nil {
		return "", "", fmt.Errorf("open stdout pipe: %v", err)
	}

	if len(background) == 0 || !background[0] {
		err = cmd.Wait()
	}

	if err != nil {
		// if there was any error, print it here
		err = fmt.Errorf("err: %v", err)
	} else {
		// otherwise, print the output from running the command
		log.Infof("command output: %v", stdout.String())
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

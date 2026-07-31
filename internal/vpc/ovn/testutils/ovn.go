/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package testutils

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	utilrand "k8s.io/apimachinery/pkg/util/rand"
)

// StartOvnContainer starts an OVN container and returns a function to stop it,
// the nbendpoint and sbendpoint for OVN Northbound and Southbound clients respectively,
// and error if occurred
func StartOvnContainer(ovnImageName string) (stopFn func() error, nbEndpoint string, sbEndpoint string, err error) {

	ovnContainerName := "ovn-central-" + utilrand.String(6)
	cmd := exec.Command("docker", "run", "--rm", "--name", ovnContainerName, "-d",
		"-p", "6641", "-p", "6642", ovnImageName, "bash", "-c",
		"/usr/share/ovn/scripts/ovn-ctl --db-nb-create-insecure-remote=yes --db-sb-create-insecure-remote=yes --db-nb-port=6641 --db-sb-port=6642 start_northd && tail -f /dev/null")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to start OVN container: %w, output: %s", err, output)
	}

	defer func() {
		if err != nil {
			_ = stopOvnContainerFn(ovnContainerName)()
		}
	}()

	// get docker assigned host port of the ovn container
	ovnNBContainerPort, err := getContainerPort(ovnContainerName, "6641")
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to get OVN container port: %w", err)
	}

	// get docker assigned host port of the ovn SB container
	ovnSBContainerPort, err := getContainerPort(ovnContainerName, "6642")
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to get OVN SB container port: %w", err)
	}

	nbEndpoint = fmt.Sprintf("tcp:127.0.0.1:%d", ovnNBContainerPort)
	sbEndpoint = fmt.Sprintf("tcp:127.0.0.1:%d", ovnSBContainerPort)

	return stopOvnContainerFn(ovnContainerName), nbEndpoint, sbEndpoint, nil
}

func stopOvnContainerFn(containerName string) func() error {
	return func() error {
		cmd := exec.Command("docker", "rm", "-f", containerName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to stop OVN container: %w, output: %s", err, output)
		}
		return nil
	}
}

// getContainerPort retrieves and parses the host port for a specific container port
func getContainerPort(containerName string, containerPort string) (int, error) {
	cmd := exec.Command("docker", "container", "port", containerName, containerPort+"/tcp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("failed to get container port: %w, output: %s", err, output)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("failed to parse container port output: %s", output)
	}

	portStr := strings.Split(lines[0], ":")[1]
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil {
		return 0, fmt.Errorf("failed to convert container port %s to int: %w", portStr, err)
	}

	return port, nil
}

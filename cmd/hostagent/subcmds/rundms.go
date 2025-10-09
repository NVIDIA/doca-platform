//go:build linux

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

package subcmds

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

const (
	dmsBinary = "/opt/mellanox/doca/services/dms/dmsd"
)

var (
	dmsPort int
)

var runDMSCmd = &cobra.Command{
	Use:   "rundms",
	Short: "Run DMS server",
	Long:  "Run DMS server",
	RunE: func(cmd *cobra.Command, args []string) error {
		devices, err := hostutil.DiscoverDPUs()
		if err != nil {
			return err
		}
		targetPCIs := []string{}
		for _, device := range devices {
			targetPCIs = append(targetPCIs, device.Address)
			klog.Infof("Found DPU with PCI address: %v\n", device.Address)
		}
		if len(targetPCIs) == 0 {
			return fmt.Errorf("no DPUs found")
		}

		dmsFlags := fmt.Sprintf("--bind_address 127.0.0.1:%d --target_pci %s --auth cert --tls_enabled=false -exec_timeout 900 -disable_unbind_at_activate -reboot_status_check none -v 99",
			dmsPort, strings.Join(targetPCIs, ","))
		klog.Infof("will start DMS server with flags: %s\n", dmsFlags)
		dms := exec.Command("bash", "-c", fmt.Sprintf("%s %s", dmsBinary, dmsFlags))
		dms.Stdout = os.Stdout
		dms.Stderr = os.Stderr
		if err := dms.Run(); err != nil {
			return fmt.Errorf("failed to run DMS server: %w", err)
		}
		return fmt.Errorf("DMS server exited with code %d", dms.ProcessState.ExitCode())
	},
}

func init() {
	runDMSCmd.Flags().IntVar(&dmsPort, "port", 9339, "the port for the DMS server")
	RootCmd.AddCommand(runDMSCmd)
}

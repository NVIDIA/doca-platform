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

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/cmd/gitlab"
)

// Global flags
var configFile string

var rootCmd = &cobra.Command{
	Use:   "dpfdev",
	Short: "DPF Development CLI",
	Long: `A CLI tool for DPF development operations.
This tool provides commands to help with DPF development workflows.`,
	SilenceUsage: true,
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		return err
	}
	return nil
}

func init() {
	// Add global flags
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "$HOME/.config/dpfdev.json", "Path to the configuration file")

	// GitLab-facing commands live in their own package.
	rootCmd.AddCommand(gitlab.Cmd)
}

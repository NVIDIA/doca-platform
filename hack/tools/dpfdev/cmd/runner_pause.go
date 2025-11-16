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
	"fmt"
	"os"
	"strconv"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

var (
	unpause bool
)

func init() {
	runnerCmd.AddCommand(runnerPauseCmd)

	// Add flags
	runnerPauseCmd.Flags().BoolVar(&unpause, "unpause", false, "Unpause the runner instead of pausing it")
}

var runnerPauseCmd = &cobra.Command{
	Use:   "pause <runner-id>",
	Short: "Pause or unpause a GitLab CI runner",
	Long:  `Pause or unpause a GitLab CI runner by ID. Use --unpause flag to unpause a paused runner.`,
	Example: `  # Pause a runner
  dpfdev runner pause 12345

  # Unpause a runner
  dpfdev runner pause 12345 --unpause`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse runner ID
		runnerID, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid runner ID: %s (must be a number)", args[0])
		}

		// Load config
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %v", err)
		}

		// Get GitLab token from environment
		token := os.Getenv("GITLAB_TOKEN")
		if token == "" {
			return fmt.Errorf("GITLAB_TOKEN environment variable is not set")
		}

		// Create GitLab client
		client := gitlab.NewClient(cfg.GitLab.Endpoint, token, cfg.GitLab.ProjectID)

		// Pause or unpause the runner
		if unpause {
			fmt.Printf("Unpausing runner %d...\n", runnerID)
			if err := client.UnpauseRunner(runnerID); err != nil {
				return fmt.Errorf("failed to unpause runner: %v", err)
			}
			fmt.Printf("✓ Runner %d has been unpaused successfully\n", runnerID)
		} else {
			fmt.Printf("Pausing runner %d...\n", runnerID)
			if err := client.PauseRunner(runnerID); err != nil {
				return fmt.Errorf("failed to pause runner: %v", err)
			}
			fmt.Printf("✓ Runner %d has been paused successfully\n", runnerID)
		}

		return nil
	},
}

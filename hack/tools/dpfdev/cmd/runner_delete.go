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
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

var (
	force bool
)

func init() {
	runnerCmd.AddCommand(runnerDeleteCmd)

	// Add flags
	runnerDeleteCmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
}

var runnerDeleteCmd = &cobra.Command{
	Use:   "delete <runner-id>",
	Short: "Delete a GitLab CI runner",
	Long:  `Delete a GitLab CI runner by ID. This action requires admin or owner permissions and cannot be undone.`,
	Example: `  # Delete a runner (with confirmation prompt)
  dpfdev runner delete 12345

  # Delete a runner without confirmation
  dpfdev runner delete 12345 --force

  # Delete a runner without confirmation (short flag)
  dpfdev runner delete 12345 -f`,
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

		// Fetch runner details for confirmation
		runnerDetails, err := client.GetRunnerDetails(runnerID)
		if err != nil {
			return fmt.Errorf("failed to fetch runner details: %v", err)
		}

		// Show runner info and ask for confirmation (unless --force is used)
		if !force {
			fmt.Printf("\n⚠️  WARNING: You are about to delete the following runner:\n\n")
			fmt.Printf("  ID:          %d\n", runnerDetails.ID)
			fmt.Printf("  Description: %s\n", runnerDetails.Description)
			fmt.Printf("  Status:      %s\n", runnerDetails.Status)
			fmt.Printf("  Online:      %t\n", runnerDetails.Online)
			fmt.Printf("\nThis action cannot be undone!\n\n")
			fmt.Printf("Type 'yes' to confirm deletion: ")

			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %v", err)
			}

			response = strings.TrimSpace(strings.ToLower(response))
			if response != "yes" {
				fmt.Println("Deletion cancelled.")
				return nil
			}
		}

		// Delete the runner
		fmt.Printf("Deleting runner %d...\n", runnerID)
		if err := client.DeleteRunner(runnerID); err != nil {
			return fmt.Errorf("failed to delete runner: %v", err)
		}

		fmt.Printf("✓ Runner %d has been deleted successfully\n", runnerID)
		return nil
	},
}

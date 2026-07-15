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

package gitlab

import (
	"fmt"
	"os"
	"strconv"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

// scheduleCmd represents the schedule command
var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Commands for working with GitLab scheduled pipelines",
	Long: `Commands for working with GitLab scheduled pipelines: listing schedules,
fetching their variables, and syncing a YAML file to GitLab.

Schedules are managed declaratively: "sync" applies a YAML file rather than
editing individual schedules imperatively. Reading works with the Maintainer
role; modifying a schedule requires the owner's token, which "sync" obtains by
taking ownership when needed.`,
}

func init() {
	Cmd.AddCommand(scheduleCmd)
}

// newGitLabClient loads the dpfdev config and builds a GitLab client from it
// and the GITLAB_TOKEN environment variable. It is the shared client
// construction for GitLab-backed commands. The config file path comes from the
// inherited "--config" persistent flag. When no config file is available it
// falls back to the CI_API_V4_URL and CI_PROJECT_ID environment variables that
// GitLab predefines in CI jobs, so no config file is needed there.
func newGitLabClient(cmd *cobra.Command) (*gitlab.Client, error) {
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN environment variable is not set")
	}

	configFile, _ := cmd.Flags().GetString("config")
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		endpoint, projectID := os.Getenv("CI_API_V4_URL"), os.Getenv("CI_PROJECT_ID")
		if endpoint != "" && projectID != "" {
			return gitlab.NewClient(endpoint, token, projectID), nil
		}
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	return gitlab.NewClient(cfg.GitLab.Endpoint, token, cfg.GitLab.ProjectID), nil
}

// parseScheduleID parses the schedule ID positional argument.
func parseScheduleID(arg string) (int, error) {
	scheduleID, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("invalid schedule ID: %s (must be a number)", arg)
	}
	if scheduleID <= 0 {
		return 0, fmt.Errorf("invalid schedule ID: %d (must be > 0)", scheduleID)
	}
	return scheduleID, nil
}

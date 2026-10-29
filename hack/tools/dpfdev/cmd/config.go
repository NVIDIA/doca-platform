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
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	dpfconfig "github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"

	"github.com/spf13/cobra"
)

// Default values and hints
const (
	defaultConfigFile    = "$HOME/.config/dpfdev.json"
	gitlabEndpointFormat = "https://gitlab-master.XXXX.com/api/v4"
	projectIDHint        = "You can find the project ID on the project's main page on GitLab, under the three dots menu."
	defaultJobsHistory   = "$HOME/.cache/dpfdev/jobs_history.json"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage dpfdev configuration",
	Long:  "Guides the user through creating or updating the dpfdev configuration file.",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		// 1. Determine config file path
		configFilePath, err := determineConfigPath(reader)
		if err != nil {
			fmt.Println(fmt.Errorf("error getting config file path %w", err))
			return nil
		}

		// 2. Load existing config or create new
		cfg, err := dpfconfig.LoadConfig(configFilePath)
		if err != nil {
			fmt.Print(fmt.Errorf("error loading config file %s %w. creating a new file", configFilePath, err))
			cfg = &dpfconfig.Config{} // Initialize empty config
		} else {
			fmt.Printf("Loaded existing configuration from %s.\n", configFilePath)
		}

		// 3. Prompt for values
		if err = promptForGitLabConfig(reader, &cfg.GitLab); err != nil {
			fmt.Print(err)
			return nil
		}

		// 4. Save the configuration
		if err = saveConfig(configFilePath, cfg); err != nil {
			fmt.Print(err)
			return nil
		}
		fmt.Printf("Configuration saved successfully to %s\n", configFilePath)
		return nil
	},
}

func determineConfigPath(reader *bufio.Reader) (string, error) {
	// Get config path from flag first
	configFlagPath, err := rootCmd.Flags().GetString("config") // Use the flag value if provided
	if err != nil {
		return "", err
	}
	if configFlagPath != "" {
		fmt.Printf("Using config file path from flag: %s\n", configFlagPath)
		return configFlagPath, nil
	}

	// Prompt if not provided via flag or if it's the default
	fmt.Printf("Enter path for configuration file (default: %s): ", defaultConfigFile)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultConfigFile, nil
	}
	return os.ExpandEnv(input), nil
}

func readInput(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}
func promptForGitLabConfig(reader *bufio.Reader, glConfig *dpfconfig.GitLabConfig) error {
	input, err := readInput(reader, fmt.Sprintf("Enter GitLab endpoint (format: %s): ", gitlabEndpointFormat))
	if err != nil {
		return err
	}
	if input == "" {
		return fmt.Errorf("endpoint for Gitlab can not be empty")
	}

	// Validate the URL
	_, err = url.ParseRequestURI(input)
	if err != nil {
		return fmt.Errorf("invalid GitLab endpoint URL '%s': %w", input, err)
	}
	glConfig.Endpoint = input

	input, err = readInput(reader, fmt.Sprintf("Enter GitLab project ID (Hint: %s): ", projectIDHint))
	if err != nil {
		return err
	}
	if input != "" {
		glConfig.ProjectID = input
	}

	// Set the default for the jobHistoryFile
	jobHistoryFilePath := defaultJobsHistory
	input, err = readInput(reader, fmt.Sprintf("Enter path for jobs history file (default: %s): ", defaultJobsHistory))
	if err != nil {
		return err
	}
	if input != "" {
		jobHistoryFilePath = input
	}

	// Expand env vars and check if the path is an existing directory or invalid
	jobHistoryFilePath = os.ExpandEnv(jobHistoryFilePath)
	info, err := os.Stat(jobHistoryFilePath)
	if err != nil {
		// This file doesn't need to exist - but other errors should exit.
		if !os.IsNotExist(err) {
			// Path doesn't exist, but os.Stat failed for another reason (permissions, invalid path format, etc.)
			return fmt.Errorf("invalid path or permissions for jobs history file '%s': %w", jobHistoryFilePath, err)
		}
	}
	if info != nil && info.IsDir() {
		return fmt.Errorf("jobs history path cannot be an existing directory: %s", jobHistoryFilePath)
	}

	glConfig.JobHistoryFile = jobHistoryFilePath // Store the expanded path

	return nil
}

func saveConfig(filePath string, cfg *dpfconfig.Config) (reterr error) {
	// Ensure the directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error creating config file %s: %w", filePath, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			reterr = err
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Pretty print JSON
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("error encoding config to JSON: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
}

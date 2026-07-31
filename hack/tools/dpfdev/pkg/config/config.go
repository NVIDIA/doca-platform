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

package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config represents the overall configuration for the dpfdev tool
type Config struct {
	GitLab GitLabConfig `json:"gitlab"`
}

// GitLabConfig represents GitLab-specific configuration
type GitLabConfig struct {
	Endpoint       string `json:"endpoint"`
	ProjectID      string `json:"projectID"`
	JobHistoryFile string `json:"jobHistoryFile"`
}

// LoadConfig loads the configuration from the specified file
func LoadConfig(configFile string) (*Config, error) {
	file, err := os.Open(os.ExpandEnv(configFile))
	if err != nil {
		return nil, fmt.Errorf("error opening config file %s: %v", configFile, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var config Config
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("error decoding config file: %v", err)
	}

	if config.GitLab.JobHistoryFile == "" {
		config.GitLab.JobHistoryFile = "job_history.json"
	}

	if config.GitLab.Endpoint == "" {
		return nil, fmt.Errorf("gitlab.projectID is required in config file")
	}

	if config.GitLab.ProjectID == "" {
		return nil, fmt.Errorf("gitlab.projectID is required in config file")
	}

	return &config, nil
}

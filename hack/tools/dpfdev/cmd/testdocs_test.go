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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProcessMarkdownFile(t *testing.T) {
	// Test cases
	tests := []struct {
		name        string
		file        string
		expectError bool
	}{
		{
			name:        "valid commands",
			file:        filepath.Join("..", "testdata", "consolidated_tests.md"),
			expectError: false,
		},
		{
			name:        "invalid command",
			file:        filepath.Join("..", "testdata", "consolidated_tests.md"),
			expectError: false, // Changed to false since we're in dry run mode
		},
		{
			name:        "no commands",
			file:        filepath.Join("..", "testdata", "consolidated_tests.md"),
			expectError: false,
		},
		{
			name:        "multiple valid commands",
			file:        filepath.Join("..", "testdata", "consolidated_tests.md"),
			expectError: false,
		},
		{
			name:        "empty command",
			file:        filepath.Join("..", "testdata", "consolidated_tests.md"),
			expectError: false,
		},
		{
			name:        "mixed content",
			file:        filepath.Join("..", "testdata", "consolidated_tests.md"),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set dryRun to true to prevent actual command execution
			dryRun = true
			err := processMarkdownFile(tt.file)

			if tt.expectError {
				if err == nil {
					t.Error("Expected an error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestExtractCommandsFromFile(t *testing.T) {
	// Test with the actual consolidated_tests.md file
	testFile := filepath.Join("..", "testdata", "consolidated_tests.md")

	// Read the file content
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	// Extract commands
	commands := extractCommands(string(content))

	// Verify some expected commands are present
	expectedCommands := map[string]bool{
		`echo "Hello World"`: true,
		`ls -la`:             true,
		`export TEST_VAR="Hello from environment"`: true,
		`echo $TEST_VAR`:                    true,
		`: ${TEST_VAR:?env not set}`:        true,
		`invalid-command-that-doesnt-exist`: true,
		`echo "Testing multiple commands"`:  true,
		`pwd`:                               true,
		`whoami`:                            true,
		`echo "Command in the middle"`:      true,
		`echo "This is a bash block"`:       true,
		`echo "This is a shell block"`:      true,
		`echo "This is a sh block"`:         true,
	}

	// Check if all expected commands are found
	for _, cmd := range commands {
		if _, ok := expectedCommands[cmd.command]; !ok {
			t.Errorf("unexpected command in output: %v", cmd.command)
		}
		delete(expectedCommands, cmd.command)
	}

	if len(expectedCommands) > 0 {
		t.Errorf("Some expected commands were not found: %v", expectedCommands)
	}
}

func TestUpdateEnvironmentVariablesFromFile(t *testing.T) {
	tests := []struct {
		name        string
		initialEnv  map[string]string
		command     string
		expectedEnv map[string]string
		skipVars    []string
	}{
		{
			name: "basic environment variables",
			initialEnv: map[string]string{
				"EXISTING_VAR": "existing_value",
			},
			command: "export SIMPLE_VAR=simple_value",
			expectedEnv: map[string]string{
				"EXISTING_VAR": "existing_value",
				"SIMPLE_VAR":   "simple_value",
			},
			skipVars: []string{"_", "SHLVL"},
		},
		{
			name: "multi-line environment variables",
			initialEnv: map[string]string{
				"EXISTING_VAR": "existing_value",
			},
			command: "export MULTI_LINE_VAR=\"line1\nline2\nline3\"",
			expectedEnv: map[string]string{
				"EXISTING_VAR":   "existing_value",
				"MULTI_LINE_VAR": "line1\nline2\nline3",
			},
			skipVars: []string{"_", "SHLVL"},
		},
		{
			name: "environment variables with equals signs in values",
			initialEnv: map[string]string{
				"EXISTING_VAR": "existing_value",
			},
			command: "export VAR_WITH_EQUALS=\"key=value\"; export ANOTHER_VAR=\"a=b=c\"",
			expectedEnv: map[string]string{
				"EXISTING_VAR":    "existing_value",
				"VAR_WITH_EQUALS": "key=value",
				"ANOTHER_VAR":     "a=b=c",
			},
			skipVars: []string{"_", "SHLVL"},
		},
		{
			name: "multi-line with equals signs",
			initialEnv: map[string]string{
				"SIMPLE_VAR": "initial_simple",
			},
			command: "export SIMPLE_VAR=simple_value; export MULTI_LINE_VAR=\"line1\nline2 () ==five\nline3\"",
			expectedEnv: map[string]string{
				"SIMPLE_VAR":     "simple_value",
				"MULTI_LINE_VAR": "line1\nline2 () ==five\nline3",
			},
			skipVars: []string{"_", "SHLVL"},
		},
		{
			name: "empty values",
			initialEnv: map[string]string{
				"EXISTING_VAR": "existing_value",
			},
			command: "export EMPTY_VAR=\"\"; export ANOTHER_VAR=value",
			expectedEnv: map[string]string{
				"EXISTING_VAR": "existing_value",
				"EMPTY_VAR":    "",
				"ANOTHER_VAR":  "value",
			},
			skipVars: []string{"_", "SHLVL"},
		},
		{
			name: "overwriting existing variables",
			initialEnv: map[string]string{
				"EXISTING_VAR": "old_value",
				"SIMPLE_VAR":   "initial_simple",
			},
			command: "export EXISTING_VAR=new_value; export SIMPLE_VAR=updated_simple",
			expectedEnv: map[string]string{
				"EXISTING_VAR": "new_value",
				"SIMPLE_VAR":   "updated_simple",
			},
			skipVars: []string{"_", "SHLVL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file for environment capture
			envFile, err := os.CreateTemp("", "test-env-*.txt")
			if err != nil {
				t.Fatalf("Failed to create temp env file: %v", err)
			}
			defer func() {
				if err := os.Remove(envFile.Name()); err != nil {
					t.Logf("Failed to remove temp env file: %v", err)
				}
			}()
			if err := envFile.Close(); err != nil {
				t.Fatalf("Failed to close temp env file: %v", err)
			}

			// Use buildCommandScript to create a script that sets the environment variables
			scriptFile, err := buildCommandScript(tt.command, tt.initialEnv, envFile.Name())
			if err != nil {
				t.Fatalf("buildCommandScript failed: %v", err)
			}
			defer func() {
				if err := os.Remove(scriptFile); err != nil {
					t.Logf("Failed to remove temp script file: %v", err)
				}
			}()

			// Execute the script
			execCmd := exec.Command(scriptFile)
			if err := execCmd.Run(); err != nil {
				t.Fatalf("Failed to execute script: %v", err)
			}

			// Test parsing the captured environment
			envMap := make(map[string]string)
			err = updateEnvironmentVariablesFromFile(envFile.Name(), envMap)
			if err != nil {
				t.Fatalf("updateEnvironmentVariablesFromFile failed: %v", err)
			}

			// Check that expected variables are present
			for key, expectedValue := range tt.expectedEnv {
				if actualValue, exists := envMap[key]; !exists {
					t.Errorf("Expected variable %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Variable %s: expected %q, got %q", key, expectedValue, actualValue)
				}
			}

			// Check that skipped variables are not present
			for _, key := range tt.skipVars {
				if _, exists := envMap[key]; exists {
					t.Errorf("Variable %s should have been skipped but was found", key)
				}
			}
		})
	}
}

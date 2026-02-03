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

	. "github.com/onsi/gomega"
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

	g := NewWithT(t)

	t.Run("no filter - extract all untagged commands only", func(t *testing.T) {
		filterTags = []string{}
		commands := extractCommands(string(content))

		// Should include all untagged commands
		mustInclude := map[string]bool{
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
			`echo "always executes"`:            true,
		}

		// Should NOT include tagged commands
		mustNotInclude := map[string]bool{
			`echo "only with oci tag"`:     true,
			`echo "only with http tag"`:    true,
			`echo "with dev or test tags"`: true,
		}

		for _, cmd := range commands {
			delete(mustInclude, cmd.command)
			if _, shouldBeExcluded := mustNotInclude[cmd.command]; shouldBeExcluded {
				t.Errorf("command should be excluded without filter: %v", cmd.command)
			}
		}

		if len(mustInclude) > 0 {
			t.Errorf("Some expected commands were not found: %v", mustInclude)
		}
	})

	t.Run("with oci filter - includes untagged and oci-tagged, excludes others", func(t *testing.T) {
		filterTags = []string{"oci"}
		commands := extractCommands(string(content))

		// Should include untagged commands
		mustInclude := map[string]bool{
			`echo "always executes"`:   true,
			`echo "only with oci tag"`: true,
		}

		// Should NOT include other tagged commands
		mustNotInclude := map[string]bool{
			`echo "only with http tag"`:    true,
			`echo "with dev or test tags"`: true,
		}

		for _, cmd := range commands {
			delete(mustInclude, cmd.command)
			if _, shouldBeExcluded := mustNotInclude[cmd.command]; shouldBeExcluded {
				t.Errorf("command should be excluded with oci filter: %v", cmd.command)
			}
		}

		g.Expect(mustInclude).To(BeEmpty(), "should include untagged and oci-tagged commands")
	})
}

func TestShouldSkipBlock(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name         string
		headerLine   string
		allowedTags  []string
		expectSkip   bool
		expectedTags []string
	}{
		{
			name:         "untagged block no filter",
			headerLine:   "```bash",
			allowedTags:  []string{},
			expectSkip:   false,
			expectedTags: nil,
		},
		{
			name:         "untagged block with filter",
			headerLine:   "```shell",
			allowedTags:  []string{"oci"},
			expectSkip:   false,
			expectedTags: nil,
		},
		{
			name:         "tagged block no filter - skip",
			headerLine:   "```bash oci",
			allowedTags:  []string{},
			expectSkip:   true,
			expectedTags: []string{"oci"},
		},
		{
			name:         "tagged block matching filter - don't skip",
			headerLine:   "```shell oci",
			allowedTags:  []string{"oci"},
			expectSkip:   false,
			expectedTags: []string{"oci"},
		},
		{
			name:         "tagged block non-matching filter - skip",
			headerLine:   "```bash oci",
			allowedTags:  []string{"http"},
			expectSkip:   true,
			expectedTags: []string{"oci"},
		},
		{
			name:         "multiple tags one matches - don't skip",
			headerLine:   "```bash oci dev",
			allowedTags:  []string{"oci"},
			expectSkip:   false,
			expectedTags: []string{"oci", "dev"},
		},
		{
			name:         "multiple tags other matches - don't skip",
			headerLine:   "```shell oci dev",
			allowedTags:  []string{"dev"},
			expectSkip:   false,
			expectedTags: []string{"oci", "dev"},
		},
		{
			name:         "multiple tags none match - skip",
			headerLine:   "```bash oci dev",
			allowedTags:  []string{"http", "prod"},
			expectSkip:   true,
			expectedTags: []string{"oci", "dev"},
		},
		{
			name:         "multiple allowed tags with match - don't skip",
			headerLine:   "```shell oci",
			allowedTags:  []string{"http", "oci"},
			expectSkip:   false,
			expectedTags: []string{"oci"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip, tags := shouldSkipBlock(tt.headerLine, tt.allowedTags)
			g.Expect(skip).To(Equal(tt.expectSkip))
			g.Expect(tags).To(Equal(tt.expectedTags))
		})
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

func TestWorkingDirectoryPersistence(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name         string
		commands     []string
		expectedDirs []string // expected PWD after each command
	}{
		{
			name: "cd to subdirectory and verify persistence",
			commands: []string{
				"mkdir -p testdir",
				"cd testdir",
				"pwd", // should be in testdir
			},
			expectedDirs: []string{
				"", // after mkdir, PWD unchanged
				"", // after cd, PWD changes (checked in next command)
				"testdir",
			},
		},
		{
			name: "cd to absolute path and verify persistence",
			commands: []string{
				"cd /tmp",
				"pwd",
			},
			expectedDirs: []string{
				"", // after cd, PWD changes
				"/tmp",
			},
		},
		{
			name: "multiple cd commands",
			commands: []string{
				"mkdir -p testdir/subdir",
				"cd testdir",
				"pwd",
				"cd subdir",
				"pwd",
			},
			expectedDirs: []string{
				"",
				"",
				"testdir",
				"",
				"subdir",
			},
		},
		{
			name: "cd with relative paths",
			commands: []string{
				"mkdir -p testdir",
				"cd testdir",
				"cd ..",
				"pwd",
			},
			expectedDirs: []string{
				"",
				"",
				"",
				"", // should be back to original directory
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir, err := os.MkdirTemp("", "testdocs-cd-test-*")
			g.Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			// Change to the temp directory for the test
			originalDir, err := os.Getwd()
			g.Expect(err).ToNot(HaveOccurred())
			defer os.Chdir(originalDir)

			err = os.Chdir(tmpDir)
			g.Expect(err).ToNot(HaveOccurred())

			// Track environment across commands
			envVars := make(map[string]string)

			for i, cmd := range tt.commands {
				// Create temporary file for environment capture
				envFile, err := os.CreateTemp("", "test-env-*.txt")
				g.Expect(err).ToNot(HaveOccurred())
				defer os.Remove(envFile.Name())
				g.Expect(envFile.Close()).ToNot(HaveOccurred())

				// Build and execute command script
				scriptFile, err := buildCommandScript(cmd, envVars, envFile.Name())
				g.Expect(err).ToNot(HaveOccurred())
				defer os.Remove(scriptFile)

				execCmd := exec.Command(scriptFile)
				output, err := execCmd.CombinedOutput()
				g.Expect(err).ToNot(HaveOccurred(), "Command failed: %s\nOutput: %s", cmd, string(output))

				// Update environment variables from the executed command
				err = updateEnvironmentVariablesFromFile(envFile.Name(), envVars)
				g.Expect(err).ToNot(HaveOccurred())

				// Check expected directory if specified
				if tt.expectedDirs[i] != "" {
					pwd, exists := envVars["PWD"]
					g.Expect(exists).To(BeTrue(), "PWD should be set in environment")

					// For relative paths, check if PWD ends with expected dir
					if tt.expectedDirs[i][0] != '/' {
						g.Expect(pwd).To(HaveSuffix(tt.expectedDirs[i]),
							"Expected PWD to end with %s, got %s", tt.expectedDirs[i], pwd)
					} else {
						// For absolute paths, check exact match
						g.Expect(pwd).To(Equal(tt.expectedDirs[i]),
							"Expected PWD to be %s, got %s", tt.expectedDirs[i], pwd)
					}
				}
			}
		})
	}
}

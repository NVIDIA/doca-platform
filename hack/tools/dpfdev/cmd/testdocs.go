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
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/report"

	"github.com/spf13/cobra"
)

var (
	file            string
	dryRun          bool
	continueOnError bool
	verbose         bool
	junitOutput     string
	printScript     bool
)

// commandRunner defines the interface for executing commands
type commandRunner interface {
	// Execute runs a command and returns the output, error, and duration
	Execute(cmd *exec.Cmd) (string, time.Duration, error)
}

// standardCommandRunner executes commands and captures output at the end
type standardCommandRunner struct{}

func (r *standardCommandRunner) Execute(cmd *exec.Cmd) (string, time.Duration, error) {
	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)
	return string(output), duration, err
}

// verboseCommandRunner executes commands and streams output in real-time
type verboseCommandRunner struct{}

func (r *verboseCommandRunner) Execute(cmd *exec.Cmd) (string, time.Duration, error) {
	startTime := time.Now()

	// Set up pipes for real-time streaming
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", time.Since(startTime), fmt.Errorf("error creating stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", time.Since(startTime), fmt.Errorf("error creating stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", time.Since(startTime), fmt.Errorf("error starting command: %v", err)
	}

	// Capture output for result while also streaming it
	var outputBuilder strings.Builder

	// Stream stdout in real-time
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println("  " + line)
			outputBuilder.WriteString(line + "\n")
		}
	}()

	// Stream stderr in real-time
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(os.Stderr, "  %s\n", line)
			outputBuilder.WriteString(line + "\n")
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()
	duration := time.Since(startTime)

	return outputBuilder.String(), duration, err
}

// getCommandRunner returns the appropriate command runner based on configuration
func getCommandRunner() commandRunner {
	if verbose {
		return &verboseCommandRunner{}
	}
	return &standardCommandRunner{}
}

// commandInfo holds information about a command found in the markdown file
type commandInfo struct {
	command  string
	lineNum  int
	blockNum int
}

// commandResult holds the result of executing a command
type commandResult struct {
	info     commandInfo
	success  bool
	output   string
	error    error
	duration time.Duration
}

const (
	successMark = "✓"
	failureMark = "✗"
	skipMark    = "-"
	greenColor  = "\033[32m"
	redColor    = "\033[31m"
	yellowColor = "\033[33m"
	resetColor  = "\033[0m"
)

var testdocsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Test markdown documentation",
	Long:  `Test markdown documentation by executing bash commands in code blocks`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !strings.HasSuffix(file, ".md") {
			return fmt.Errorf("file must be a markdown file (.md)")
		}
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", file)
		}
		// Check for mutually exclusive flags
		if printScript && (dryRun || verbose || junitOutput != "" || continueOnError) {
			return fmt.Errorf("--print-script cannot be used with other execution flags (--dry-run, --verbose, --junit, --continue-on-failure)")
		}

		err := processMarkdownFile(file)
		if err != nil {
			return err
		}
		return nil
	},
}

func init() {
	testCmd.AddCommand(testdocsCmd)

	// Add flags
	testdocsCmd.Flags().StringVarP(&file, "file", "f", "", "Path to the markdown file to test")
	testdocsCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print commands without executing them")
	testdocsCmd.Flags().BoolVar(&continueOnError, "continue-on-failure", false, "Continue executing commands even if one fails")
	testdocsCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show output for all commands")
	testdocsCmd.Flags().StringVarP(&junitOutput, "junit", "x", "", "Path to output JUnit XML report file")
	testdocsCmd.Flags().BoolVar(&printScript, "print-script", false, "Print all commands as a script without executing them")

	// Mark file flag as required
	_ = testdocsCmd.MarkFlagRequired("file")
}

// JUnitTestSuites represents the root element of the JUnit XML
type JUnitTestSuites struct {
	XMLName    xml.Name                `xml:"testsuites"`
	TestSuites []report.JUnitTestSuite `xml:"testsuite"`
}

// extractCommands reads the markdown file and returns a slice of commandInfo
func extractCommands(content string) []commandInfo {
	var commands []commandInfo

	codeBlocks := extractCodeBlocks(content)
	// Process each match
	for blockNum, block := range codeBlocks {
		// Calculate the line number of the start of the code block content

		// Extract the commands from this block
		startLineNumber := block.LineNumber
		blockContent := block.Content

		// Skip completely empty blocks
		if strings.TrimSpace(blockContent) == "" {
			continue
		}

		blockCommands := strings.Split(blockContent, "\n")

		// Process each command in the block
		for i, cmd := range blockCommands {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" || strings.HasPrefix(cmd, "#") {
				continue
			}

			commands = append(commands, commandInfo{
				command:  cmd,
				lineNum:  startLineNumber + i,
				blockNum: blockNum + 1,
			})
		}
	}

	return commands
}

// codeBlock represents a code block found in markdown
type codeBlock struct {
	Content    string // The content of the code block
	LineNumber int    // The line number where the code block starts
}

// extractCodeBlocks finds all bash/shell code blocks in markdown content
// and returns them along with their starting line numbers
func extractCodeBlocks(content string) []codeBlock {
	var codeBlocks []codeBlock

	lines := strings.Split(content, "\n")
	inCodeBlock := false
	currentBlock := codeBlock{}
	blockContent := []string{}

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if !inCodeBlock {
			// Check if we're entering a code block
			if trimmedLine == "```bash" ||
				trimmedLine == "```shell" ||
				trimmedLine == "```sh" {
				inCodeBlock = true
				currentBlock.LineNumber = i + 2 // +1 for 0-index to 1-index, +1 to skip the opening ```
				blockContent = []string{}
			}
		} else {
			// Check if we're exiting a code block
			if trimmedLine == "```" {
				inCodeBlock = false
				currentBlock.Content = strings.Join(blockContent, "\n")
				codeBlocks = append(codeBlocks, currentBlock)
			} else {
				// Add line to current block
				blockContent = append(blockContent, line)
			}
		}
	}

	return codeBlocks
}

// buildCommandScript creates a script file that executes a command while preserving environment variables
// Returns the path to the script file or an error
func buildCommandScript(cmd string, envVars map[string]string, envFileName string) (string, error) {
	// Create a temporary script file
	tmpFile, err := os.CreateTemp("", "dpfdev-cmd-*.sh")
	if err != nil {
		return "", fmt.Errorf("error creating temp file: %v", err)
	}
	tmpFileName := tmpFile.Name()

	// Build script content
	var scriptContent strings.Builder

	// Add shebang
	scriptContent.WriteString("#!/usr/bin/env bash\n\n")

	// Set environment variables from previous command
	for k, v := range envVars {
		// Escape single quotes in the value
		escapedValue := strings.ReplaceAll(v, "'", "'\\''")
		scriptContent.WriteString(fmt.Sprintf("export %s='%s'\n", k, escapedValue))
	}

	// Add the command with proper error checking
	scriptContent.WriteString("\n")
	scriptContent.WriteString("set -e\n") // Exit on error
	scriptContent.WriteString(cmd)
	scriptContent.WriteString("\nCMD_EXIT_CODE=$?\n")
	scriptContent.WriteString("set +e\n\n")

	// Add command to save the environment to a file using null-terminated output
	scriptContent.WriteString(fmt.Sprintf("env -0 > %s\n", envFileName))
	scriptContent.WriteString(fmt.Sprintf("echo $CMD_EXIT_CODE > %s.exit\n", envFileName))

	// Write the script to the file
	if _, err := tmpFile.WriteString(scriptContent.String()); err != nil {
		if err := tmpFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		if err := os.Remove(tmpFileName); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		return "", fmt.Errorf("error writing to temp file: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
	}

	// Make the script executable
	if err := os.Chmod(tmpFileName, 0755); err != nil {
		if err := os.Remove(tmpFileName); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		return "", fmt.Errorf("error making script executable: %v", err)
	}

	return tmpFileName, nil
}

// updateEnvironmentVariablesFromFile reads environment variables from a file and updates the env map
func updateEnvironmentVariablesFromFile(envFile string, currentEnv map[string]string) error {
	// Read the environment variables from the file
	envData, err := os.ReadFile(envFile)
	if err != nil {
		return fmt.Errorf("error reading environment file: %v", err)
	}

	envLines := strings.Split(string(envData), "\x00")

	// Update our environment map
	for _, line := range envLines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			value := parts[1]

			// Skip some variables that shouldn't be propagated
			if key == "_" || key == "SHLVL" {
				continue
			}

			currentEnv[key] = value
		}
	}

	return nil
}

// executeCommands processes the extracted commands
func executeCommands(commands []commandInfo) []commandResult {
	results := []commandResult{}
	var failedCount int

	if len(commands) == 0 {
		return results
	}

	// Handle print-script mode - print all commands as a script and return
	if printScript {
		for _, cmd := range commands {
			fmt.Println(cmd.command)

			results = append(results, commandResult{
				info:     cmd,
				success:  true,
				output:   "Skipped (print-script)",
				error:    nil,
				duration: 0,
			})
		}
		return results
	}

	if dryRun {
		for _, cmd := range commands {
			fmt.Printf("L%d --> %s\n", cmd.lineNum, cmd.command)
			fmt.Printf("  %s%s%s Skipped (dry-run)\n", yellowColor, skipMark, resetColor)
			results = append(results, commandResult{
				info:     cmd,
				success:  true,
				output:   "Skipped (dry-run)",
				error:    nil,
				duration: 0,
			})
		}
		return results
	}

	// Get the appropriate command runner
	runner := getCommandRunner()

	// Keep track of environment variables
	envVars := make(map[string]string)

	// Process each command
	for _, cmd := range commands {
		fmt.Printf("L%d --> %s\n", cmd.lineNum, cmd.command)

		result := commandResult{
			info: cmd,
		}

		// Create a temporary file for environment capture
		envFile, err := os.CreateTemp("", "dpfdev-env-*.txt")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		if err = envFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		defer func() {
			if err := os.Remove(envFile.Name()); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
		}()

		// Build a script to handle running the command and capturing the environment.
		// The script will write the environment to a file and capture the exit code.
		scriptFile, err := buildCommandScript(cmd.command, envVars, envFile.Name())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		defer func() {
			if err := os.Remove(scriptFile); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
		}()

		// Execute the script
		execCmd := exec.Command(scriptFile)
		output, duration, err := runner.Execute(execCmd)

		// Read the exit code from the file
		exitCodeSuccess := true
		exitCodeData, readExitErr := os.ReadFile(envFile.Name() + ".exit")
		if readExitErr == nil {
			exitCode := strings.TrimSpace(string(exitCodeData))
			if exitCode != "0" {
				exitCodeSuccess = false
				if err == nil {
					// If the command runner didn't report an error but the exit code is non-zero
					err = fmt.Errorf("command exited with code %s", exitCode)
				}
			}
		}

		result.output = strings.TrimSpace(output)
		result.error = err
		result.duration = duration

		// Determine success based on exit code and command runner error
		if !exitCodeSuccess || err != nil {
			result.success = false
			failedCount++
			errMsg := fmt.Sprintf("error executing command '%s' (line %d): %v\n",
				cmd.command, cmd.lineNum, err)
			// Only append output if not verbose.
			// If verbose is enabled we log the output already.
			if !verbose {
				errMsg += fmt.Sprintf("   %s", result.output)
			}
			fmt.Fprintf(os.Stderr, "  %s%s Failed%s: %s (%.2fs)\n", redColor, failureMark, resetColor, errMsg, result.duration.Seconds())

			if !continueOnError {
				results = append(results, result)
				break
			}
		} else {
			result.success = true
			fmt.Printf("  %s%s Success%s (%.2fs)\n", greenColor, successMark, resetColor, result.duration.Seconds())
		}
		// Read and update environment variables
		if err := updateEnvironmentVariablesFromFile(envFile.Name(), envVars); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}

		results = append(results, result)
	}

	if failedCount > 0 {
		fmt.Printf("\nSummary: %d command(s) failed\n", failedCount)
	}

	return results
}

// generateJUnitXML creates a JUnit XML report from command results
func generateJUnitXML(results []commandResult, filename string) error {
	if filename == "" {
		return nil
	}

	// Create test suite
	suite := report.JUnitTestSuite{
		Name:  filepath.Base(file),
		Tests: len(results),
	}

	// Process results
	var totalTime float64
	for _, result := range results {
		testCase := report.JUnitTestCase{
			Name:      fmt.Sprintf("L%d: %s", result.info.lineNum, result.info.command),
			Classname: fmt.Sprintf("block.%d", result.info.blockNum),
			Time:      result.duration.Seconds(),
		}

		// Add stdout as a CDATA section in a custom XML element
		// This is done by manually adding the XML after encoding

		if !result.success {
			suite.Failures++
			testCase.Failure = &report.JUnitFailure{
				Message: fmt.Sprintf("Command failed: %v", result.error),
				Type:    "CommandFailure",
				Content: result.output,
			}
		}

		suite.TestCases = append(suite.TestCases, testCase)
		totalTime += testCase.Time
	}

	suite.Time = totalTime

	// Create a test suites wrapper
	suites := JUnitTestSuites{
		TestSuites: []report.JUnitTestSuite{suite},
	}

	// Check if we need to merge with an existing file
	if _, err := os.Stat(filename); err == nil {
		// File exists, try to read and parse it
		existingData, err := os.ReadFile(filename)
		if err == nil {
			var existingSuites JUnitTestSuites
			if err := xml.Unmarshal(existingData, &existingSuites); err == nil {
				// Merge the test suites
				suites.TestSuites = append(existingSuites.TestSuites, suite)
			}
		}
	}

	// Write to file
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create XML report file: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("Failed to close file: %v\n", err)
		}
	}()

	_, err = f.WriteString(xml.Header)
	if err != nil {
		return err
	}
	encoder := xml.NewEncoder(f)
	encoder.Indent("", "  ")
	if err := encoder.Encode(suites); err != nil {
		return fmt.Errorf("failed to encode XML report: %v", err)
	}

	fmt.Printf("\nJUnit XML report written to: %s\n", filename)
	return nil
}

func processMarkdownFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("error reading file %s: %v", path, err)
	}

	commands := extractCommands(string(content))
	results := executeCommands(commands)

	// Generate JUnit XML report if requested
	if junitOutput != "" {
		if err := generateJUnitXML(results, junitOutput); err != nil {
			return err
		}
	}

	// Return error if any command failed and we're not continuing on error
	for _, result := range results {
		if !result.success && !continueOnError {
			return fmt.Errorf("command execution failed: L%d > :%s", result.info.lineNum, result.info.command)
		}
	}

	return nil
}

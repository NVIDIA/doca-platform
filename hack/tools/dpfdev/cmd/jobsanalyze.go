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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

const dateFormat = "2006-01-02"

var (
	startDate       string
	endDate         string
	branch          string
	jobName         string
	skipDownload    bool
	printTable      bool
	printFailedURLs bool
	runnerName      string
	allRefs         bool
)

func init() {
	jobsCmd.AddCommand(jobsAnalyzeCmd)

	// Add flags
	jobsAnalyzeCmd.Flags().StringVar(&startDate, "start", time.Now().AddDate(0, 0, -7).Format(dateFormat), "Start date for job history (YYYY-MM-DD)")
	jobsAnalyzeCmd.Flags().StringVar(&endDate, "end", "", "End date for job history (YYYY-MM-DD)")
	jobsAnalyzeCmd.Flags().StringVar(&branch, "branch", "main", "Filter jobs by branch name")
	jobsAnalyzeCmd.Flags().StringVar(&jobName, "job", "", "Filter jobs by job name (contains match)")
	jobsAnalyzeCmd.Flags().BoolVar(&skipDownload, "skip-download", false, "Skip downloading new job data")
	jobsAnalyzeCmd.Flags().BoolVar(&printTable, "print-table", true, "Print analysis table")
	jobsAnalyzeCmd.Flags().BoolVar(&printFailedURLs, "print-failed-urls", false, "Print urls of failed jobs")
	jobsAnalyzeCmd.Flags().StringVar(&runnerName, "runner", "", "Filter jobs by runner/worker description/label (contains match)")
	jobsAnalyzeCmd.Flags().BoolVar(&allRefs, "all-refs", false, "Analyze jobs across all references (branches, tags, and MRs). If enabled, the branch flag is ignored.")
}

// JobHistory represents the full job history data structure
type JobHistory struct {
	LastUpdated time.Time    `json:"last_updated"`
	Jobs        []gitlab.Job `json:"jobs"`
}

var jobsAnalyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze GitLab job history",
	Long:  `Analyze GitLab job history, optionally filter by branch and job name, and calculate statistics for test analysis`,
	Example: `  # Analyze jobs using the default configuration and the last 7 days of history
  dpfdev jobs analyze

  # Analyze jobs using a specific config file
  dpfdev jobs analyze --config /path/to/your/config.json

  # Analyze jobs from the last 14 days:
  dpfdev jobs analyze --start $(date -d "14 days ago" +%Y-%m-%d)

  # Analyze jobs for a specific branch (e.g., 'main')
  dpfdev jobs analyze --branch main

  # Analyze jobs for a specific job name (e.g., 'test')
  dpfdev jobs analyze --job test

  # Analyze jobs for a specific runner/worker (contains match in description/label, e.g., 'my-runner')
  dpfdev jobs analyze --runner my-runner

  # Analyze jobs for a specific branch and runner (runner description contains match)
  dpfdev jobs analyze --branch main --runner worker-42

  # Analyze jobs for a specific branch and job name, print failed job URLs
  dpfdev jobs analyze --branch main --job test --print-failed-urls

  # Analyze jobs between specific dates
  dpfdev jobs analyze --start 2024-01-01 --end 2024-01-31`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate date formats first
		if err := validateDateFormat(startDate, false); err != nil {
			return err
		}
		if err := validateDateFormat(endDate, true); err != nil {
			return err
		}

		// If allRefs is not enabled, ensure branch is never empty
		if !allRefs && branch == "" {
			return fmt.Errorf("branch parameter cannot be empty, please specify a branch name or use --all-refs")
		}

		// Load configuration
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			return fmt.Errorf("failed to load config %w", err)
		}

		start, err := time.Parse(dateFormat, startDate)
		if err != nil {
			return fmt.Errorf("failed to parse start date '%s' %w", startDate, err)
		}
		// Set time to start of day
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)

		var end time.Time
		if endDate != "" {
			end, err = time.Parse(dateFormat, endDate)
			if err != nil {
				return fmt.Errorf("failed to parse end date '%s': %w", endDate, err)
			}
			if err != nil {
				return fmt.Errorf("failed to parse end date '%s': %w", endDate, err)
			}
			// Set time to end of day
			end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, time.UTC)
		}

		// Use the job history file path from config
		outputFile := os.ExpandEnv(cfg.GitLab.JobHistoryFile)

		// 1. Fetch and save jobs
		history, err := getJobHistory(cfg.GitLab, start, outputFile, skipDownload)
		if err != nil {
			// Do not return the error to cobra as this will trigger printing the usage command.
			log.Print(fmt.Errorf("could not get job history %w", err))
			return nil
		}

		// 2. Filter jobs if needed
		filteredJobs := history.Jobs
		// If allRefs is enabled, ignore branch filter, but still allow runnerName and jobName filtering
		if allRefs {
			filteredJobs = filterJobs(filteredJobs, "", jobName, runnerName, start, end)
		} else {
			filteredJobs = filterJobs(filteredJobs, branch, jobName, runnerName, start, end)
		}
		if len(filteredJobs) == 0 {
			// Build a descriptive message about the filters applied
			message := fmt.Sprintf("no jobs found matching the criteria: branch=%s, start_date=%s", branch, start.Format(dateFormat))

			if !end.IsZero() {
				message += fmt.Sprintf(", end_date='%s'", end.Format(dateFormat))
			}
			if jobName != "" {
				message += fmt.Sprintf(", job_name='%s'", jobName)
			}
			if runnerName != "" {
				message += fmt.Sprintf(", runner_name='%s'", runnerName)
			}

			log.Print(message) // Use log.Print as we aren't returning an error
			return nil
		}

		// 3. Display the statistics
		if printTable {
			if err := printJobAnalysis(filteredJobs); err != nil {
				// Do not return the error to cobra as this will trigger printing the usage command.
				log.Print(err)
				return nil
			}
		}

		// Print URLs of failed jobs if requested
		if printFailedURLs {
			printFailedJobURLs(filteredJobs)
		}

		return nil
	},
}

// getJobHistory downloads job history from GitLab and merges with existing history
// Returns the list of jobs and any error that occurred
func getJobHistory(config config.GitLabConfig, start time.Time, outputFile string, skipDownload bool) (*JobHistory, error) {
	// Try to load existing job history
	history, err := loadJobHistory(outputFile)
	if err != nil {
		// If we couldn't load the history, create a new one
		history = &JobHistory{
			LastUpdated: time.Time{},
			Jobs:        []gitlab.Job{},
		}
	}

	if skipDownload {
		if len(history.Jobs) == 0 && os.IsNotExist(err) { // Check if file didn't exist AND skipDownload is true
			return nil, fmt.Errorf("job history: file does not exist (%s) and download was skipped", outputFile)
		}
		return history, nil
	}

	// Get the gitlab token from the environment
	token := os.Getenv("GITLAB_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITLAB_API_TOKEN environment variable is not set")
	}
	// Initialize GitLab client
	client := gitlab.NewClient(config.Endpoint, token, config.ProjectID)

	// Determine the date to fetch jobs since.
	fetchSinceDate := history.LastUpdated // Default to last update time
	var oldestJobDate time.Time
	if len(history.Jobs) > 0 {
		oldestJobDate = history.Jobs[0].CreatedAt // Assume sorted initially, but verify
		for _, job := range history.Jobs {
			if job.CreatedAt.Before(oldestJobDate) {
				oldestJobDate = job.CreatedAt
			}
		}
	}

	needsBackfill := false
	if !oldestJobDate.IsZero() {
		// Normalize both dates to the start of their respective days (UTC) before comparing
		startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		oldestJobDay := time.Date(oldestJobDate.Year(), oldestJobDate.Month(), oldestJobDate.Day(), 0, 0, 0, 0, time.UTC)
		if startDay.Before(oldestJobDay) {
			needsBackfill = true
		}
	}

	// If we need to backfill from an earlier start date, fetch everything from that point.
	// Otherwise, if the history is empty (first run) or LastUpdated is zero, fetch from the user's start date.
	if needsBackfill || fetchSinceDate.IsZero() {
		fetchSinceDate = start
	}

	// Fetch jobs created *after* the determined fetch date.
	// Look for jobs a few hours older than the start date. This means we collect updated status for
	// jobs that may have been running the last time this was run.
	// Jobs are de-duplicated later on.
	newJobs, err := client.ListJobs(log.Writer(), fetchSinceDate.Add(-(time.Hour * 4)))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch jobs %w", err)
	}

	// Filter out skipped jobs
	validJobs := []gitlab.Job{}
	for _, job := range newJobs {
		if job.Status == "skipped" {
			continue
		}
		validJobs = append(validJobs, job)
	}

	// If we have new jobs, merge them with existing ones and save.
	if len(validJobs) > 0 {
		// Add new jobs to the history
		jobMap := make(map[int]gitlab.Job)
		existingJobCount := len(history.Jobs) // Store count before merge

		// First, index existing jobs by ID
		for _, job := range history.Jobs {
			jobMap[job.ID] = job
		}

		// Add or update jobs from the new list
		for _, job := range validJobs {
			jobMap[job.ID] = job
		}

		// Convert map back to slice
		history.Jobs = make([]gitlab.Job, 0, len(jobMap))
		for _, job := range jobMap {
			history.Jobs = append(history.Jobs, job)
		}

		// Calculate the actual number of new jobs added
		newJobCount := len(history.Jobs) - existingJobCount
		fmt.Printf("Added %d jobs to the history\n", newJobCount)

		// Update last updated time
		history.LastUpdated = time.Now().UTC()

		// Save updated history
		if err := saveJobHistory(history, outputFile); err != nil {
			return nil, err
		}
	}
	return history, nil
}

// loadJobHistory loads job history from a JSON file
func loadJobHistory(filename string) (*JobHistory, error) {
	if !fileExists(filename) {
		return nil, os.ErrNotExist
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open job history file %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read job history file %w", err)
	}

	var history JobHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to parse job history file %w", err)
	}

	return &history, nil
}

// saveJobHistory saves job history to a JSON file
func saveJobHistory(history *JobHistory, filename string) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job history %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write job history file %w", err)
	}
	return nil
}

// filterJobs filters a list of jobs by branch, job name, runner description (contains, case-insensitive), and date range
func filterJobs(jobs []gitlab.Job, branch, jobName, runnerDesc string, start, end time.Time) []gitlab.Job {
	if branch == "" && jobName == "" && runnerDesc == "" && start.IsZero() && end.IsZero() {
		return jobs
	}

	filtered := []gitlab.Job{}
	for _, job := range jobs {
		// Filter by start date if provided
		if !start.IsZero() && job.CreatedAt.Before(start) {
			continue
		}

		// Filter by end date if provided
		if !end.IsZero() && job.CreatedAt.After(end) {
			continue
		}

		// Filter by branch (case insensitive)
		if branch != "" && !strings.EqualFold(job.Branch, branch) {
			continue
		}

		// Filter by job name (case insensitive contains)
		if jobName != "" && !strings.EqualFold(strings.ToLower(job.Name), strings.ToLower(jobName)) {
			continue
		}

		// Filter by runner description (case insensitive contains)
		if runnerDesc != "" {
			if job.Runner == nil || !strings.Contains(strings.ToLower(job.Runner.Description), strings.ToLower(runnerDesc)) {
				continue
			}
		}

		filtered = append(filtered, job)
	}

	return filtered
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// printJobAnalysis analyzes job data and prints statistics
func printJobAnalysis(jobs []gitlab.Job) error {
	if len(jobs) == 0 {
		return fmt.Errorf("no jobs found matching the specified criteria")
	}
	// Group jobs by name
	jobStats := make(map[string]struct {
		TotalRuns       int
		Failures        int
		TotalDuration   float64
		SuccessDuration float64
		SuccessfulJobs  int
	})

	// Calculate statistics for each job
	for _, job := range jobs {
		stats := jobStats[job.Name]
		stats.TotalRuns++

		// Skip jobs with invalid duration
		if job.Duration < 0 {
			stats.TotalRuns-- // Don't count this run in the total
			continue
		}

		// Only count successful jobs for average duration calculation
		switch job.Status {
		case "success":
			stats.SuccessDuration += job.Duration
			stats.SuccessfulJobs++
		case "failed":
			stats.Failures++
		}

		stats.TotalDuration += job.Duration
		jobStats[job.Name] = stats
	}

	// Print header
	fmt.Printf("%-40s | %-10s | %-10s | %-15s | %-15s\n", "Job Name", "Total Runs", "Failures", "Success Rate", "Avg Duration")
	fmt.Println(strings.Repeat("-", 100))

	// Print statistics for each job
	// Get job names and sort them alphabetically
	jobNames := make([]string, 0, len(jobStats))
	for name := range jobStats {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames) // Use sort.Strings for alphabetical sorting

	for _, name := range jobNames { // Iterate over sorted names
		stats := jobStats[name] // Get stats using the sorted name
		successRate := 0.0
		if stats.TotalRuns > 0 {
			successRate = float64(stats.TotalRuns-stats.Failures) / float64(stats.TotalRuns) * 100
		}

		avgDuration := 0.0
		if stats.SuccessfulJobs > 0 {
			avgDuration = stats.SuccessDuration / float64(stats.SuccessfulJobs)
		}

		fmt.Printf("%-40s | %-10d | %-10d | %6.2f%%      | %15s\n",
			name,
			stats.TotalRuns,
			stats.Failures,
			successRate,
			time.Duration(avgDuration*float64(time.Second)).Round(time.Second))
	}

	return nil
}

// printFailedJobURLs prints the URLs of failed jobs
func printFailedJobURLs(jobs []gitlab.Job) {
	failedJobs := make(map[string][]string)

	// Group failed jobs by name
	for _, job := range jobs {
		if job.Status == "failed" {
			failedJobs[job.Name] = append(failedJobs[job.Name], job.URL)
		}
	}

	fmt.Println(strings.Repeat("-", 100))

	for _, urls := range failedJobs {
		for _, url := range urls {
			fmt.Printf("  %s\n", url)
		}
		fmt.Println()
	}
}

// validateDateFormat checks if the date string matches the YYYY-MM-DD format
func validateDateFormat(dateStr string, canBeEmpty bool) error {
	datePattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	// Allow empty string for endDate
	if dateStr == "" && canBeEmpty {
		return nil
	}
	if !datePattern.MatchString(dateStr) {
		return fmt.Errorf("invalid format for  flag: expected YYYY-MM-DD, got '%s'", dateStr)
	}
	return nil
}

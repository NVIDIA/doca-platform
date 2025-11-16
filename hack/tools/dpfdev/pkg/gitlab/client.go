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

package gitlab

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Job represents a GitLab CI job
type Job struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Duration  float64   `json:"duration"`
	Pipeline  struct {
		ID int `json:"id"`
	} `json:"pipeline"`
	Branch string `json:"ref"`
	URL    string `json:"web_url"`
	Runner *struct {
		ID          int    `json:"id"`
		Description string `json:"description"`
	} `json:"runner,omitempty"`
}

// Runner represents a GitLab CI runner
type Runner struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
	Paused      bool   `json:"paused"`
	IsShared    bool   `json:"is_shared"`
	RunnerType  string `json:"runner_type"`
	Name        string `json:"name,omitempty"`
	Online      bool   `json:"online"`
	Status      string `json:"status"`
	IPAddress   string `json:"ip_address"`
}

// RunnerDetails represents detailed information about a GitLab CI runner.
type RunnerDetails struct {
	Active          bool      `json:"active"`
	Paused          bool      `json:"paused"`
	Architecture    string    `json:"architecture,omitempty"`
	Description     string    `json:"description"`
	ID              int       `json:"id"`
	IPAddress       string    `json:"ip_address"`
	IsShared        bool      `json:"is_shared"`
	RunnerType      string    `json:"runner_type"`
	ContactedAt     time.Time `json:"contacted_at"`
	MaintenanceNote string    `json:"maintenance_note,omitempty"`
	Name            string    `json:"name,omitempty"`
	Online          bool      `json:"online"`
	Status          string    `json:"status"`
	Platform        string    `json:"platform,omitempty"`
	Projects        []struct {
		ID                int    `json:"id"`
		Name              string `json:"name"`
		NameWithNamespace string `json:"name_with_namespace"`
		Path              string `json:"path"`
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"projects"`
	Revision       string   `json:"revision,omitempty"`
	TagList        []string `json:"tag_list"`
	Version        string   `json:"version,omitempty"`
	AccessLevel    string   `json:"access_level"`
	MaximumTimeout int      `json:"maximum_timeout"`
}

// Client represents a GitLab API client
type Client struct {
	baseURL   string
	token     string
	projectID string
	client    *http.Client
}

// NewClient creates a new GitLab API client
func NewClient(endpoint, token, projectID string) *Client {
	return &Client{
		baseURL:   endpoint,
		token:     token,
		projectID: projectID,
		client:    &http.Client{},
	}
}

// ListJobs retrieves all jobs for a project within the given date range
func (c *Client) ListJobs(w io.Writer, start time.Time) ([]Job, error) {
	var allJobs []Job
	page := 1
	perPage := 100

	for {
		jobs, err := c.getJobsPage(page, perPage)
		if err != nil {
			return nil, err
		}

		if len(jobs) == 0 {
			break
		}

		// Filter jobs by date
		for _, job := range jobs {
			// Skip jobs created before start date.
			if !start.IsZero() && job.CreatedAt.Before(start) {
				continue
			}

			allJobs = append(allJobs, job)
		}

		// If we've found jobs older than our start date, we can stop paginating
		if c.hasOlderJobs(w, jobs, start) {
			break
		}

		page++
	}

	return allJobs, nil
}

// hasOlderJobs checks if any jobs in the list are older than the specified date
func (c *Client) hasOlderJobs(w io.Writer, jobs []Job, start time.Time) bool {
	if start.IsZero() {
		return false
	}

	for _, job := range jobs {
		if job.CreatedAt.Before(start) {
			return true
		}
	}

	if len(jobs) > 0 {
		lastJob := jobs[len(jobs)-1]
		_, _ = fmt.Fprintf(w, "Processing jobs with start time %s\n", lastJob.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return false
}

// getJobsPage retrieves a single page of jobs from GitLab
func (c *Client) getJobsPage(page, perPage int) ([]Job, error) {
	url := fmt.Sprintf("%s/projects/%s/jobs?page=%d&per_page=%d",
		c.baseURL,
		c.projectID,
		page,
		perPage)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var jobs []Job
	if err := json.Unmarshal(body, &jobs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return jobs, nil
}

// ListRunners retrieves all runners for a gitlab project. all enabled runners for the project are listed.
func (c *Client) ListRunners(tagFilters string, typeFilters string) ([]Runner, error) {
	var allRunners []Runner
	page := 1
	perPage := 100

	for {
		runners, err := c.getRunnersPage(page, perPage, tagFilters, typeFilters)
		if err != nil {
			return nil, err
		}

		if len(runners) == 0 {
			break
		}

		allRunners = append(allRunners, runners...)
		page++
	}

	return allRunners, nil
}

// getRunnersPage retrieves a single page of runners from GitLab
func (c *Client) getRunnersPage(page, perPage int, tagFilters string, typeFilters string) ([]Runner, error) {
	queryParams := url.Values{}

	queryParams.Add("page", strconv.Itoa(page))
	queryParams.Add("per_page", strconv.Itoa(perPage))

	if tagFilters != "" {
		queryParams.Add("tag_list", tagFilters)
	}
	if typeFilters != "" {
		queryParams.Add("type", typeFilters)
	}

	url := fmt.Sprintf("%s/projects/%s/runners?%s",
		c.baseURL,
		c.projectID,
		queryParams.Encode())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var runners []Runner
	if err := json.Unmarshal(body, &runners); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return runners, nil
}

// GetRunnerDetails fetches detailed information about a specific runner by ID.
func (c *Client) GetRunnerDetails(runnerID int) (*RunnerDetails, error) {
	url := fmt.Sprintf("%s/runners/%d", c.baseURL, runnerID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var runnerDetails RunnerDetails
	if err := json.Unmarshal(body, &runnerDetails); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &runnerDetails, nil
}

// PauseRunner pauses a runner by ID.
func (c *Client) PauseRunner(runnerID int) error {
	return c.updateRunnerPausedState(runnerID, true)
}

// UnpauseRunner unpauses a runner by ID.
func (c *Client) UnpauseRunner(runnerID int) error {
	return c.updateRunnerPausedState(runnerID, false)
}

// updateRunnerPausedState updates the paused state of a runner.
func (c *Client) updateRunnerPausedState(runnerID int, paused bool) error {
	url := fmt.Sprintf("%s/runners/%d", c.baseURL, runnerID)

	// Create JSON payload
	payload := map[string]interface{}{
		"paused": paused,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("PUT", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteRunner deletes a runner by ID from the GitLab instance.
// Note: This requires admin or owner permissions for the runner.
func (c *Client) DeleteRunner(runnerID int) error {
	url := fmt.Sprintf("%s/runners/%d", c.baseURL, runnerID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body for error messages
	body, _ := io.ReadAll(resp.Body)

	// GitLab returns 204 No Content on successful deletion
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

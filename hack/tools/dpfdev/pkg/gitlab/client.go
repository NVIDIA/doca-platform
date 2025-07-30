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

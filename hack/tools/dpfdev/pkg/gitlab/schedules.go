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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PipelineSchedule represents a GitLab pipeline schedule. Variables are only
// populated by GetPipelineSchedule, and only when the caller has the
// Maintainer role or above (or owns the schedule); GitLab omits the field
// otherwise, which is reflected here as a nil slice.
type PipelineSchedule struct {
	ID           int       `json:"id"`
	Description  string    `json:"description"`
	Ref          string    `json:"ref"`
	Cron         string    `json:"cron"`
	CronTimezone string    `json:"cron_timezone"`
	Active       bool      `json:"active"`
	NextRunAt    time.Time `json:"next_run_at"`
	Owner        struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"owner"`
	Variables []PipelineScheduleVariable `json:"variables"`
}

// PipelineScheduleVariable represents a variable of a pipeline schedule.
type PipelineScheduleVariable struct {
	Key          string `json:"key"`
	VariableType string `json:"variable_type"`
	Value        string `json:"value"`
	Raw          bool   `json:"raw"`
}

// APIError is returned for GitLab API responses with a non-success status
// code, so callers can distinguish e.g. 403 (missing permission) from 404
// (not found) and act on it.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, e.Body)
}

// IsForbidden reports whether the error is a GitLab 403 response.
func IsForbidden(err error) bool { return errorHasStatus(err, http.StatusForbidden) }

// IsNotFound reports whether the error is a GitLab 404 response.
func IsNotFound(err error) bool { return errorHasStatus(err, http.StatusNotFound) }

func errorHasStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// ListPipelineSchedules retrieves all pipeline schedules of the project.
// The list endpoint does not include variables; use GetPipelineSchedule for
// a single schedule with its variables.
func (c *Client) ListPipelineSchedules() ([]PipelineSchedule, error) {
	var allSchedules []PipelineSchedule
	page := 1
	perPage := 100

	for {
		url := fmt.Sprintf("%s/projects/%s/pipeline_schedules?page=%d&per_page=%d",
			c.baseURL, c.projectID, page, perPage)

		var schedules []PipelineSchedule
		if err := c.doJSONRequest("GET", url, nil, &schedules); err != nil {
			return nil, err
		}

		allSchedules = append(allSchedules, schedules...)
		if len(schedules) < perPage {
			break
		}
		page++
	}

	return allSchedules, nil
}

// GetPipelineSchedule retrieves a single pipeline schedule including its
// variables. Reading the variables of a schedule owned by another user works
// with the Maintainer role or above and does not require taking ownership;
// for callers below Maintainer that do not own the schedule, GitLab omits
// the variables field and Variables is nil.
func (c *Client) GetPipelineSchedule(scheduleID int) (*PipelineSchedule, error) {
	url := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d", c.baseURL, c.projectID, scheduleID)

	var schedule PipelineSchedule
	if err := c.doJSONRequest("GET", url, nil, &schedule); err != nil {
		return nil, err
	}

	return &schedule, nil
}

// CreatePipelineSchedule creates a new pipeline schedule owned by the
// token's user. Requires the Developer role and access to the (possibly
// protected) ref.
func (c *Client) CreatePipelineSchedule(description, ref, cron, cronTimezone string, active bool) (*PipelineSchedule, error) {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules", c.baseURL, c.projectID)

	var schedule PipelineSchedule
	if err := c.doJSONRequest("POST", endpoint, scheduleForm(description, ref, cron, cronTimezone, active), &schedule); err != nil {
		return nil, err
	}

	return &schedule, nil
}

// UpdatePipelineSchedule updates the schedule's fields. Like the variable
// modification calls it requires the schedule owner's token (or instance
// admin) and returns 403 otherwise; ownership is never changed by this call.
func (c *Client) UpdatePipelineSchedule(scheduleID int, description, ref, cron, cronTimezone string, active bool) error {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d", c.baseURL, c.projectID, scheduleID)

	return c.doJSONRequest("PUT", endpoint, scheduleForm(description, ref, cron, cronTimezone, active), nil)
}

// DeletePipelineSchedule deletes a pipeline schedule. Unlike updates this
// only requires the Maintainer role, not schedule ownership.
func (c *Client) DeletePipelineSchedule(scheduleID int) error {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d", c.baseURL, c.projectID, scheduleID)

	return c.doJSONRequest("DELETE", endpoint, nil, nil)
}

// scheduleForm builds the request body shared by the schedule create and
// update endpoints. cronTimezone is omitted when empty, keeping the GitLab
// default ("UTC") respectively the schedule's current value.
func scheduleForm(description, ref, cron, cronTimezone string, active bool) url.Values {
	form := url.Values{}
	form.Set("description", description)
	form.Set("ref", ref)
	form.Set("cron", cron)
	form.Set("active", fmt.Sprintf("%t", active))
	if cronTimezone != "" {
		form.Set("cron_timezone", cronTimezone)
	}
	return form
}

// CreatePipelineScheduleVariable creates a new variable on a pipeline
// schedule. GitLab authorizes this with update_pipeline_schedule, which only
// the schedule owner (or an instance administrator) holds, so this fails
// with 403 for schedules owned by other users. Ownership is never changed
// by this call.
func (c *Client) CreatePipelineScheduleVariable(scheduleID int, key, value, variableType string) error {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d/variables", c.baseURL, c.projectID, scheduleID)

	form := scheduleVariableForm(value, variableType)
	form.Set("key", key)

	return c.doJSONRequest("POST", endpoint, form, nil)
}

// UpdatePipelineScheduleVariable updates the value of an existing pipeline
// schedule variable. Like CreatePipelineScheduleVariable it requires the
// schedule owner's token (or instance admin) and returns 403 otherwise;
// ownership is never changed by this call. Returns 404 if the variable does
// not exist on the schedule.
func (c *Client) UpdatePipelineScheduleVariable(scheduleID int, key, value, variableType string) error {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d/variables/%s",
		c.baseURL, c.projectID, scheduleID, url.PathEscape(key))

	return c.doJSONRequest("PUT", endpoint, scheduleVariableForm(value, variableType), nil)
}

// scheduleVariableForm builds the request body shared by the schedule
// variable create and update endpoints.
func scheduleVariableForm(value, variableType string) url.Values {
	form := url.Values{}
	form.Set("value", value)
	if variableType != "" {
		form.Set("variable_type", variableType)
	}
	return form
}

// DeletePipelineScheduleVariable deletes a variable from a pipeline
// schedule. Like DeletePipelineSchedule this only requires the Maintainer
// role, not schedule ownership.
func (c *Client) DeletePipelineScheduleVariable(scheduleID int, key string) error {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d/variables/%s",
		c.baseURL, c.projectID, scheduleID, url.PathEscape(key))

	return c.doJSONRequest("DELETE", endpoint, nil, nil)
}

// TakeOwnershipPipelineSchedule makes the token's user the owner of the
// schedule and returns the updated schedule. Requires the Maintainer role.
// This is one-way: GitLab has no way to assign ownership to another user, so
// the previous owner can only get the schedule back by taking ownership
// themselves. From this call on, the schedule runs with the new owner's
// permissions.
func (c *Client) TakeOwnershipPipelineSchedule(scheduleID int) (*PipelineSchedule, error) {
	endpoint := fmt.Sprintf("%s/projects/%s/pipeline_schedules/%d/take_ownership", c.baseURL, c.projectID, scheduleID)

	var schedule PipelineSchedule
	if err := c.doJSONRequest("POST", endpoint, nil, &schedule); err != nil {
		return nil, err
	}

	return &schedule, nil
}

// doJSONRequest performs an authenticated request against the GitLab API and
// decodes the JSON response into out (if non-nil). Non-2xx responses are
// returned as *APIError, so callers can classify them with IsForbidden and
// IsNotFound. Form values, if any, are sent url-encoded in the body. It is
// not tied to schedules; new endpoint wrappers should prefer it over
// hand-rolling the request/response handling.
func (c *Client) doJSONRequest(method, endpoint string, form url.Values, out interface{}) error {
	var reqBody io.Reader
	if form != nil {
		reqBody = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequest(method, endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("failed to decode response: %v", err)
		}
	}

	return nil
}

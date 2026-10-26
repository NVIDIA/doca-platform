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

import "fmt"

// PipelineDetails holds the fields of a single pipeline that its list
// representation lacks.
type PipelineDetails struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	// DetailedStatus.Label is what the UI shows, e.g. "passed with warnings"
	// for a pipeline whose status is success but an allowed-to-fail job failed.
	DetailedStatus struct {
		Label string `json:"label"`
	} `json:"detailed_status"`
}

// Bridge is a job that triggers a downstream (child or multi-project) pipeline.
type Bridge struct {
	Name               string `json:"name"`
	Status             string `json:"status"`
	AllowFailure       bool   `json:"allow_failure"`
	DownstreamPipeline *struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"downstream_pipeline"`
}

// GetPipeline retrieves a single pipeline including its detailed status.
func (c *Client) GetPipeline(pipelineID int) (*PipelineDetails, error) {
	endpoint := fmt.Sprintf("%s/projects/%s/pipelines/%d", c.baseURL, c.projectID, pipelineID)

	var pipeline PipelineDetails
	if err := c.doJSONRequest("GET", endpoint, nil, &pipeline); err != nil {
		return nil, err
	}

	return &pipeline, nil
}

// ListPipelineBridges retrieves the trigger jobs of a pipeline with their
// downstream pipelines. Failures of downstream pipelines do not appear in the
// pipeline's own jobs list.
func (c *Client) ListPipelineBridges(pipelineID int) ([]Bridge, error) {
	endpoint := fmt.Sprintf("%s/projects/%s/pipelines/%d/bridges?per_page=100", c.baseURL, c.projectID, pipelineID)

	var bridges []Bridge
	if err := c.doJSONRequest("GET", endpoint, nil, &bridges); err != nil {
		return nil, err
	}

	return bridges, nil
}

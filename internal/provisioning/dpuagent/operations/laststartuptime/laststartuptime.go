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

package laststartuptime

import (
	"context"
	"time"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type ReportLastStartupTime struct {
}

func (r *ReportLastStartupTime) Name() string {
	return "Report Last Startup Time"
}

func (r *ReportLastStartupTime) ConditionType() string {
	return "LastStartupTimeReported"
}

func (r *ReportLastStartupTime) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (r *ReportLastStartupTime) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return true
}

func (r *ReportLastStartupTime) Execute(execCtx context.Context, optCtx *operations.Context) error {
	now := metav1.Now()
	optCtx.Status.LastStartupTime = &now
	klog.Infof("Reported LastStartupTime: %s", now.Format(time.RFC3339))
	return nil
}

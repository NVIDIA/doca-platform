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

package staticfiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
)

const (
	defaultRootFS = "/"
)

type VerifyStaticFiles struct {
	rootFS string
}

func (s *VerifyStaticFiles) Name() string {
	return "Verify Static Files"
}

func (s *VerifyStaticFiles) ConditionType() string {
	return "StaticFilesVerified"
}

func (s *VerifyStaticFiles) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (s *VerifyStaticFiles) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *VerifyStaticFiles) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if s.rootFS == "" {
		s.rootFS = defaultRootFS
	}
	for _, file := range optCtx.DPUFlavor.Spec.ConfigFiles {
		filePath := filepath.Join(s.rootFS, file.Path)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return fmt.Errorf("file %s does not exist", filePath)
		}
	}
	return nil
}

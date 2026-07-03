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
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		filePath := resolvePath(s.rootFS, file.Path)
		fileType := provisioningv1.ConfigFileTypeCloudInit
		if file.Type != nil {
			fileType = *file.Type
		}
		switch fileType {
		case provisioningv1.ConfigFileTypeCloudInit:
			// cloud-init files are materialized before dpu-agent starts.
			if _, err := os.Stat(filePath); err != nil {
				return fmt.Errorf("file %s: %w", filePath, err)
			}
		case provisioningv1.ConfigFileTypeAgentApplied:
			fileMode, err := parseFileMode(file.Permissions)
			if err != nil {
				return fmt.Errorf("invalid permissions for %s: %w", filePath, err)
			}
			content, err := s.resolveContentFromConfigMap(execCtx, optCtx, file)
			if err != nil {
				return err
			}
			if err := writeConfigFile(filePath, file.Operation, content, fileMode); err != nil {
				return fmt.Errorf("writing config file %s: %w", filePath, err)
			}
			if err := applyPermissions(filePath, file.Permissions); err != nil {
				return fmt.Errorf("applying permissions to %s: %w", filePath, err)
			}
		default:
			return fmt.Errorf("unsupported config file type %q for %s", fileType, file.Path)
		}
	}
	return nil
}

func (s *VerifyStaticFiles) resolveContentFromConfigMap(execCtx context.Context, optCtx *operations.Context, file provisioningv1.ConfigFile) (string, error) {
	if optCtx.Client == nil {
		return "", fmt.Errorf("kubernetes client is not initialized")
	}
	if file.ContentFrom == nil || file.ContentFrom.ConfigMapKeyRef == nil {
		return "", fmt.Errorf("contentFrom.configMapKeyRef is required for file %s", file.Path)
	}
	ref := file.ContentFrom.ConfigMapKeyRef
	nsName := client.ObjectKey{
		Namespace: optCtx.Options.DPUNamespace,
		Name:      ref.Name,
	}
	cm := &corev1.ConfigMap{}
	if err := optCtx.Client.Get(execCtx, nsName, cm); err != nil {
		return "", fmt.Errorf("failed to resolve config file %s from ConfigMap %s/%s key %s: %w", file.Path, nsName.Namespace, nsName.Name, ref.Key, err)
	}
	content, ok := cm.Data[ref.Key]
	if !ok {
		binaryContent, binaryOK := cm.BinaryData[ref.Key]
		if !binaryOK {
			return "", fmt.Errorf("failed to resolve config file %s from ConfigMap %s/%s key %s: key not found", file.Path, nsName.Namespace, nsName.Name, ref.Key)
		}
		return string(binaryContent), nil
	}
	return content, nil
}

func writeConfigFile(filePath string, operation provisioningv1.DPUFlavorFileOp, content string, fileMode os.FileMode) error {
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("creating parent directory %s: %w", parentDir, err)
	}
	if operation == provisioningv1.FileAppend {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
		if err != nil {
			return fmt.Errorf("opening %s for append: %w", filePath, err)
		}
		if _, err := file.WriteString(content); err != nil {
			_ = file.Close()
			return fmt.Errorf("appending content to %s: %w", filePath, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("closing %s after append: %w", filePath, err)
		}
		return nil
	}
	if err := os.WriteFile(filePath, []byte(content), fileMode); err != nil {
		return fmt.Errorf("writing content to %s: %w", filePath, err)
	}
	return nil
}

func parseFileMode(permissions string) (os.FileMode, error) {
	// Default to restrictive creation mode to avoid transiently broad permissions.
	if permissions == "" {
		return 0600, nil
	}
	mode, err := strconv.ParseUint(permissions, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(mode), nil
}

func applyPermissions(path, permissions string) error {
	if permissions == "" {
		return nil
	}
	mode, err := strconv.ParseUint(permissions, 8, 32)
	if err != nil {
		return fmt.Errorf("invalid permissions %q: %w", permissions, err)
	}
	if err := os.Chmod(path, os.FileMode(mode)); err != nil {
		return fmt.Errorf("chmod %s to %s: %w", path, permissions, err)
	}
	return nil
}

func resolvePath(rootFS, targetPath string) string {
	cleanPath := filepath.Clean("/" + strings.TrimPrefix(targetPath, "/"))
	return filepath.Join(rootFS, strings.TrimPrefix(cleanPath, "/"))
}

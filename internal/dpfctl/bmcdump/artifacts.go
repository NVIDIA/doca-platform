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

package bmcdump

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func artifactTargetName(target logTarget) string {
	name := target.IP
	if target.Port != defaultPort {
		name = fmt.Sprintf("%s-%d", name, target.Port)
	}
	if target.CredentialSecret != "" {
		name = fmt.Sprintf("%s-%s", name, target.CredentialSecret)
	}
	return strings.NewReplacer("/", "_", ":", "_").Replace(name)
}

func baseURL(target logTarget) string {
	host := strings.TrimPrefix(target.IP, urlScheme)

	// If the host already carries a port (e.g. "10.0.0.10:8443" or
	// "[2001:db8::10]:8443"), honor it and normalize IPv6 bracketing.
	if h, port, err := net.SplitHostPort(host); err == nil {
		return urlScheme + net.JoinHostPort(h, port)
	}

	// No port in the input host. Strip any surrounding brackets so we can
	// re-bracket IPv6 literals consistently below.
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	if target.Port == defaultPort {
		if strings.Contains(host, ":") {
			return urlScheme + "[" + host + "]"
		}
		return urlScheme + host
	}
	return urlScheme + net.JoinHostPort(host, strconv.FormatUint(uint64(target.Port), 10))
}

func writeMetadata(target logTarget, targetDir string, namespace string) error {
	metadata := fmt.Sprintf("BMC IP: %s\n"+
		"BMC Port: %d\n"+
		"DPU Devices: %s\n"+
		"Credential Secret: %s/%s\n",
		target.IP,
		target.Port,
		strings.Join(target.DPUDevices, ", "),
		namespace,
		target.CredentialSecret,
	)
	if err := os.WriteFile(filepath.Join(targetDir, "metadata.txt"), []byte(metadata), 0600); err != nil {
		return fmt.Errorf("writing bmc target metadata for %s: %w", target.IP, err)
	}
	return nil
}

// appendMetadata records what the collector discovered and decided. It shares
// metadata.txt rather than adding an artifact because the notes describe the
// same target the file already identifies.
func appendMetadata(targetDir string, notes []string) (err error) {
	if len(notes) == 0 {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(targetDir, "metadata.txt"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("appending bmc target metadata in %s: %w", targetDir, err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	if _, err := fmt.Fprintf(file, "\n%s\n", strings.Join(notes, "\n")); err != nil {
		return fmt.Errorf("appending bmc target metadata in %s: %w", targetDir, err)
	}
	return nil
}

func writeJSONArtifact(path string, obj map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

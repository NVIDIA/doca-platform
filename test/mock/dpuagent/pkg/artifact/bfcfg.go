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

package artifact

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"
)

// bfcfgUserDataPath is where bf.cfg writes the cloud-init user-data: the target root file system
// is mounted at /mnt while bfb_modify_os runs.
const bfcfgUserDataPath = "/mnt" + cloudinit.UserDataPath

// UserDataFromBFCFG extracts the cloud-init user-data from a bf.cfg. The default bf.cfg template
// writes each cloud-init file with `cat << \EOF > <path>` ... `EOF`; the heredoc whose target is
// the NoCloud user-data path is returned verbatim.
func UserDataFromBFCFG(bfcfg []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(bfcfg))
	scanner.Buffer(make([]byte, 0, 64*1024), MaxBFCFGSize)
	var (
		content   []string
		inHeredoc bool
		found     bool
	)
	for scanner.Scan() {
		line := scanner.Text()
		if inHeredoc {
			if strings.TrimSpace(line) == "EOF" {
				found = true
				break
			}
			content = append(content, line)
			continue
		}
		if heredocTarget(line) == bfcfgUserDataPath {
			inHeredoc = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan bf.cfg: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("bf.cfg does not contain a heredoc writing %s", bfcfgUserDataPath)
	}
	return []byte(strings.Join(content, "\n")), nil
}

// heredocTarget returns the redirect target of a `cat << \EOF > <path>` line, or "".
func heredocTarget(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "cat ") || !strings.Contains(trimmed, "<<") {
		return ""
	}
	idx := strings.LastIndex(trimmed, ">")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[idx+1:])
}

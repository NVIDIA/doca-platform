// Copyright 2026 NVIDIA
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package withpolling

import (
	"fmt"
	"time"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("withpolling", New)
}

// Settings configures the withpolling plugin via golangci-lint's
// `linters.settings.custom.withpolling.settings` block.
type Settings struct {
	// MaxPolling is the largest allowed WithPolling(...) duration, parsed with
	// time.ParseDuration (e.g. "1s"). Defaults to DefaultMaxPolling when empty.
	MaxPolling string `json:"maxPolling"`
}

type plugin struct {
	maxPolling time.Duration
}

// New constructs the withpolling golangci-lint plugin.
func New(settings any) (register.LinterPlugin, error) {
	s, err := register.DecodeSettings[Settings](settings)
	if err != nil {
		return nil, fmt.Errorf("withpolling: %w", err)
	}

	maxPolling := DefaultMaxPolling

	if s.MaxPolling != "" {
		maxPolling, err = time.ParseDuration(s.MaxPolling)
		if err != nil {
			return nil, fmt.Errorf("withpolling: invalid maxPolling %q: %w", s.MaxPolling, err)
		}
	}

	return plugin{maxPolling: maxPolling}, nil
}

func (p plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{NewAnalyzer(p.maxPolling)}, nil
}

func (plugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}

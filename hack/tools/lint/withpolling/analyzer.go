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

// Package withpolling implements a golangci-lint plugin that enforces the
// test/e2e/doc/README.md convention that Gomega's WithPolling(...) is never
// set to more than time.Second.
package withpolling

import (
	"go/ast"
	"go/constant"
	"time"

	"golang.org/x/tools/go/analysis"
)

// DefaultMaxPolling is the maxPolling used when no configuration is given,
// matching the convention documented in test/e2e/doc/README.md.
const DefaultMaxPolling = time.Second

// Analyzer flags WithPolling(d) calls whose argument is a compile-time
// constant time.Duration greater than DefaultMaxPolling.
var Analyzer = NewAnalyzer(DefaultMaxPolling)

// NewAnalyzer builds an analyzer that flags WithPolling(d) calls whose
// argument is a compile-time constant time.Duration greater than maxPolling.
func NewAnalyzer(maxPolling time.Duration) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "withpollingmax",
		Doc:  "reports WithPolling(...) calls set to more than the configured maximum",
		Run: func(pass *analysis.Pass) (any, error) {
			return run(pass, maxPolling)
		},
	}
}

func run(pass *analysis.Pass, maxPolling time.Duration) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WithPolling" || len(call.Args) != 1 {
				return true
			}

			arg := call.Args[0]

			tv, ok := pass.TypesInfo.Types[arg]
			if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
				// Not a compile-time constant (e.g. a variable computed at
				// runtime) - nothing we can statically verify.
				return true
			}

			d, exact := constant.Int64Val(tv.Value)
			if !exact || time.Duration(d) <= maxPolling {
				return true
			}

			pass.Reportf(arg.Pos(), "WithPolling(%s) exceeds the %s maximum; see test/e2e/doc/README.md",
				time.Duration(d), maxPolling)

			return true
		})
	}

	return nil, nil
}

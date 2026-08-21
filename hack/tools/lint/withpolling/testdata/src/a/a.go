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

package a

import "time"

type asserter struct{}

func (a asserter) WithPolling(time.Duration) asserter { return a }

const namedPollInterval = 2 * time.Second

func f() {
	var v asserter

	v.WithPolling(500 * time.Millisecond) // ok, sub-second

	v.WithPolling(time.Second) // ok, exactly the maximum

	v.WithPolling(5 * time.Second) // want `WithPolling\(5s\) exceeds the 1s maximum; see test/e2e/doc/README.md`

	v.WithPolling(namedPollInterval) // want `WithPolling\(2s\) exceeds the 1s maximum; see test/e2e/doc/README.md`

	d := computeDuration()
	v.WithPolling(d) // ok, not a compile-time constant
}

func computeDuration() time.Duration {
	return time.Duration(3) * time.Second
}

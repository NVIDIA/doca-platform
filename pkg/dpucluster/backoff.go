/*
Copyright 2025 NVIDIA

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

package dpucluster

import (
	"hash/maphash"
	"math"
	"math/rand/v2"
	"time"
)

// Backoff is a struct that implements a backoff algorithm.
type Backoff struct {
	duration time.Duration
	factor   float64
	jitter   float64
}

// NewBackoff creates a new Backoff instance.
// It takes a duration, a factor and a jitter as parameters.
func NewBackoff(d time.Duration, factor, jitter float64) *Backoff {
	return &Backoff{
		duration: d,
		factor:   factor,
		jitter:   jitter,
	}
}

// GetNextBackOff returns the backoff duration for the given attempt.
// The backoff is calculated as:
//
//	temp = backoff * factor ^ attempt
//	interval = temp * (1 - jitter) + rand.Int64N(2 * jitter * temp)
//
// e.g backoff = 1s, factor = 2, jitter = 0.1
// interval will be >= 0.9s and < 1.1s which means 10% jitter
func (b *Backoff) GetNextBackOff(attempt int) time.Duration {
	if attempt < 0 {
		return 0
	}
	return b.exponentialBackoff(attempt)
}

func (b *Backoff) exponentialBackoff(attempt int) time.Duration {
	var h maphash.Hash
	h.SetSeed(maphash.MakeSeed())
	//	temp = backoff * factor ^ attempt
	//	interval = temp * (1 - jitter) + rand.Int64N(2 * jitter * temp)
	rand := rand.New(rand.NewPCG(0, h.Sum64()))
	temp := float64(b.duration) * math.Pow(b.factor, float64(attempt))
	return time.Duration(temp*(1-b.jitter)) + time.Duration(rand.Int64N(int64(2*b.jitter*temp)))
}

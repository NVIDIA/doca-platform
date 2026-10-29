/*
Copyright 2024 NVIDIA

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

package controllers

import (
	"fmt"
	"math/rand/v2"
	"time"
)

const (
	maxJitter = 10 * time.Second
	maxTTL    = 24 * time.Hour
)

// requiresRefresh returns true if the token is older than 80% of its total
// ttl, or if the token is older than 24 hours.
// adapted from https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/token/token_manager.go#L171
func requiresRefresh(exp, iat time.Time) bool {
	if exp.IsZero() || iat.IsZero() {
		return false
	}
	now := time.Now()
	expirationSeconds := getExpirationSeconds(exp, iat)
	jitter := time.Duration(rand.Float64()*maxJitter.Seconds()) * time.Second

	// Require a refresh if the token is older than 24 hours.
	if now.After(iat.Add(maxTTL - jitter)) {
		return true
	}

	// Require a refresh if within 20% of the TTL plus a jitter from the expiration time.
	if now.After(exp.Add(-1*time.Duration((expirationSeconds*20)/100)*time.Second - jitter)) {
		return true
	}
	return false
}

// getExpirationSeconds returns the difference between the expiration and issued at time in seconds.
func getExpirationSeconds(exp, iat time.Time) int64 {
	return int64(exp.Sub(iat).Seconds())
}

// calculateNextRequeueTime returns the time until the next requeue should happen.
func calculateNextRequeueTime(exp, iat time.Time) (time.Time, error) {
	if exp.IsZero() || iat.IsZero() {
		return time.Time{}, fmt.Errorf("expiration or issued at time is not set")
	}
	expirationSeconds := getExpirationSeconds(exp, iat)
	// calculate 80% of the total TTL and add a jitter to it.
	jitter := time.Duration(rand.Float64()*maxJitter.Seconds()) * time.Second
	return exp.Add(-1*time.Duration((expirationSeconds*20)/100)*time.Second + jitter), nil
}

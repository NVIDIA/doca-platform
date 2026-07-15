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

package gitlab

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"
)

// boolPtr returns a pointer to b, for scheduleSpec.Active.
func boolPtr(b bool) *bool { return &b }

// captureStdout runs f with os.Stdout redirected to a pipe and returns what it
// wrote.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	f()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// TestScheduleSyncDryRunIsReadOnly is the safety guarantee that "dpfdev
// schedule sync" without --apply never mutates GitLab. It drives a dry-run
// against a fake GitLab whose state drifts from the file in every possible
// direction (a schedule to update, variables to add/change/delete, a
// schedule to create, and an orphan to prune) so the planner exercises every
// write path, then fails if any request other than GET reaches the server.
func TestScheduleSyncDryRunIsReadOnly(t *testing.T) {
	var mu sync.Mutex
	var violations []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mu.Lock()
			violations = append(violations, r.Method+" "+r.URL.Path)
			mu.Unlock()
			// Still answer so the client does not error out; the test asserts
			// on the recorded violation, not on client behaviour.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}

		switch {
		case r.URL.Path == "/projects/1/pipeline_schedules":
			// List: one schedule present in the file (with drift) and one
			// orphan that only exists in GitLab.
			_, _ = w.Write([]byte(`[
				{"id":10,"description":"keep","ref":"refs/heads/main","cron":"0 1 * * *","active":true,"owner":{"username":"alice"}},
				{"id":20,"description":"orphan","ref":"refs/heads/main","cron":"0 2 * * *","active":true,"owner":{"username":"bob"}}
			]`))
		case r.URL.Path == "/projects/1/pipeline_schedules/10":
			// Detail with variables that differ from the file: KEEP_VAR
			// changes value, DROP_VAR is removed, ADD_VAR is added by the file.
			_, _ = w.Write([]byte(`{"id":10,"description":"keep","ref":"refs/heads/main","cron":"0 1 * * *","active":true,"owner":{"username":"alice"},
				"variables":[
					{"key":"KEEP_VAR","variable_type":"env_var","value":"old"},
					{"key":"DROP_VAR","variable_type":"env_var","value":"x"}
				]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	file := &scheduleFile{Schedules: []scheduleSpec{
		{
			Description: "keep",
			Ref:         "refs/heads/main",
			Cron:        "0 5 * * *", // drift: cron changed
			Active:      boolPtr(false),
			Variables: []variableSpec{
				{Key: "KEEP_VAR", Value: "new"}, // changed value
				{Key: "ADD_VAR", Value: "y"},    // new variable
			},
		},
		{
			Description: "brand-new", // not in GitLab: create path
			Ref:         "refs/heads/main",
			Cron:        "0 6 * * *",
		},
	}}

	client := gitlab.NewClient(server.URL, "token", "1")
	syncer := &scheduleSyncer{client: client, dryRun: true, prune: true}

	if err := syncer.sync(file); err != nil {
		t.Fatalf("dry-run sync returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(violations) > 0 {
		t.Fatalf("dry-run sync issued %d mutating request(s): %s", len(violations), strings.Join(violations, ", "))
	}

	// Sanity-check that the plan actually engaged every write path, otherwise
	// the read-only assertion would pass trivially.
	if syncer.created == 0 || syncer.updated == 0 || syncer.deleted == 0 {
		t.Fatalf("planner did not exercise all paths: created=%d updated=%d deleted=%d",
			syncer.created, syncer.updated, syncer.deleted)
	}
}

// TestScheduleSyncApplyIssuesWrites is the counterpart: with dryRun false the
// same drift must produce mutating requests, proving the dry-run result above
// is meaningful and not an artefact of the plan being empty.
func TestScheduleSyncApplyIssuesWrites(t *testing.T) {
	var mu sync.Mutex
	writes := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mu.Lock()
			writes++
			mu.Unlock()
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/1/pipeline_schedules":
			_, _ = w.Write([]byte(`[{"id":10,"description":"keep","ref":"refs/heads/main","cron":"0 1 * * *","active":true,"owner":{"username":"alice"}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/1/pipeline_schedules/10":
			_, _ = w.Write([]byte(`{"id":10,"description":"keep","ref":"refs/heads/main","cron":"0 1 * * *","active":true,"owner":{"username":"alice"},"variables":[]}`))
		default:
			// Answer create/update/take_ownership calls with a schedule body.
			_, _ = w.Write([]byte(`{"id":10,"description":"keep","ref":"refs/heads/main","cron":"0 5 * * *","active":true,"owner":{"username":"alice"}}`))
		}
	}))
	defer server.Close()

	file := &scheduleFile{Schedules: []scheduleSpec{{
		Description: "keep",
		Ref:         "refs/heads/main",
		Cron:        "0 5 * * *", // drift
	}}}

	client := gitlab.NewClient(server.URL, "token", "1")
	syncer := &scheduleSyncer{client: client, dryRun: false, prune: false}
	if err := syncer.sync(file); err != nil {
		t.Fatalf("apply sync returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if writes == 0 {
		t.Fatal("apply sync issued no mutating requests; the dry-run test would be vacuous")
	}
}

// TestFetchDetailsConcurrency checks that fetchDetails retrieves every id and
// never exceeds the configured concurrency.
func TestFetchDetailsConcurrency(t *testing.T) {
	var inFlight, maxInFlight int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		_, _ = w.Write([]byte(`{"id":1,"description":"x","ref":"main","cron":"* * * * *","variables":[]}`))
	}))
	defer server.Close()

	client := gitlab.NewClient(server.URL, "token", "1")
	s := &scheduleSyncer{client: client, concurrency: 3}

	ids := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	details, err := s.fetchDetails(ids)
	if err != nil {
		t.Fatalf("fetchDetails: %v", err)
	}
	if len(details) != len(ids) {
		t.Fatalf("fetched %d details, want %d", len(details), len(ids))
	}
	if maxInFlight > 3 {
		t.Fatalf("max concurrent requests %d exceeded limit 3", maxInFlight)
	}
}

func TestPlanExitCode(t *testing.T) {
	tests := []struct {
		name    string
		created int
		updated int
		deleted int
		want    int
	}{
		{name: "in sync", want: planExitInSync},
		{name: "creates only", created: 1, want: planExitDrift},
		{name: "updates only", updated: 2, want: planExitDrift},
		{name: "deletes only", deleted: 1, want: planExitDrift},
		{name: "mixed changes", created: 1, updated: 5, deleted: 2, want: planExitDrift},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &scheduleSyncer{created: tt.created, updated: tt.updated, deleted: tt.deleted}
			if got := planExitCode(s); got != tt.want {
				t.Fatalf("planExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}


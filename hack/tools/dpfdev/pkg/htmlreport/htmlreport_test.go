/*
Copyright 2026 NVIDIA CORPORATION & AFFILIATES

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

package htmlreport

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// mkfile creates path and any missing parent directories, writing content.
func mkfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverDumps(t *testing.T) {
	root := t.TempDir()

	// A representative CI artifact tree: two sibling dumps, a nested per-test
	// dump under failed_tests/, and a logs/ tree that is NOT a resource dump.
	mkfile(t, filepath.Join(root, "pre-cleanup", "main", "Resources", "ConfigMap", "foo.yaml"), "apiVersion: v1\n")
	mkfile(t, filepath.Join(root, "pre-cleanup", "dpu-cluster-1", "Events", "Pod", "ns", "bar.events"), "items: []\n")
	mkfile(t, filepath.Join(root, "deletion-stuck", "main", "Resources", "DPU", "baz.yaml"), "apiVersion: v1\n")
	mkfile(t, filepath.Join(root, "failed_tests", "mytest", "main", "Resources", "Pod", "ns", "qux.yaml"), "apiVersion: v1\n")
	mkfile(t, filepath.Join(root, "logs", "host-cluster", "ns", "pod", "container.log"), "log line\n")
	mkfile(t, filepath.Join(root, "some-file.txt"), "loose file\n")

	got, err := DiscoverDumps(root)
	if err != nil {
		t.Fatalf("DiscoverDumps: %v", err)
	}
	// Sorted, de-duplicated; logs/ and loose files excluded.
	want := []string{
		filepath.Join(root, "deletion-stuck"),
		filepath.Join(root, "failed_tests", "mytest"),
		filepath.Join(root, "pre-cleanup"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverDumps() =\n  %v\nwant\n  %v", got, want)
	}
}

func TestDiscoverDumps_RootIsDump(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "main", "Resources", "ConfigMap", "foo.yaml"), "apiVersion: v1\n")
	mkfile(t, filepath.Join(root, "dpu-cluster-1", "Events", "Pod", "ns", "bar.events"), "items: []\n")

	got, err := DiscoverDumps(root)
	if err != nil {
		t.Fatalf("DiscoverDumps: %v", err)
	}
	want := []string{root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverDumps() = %v, want %v", got, want)
	}
}

func TestDiscoverDumps_NoDumps(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "logs", "host-cluster", "ns", "pod", "c.log"), "x\n")
	mkfile(t, filepath.Join(root, "some-file.txt"), "x\n")

	got, err := DiscoverDumps(root)
	if err != nil {
		t.Fatalf("DiscoverDumps: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DiscoverDumps() = %v, want none", got)
	}
}

func TestDiscoverDumps_MissingRoot(t *testing.T) {
	if _, err := DiscoverDumps(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing root directory")
	}
}

func TestBuildDumpEntry(t *testing.T) {
	root := t.TempDir()
	dump := filepath.Join(root, "pre-cleanup")
	mkfile(t, filepath.Join(dump, "main", "Resources", "ConfigMap", "dpf-operator-system", "foo.yaml"), "apiVersion: v1\nkind: ConfigMap\n")
	mkfile(t, filepath.Join(dump, "dpu-cluster-1", "Resources", "Pod", "ns", "bar.yaml"), "apiVersion: v1\nkind: Pod\n")

	entry, err := buildDumpEntry(dump, root)
	if err != nil {
		t.Fatalf("buildDumpEntry: %v", err)
	}
	if entry.Name != "pre-cleanup" {
		t.Errorf("entry.Name = %q, want %q", entry.Name, "pre-cleanup")
	}
	if len(entry.Clusters) != 2 {
		t.Fatalf("clusters = %d, want 2", len(entry.Clusters))
	}
	// "main" sorts first.
	if entry.Clusters[0].Name != "main" {
		t.Errorf("first cluster = %q, want main", entry.Clusters[0].Name)
	}
	// File paths must be relative to root, i.e. include the dump prefix.
	got := entry.Clusters[0].Resources[0].Files[0].Path
	want := "pre-cleanup/main/Resources/ConfigMap/dpf-operator-system/foo.yaml"
	if got != want {
		t.Errorf("file path = %q, want %q", got, want)
	}
}

func TestBuildDumpEntry_RootIsDump(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "main", "Resources", "ConfigMap", "foo.yaml"), "apiVersion: v1\n")

	entry, err := buildDumpEntry(root, root)
	if err != nil {
		t.Fatalf("buildDumpEntry: %v", err)
	}
	// When root is itself the dump, the entry is named after root and file paths
	// have no dump prefix.
	if entry.Name != filepath.Base(root) {
		t.Errorf("entry.Name = %q, want %q", entry.Name, filepath.Base(root))
	}
	got := entry.Clusters[0].Resources[0].Files[0].Path
	want := "main/Resources/ConfigMap/foo.yaml"
	if got != want {
		t.Errorf("file path = %q, want %q", got, want)
	}
}

func TestGenerate(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "pre-cleanup", "main", "Resources", "ConfigMap", "foo.yaml"), "apiVersion: v1\n")
	mkfile(t, filepath.Join(root, "deletion-stuck", "main", "Resources", "DPU", "baz.yaml"), "apiVersion: v1\n")
	mkfile(t, filepath.Join(root, "logs", "host-cluster", "kube-system", "coredns-abc", "coredns.log"), "hello\n")
	mkfile(t, filepath.Join(root, "ci-bootstrap.log"), "ci step output\n")

	dumps, err := DiscoverDumps(root)
	if err != nil {
		t.Fatalf("DiscoverDumps: %v", err)
	}
	out := filepath.Join(root, "artifacts-browser.html")
	if err := Generate(root, dumps, out); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	html := string(data)
	// The embedded index must carry both dumps as entries with root-relative
	// paths, plus the container logs and the top-level CI logs.
	for _, want := range []string{
		`"entries"`,
		`"name":"deletion-stuck"`,
		`"name":"pre-cleanup"`,
		`pre-cleanup/main/Resources/ConfigMap/foo.yaml`,
		`"logs"`,
		`"name":"host-cluster"`,
		`logs/host-cluster/kube-system/coredns-abc/coredns.log`,
		`"cilogs"`,
		`"n":"ci-bootstrap.log"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("generated HTML missing %q", want)
		}
	}
}

func TestBuildLogs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "logs", "host-cluster", "kube-system", "coredns-abc", "coredns.log"), "log line\n")
	mkfile(t, filepath.Join(root, "logs", "dpu-cluster-1", "default", "app-xyz", "main.log"), "x\n")
	// A non-.log file must be ignored.
	mkfile(t, filepath.Join(root, "logs", "host-cluster", "kube-system", "coredns-abc", "notes.txt"), "ignore\n")

	clusters, err := buildLogs(root)
	if err != nil {
		t.Fatalf("buildLogs: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters = %d, want 2", len(clusters))
	}
	// host-cluster sorts first, then the rest alphabetically.
	if clusters[0].Name != "host-cluster" || clusters[1].Name != "dpu-cluster-1" {
		t.Fatalf("cluster order = [%q %q], want [host-cluster dpu-cluster-1]", clusters[0].Name, clusters[1].Name)
	}
	hc := clusters[0]
	if len(hc.Files) != 1 {
		t.Fatalf("host-cluster files = %d, want 1 (.txt ignored)", len(hc.Files))
	}
	f := hc.Files[0]
	if f.Namespace != "kube-system" || f.Pod != "coredns-abc" || f.Container != "coredns" {
		t.Errorf("parsed ns=%q pod=%q container=%q, want kube-system/coredns-abc/coredns", f.Namespace, f.Pod, f.Container)
	}
	if f.Path != "logs/host-cluster/kube-system/coredns-abc/coredns.log" {
		t.Errorf("path = %q, want logs/host-cluster/kube-system/coredns-abc/coredns.log", f.Path)
	}
}

func TestBuildLogs_None(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "pre-cleanup", "main", "Resources", "ConfigMap", "foo.yaml"), "apiVersion: v1\n")

	clusters, err := buildLogs(root)
	if err != nil {
		t.Fatalf("buildLogs: %v", err)
	}
	if clusters != nil {
		t.Errorf("buildLogs with no logs/ dir = %v, want nil", clusters)
	}
}

func TestBuildCILogs(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "ci-b.log"), "b\n")
	mkfile(t, filepath.Join(root, "ci-a.log"), "a\n")
	mkfile(t, filepath.Join(root, "resolved-bfb-url.txt"), "x\n") // non-.log ignored
	// Nested .log files belong to logs/ or dumps, not CI logs.
	mkfile(t, filepath.Join(root, "logs", "host-cluster", "ns", "pod", "c.log"), "x\n")

	logs, err := buildCILogs(root)
	if err != nil {
		t.Fatalf("buildCILogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("ci logs = %d, want 2 (only top-level .log)", len(logs))
	}
	// Sorted by name.
	if logs[0].Name != "ci-a.log" || logs[1].Name != "ci-b.log" {
		t.Errorf("order = [%q %q], want [ci-a.log ci-b.log]", logs[0].Name, logs[1].Name)
	}
	if logs[0].Path != "ci-a.log" {
		t.Errorf("path = %q, want ci-a.log", logs[0].Path)
	}
}

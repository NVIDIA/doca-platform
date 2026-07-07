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

// Package htmlreport generates a static HTML viewer for artifact directories
// collected by dpfdev collect. Artifacts have the structure:
//
//	<artifactDir>/<cluster>/Resources/<Kind>/[<namespace>/]<name>.yaml
//	<artifactDir>/<cluster>/Events/<Kind>/<namespace>/<name>.events
package htmlreport

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileEntry represents a single resource or event file.
type FileEntry struct {
	Path              string `json:"p"`
	Namespace         string `json:"ns,omitempty"`
	Name              string `json:"n"`
	ConditionMessages int    `json:"cm,omitempty"` // 1 if a Ready condition is not True
}

// KindGroup groups files of the same Kind within a cluster section.
type KindGroup struct {
	Kind       string       `json:"k"`
	Domain     string       `json:"d"`            // e.g. "core", "k8s.io", "nvidia.com"
	Sub        string       `json:"s,omitempty"`  // e.g. "admissionregistration", "svc.dpu"
	APIVersion string       `json:"av,omitempty"` // e.g. "v1", "apps/v1", "provisioning.dpu.nvidia.com/v1alpha1"
	Files      []*FileEntry `json:"f"`
}

// ClusterIndex holds the resource and event index for one cluster directory.
type ClusterIndex struct {
	Name      string       `json:"name"`
	Resources []*KindGroup `json:"resources"`
	Events    []*KindGroup `json:"events"`
}

// DumpEntry is one resource dump (e.g. "pre-dpf-operator-config-cleanup") and
// the clusters captured within it.
type DumpEntry struct {
	Name     string          `json:"name"` // dump path relative to the traversed root
	Clusters []*ClusterIndex `json:"clusters"`
}

// LogEntry is a single container log file collected under logs/.
type LogEntry struct {
	Path      string `json:"p"` // path relative to the traversed root, used for fetch()
	Namespace string `json:"ns,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"c"` // container name (log file stem)
}

// LogCluster groups container logs for one cluster directory under logs/.
type LogCluster struct {
	Name  string      `json:"name"`
	Files []*LogEntry `json:"f"`
}

// CILog is a top-level *.log file (CI job step log) directly under the root.
type CILog struct {
	Path string `json:"p"` // path relative to the traversed root, used for fetch()
	Name string `json:"n"` // file name
}

// Index is the top-level data structure serialised into the HTML page. It holds
// one entry per resource dump discovered under the traversed root directory,
// optionally the container logs found under logs/, and any top-level CI logs.
type Index struct {
	Dir     string        `json:"dir"` // base name of the traversed root directory
	Entries []*DumpEntry  `json:"entries"`
	Logs    []*LogCluster `json:"logs,omitempty"`
	CILogs  []*CILog      `json:"cilogs,omitempty"`
}

// DiscoverDumps walks root and returns the resource dump directories found
// underneath it. A dump is a directory whose <cluster> children contain a
// Resources/ or Events/ section directory, i.e. the grandparent of any such
// section directory (<dump>/<cluster>/Resources). The returned paths are
// de-duplicated and sorted.
//
// Pointing DiscoverDumps at a single dump directory returns that directory
// itself, so callers can pass either an artifact root containing many dumps
// (e.g. .../data with pre-/post-cleanup dumps) or one dump directory directly.
func DiscoverDumps(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries but keep walking their siblings.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if name := d.Name(); name == "Resources" || name == "Events" {
			// <dump>/<cluster>/{Resources,Events}: the dump is the grandparent.
			// No need to descend into the section dir once it is found.
			seen[filepath.Dir(filepath.Dir(path))] = struct{}{}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	dumps := make([]string, 0, len(seen))
	for d := range seen {
		dumps = append(dumps, d)
	}
	sort.Strings(dumps)
	return dumps, nil
}

// Generate builds a single artifacts-browser.html at outputPath covering every
// resource dump in dumps (as returned by DiscoverDumps). File references in the
// page are relative to root, so the generated HTML must be served from root with
// the dump directories beneath it.
//
// All paths are resolved to absolute before use so that relative inputs work.
func Generate(root string, dumps []string, outputPath string) error {
	var err error
	if root, err = filepath.Abs(root); err != nil {
		return err
	}
	if outputPath, err = filepath.Abs(outputPath); err != nil {
		return err
	}

	idx := &Index{Dir: filepath.Base(root), Entries: []*DumpEntry{}}
	for _, dump := range dumps {
		absDump, err := filepath.Abs(dump)
		if err != nil {
			return err
		}
		entry, err := buildDumpEntry(absDump, root)
		if err != nil {
			return err
		}
		if len(entry.Clusters) == 0 {
			continue
		}
		idx.Entries = append(idx.Entries, entry)
	}

	logs, err := buildLogs(root)
	if err != nil {
		return err
	}
	idx.Logs = logs

	ciLogs, err := buildCILogs(root)
	if err != nil {
		return err
	}
	idx.CILogs = ciLogs

	indexJSON, err := json.Marshal(idx)
	if err != nil {
		return err
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return renderTemplate(f, template.JS(indexJSON)) //nolint:gosec // controlled JSON data
}

// buildDumpEntry builds the cluster index for a single dump directory. File
// paths are recorded relative to root (not dumpDir) so a single HTML page served
// from root can fetch files across every dump.
func buildDumpEntry(dumpDir, root string) (*DumpEntry, error) {
	dirEntries, err := os.ReadDir(dumpDir)
	if err != nil {
		return nil, err
	}

	name, err := filepath.Rel(root, dumpDir)
	if err != nil {
		return nil, err
	}
	if name == "." {
		// root is itself a single dump.
		name = filepath.Base(dumpDir)
	}

	entry := &DumpEntry{Name: filepath.ToSlash(name), Clusters: []*ClusterIndex{}}
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue
		}
		clusterName := e.Name()
		clusterDir := filepath.Join(dumpDir, clusterName)

		resources, err := buildKindGroups(filepath.Join(clusterDir, "Resources"), root)
		if err != nil {
			return nil, err
		}
		events, err := buildKindGroups(filepath.Join(clusterDir, "Events"), root)
		if err != nil {
			return nil, err
		}
		if len(resources) == 0 && len(events) == 0 {
			continue
		}

		entry.Clusters = append(entry.Clusters, &ClusterIndex{
			Name:      clusterName,
			Resources: resources,
			Events:    events,
		})
	}

	// Sort with "main" first, then alphabetically.
	sort.Slice(entry.Clusters, func(i, j int) bool {
		if entry.Clusters[i].Name == "main" {
			return true
		}
		if entry.Clusters[j].Name == "main" {
			return false
		}
		return entry.Clusters[i].Name < entry.Clusters[j].Name
	})

	return entry, nil
}

// buildLogs collects container logs from <root>/logs, if that directory exists.
// The expected layout is logs/<cluster>/<namespace>/<pod>/<container>.log (as
// produced by hack/scripts/log-collector.sh). A missing logs/ directory is not
// an error. Paths are recorded relative to root so the page can fetch them.
func buildLogs(root string) ([]*LogCluster, error) {
	logsDir := filepath.Join(root, "logs")
	clusterEntries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var clusters []*LogCluster
	for _, ce := range clusterEntries {
		if !ce.IsDir() {
			continue
		}
		clusterName := ce.Name()
		clusterDir := filepath.Join(logsDir, clusterName)

		var files []*LogEntry
		err := filepath.WalkDir(clusterDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".log") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			// Path relative to the cluster dir: <namespace>/<pod>/<container>.log.
			sub, _ := filepath.Rel(clusterDir, path)
			parts := strings.Split(sub, string(os.PathSeparator))
			var ns, pod string
			switch len(parts) {
			case 3:
				ns, pod = parts[0], parts[1]
			case 2:
				pod = parts[0]
			}
			files = append(files, &LogEntry{
				Path:      filepath.ToSlash(rel),
				Namespace: ns,
				Pod:       pod,
				Container: strings.TrimSuffix(d.Name(), ".log"),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}

		sort.Slice(files, func(i, j int) bool {
			if files[i].Namespace != files[j].Namespace {
				return files[i].Namespace < files[j].Namespace
			}
			if files[i].Pod != files[j].Pod {
				return files[i].Pod < files[j].Pod
			}
			return files[i].Container < files[j].Container
		})
		clusters = append(clusters, &LogCluster{Name: clusterName, Files: files})
	}

	// Sort with the host cluster first, then the rest alphabetically.
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Name == "host-cluster" {
			return true
		}
		if clusters[j].Name == "host-cluster" {
			return false
		}
		return clusters[i].Name < clusters[j].Name
	})
	return clusters, nil
}

// buildCILogs lists the *.log files directly under root (CI job step logs). It
// does not recurse; nested logs live under logs/ and the dump directories.
func buildCILogs(root string) ([]*CILog, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var logs []*CILog
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		logs = append(logs, &CILog{Path: e.Name(), Name: e.Name()})
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Name < logs[j].Name
	})
	return logs, nil
}

func buildKindGroups(sectionDir, root string) ([]*KindGroup, error) {
	kindEntries, err := os.ReadDir(sectionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var groups []*KindGroup
	for _, ke := range kindEntries {
		if !ke.IsDir() {
			continue
		}
		kind := ke.Name()
		kindDir := filepath.Join(sectionDir, kind)

		apiVersion := detectAPIVersion(kindDir)
		var apiGroup string
		if g, _, ok := strings.Cut(apiVersion, "/"); ok {
			apiGroup = g
		}
		domain, sub := splitAPIGroup(apiGroup)

		var files []*FileEntry
		err := filepath.WalkDir(kindDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			// Determine namespace and name from path relative to kindDir.
			subPath, _ := filepath.Rel(kindDir, path)
			parts := strings.Split(subPath, string(os.PathSeparator))
			var ns, name string
			switch len(parts) {
			case 1:
				name = strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
			case 2:
				ns = parts[0]
				name = strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))
			default:
				name = subPath
			}
			files = append(files, &FileEntry{
				Path:              filepath.ToSlash(rel),
				Namespace:         ns,
				Name:              name,
				ConditionMessages: hasNotReadyCondition(path),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		groups = append(groups, &KindGroup{
			Kind:       kind,
			Domain:     domain,
			Sub:        sub,
			APIVersion: apiVersion,
			Files:      files,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Kind < groups[j].Kind
	})

	return groups, nil
}

// splitAPIGroup decomposes a full API group into a display domain and optional sub-group.
//
//	""                               → domain="core",      sub=""
//	"apps"                           → domain="apps",      sub=""
//	"admissionregistration.k8s.io"   → domain="k8s.io",   sub="admissionregistration"
//	"svc.dpu.nvidia.com"             → domain="nvidia.com", sub="svc.dpu"
func splitAPIGroup(g string) (domain, sub string) {
	if g == "" {
		return "core", ""
	}
	parts := strings.Split(g, ".")
	if len(parts) <= 1 {
		return g, ""
	}
	return strings.Join(parts[len(parts)-2:], "."), strings.Join(parts[:len(parts)-2], ".")
}

// hasNotReadyCondition returns 1 if the YAML file at path has a
// .status.conditions[] entry with type=Ready and status!=True, 0 otherwise.
// Uses a line-based state machine to avoid pulling in a full YAML parser.
func hasNotReadyCondition(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	const (
		stRoot = iota
		stStatus
		stConditions
	)
	state := stRoot
	statusIndent := -1
	condIndent := -1
	itemType := ""
	itemStatus := ""

	check := func() bool {
		return strings.EqualFold(itemType, "Ready") &&
			itemStatus != "" && !strings.EqualFold(itemStatus, "True")
	}

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(trimmed)
		switch state {
		case stRoot:
			if strings.HasPrefix(trimmed, "status:") {
				state = stStatus
				statusIndent = indent
			}
		case stStatus:
			if indent <= statusIndent && !strings.HasPrefix(trimmed, "-") {
				state = stRoot
				continue
			}
			if strings.HasPrefix(trimmed, "conditions:") {
				state = stConditions
				condIndent = indent
				itemType, itemStatus = "", ""
			}
		case stConditions:
			if indent <= condIndent && !strings.HasPrefix(trimmed, "-") {
				if check() {
					return 1
				}
				state = stStatus
				continue
			}
			// New list item at the conditions indent level.
			if indent == condIndent && strings.HasPrefix(trimmed, "- ") {
				if check() {
					return 1
				}
				itemType, itemStatus = "", ""
				// The `- ` may be immediately followed by a field on the same line.
				trimmed = strings.TrimPrefix(trimmed, "- ")
			}
			if rest, ok := strings.CutPrefix(trimmed, "type:"); ok {
				itemType = strings.Trim(strings.TrimSpace(rest), `"'`)
			} else if rest, ok := strings.CutPrefix(trimmed, "status:"); ok {
				itemStatus = strings.Trim(strings.TrimSpace(rest), `"'`)
			}
		}
	}
	if state == stConditions && check() {
		return 1
	}
	return 0
}

// detectAPIVersion reads the apiVersion field from the first YAML file found in kindDir.
// Returns the full apiVersion string, e.g. "v1", "apps/v1", "provisioning.dpu.nvidia.com/v1alpha1".
func detectAPIVersion(kindDir string) string {
	var apiVersion string
	_ = filepath.WalkDir(kindDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return filepath.SkipAll
		}
		defer f.Close()
		buf := make([]byte, 256)
		n, _ := io.ReadAtLeast(f, buf, 1)
		content := string(buf[:n])
		for _, line := range strings.Split(content, "\n") {
			rest, ok := strings.CutPrefix(line, "apiVersion:")
			if !ok {
				continue
			}
			apiVersion = strings.TrimSpace(rest)
			break
		}
		return filepath.SkipAll
	})
	return apiVersion
}

func renderTemplate(w io.Writer, indexJSON template.JS) error {
	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, indexJSON)
}

//go:embed template.html
var htmlTemplate string

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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// The dump comes from the BMC, so every size below is a limit on untrusted input.
const (
	// maxPrintedLogBytes bounds the log output of one collection, so a faulty BMC cannot flood the
	// log pipeline. Measured: journal 0.5 MB, console 0.2 to 0.4 MB, event log 1 to 2.6 MB.
	maxPrintedLogBytes = 4 << 20
	// maxUnpackedDumpBytes bounds how much of one archive is decompressed. Measured dumps unpack to
	// 8 to 11 MB.
	maxUnpackedDumpBytes = 256 << 20
	// maxLogFileBytes bounds one printed file held in memory.
	maxLogFileBytes = 16 << 20
	// maxLogLineBytes bounds the message of one printed line.
	maxLogLineBytes = 16 << 10
	// maxLogFieldBytes bounds the other fields of a line, which name a file or a unit.
	maxLogFieldBytes = 256
	// maxDumpWindowBytes bounds the memory the decompressor may be asked for by the archive.
	maxDumpWindowBytes = 64 << 20
	// maxResultErrorBytes bounds the error text in the result line; it can quote a BMC response.
	maxResultErrorBytes = 1 << 10
)

// Log sources, in the order they are printed. The order is also the priority under
// maxPrintedLogBytes: the event log is the largest and the least specific, so it comes last and is
// the first to lose lines.
const (
	sourceState    = "state"
	sourceJournal  = "journal"
	sourceKernel   = "kernel"
	sourceConsole  = "console"
	sourcePostCode = "postcode"
	sourceEventLog = "eventlog"
)

var printedSources = []string{sourceState, sourceJournal, sourceKernel, sourceConsole, sourcePostCode, sourceEventLog}

// printedFiles maps a file of a dump, by its path below the dump's top directory, to its log source.
// Everything else stays in the archive. The names are those of the BF3 and BF4 dumps measured in
// November 2026; a name the BMC stops using is simply not printed.
var printedFiles = map[string]string{
	"bmc-state.log":       sourceState,
	"chassis-state.log":   sourceState,
	"host-state.log":      sourceState,
	"failed-services.log": sourceState,
	"fw-version.log":      sourceState,
	"uptime.log":          sourceState,
	"journal-pretty.log":  sourceJournal,
	"dmesg.log":           sourceKernel,
	"elogall.log":         sourceEventLog,
	// The DPU console capture: a plain file in the BF3 Manager dump, a tar in the BF4 System dump.
	"dpulogs/dpu_console": sourceConsole,
	consoleTarName:        sourceConsole,
	"biospostcode.log":    sourcePostCode,
}

const consoleTarName = "dpu_console.tar"

type logLine struct {
	Source  string `json:"source"`
	File    string `json:"file,omitempty"`
	BMCTime string `json:"bmc_time,omitempty"`
	Unit    string `json:"unit,omitempty"`
	Message string `json:"message"`
}

type resultLine struct {
	Source  string   `json:"source"`
	OK      bool     `json:"ok"`
	Dumps   []string `json:"dumps"`
	Archive string   `json:"archive"`
	Lines   int      `json:"lines"`
	Dropped int      `json:"dropped"`
	Error   string   `json:"error,omitempty"`
}

// logTail keeps the newest lines of one source that fit in budget bytes, so that what is held in
// memory is bounded by what can be printed and not by what the BMC sent.
type logTail struct {
	lines   []logLine
	size    int
	dropped int
}

// logTails holds one tail per source.
type logTails struct {
	budget   int
	bySource map[string]*logTail
}

// of returns the tail of one source, creating it on first use.
func (t *logTails) of(source string) *logTail {
	if t.bySource == nil {
		t.bySource = map[string]*logTail{}
	}
	if t.bySource[source] == nil {
		t.bySource[source] = &logTail{}
	}
	return t.bySource[source]
}

// lineOverheadBytes is about what the JSON around the fields of a line takes.
const lineOverheadBytes = 64

// lineSize is about what a line takes once printed.
func lineSize(line logLine) int {
	return lineOverheadBytes + len(line.Message) + len(line.File) + len(line.Unit) + len(line.BMCTime)
}

// add appends a line to the tail of its source and drops the oldest lines of that source that no
// longer fit the budget.
func (t *logTails) add(line logLine) {
	tail := t.of(line.Source)
	tail.lines = append(tail.lines, line)
	tail.size += lineSize(line)
	for tail.size > t.budget && len(tail.lines) > 0 {
		tail.size -= lineSize(tail.lines[0])
		tail.lines = tail.lines[1:]
		tail.dropped++
	}
}

type printSummary struct {
	// dumps are the units whose archive was downloaded.
	dumps   []string
	lines   int
	dropped int
	err     error
}

// logOutput writes the JSON lines of one collection: a header first, the logs, and a result last.
type logOutput struct {
	w io.Writer
	// dir is where the collector put the dumps, and archive says what became of them.
	dir     string
	archive string
	// writeErr is the first failure to write the header or the result.
	writeErr error
}

// newLogOutput returns the output of one collection, with its header line prepared.
func newLogOutput(opts CollectOptions) *logOutput {
	header := maps.Clone(opts.Header)
	if header == nil {
		header = map[string]string{}
	}
	header["source"] = "header"
	if opts.Serial != "" {
		header["serial"] = opts.Serial
	}
	out := &logOutput{w: opts.PrintLogs, archive: "not requested"}
	out.writeErr = out.writeJSON(header)
	return out
}

// pruned records that removing older archives failed. The archive of this run is not affected.
func (o *logOutput) pruned(err error) {
	if err != nil {
		o.archive += "; pruning older archives failed: " + err.Error()
	}
}

// finish prints the logs of whatever was downloaded, then the result. It runs on every way out of
// Collect, so a collection always ends with a result line. It returns the error of a dump that
// could not be read or of a line that could not be written: such a collection has failed, whatever
// was downloaded.
func (o *logOutput) finish(collectErr error) error {
	summary := printSummary{dumps: []string{}}
	if o.dir != "" {
		summary = printDumpLogs(o.w, o.dir, maxPrintedLogBytes)
	}
	if strings.HasPrefix(o.archive, "stored") && len(summary.dumps) == 0 {
		o.archive = "nothing to store"
	}
	result := resultLine{
		Source:  "result",
		OK:      collectErr == nil && summary.err == nil,
		Dumps:   summary.dumps,
		Archive: o.archive,
		Lines:   summary.lines,
		Dropped: summary.dropped,
	}
	if err := errors.Join(collectErr, summary.err); err != nil {
		result.Error = strings.ToValidUTF8(err.Error(), "�")
		if len(result.Error) > maxResultErrorBytes {
			result.Error = strings.ToValidUTF8(result.Error[:maxResultErrorBytes], "")
		}
	}
	return errors.Join(summary.err, o.writeErr, o.writeJSON(result))
}

// writeJSON prints one value as a JSON line.
func (o *logOutput) writeJSON(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = o.w.Write(append(line, '\n'))
	return err
}

// printDumpLogs prints the selected logs of the dumps under dir, within budget bytes of output.
func printDumpLogs(w io.Writer, dir string, budget int) printSummary {
	summary := printSummary{dumps: []string{}}
	tails := &logTails{budget: budget}
	for _, unit := range []string{managerUnitName, systemUnitName} {
		archive := filepath.Join(dir, unit, dumpArchiveName)
		if _, err := os.Stat(archive); err != nil {
			continue
		}
		summary.dumps = append(summary.dumps, unit)
		if err := readDumpLogs(archive, maxUnpackedDumpBytes, tails); err != nil {
			summary.err = errors.Join(summary.err, fmt.Errorf("reading %s dump: %w", unit, err))
		}
	}

	for _, source := range printedSources {
		// Encoded first, because the budget is on what is written.
		tail := tails.of(source)
		summary.dropped += tail.dropped
		encoded := make([][]byte, 0, len(tail.lines))
		for _, line := range tail.lines {
			if data, err := json.Marshal(line); err == nil {
				encoded = append(encoded, append(data, '\n'))
			}
		}
		// The newest lines are at the end, so the ones kept are the longest tail that fits.
		first := len(encoded)
		for used := 0; first > 0 && used+len(encoded[first-1]) <= budget; first-- {
			used += len(encoded[first-1])
		}
		summary.dropped += first
		for _, data := range encoded[first:] {
			if _, err := w.Write(data); err != nil {
				summary.err = errors.Join(summary.err, err)
				return summary
			}
			budget -= len(data)
			summary.lines++
		}
	}
	return summary
}

// readDumpLogs adds the printed lines of one dump archive to tails. It unpacks at most maxUnpacked
// bytes, counting what the console capture unpacks to; what it found up to there is kept.
func readDumpLogs(archive string, maxUnpacked int64, tails *logTails) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	decoder, err := zstd.NewReader(file, zstd.WithDecoderMaxWindow(maxDumpWindowBytes), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer decoder.Close()

	tooLarge := fmt.Errorf("the archive unpacks to more than %d bytes", maxUnpacked)
	unpacked := &io.LimitedReader{R: decoder, N: maxUnpacked}
	reader := tar.NewReader(unpacked)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) && unpacked.N > 0 {
			// The tar can end before the compressed stream does; damage after it is found here.
			if _, err := io.Copy(io.Discard, unpacked); err != nil || unpacked.N > 0 {
				return err
			}
		}
		if unpacked.N <= 0 {
			return tooLarge
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// Dumps hold one top directory, named after the dump; the files are matched below it.
		_, name, _ := strings.Cut(header.Name, "/")
		source, ok := printedFiles[name]
		if !ok {
			continue
		}
		// A file over the limit keeps its newest part, and the error of a cut-off file is not one:
		// what was read is printed.
		switch {
		case source == sourceJournal:
			journalLines(tailOf(reader, maxLogFileBytes, "\n{"), tails.add)
		case name == consoleTarName:
			// A tar is read from its start. The inner files are compressed again, so their size is
			// taken from the same limit.
			data, _ := io.ReadAll(io.LimitReader(reader, maxLogFileBytes))
			if !consoleTarLines(data, &unpacked.N, tails.add) {
				return tooLarge
			}
		case source == sourceState:
			textLines(source, name, tailOf(reader, maxLogFileBytes, "\n"), tails.add)
		default:
			textLines(source, "", tailOf(reader, maxLogFileBytes, "\n"), tails.add)
		}
	}
}

// tailOf reads r to its end and returns its last limit bytes. When that cuts the content, the
// result starts after the first boundary, which is where an entry starts: "\n" for lines, "\n{" for
// the journal's objects.
func tailOf(r io.Reader, limit int, boundary string) []byte {
	var tail []byte
	cut := false
	chunk := make([]byte, 64<<10)
	for {
		n, err := r.Read(chunk)
		tail = append(tail, chunk[:n]...)
		if len(tail) > 2*limit {
			tail, cut = append(tail[:0], tail[len(tail)-limit:]...), true
		}
		if err != nil {
			break
		}
	}
	if len(tail) > limit {
		tail, cut = tail[len(tail)-limit:], true
	}
	if cut {
		if start := bytes.Index(tail, []byte(boundary)); start >= 0 {
			tail = tail[start+1:]
		}
	}
	return tail
}

// bounded cuts a field that names a file or a unit.
func bounded(field string) string {
	if len(field) > maxLogFieldBytes {
		field = field[:maxLogFieldBytes]
	}
	return strings.ToValidUTF8(field, "")
}

// textLines turns a text file into one line per non-empty line, each bounded and valid UTF-8.
func textLines(source, file string, data []byte, add func(logLine)) {
	file = bounded(file)
	for len(data) > 0 {
		var raw []byte
		raw, data, _ = bytes.Cut(data, []byte("\n"))
		raw = bytes.TrimRight(raw, "\r")
		if len(raw) == 0 {
			continue
		}
		if len(raw) > maxLogLineBytes {
			raw = raw[:maxLogLineBytes]
		}
		add(logLine{Source: source, File: file, Message: strings.ToValidUTF8(string(raw), "�")})
	}
}

// journalLines reads the BMC journal, a stream of JSON objects (journalctl -o json-pretty). An object
// starts at the start of a line, so a damaged entry is skipped and the ones after it are kept.
func journalLines(data []byte, add func(logLine)) {
	for i, part := range bytes.Split(data, []byte("\n{")) {
		if i > 0 {
			part = append([]byte("{"), part...)
		}
		journalEntries(part, add)
	}
}

// journalEntries reads the journal objects that follow each other in data, up to a damaged one.
func journalEntries(data []byte, add func(logLine)) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		entry := map[string]any{}
		if err := decoder.Decode(&entry); err != nil {
			return
		}
		line := logLine{Source: sourceJournal, Message: journalText(entry["MESSAGE"])}
		if len(line.Message) > maxLogLineBytes {
			line.Message = strings.ToValidUTF8(line.Message[:maxLogLineBytes], "")
		}
		line.Unit, _ = entry["SYSLOG_IDENTIFIER"].(string)
		if line.Unit == "" {
			line.Unit, _ = entry["_SYSTEMD_UNIT"].(string)
		}
		line.Unit = bounded(line.Unit)
		// The BMC's own clock, which can be wrong; it orders the entries but is not compared to ours.
		if stamp, _ := entry["__REALTIME_TIMESTAMP"].(string); stamp != "" {
			if micros, err := strconv.ParseInt(stamp, 10, 64); err == nil {
				line.BMCTime = time.UnixMicro(micros).UTC().Format(time.RFC3339)
			}
		}
		add(line)
	}
}

// journalText returns a journal field as text. journald writes a field that is not plain text as an
// array of byte values.
func journalText(field any) string {
	switch value := field.(type) {
	case string:
		return value
	case []any:
		raw := make([]byte, 0, len(value))
		for _, item := range value {
			if number, ok := item.(float64); ok && number >= 0 && number <= 255 {
				raw = append(raw, byte(number))
			}
		}
		return strings.ToValidUTF8(string(raw), "�")
	}
	return ""
}

// consoleTarLines reads the BF4 console capture: a tar of console logs, some of them gzipped. What
// the gzipped ones unpack to is taken from remaining; it reports false when that runs out.
func consoleTarLines(data []byte, remaining *int64, add func(logLine)) bool {
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err != nil {
			return true
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		content, _ := io.ReadAll(io.LimitReader(reader, maxLogFileBytes))
		if unzipped, err := gzip.NewReader(bytes.NewReader(content)); err == nil {
			// All of it is unpacked and counted; the newest part is kept, as for the other logs.
			limited := &io.LimitedReader{R: unzipped, N: *remaining + 1}
			content = tailOf(limited, maxLogFileBytes, "\n")
			if *remaining = limited.N - 1; *remaining < 0 {
				return false
			}
		}
		textLines(sourceConsole, header.Name, content, add)
	}
}

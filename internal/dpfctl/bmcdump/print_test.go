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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	. "github.com/onsi/gomega"
)

// tarOf builds a tar with the files in the order given, as name, content pairs.
func tarOf(t *testing.T, nameContent ...string) []byte {
	t.Helper()
	g := NewWithT(t)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i := 0; i < len(nameContent); i += 2 {
		name, content := nameContent[i], nameContent[i+1]
		g.Expect(tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg})).To(Succeed())
		_, err := tw.Write([]byte(content))
		g.Expect(err).NotTo(HaveOccurred())
	}
	g.Expect(tw.Close()).To(Succeed())
	return buf.Bytes()
}

// dumpArchive builds a dump the way the BMC ships it: a zstd-compressed tar.
func dumpArchive(t *testing.T, nameContent ...string) []byte {
	t.Helper()
	g := NewWithT(t)
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = zw.Write(tarOf(t, nameContent...))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(zw.Close()).To(Succeed())
	return buf.Bytes()
}

func gzipOf(t *testing.T, content string) string {
	t.Helper()
	g := NewWithT(t)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(content))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(zw.Close()).To(Succeed())
	return buf.String()
}

// The file names and the journal fields are those of the dumps measured on BF3 and BF4.
const journalFixture = `{
	"__REALTIME_TIMESTAMP" : "1794758000000000",
	"SYSLOG_IDENTIFIER" : "ipmid",
	"MESSAGE" : "first entry"
}
{
	"__REALTIME_TIMESTAMP" : "1794758001000000",
	"_SYSTEMD_UNIT" : "rshim.service",
	"MESSAGE" : [ 98, 121, 116, 101, 115 ]
}
`

func managerDumpFixture(t *testing.T) []byte {
	return dumpArchive(t,
		"obmcdump_1_1794758788/bmc-state.log", "xyz.openbmc_project.State.BMC.BMCState.Ready\n",
		"obmcdump_1_1794758788/journal-pretty.log", journalFixture,
		"obmcdump_1_1794758788/dmesg.log", "[    0.000000] Booting Linux\n[    1.000000] second line\n",
		"obmcdump_1_1794758788/elogall.log", "event 1\nevent 2\nevent 3\n",
		"obmcdump_1_1794758788/dpulogs/dpu_console", "bf3 console line\n",
		"obmcdump_1_1794758788/nicDeviceDebugInfo.log", "not printed\n",
		"obmcdump_1_1794758788/varfilelist.log", "not printed either\n",
	)
}

func systemDumpFixture(t *testing.T) []byte {
	console := tarOf(t,
		"dpu_console/dpu_console_active", "bf4 console line\n",
		"console.log.gz", gzipOf(t, "older console line\n"),
	)
	return dumpArchive(t,
		"systemdump_1_1794758480/biospostcode.log", "0x01 0x02 0x03\n",
		"systemdump_1_1794758480/dpu_console.tar", string(console),
		"systemdump_1_1794758480/erot_dump_16.bin", "\x00\x01binary",
	)
}

// writeDumps lays out a finished run the way the collector does.
func writeDumps(t *testing.T, dumps map[string][]byte) string {
	t.Helper()
	g := NewWithT(t)
	dir := t.TempDir()
	for unit, archive := range dumps {
		g.Expect(os.MkdirAll(filepath.Join(dir, unit), 0700)).To(Succeed())
		g.Expect(os.WriteFile(filepath.Join(dir, unit, dumpArchiveName), archive, 0600)).To(Succeed())
	}
	return dir
}

func decodeLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	g := NewWithT(t)
	raw := strings.Split(strings.TrimSpace(output), "\n")
	lines := make([]map[string]any, 0, len(raw))
	for _, line := range raw {
		entry := map[string]any{}
		g.Expect(json.Unmarshal([]byte(line), &entry)).To(Succeed(), "every line must be one JSON object: %q", line)
		lines = append(lines, entry)
	}
	return lines
}

func messagesOf(lines []map[string]any, source string) []string {
	var messages []string
	for _, line := range lines {
		if line["source"] == source {
			messages = append(messages, line["message"].(string))
		}
	}
	return messages
}

func TestPrintDumpLogsPrintsTheSelectedFilesOfBothDumps(t *testing.T) {
	g := NewWithT(t)

	dir := writeDumps(t, map[string][]byte{managerUnitName: managerDumpFixture(t), systemUnitName: systemDumpFixture(t)})
	var out bytes.Buffer

	summary := printDumpLogs(&out, dir, maxPrintedLogBytes)

	g.Expect(summary.err).NotTo(HaveOccurred())
	g.Expect(summary.dumps).To(Equal([]string{managerUnitName, systemUnitName}))
	g.Expect(summary.dropped).To(BeZero())
	lines := decodeLines(t, out.String())
	g.Expect(summary.lines).To(Equal(len(lines)))

	g.Expect(lines).To(ContainElement(SatisfyAll(
		HaveKeyWithValue("source", "journal"),
		HaveKeyWithValue("bmc_time", "2026-11-15T15:53:20Z"),
		HaveKeyWithValue("unit", "ipmid"),
		HaveKeyWithValue("message", "first entry"),
	)))
	// A message the journal stores as bytes, and the unit taken from the systemd unit.
	g.Expect(lines).To(ContainElement(SatisfyAll(
		HaveKeyWithValue("unit", "rshim.service"),
		HaveKeyWithValue("message", "bytes"),
	)))
	g.Expect(lines).To(ContainElement(SatisfyAll(
		HaveKeyWithValue("source", "state"),
		HaveKeyWithValue("file", "bmc-state.log"),
		HaveKeyWithValue("message", "xyz.openbmc_project.State.BMC.BMCState.Ready"),
	)))
	g.Expect(messagesOf(lines, "kernel")).To(Equal([]string{"[    0.000000] Booting Linux", "[    1.000000] second line"}))
	g.Expect(messagesOf(lines, "eventlog")).To(Equal([]string{"event 1", "event 2", "event 3"}))
	g.Expect(messagesOf(lines, "postcode")).To(Equal([]string{"0x01 0x02 0x03"}))
	// The BF3 capture is a plain file; the BF4 one is a tar holding plain and gzipped logs.
	g.Expect(messagesOf(lines, "console")).To(ConsistOf("bf3 console line", "bf4 console line", "older console line"))

	g.Expect(out.String()).NotTo(ContainSubstring("not printed"))
	g.Expect(out.String()).NotTo(ContainSubstring("binary"))
}

func TestPrintDumpLogsDropsTheOldestEventLogLinesFirst(t *testing.T) {
	g := NewWithT(t)

	var eventLog strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&eventLog, "event %03d\n", i)
	}
	dir := writeDumps(t, map[string][]byte{managerUnitName: dumpArchive(t,
		"obmcdump_1_1/journal-pretty.log", journalFixture,
		"obmcdump_1_1/dmesg.log", "kernel line\n",
		"obmcdump_1_1/elogall.log", eventLog.String(),
	)})
	var out bytes.Buffer
	const budget = 2000

	summary := printDumpLogs(&out, dir, budget)

	g.Expect(out.Len()).To(BeNumerically("<=", budget))
	lines := decodeLines(t, out.String())
	// Everything else is printed whole; only the event log pays for the limit.
	g.Expect(messagesOf(lines, "journal")).To(HaveLen(2))
	g.Expect(messagesOf(lines, "kernel")).To(HaveLen(1))
	events := messagesOf(lines, "eventlog")
	g.Expect(events).NotTo(BeEmpty())
	g.Expect(events[len(events)-1]).To(Equal("event 200"), "the newest lines are the ones kept")
	g.Expect(summary.dropped).To(Equal(200 - len(events)))
	g.Expect(summary.dropped).To(BeNumerically(">", 0))
}

func TestPrintDumpLogsKeepsWhatItParsedOfADamagedJournal(t *testing.T) {
	g := NewWithT(t)

	dir := writeDumps(t, map[string][]byte{managerUnitName: dumpArchive(t,
		"obmcdump_1_1/journal-pretty.log", journalFixture+`{"MESSAGE": "cut o`,
	)})
	var out bytes.Buffer

	printDumpLogs(&out, dir, maxPrintedLogBytes)

	g.Expect(messagesOf(decodeLines(t, out.String()), "journal")).To(Equal([]string{"first entry", "bytes"}))
}

// Only the newest part of a long journal is kept, so what follows a damaged entry is what matters.
func TestJournalLinesSkipsADamagedEntry(t *testing.T) {
	g := NewWithT(t)

	var messages []string
	journalLines([]byte(`{"MESSAGE": "before"}
{"MESSAGE": "damaged", "PRIORITY": }
{"MESSAGE": "after"}
{"MESSAGE": "newest"}
`), func(l logLine) { messages = append(messages, l.Message) })

	g.Expect(messages).To(Equal([]string{"before", "after", "newest"}))
}

// The tar can end before the compressed stream does, and what follows it can be damaged.
func TestReadDumpLogsFailsOnDamageAfterTheTar(t *testing.T) {
	g := NewWithT(t)

	archive := filepath.Join(t.TempDir(), dumpArchiveName)
	damagedFrame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0xff, 0xff, 0xff, 0xff}
	g.Expect(os.WriteFile(archive, append(dumpArchive(t, "obmcdump_1_1/dmesg.log", "kept\n"), damagedFrame...), 0600)).To(Succeed())
	tails := &logTails{budget: maxPrintedLogBytes}

	err := readDumpLogs(archive, maxUnpackedDumpBytes, tails)

	g.Expect(err).To(HaveOccurred())
	g.Expect(tails.of("kernel").lines).To(HaveLen(1), "what was read is still printed")
}

func TestReadDumpLogsStopsAtTheUnpackedLimit(t *testing.T) {
	g := NewWithT(t)

	archive := filepath.Join(t.TempDir(), dumpArchiveName)
	g.Expect(os.WriteFile(archive, dumpArchive(t,
		"obmcdump_1_1/dmesg.log", "kept\n",
		"obmcdump_1_1/varfilelist.log", strings.Repeat("x", 100_000),
		"obmcdump_1_1/elogall.log", "never reached\n",
	), 0600)).To(Succeed())
	tails := &logTails{budget: maxPrintedLogBytes}

	err := readDumpLogs(archive, 50_000, tails)

	g.Expect(err).To(MatchError(ContainSubstring("unpacks to more than")))
	g.Expect(tails.of("kernel").lines).To(HaveLen(1))
	g.Expect(tails.of("eventlog").lines).To(BeEmpty())
}

// A hostile or faulty BMC must not make the collection hold more than it will print.
func TestReadDumpLogsKeepsOnlyTheNewestLinesThatCanBePrinted(t *testing.T) {
	g := NewWithT(t)

	var kernel strings.Builder
	for i := 1; i <= 10_000; i++ {
		fmt.Fprintf(&kernel, "%d\n", i)
	}
	archive := filepath.Join(t.TempDir(), dumpArchiveName)
	g.Expect(os.WriteFile(archive, dumpArchive(t, "obmcdump_1_1/dmesg.log", kernel.String()), 0600)).To(Succeed())
	tails := &logTails{budget: 2000}

	g.Expect(readDumpLogs(archive, maxUnpackedDumpBytes, tails)).To(Succeed())

	kept := tails.of("kernel")
	g.Expect(len(kept.lines)).To(BeNumerically("<", 100))
	g.Expect(kept.lines[len(kept.lines)-1].Message).To(Equal("10000"))
	g.Expect(kept.dropped).To(Equal(10_000 - len(kept.lines)))
}

// The console capture is a tar of gzipped files, and what those unpack to counts too.
func TestReadDumpLogsCountsTheUnpackedConsoleCapture(t *testing.T) {
	g := NewWithT(t)

	console := tarOf(t, "a.gz", gzipOf(t, strings.Repeat("x\n", 50_000)), "b.gz", gzipOf(t, strings.Repeat("y\n", 50_000)))
	archive := filepath.Join(t.TempDir(), dumpArchiveName)
	g.Expect(os.WriteFile(archive, dumpArchive(t, "obmcdump_1_1/"+consoleTarName, string(console)), 0600)).To(Succeed())

	err := readDumpLogs(archive, 150_000, &logTails{budget: maxPrintedLogBytes})

	g.Expect(err).To(MatchError(ContainSubstring("unpacks to more than")))
}

func TestTextLinesBoundsEachLine(t *testing.T) {
	g := NewWithT(t)

	var lines []logLine
	textLines("kernel", strings.Repeat("f", maxLogFieldBytes+10),
		[]byte(strings.Repeat("a", maxLogLineBytes+10)+"\r\n\n\xff ok\n"), func(l logLine) { lines = append(lines, l) })

	g.Expect(lines).To(HaveLen(2))
	g.Expect(lines[0].Message).To(HaveLen(maxLogLineBytes))
	g.Expect(lines[0].File).To(HaveLen(maxLogFieldBytes))
	g.Expect(lines[1].Message).To(Equal("� ok"), "invalid bytes must not break the JSON line")
}

func TestJournalLinesBoundsTheUnitName(t *testing.T) {
	g := NewWithT(t)

	var lines []logLine
	journalLines([]byte(`{"MESSAGE":"m","SYSLOG_IDENTIFIER":"`+strings.Repeat("u", maxLogFieldBytes+10)+`"}`),
		func(l logLine) { lines = append(lines, l) })

	g.Expect(lines).To(HaveLen(1))
	g.Expect(lines[0].Unit).To(HaveLen(maxLogFieldBytes))
}

// A dump that cannot be read is a failed collection, even though it was downloaded.
func TestCollectFailsWhenADumpCannotBeRead(t *testing.T) {
	g := NewWithT(t)

	fixture := bf4Fixture()
	fixture.attachments = map[string][]byte{
		fixture.managerDumpPath(): managerDumpFixture(t),
		fixture.systemDumpPath():  []byte("<html>not a dump</html>"),
	}
	var out bytes.Buffer

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   strings.TrimPrefix(fixtureServer(t, fixture).URL, "https://"),
		PasswordFile: passwordFileFor(t),
		TLSConfig:    insecureTLS(),
		OutputDir:    t.TempDir(),
		PrintLogs:    &out,
	})

	g.Expect(err).To(MatchError(ContainSubstring("reading system dump")))
	lines := decodeLines(t, out.String())
	g.Expect(messagesOf(lines, "journal")).To(HaveLen(2), "the readable dump is still printed")
	g.Expect(lines[len(lines)-1]).To(HaveKeyWithValue("ok", false))
}

// The logs matter more than the archive: a directory that cannot be written costs only the archive.
func TestCollectStillPrintsLogsWhenTheArchiveDirectoryCannotBeWritten(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes)
	archiveDir := t.TempDir()
	// A file where the serial's directory should be.
	g.Expect(os.WriteFile(filepath.Join(archiveDir, "MT2616606M28"), nil, 0600)).To(Succeed())
	t.Setenv("TMPDIR", t.TempDir())
	var out bytes.Buffer

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   printingFixtureServer(t),
		PasswordFile: passwordFileFor(t),
		TLSConfig:    insecureTLS(),
		ArchiveDir:   archiveDir,
		Serial:       "MT2616606M28",
		PrintLogs:    &out,
	})

	g.Expect(err).NotTo(HaveOccurred())
	lines := decodeLines(t, out.String())
	g.Expect(messagesOf(lines, "journal")).To(HaveLen(2))
	g.Expect(lines[len(lines)-1]["archive"]).To(ContainSubstring("MT2616606M28"))
}

func runDirWithDump(t *testing.T, serialDir, name string) string {
	t.Helper()
	dir := filepath.Join(serialDir, name)
	g := NewWithT(t)
	g.Expect(os.MkdirAll(filepath.Join(dir, managerUnitName), 0700)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, managerUnitName, dumpArchiveName), []byte("dump"), 0600)).To(Succeed())
	return dir
}

func TestFinishArchiveKeepsTheNewestRunsAndTouchesNothingElse(t *testing.T) {
	g := NewWithT(t)

	// A glob character in the path must not hide the dump that was just stored.
	serialDir := filepath.Join(t.TempDir(), "bmc[1]", "MT2616606M28")
	for _, name := range []string{"20261101T000000Z-a", "20261102T000000Z-a", "20261103T000000Z-a"} {
		runDirWithDump(t, serialDir, name)
	}
	newest := runDirWithDump(t, serialDir, "20261104T000000Z-b")
	// Not runs of this command: left alone and not counted.
	g.Expect(os.MkdirAll(filepath.Join(serialDir, "bfb-images"), 0700)).To(Succeed())
	// Another Job's run that has no dump yet: not counted, and not removed while it may be running.
	inProgress := time.Now().UTC().Format(archiveRunTimeFormat) + "-c"
	g.Expect(os.MkdirAll(filepath.Join(serialDir, inProgress), 0700)).To(Succeed())
	// A run that was killed long ago and left no dump.
	g.Expect(os.MkdirAll(filepath.Join(serialDir, "20261001T000000Z-d"), 0700)).To(Succeed())

	g.Expect(finishArchive(newest)).To(Succeed())

	entries, err := os.ReadDir(serialDir)
	g.Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	g.Expect(names).To(ConsistOf("20261102T000000Z-a", "20261103T000000Z-a", "20261104T000000Z-b", "bfb-images", inProgress))
}

func TestCollectReportsAPruningFailureInTheResult(t *testing.T) {
	g := NewWithT(t)

	out := &logOutput{archive: "stored"}
	out.pruned(errors.New("boom"))
	g.Expect(out.archive).To(Equal("stored; pruning older archives failed: boom"))
}

// printingFixtureServer serves real dump archives, so a whole run can be printed.
func printingFixtureServer(t *testing.T) (address string) {
	t.Helper()
	fixture := bf4Fixture()
	fixture.attachments = map[string][]byte{
		fixture.managerDumpPath(): managerDumpFixture(t),
		fixture.systemDumpPath():  systemDumpFixture(t),
	}
	return strings.TrimPrefix(fixtureServer(t, fixture).URL, "https://")
}

func passwordFileFor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "password")
	NewWithT(t).Expect(os.WriteFile(path, []byte("password"), 0600)).To(Succeed())
	return path
}

func TestCollectPrintsHeaderLogsAndResult(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes)
	archiveDir := t.TempDir()
	var out bytes.Buffer

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   printingFixtureServer(t),
		PasswordFile: passwordFileFor(t),
		TLSConfig:    insecureTLS(),
		ArchiveDir:   archiveDir,
		Serial:       "MT2616606M28",
		DPUUID:       "uid-1",
		StopAt:       time.Now().Add(time.Minute),
		PrintLogs:    &out,
		Header:       map[string]string{"trigger": "failure", "dpu": "dpu-1", "failed_condition": "OSInstalled"},
	})

	g.Expect(err).NotTo(HaveOccurred())
	lines := decodeLines(t, out.String())
	g.Expect(lines[0]).To(Equal(map[string]any{
		"source": "header", "serial": "MT2616606M28", "trigger": "failure", "dpu": "dpu-1", "failed_condition": "OSInstalled",
	}))
	g.Expect(messagesOf(lines, "journal")).To(HaveLen(2))
	g.Expect(messagesOf(lines, "postcode")).To(HaveLen(1))
	g.Expect(lines[len(lines)-1]).To(Equal(map[string]any{
		"source": "result", "ok": true, "dumps": []any{"manager", "system"}, "archive": "stored",
		"lines": float64(len(lines) - 2), "dropped": float64(0),
	}))
	// Nothing but JSON lines: the progress output of a run by hand would break the log pipeline.
	g.Expect(out.String()).NotTo(ContainSubstring("Collecting"))
}

func TestCollectPrintsAResultWhenTheBMCFails(t *testing.T) {
	g := NewWithT(t)

	server := fixtureServer(t, &bmcFixture{product: "BlueField-4", managerID: "wrong", systemID: "wrong"})
	var out bytes.Buffer

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   strings.TrimPrefix(server.URL, "https://"),
		PasswordFile: passwordFileFor(t),
		TLSConfig:    insecureTLS(),
		OutputDir:    t.TempDir(),
		PrintLogs:    &out,
	})

	g.Expect(err).To(HaveOccurred())
	lines := decodeLines(t, out.String())
	g.Expect(lines[0]).To(HaveKeyWithValue("source", "header"))
	result := lines[len(lines)-1]
	g.Expect(result).To(HaveKeyWithValue("source", "result"))
	g.Expect(result).To(HaveKeyWithValue("ok", false))
	g.Expect(result).To(HaveKeyWithValue("dumps", BeEmpty()))
	g.Expect(result).To(HaveKeyWithValue("archive", "not requested"))
	g.Expect(result["error"]).To(ContainSubstring(http.StatusText(http.StatusNotFound)))
}

func TestCollectDoesNotPrintDumpsOfAnEarlierRun(t *testing.T) {
	g := NewWithT(t)

	// An output directory used before still holds that run's dumps.
	outputDir := writeDumps(t, map[string][]byte{managerUnitName: managerDumpFixture(t)})
	server := fixtureServer(t, &bmcFixture{product: "BlueField-4", managerID: "wrong", systemID: "wrong"})
	var out bytes.Buffer

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   strings.TrimPrefix(server.URL, "https://"),
		PasswordFile: passwordFileFor(t),
		TLSConfig:    insecureTLS(),
		OutputDir:    outputDir,
		PrintLogs:    &out,
	})

	g.Expect(err).To(HaveOccurred())
	lines := decodeLines(t, out.String())
	g.Expect(lines).To(HaveLen(2), "only the header and the result")
	g.Expect(lines[1]).To(HaveKeyWithValue("dumps", BeEmpty()))
}

func TestConsoleTarLinesKeepsTheNewestLinesOfALargeLog(t *testing.T) {
	g := NewWithT(t)

	var log strings.Builder
	for i := 0; log.Len() <= maxLogFileBytes; i++ {
		fmt.Fprintf(&log, "console line %08d\n", i)
	}
	log.WriteString("the newest line\n")
	remaining := int64(maxUnpackedDumpBytes)
	var messages []string

	ok := consoleTarLines(tarOf(t, "console.log.gz", gzipOf(t, log.String())), &remaining, func(line logLine) {
		messages = append(messages, line.Message)
	})

	g.Expect(ok).To(BeTrue())
	g.Expect(messages[len(messages)-1]).To(Equal("the newest line"))
	g.Expect(messages[0]).NotTo(Equal("console line 00000000"))
	g.Expect(remaining).To(Equal(int64(maxUnpackedDumpBytes-log.Len())), "all of what was unpacked is counted")
}

func TestCollectStillPrintsLogsWhenTheArchiveVolumeIsLow(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes-1)
	archiveDir := t.TempDir()
	var out bytes.Buffer
	// The dumps go to the system temporary directory; point it somewhere the test can inspect.
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)

	err := Collect(context.Background(), nil, CollectOptions{
		BMCAddress:   printingFixtureServer(t),
		PasswordFile: passwordFileFor(t),
		TLSConfig:    insecureTLS(),
		ArchiveDir:   archiveDir,
		Serial:       "MT2616606M28",
		PrintLogs:    &out,
	})

	g.Expect(err).NotTo(HaveOccurred())
	lines := decodeLines(t, out.String())
	g.Expect(messagesOf(lines, "journal")).To(HaveLen(2))
	result := lines[len(lines)-1]
	g.Expect(result).To(HaveKeyWithValue("ok", true))
	g.Expect(result["archive"]).To(ContainSubstring("below the"))
	g.Expect(os.ReadDir(archiveDir)).To(BeEmpty())
	g.Expect(os.ReadDir(scratch)).To(BeEmpty(), "the dumps are removed once their logs are printed")
}

func TestCollectOptionsValidatePrintLogs(t *testing.T) {
	g := NewWithT(t)

	err := CollectOptions{PrintLogs: &bytes.Buffer{}}.validate()

	g.Expect(err).To(MatchError(ContainSubstring("requires a bmc address")))
}

func TestNewArchiveRunDirCreatesAMissingArchiveDirectory(t *testing.T) {
	g := NewWithT(t)

	archiveDir := filepath.Join(t.TempDir(), "not", "there", "yet")

	runDir, err := newArchiveRunDir(CollectOptions{ArchiveDir: archiveDir, Serial: "MT2616606M28", ArchiveMinFreeBytes: 1})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(runDir).To(BeADirectory())
}

// Two collections of one DPU in the same second must not write into each other's directory.
func TestNewArchiveRunDirIsNeverSharedByTwoRuns(t *testing.T) {
	g := NewWithT(t)

	stubFreeBytes(t, minArchiveFreeBytes)
	opts := CollectOptions{ArchiveDir: t.TempDir(), Serial: "MT2616606M28", DPUUID: "uid-1"}

	first, err := newArchiveRunDir(opts)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first).To(BeADirectory())
	second, err := newArchiveRunDir(opts)
	if err == nil {
		g.Expect(second).NotTo(Equal(first))
	}
}

// Other runs of the serial can finish while this one collects. Its dumps are still to be printed.
func TestFinishArchiveKeepsTheRunItFinishes(t *testing.T) {
	g := NewWithT(t)

	serialDir := filepath.Join(t.TempDir(), "MT2616606M28")
	current := runDirWithDump(t, serialDir, "20261101T000000Z-a")
	for _, later := range []string{"20261102T000000Z-a", "20261103T000000Z-a", "20261104T000000Z-a"} {
		runDirWithDump(t, serialDir, later)
	}

	g.Expect(finishArchive(current)).To(Succeed())

	g.Expect(hasDump(current)).To(BeTrue())
}

func TestTailOfKeepsTheNewestWholeEntries(t *testing.T) {
	g := NewWithT(t)

	g.Expect(string(tailOf(strings.NewReader("one\ntwo\nthree\n"), 100, "\n"))).To(Equal("one\ntwo\nthree\n"))
	// Cut inside "two": the newest whole line is what is left.
	g.Expect(string(tailOf(strings.NewReader("one\ntwo\nthree\n"), 8, "\n"))).To(Equal("three\n"))
	// A journal is cut at the start of an entry, so it can still be parsed.
	journal := "{\n\t\"MESSAGE\" : \"old\"\n}\n{\n\t\"MESSAGE\" : \"new\"\n}\n"
	g.Expect(string(tailOf(strings.NewReader(journal), 30, "\n{"))).To(Equal("{\n\t\"MESSAGE\" : \"new\"\n}\n"))
	// Longer than the buffer it is read through.
	long := strings.Repeat("0123456789\n", 50_000) + "last\n"
	g.Expect(string(tailOf(strings.NewReader(long), 20, "\n"))).To(Equal("0123456789\nlast\n"))
}

// failingWriter refuses the line of one source.
type failingWriter struct{ source string }

func (w failingWriter) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte(`"source":"`+w.source+`"`)) {
		return 0, errors.New("the pipe is closed")
	}
	return len(data), nil
}

// A stream without its header or result line is not a collection the log store can use.
func TestCollectFailsWhenTheOutputCannotBeWritten(t *testing.T) {
	for _, source := range []string{"header", "journal", "result"} {
		t.Run(source, func(t *testing.T) {
			g := NewWithT(t)

			err := Collect(context.Background(), nil, CollectOptions{
				BMCAddress:   printingFixtureServer(t),
				PasswordFile: passwordFileFor(t),
				TLSConfig:    insecureTLS(),
				OutputDir:    t.TempDir(),
				PrintLogs:    failingWriter{source: source},
			})

			g.Expect(err).To(MatchError(ContainSubstring("the pipe is closed")))
		})
	}
}

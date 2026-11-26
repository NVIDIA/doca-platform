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

package cmd

import (
	"os"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

// parseDumpBMCFlags runs the real flag set over args and returns the options the command would use.
func parseDumpBMCFlags(t *testing.T, args ...string) error {
	t.Helper()
	dumpBMCOpts = dumpBMCFlags{}
	t.Cleanup(func() { dumpBMCOpts = dumpBMCFlags{} })
	cmd := &cobra.Command{}
	addDumpBMCFlags(cmd)
	return cmd.ParseFlags(args)
}

func TestDumpBMCCollectOptionsMapsTheSingleBMCFlags(t *testing.T) {
	g := NewWithT(t)

	g.Expect(parseDumpBMCFlags(t,
		"--bmc-address", "10.0.0.10:8443",
		"--password-file", "/secret/password",
		"--ca-cert-file", "/ca/ca.crt",
		"--stop-at", "2026-11-17T12:00:00Z",
		"--archive-dir", "/archive",
		"--archive-min-free", "200Mi",
		"--serial", "MT2616606M28",
		"--dpu-uid", "uid-1",
		"--timeout", "10m",
	)).To(Succeed())

	opts, err := dumpBMCCollectOptions()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(opts.BMCAddress).To(Equal("10.0.0.10:8443"))
	g.Expect(opts.PasswordFile).To(Equal("/secret/password"))
	g.Expect(opts.CACertFile).To(Equal("/ca/ca.crt"))
	g.Expect(opts.StopAt).To(Equal(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)))
	g.Expect(opts.ArchiveDir).To(Equal("/archive"))
	g.Expect(opts.ArchiveMinFreeBytes).To(Equal(int64(200 << 20)))
	g.Expect(opts.Serial).To(Equal("MT2616606M28"))
	g.Expect(opts.DPUUID).To(Equal("uid-1"))
	g.Expect(opts.TaskTimeout).To(Equal(10 * time.Minute))
}

func TestDumpBMCCollectOptionsDefaults(t *testing.T) {
	g := NewWithT(t)

	g.Expect(parseDumpBMCFlags(t)).To(Succeed())

	opts, err := dumpBMCCollectOptions()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(opts.StopAt.IsZero()).To(BeTrue())
	g.Expect(opts.ArchiveMinFreeBytes).To(Equal(int64(2 << 30)))
}

func TestDumpBMCCollectOptionsRejectsBadValues(t *testing.T) {
	for name, args := range map[string][]string{
		"--stop-at":          {"--stop-at", "tomorrow"},
		"--archive-min-free": {"--archive-min-free", "plenty"},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(parseDumpBMCFlags(t, args...)).To(Succeed())

			_, err := dumpBMCCollectOptions()

			g.Expect(err).To(MatchError(ContainSubstring(name)))
		})
	}
}

func TestDumpBMCCollectOptionsMapsTheLogPrintingFlags(t *testing.T) {
	g := NewWithT(t)

	g.Expect(parseDumpBMCFlags(t,
		"--print-logs",
		"--header", "trigger=failure",
		"--header", "dpu=dpu-1,failed_reason=InstallationTimeout",
	)).To(Succeed())

	opts, err := dumpBMCCollectOptions()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(opts.PrintLogs).To(BeIdenticalTo(os.Stdout))
	g.Expect(opts.Header).To(Equal(map[string]string{
		"trigger": "failure", "dpu": "dpu-1", "failed_reason": "InstallationTimeout",
	}))
}

func TestDumpBMCCollectOptionsPrintsNoLogsByDefault(t *testing.T) {
	g := NewWithT(t)

	g.Expect(parseDumpBMCFlags(t)).To(Succeed())

	opts, err := dumpBMCCollectOptions()

	g.Expect(err).NotTo(HaveOccurred())
	// A nil *os.File stored in the interface would not be nil, and would turn printing on.
	g.Expect(opts.PrintLogs).To(BeNil())
}

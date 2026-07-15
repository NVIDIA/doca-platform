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

// Package gitlab holds the "dpfdev gitlab" command group: subcommands that
// talk to the GitLab API (pipeline schedules today, runners and others over
// time). The root command wires Cmd in.
package gitlab

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Cmd is the parent "gitlab" command. Subcommands attach to it in their own
// init functions.
var Cmd = &cobra.Command{
	Use:   "gitlab",
	Short: "Commands that interact with the GitLab API",
	Long:  `Commands that interact with the GitLab API, such as managing scheduled pipelines.`,
	// Configure logging before any subcommand runs. Cobra runs the nearest
	// PersistentPreRun, and no gitlab subcommand defines its own.
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		if debug, _ := cmd.Flags().GetBool("debug"); debug {
			debugEnabled = true
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
		}
	},
}

// debugEnabled reports whether --debug was set. Commands use it to skip plain
// progress output that the debug logs already cover in more detail.
var debugEnabled bool

func init() {
	// Inherited by every gitlab subcommand. Without it, slog.Debug calls are
	// below the default level and produce no output.
	Cmd.PersistentFlags().Bool("debug", false, "Enable debug logging to stderr")
}

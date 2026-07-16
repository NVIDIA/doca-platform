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

// Package util contains shared dpfctl helpers.
package util

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// CLI output helpers for consistent formatting.

// Verbose controls whether debug output is printed.
var Verbose bool

// Debug prints a debug message (only when --verbose is set).
func Debug(format string, a ...any) {
	if Verbose {
		fmt.Printf("  [debug] "+format+"\n", a...)
	}
}

// Step prints a top-level step heading.
func Step(format string, a ...any) {
	fmt.Printf("● "+format+"\n", a...)
}

// Success prints a success message indented under the current step.
func Success(format string, a ...any) {
	fmt.Printf("  ✓ "+format+"\n", a...)
}

// Failure prints a failure message indented under the current step.
func Failure(format string, a ...any) {
	fmt.Printf("  ✗ "+format+"\n", a...)
}

// Info prints an informational message indented under the current step.
func Info(format string, a ...any) {
	fmt.Printf("  "+format+"\n", a...)
}

// Warn prints a warning message.
func Warn(format string, a ...any) {
	fmt.Printf("  ! "+format+"\n", a...)
}

// Result prints a final result line.
func Result(format string, a ...any) {
	fmt.Printf("✓ "+format+"\n", a...)
}

// ResultFail prints a final failure result line.
func ResultFail(format string, a ...any) {
	fmt.Printf("✗ "+format+"\n", a...)
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// isTTY reports whether stdout is a terminal.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// StartSpinner starts an animated spinner with a message. Returns a stop function
// that clears the spinner line. Call stop() before printing the next line.
// In non-TTY environments (CI, kubectl exec), prints a static line instead.
func StartSpinner(format string, a ...any) func() {
	msg := fmt.Sprintf(format, a...)

	if !isTTY() {
		Info("%s", msg)
		return func() {}
	}

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		frame := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				fmt.Print("\r\033[K") // clear line
				return
			case <-ticker.C:
				fmt.Printf("\r  %s %s", spinnerFrames[frame%len(spinnerFrames)], msg)
				frame++
			}
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}

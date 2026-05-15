// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

// Package clitest gives pipit's command tests a uniform way to drive subcommand
// entrypoints with captured IO.
package clitest

import (
	"bytes"
	"context"
	"io"
	"strings"

	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// Result captures the outcome of a Run invocation.
type Result struct {
	// Stdout is the captured standard output.
	Stdout string

	// Stderr is the captured standard error.
	Stderr string

	// Code is the exit code the command returned.
	Code int
}

// CommandFunc is the canonical signature every pipit subcommand exposes: (context, args,
// IO bundle) -> exit code.
type CommandFunc func(ctx context.Context, args []string, streams output.IO) int

// Run drives the given command with captured IO and returns its result.
//
// Takes command (CommandFunc) which is the subcommand entrypoint to invoke.
// Takes args ([]string) which holds the positional command arguments.
// Takes stdin (string) which is the literal text fed to standard input.
//
// Returns Result which captures the exit code and captured output.
func Run(ctx context.Context, command CommandFunc, args []string, stdin string) Result {
	var stdout, stderr bytes.Buffer
	streams := output.IO{
		Stdin:      strings.NewReader(stdin),
		Stdout:     &stdout,
		Stderr:     &stderr,
		ColourMode: output.ColourNever,
	}
	code := command(ctx, args, streams)
	return Result{
		Code:   code,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
}

// RunWithReader drives the command with a non-string stdin reader.
//
// Takes command (CommandFunc) which is the subcommand entrypoint to invoke.
// Takes args ([]string) which holds the positional command arguments.
// Takes stdin (io.Reader) which supplies the standard input stream.
//
// Returns Result which captures the exit code and captured output.
func RunWithReader(ctx context.Context, command CommandFunc, args []string, stdin io.Reader) Result {
	var stdout, stderr bytes.Buffer
	streams := output.IO{
		Stdin:      stdin,
		Stdout:     &stdout,
		Stderr:     &stderr,
		ColourMode: output.ColourNever,
	}
	code := command(ctx, args, streams)
	return Result{
		Code:   code,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
}

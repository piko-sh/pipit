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

// Package output centralises CLI writer plumbing: TTY detection, colour mode resolution,
// and a sink type passed to every subcommand.
package output

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// ColourMode selects how ANSI escape sequences are emitted.
type ColourMode int

const (
	// ColourAuto enables colour when stdout is a TTY and NO_COLOR is unset.
	ColourAuto ColourMode = iota

	// ColourAlways unconditionally emits ANSI.
	ColourAlways

	// ColourNever suppresses ANSI.
	ColourNever
)

// IO bundles the standard streams plus colour preference. Passed to every subcommand so
// unit tests can inject buffers and golden compare.
type IO struct {
	// Stdin is the input stream.
	Stdin io.Reader

	// Stdout is the primary output stream.
	Stdout io.Writer

	// Stderr is the error stream.
	Stderr io.Writer

	// ColourMode controls ANSI emission.
	ColourMode ColourMode
}

// ShouldColour reports whether ANSI sequences should be emitted to the given writer under
// the configured ColourMode.
//
// Returns bool which is true when ANSI escapes should be written.
func (streams IO) ShouldColour() bool {
	switch streams.ColourMode {
	case ColourAlways:
		return true
	case ColourNever:
		return false
	case ColourAuto:
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	file, ok := streams.Stdout.(*os.File)
	if !ok {
		return false
	}
	return IsTerminal(file)
}

// IsStdinTTY reports whether stdin is a terminal. Used by the REPL to fall back to
// line-buffered mode when stdin is a pipe.
//
// Returns bool which is true when stdin is a terminal.
func (streams IO) IsStdinTTY() bool {
	file, ok := streams.Stdin.(*os.File)
	if !ok {
		return false
	}
	return IsTerminal(file)
}

// Real returns an IO bundle wired to the process's actual streams, with colour
// auto-detected.
//
// Returns IO which wraps the process streams with ColourAuto selected.
func Real() IO {
	return IO{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		ColourMode: ColourAuto,
	}
}

// IsTerminal reports whether file is a terminal, including a Cygwin or MSYS pseudo
// terminal.
//
// Takes file (*os.File) which is the stream to check.
//
// Returns bool which is true when file is a terminal.
func IsTerminal(file *os.File) bool {
	return isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
}

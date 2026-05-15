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

package cli

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// isolatedTerminalWriter wraps a terminal writer to render control characters visibly.
type isolatedTerminalWriter struct {
	// destination is the underlying host terminal writer.
	destination io.Writer
}

// Write renders control characters visibly instead of operating the host terminal.
//
// Takes data ([]byte) which is one output chunk. Ordinary text, tabs and line feeds pass
// through.
//
// Returns int which is the original byte count, sent only after the transformed chunk is
// fully written.
// Returns error when the underlying writer fails.
func (writer isolatedTerminalWriter) Write(data []byte) (int, error) {
	var text strings.Builder
	for _, character := range string(data) {
		if character != '\n' && character != '\t' && (unicode.IsControl(character) || unicode.Is(unicode.Cf, character)) {
			quoted := strconv.QuoteRuneToASCII(character)
			text.WriteString(quoted[1 : len(quoted)-1])
		} else {
			_, _ = text.WriteRune(character)
		}
	}
	safe := text.String()
	count, err := io.WriteString(writer.destination, safe)
	if err != nil {
		return 0, err
	}
	if count != len(safe) {
		return 0, io.ErrShortWrite
	}
	return len(data), nil
}

// isolatedOutputWriter protects terminals without changing redirected data.
//
// Takes writer (io.Writer) which is the host-selected output writer.
//
// Returns io.Writer which is a terminal-safe wrapper only for an actual terminal file
// descriptor.
func isolatedOutputWriter(writer io.Writer) io.Writer {
	file, ok := writer.(*os.File)
	if !ok || !output.IsTerminal(file) {
		return writer
	}
	return isolatedTerminalWriter{destination: writer}
}

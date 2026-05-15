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
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode"
)

func TestIsolatedTerminalOutputEscapesControls(t *testing.T) {
	t.Parallel()
	var destination bytes.Buffer
	writer := isolatedTerminalWriter{destination: &destination}
	input := "hello 世界\n\t\x1b]52;c;clipboard\a\r\b\x00\x7f\u009b31m\u202ereversed\u2066hidden\u2069"
	count, err := writer.Write([]byte(input))
	if err != nil || count != len(input) {
		t.Fatalf("unexpected write result: %d %v", count, err)
	}
	output := destination.String()
	if !strings.HasPrefix(output, "hello 世界\n\t") || !strings.Contains(output, "\\x1b") {
		t.Fatalf("ordinary text or escape rendering changed: %q", output)
	}
	for _, character := range output {
		if character != '\n' && character != '\t' && (unicode.IsControl(character) || unicode.Is(unicode.Cf, character)) {
			t.Fatalf("terminal control survived: %q", output)
		}
	}
}

func TestIsolatedRedirectedOutputPreservesBytes(t *testing.T) {
	t.Parallel()
	var destination bytes.Buffer
	writer := isolatedOutputWriter(&destination)
	input := []byte("\x1b[31m\xff\r\n")
	count, err := writer.Write(input)
	if err != nil || count != len(input) || !bytes.Equal(input, destination.Bytes()) {
		t.Fatalf("redirected output changed: %q %v", destination.Bytes(), err)
	}
}

type isolatedFailingWriter struct {
	failure error
}

func (writer isolatedFailingWriter) Write([]byte) (int, error) {
	return 0, writer.failure
}

func TestIsolatedTerminalOutputPropagatesWriteFailures(t *testing.T) {
	t.Parallel()
	failure := errors.New("output failed")
	for _, test := range []struct {
		failure  error
		expected error
	}{
		{failure: failure, expected: failure},
		{failure: nil, expected: io.ErrShortWrite},
	} {
		writer := isolatedTerminalWriter{destination: isolatedFailingWriter{failure: test.failure}}
		count, err := writer.Write([]byte("\x1b"))
		if count != 0 || !errors.Is(err, test.expected) {
			t.Fatalf("output failure hidden: %d %v", count, err)
		}
	}
}

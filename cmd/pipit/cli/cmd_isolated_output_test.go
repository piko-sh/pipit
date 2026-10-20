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

	"github.com/stretchr/testify/require"
)

func TestIsolatedTerminalOutputEscapesControls(t *testing.T) {
	t.Parallel()
	var destination bytes.Buffer
	writer := isolatedTerminalWriter{destination: &destination}
	input := "hello 世界\n\t\x1b]52;c;clipboard\a\r\b\x00\x7f\u009b31m\u202ereversed\u2066hidden\u2069"
	count, err := writer.Write([]byte(input))
	require.NoError(t, err)
	require.Len(t, input, count)
	output := destination.String()
	require.True(t, strings.HasPrefix(output, "hello 世界\n\t"), "ordinary text and escapes render as before")
	require.Contains(t, output, "\\x1b")
	for _, character := range output {
		if character == '\n' || character == '\t' {
			continue
		}
		require.False(t, unicode.IsControl(character) || unicode.Is(unicode.Cf, character), "terminal control %q survives in %q", character, output)
	}
}

func TestIsolatedRedirectedOutputPreservesBytes(t *testing.T) {
	t.Parallel()
	var destination bytes.Buffer
	writer := isolatedOutputWriter(&destination)
	input := []byte("\x1b[31m\xff\r\n")
	count, err := writer.Write(input)
	require.NoError(t, err)
	require.Equal(t, len(input), count)
	require.Equal(t, input, destination.Bytes(), "redirected output keeps its bytes")
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
		require.Equal(t, 0, count, "an output failure is reported")
		require.ErrorIs(t, err, test.expected)
	}
}

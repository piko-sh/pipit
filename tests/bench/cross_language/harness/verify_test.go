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

//go:build crosslang

package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormaliseStdout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{name: "collapses CRLF to LF", input: []byte("hello\r\nworld\r\n"), want: "hello\nworld"},
		{name: "strips a single trailing newline", input: []byte("hello\nworld\n"), want: "hello\nworld"},
		{name: "keeps blank interior lines", input: []byte("hello\n\nworld\n"), want: "hello\n\nworld"},
		{name: "trims trailing whitespace per line", input: []byte("hello   \t\nworld\n"), want: "hello\nworld"},
		{name: "handles empty input", input: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, string(NormaliseStdout(tt.input)))
		})
	}
}

func TestSHA256HexIsDeterministic(t *testing.T) {
	t.Parallel()
	hash1 := SHA256Hex([]byte("pipit"))
	hash2 := SHA256Hex([]byte("pipit"))
	assert.Equal(t, hash1, hash2)
	assert.Len(t, hash1, 64)
}

func TestParseInnerElapsedNanos(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		stderr        []byte
		wantRemainder string
		wantNanos     int64
	}{
		{name: "extracts the value and keeps the remainder", stderr: []byte("INNER_ELAPSED_NS=12345\nsome other warning\n"), wantNanos: 12345, wantRemainder: "some other warning"},
		{name: "last marker wins", stderr: []byte("INNER_ELAPSED_NS=1\nINNER_ELAPSED_NS=999\n"), wantNanos: 999, wantRemainder: ""},
		{name: "zero when absent", stderr: []byte("just a warning\n"), wantNanos: 0, wantRemainder: "just a warning"},
		{name: "malformed values stay in the remainder", stderr: []byte("INNER_ELAPSED_NS=abc\nINNER_ELAPSED_NS=42\n"), wantNanos: 42, wantRemainder: "INNER_ELAPSED_NS=abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			nanos, remainder := ParseInnerElapsedNanos(tt.stderr)
			assert.Equal(t, tt.wantNanos, nanos)
			assert.Equal(t, tt.wantRemainder, remainder)
		})
	}
}

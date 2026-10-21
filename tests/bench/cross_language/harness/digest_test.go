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
		{name: "collapses a lone CR to LF", input: []byte("hello\rworld"), want: "hello\nworld"},
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

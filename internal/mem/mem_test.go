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

package mem_test

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/mem"
)

func TestStringMatchesBuiltinConversion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input []byte
		want  string
	}{
		{name: "nil slice", input: nil, want: ""},
		{name: "empty slice", input: []byte{}, want: ""},
		{name: "ascii", input: []byte("hello"), want: "hello"},
		{name: "embedded nul", input: []byte{'a', 0, 'b'}, want: "a\x00b"},
		{name: "multibyte", input: []byte("héllo, 世界"), want: "héllo, 世界"},
		{name: "invalid utf8 preserved", input: []byte{0xff, 0xfe}, want: "\xff\xfe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mem.String(tc.input)
			require.Equal(t, tc.want, got)
			require.Equal(t, string(tc.input), got)
		})
	}
}

func TestBytesMatchesBuiltinConversion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  []byte
	}{
		{name: "empty", input: "", want: nil},
		{name: "ascii", input: "hello", want: []byte("hello")},
		{name: "embedded nul", input: "a\x00b", want: []byte{'a', 0, 'b'}},
		{name: "multibyte", input: "héllo, 世界", want: []byte("héllo, 世界")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mem.Bytes(tc.input)
			require.Len(t, got, len(tc.want))
			if len(tc.want) > 0 {
				require.Equal(t, tc.want, got)
			}
			require.Equal(t, tc.input, string(got))
		})
	}
}

func TestRoundTrips(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "x", "hello world", "héllo, 世界", strings.Repeat("ab", 512)}
	for _, s := range inputs {
		require.Equal(t, s, mem.String(mem.Bytes(s)), "String(Bytes(%q))", s)
		b := []byte(s)
		require.Equal(t, s, string(mem.Bytes(mem.String(b))), "Bytes(String(%q))", s)
	}
}

func TestStringDoesNotAllocateOrCopyWhenLaneAliases(t *testing.T) {
	t.Parallel()
	b := []byte("aliasing check")
	s := mem.String(b)
	require.Equal(t, "aliasing check", s)

	aliased := unsafe.StringData(s) == &b[0]
	require.Equal(t, laneAliases, aliased, "String must alias exactly when the lane documents it")
}

func TestBytesSharesBackingStoreWhenLaneAliases(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("z", 16)
	b := mem.Bytes(s)
	require.Len(t, b, 16)

	aliased := unsafe.SliceData(b) == unsafe.StringData(s)
	require.Equal(t, laneAliases, aliased, "Bytes must alias exactly when the lane documents it")
}

func TestBytesResultIsIndependentWhenLaneCopies(t *testing.T) {
	t.Parallel()
	if laneAliases {
		t.Skip("the unsafe lane forbids writing through Bytes' result")
	}
	s := strings.Repeat("q", 4)
	b := mem.Bytes(s)
	b[0] = 'Q'
	require.Equal(t, "qqqq", s, "the safe lane must hand out a copy")
}

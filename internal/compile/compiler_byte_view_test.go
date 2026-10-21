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

package compile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestReadOnlyByteConversionsCompileAsStringReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a local read by index and len", source: "func f(s string) int { b := []byte(s); return len(b) + int(b[3]) }"},
		{name: "a local ranged by value", source: "func f(s string) int { b := []byte(s); n := 0; for _, c := range b { n += int(c) }; return n }"},
		{name: "a local ranged by key and value", source: "func f(s string) int { b := []byte(s); n := 0; for i, c := range b { n += i * int(c) }; return n }"},
		{name: "a local ranged with no variables", source: "func f(s string) int { b := []byte(s); n := 0; for range b { n++ }; return n }"},
		{name: "a local in a counted loop", source: "func f(s string) int { b := []byte(s); n := 0; for i := 0; i < len(b); i++ { if b[i] == ',' { n++ } }; return n }"},
		{name: "a conversion ranged in place", source: "func f(s string) int { n := 0; for _, c := range []byte(s) { n += int(c) }; return n }"},
		{name: "a conversion measured in place", source: "func f(s string) int { return len([]byte(s)) }"},
		{name: "a conversion indexed in place", source: "func f(s string) byte { return []byte(s)[1] }"},
		{name: "a literal converted", source: "func f() int { b := []byte(\"hello\"); return int(b[0]) + len(b) }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")
			require.False(t, bodyContainsTier1SubOp(compiled, isa.SubOpConvertStringToBytes),
				"a []byte that is only read needs no copy of the string")
		})
	}
}

func TestByteConversionsThatMayBeWrittenOrEscapeKeepTheCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an element written", source: "func f(s string) int { b := []byte(s); b[0] = 'x'; return int(b[0]) }"},
		{name: "an element incremented", source: "func f(s string) int { b := []byte(s); b[0]++; return int(b[0]) }"},
		{name: "an element compound-assigned", source: "func f(s string) int { b := []byte(s); b[0] += 1; return int(b[0]) }"},
		{name: "an element addressed", source: "func f(s string) int { b := []byte(s); p := &b[0]; return int(*p) }"},
		{name: "returned", source: "func f(s string) []byte { b := []byte(s); return b }"},
		{name: "returned in place", source: "func f(s string) []byte { return []byte(s) }"},
		{name: "passed to a function", source: "func g(b []byte) int { return len(b) }\nfunc f(s string) int { b := []byte(s); return g(b) }"},
		{name: "sliced", source: "func f(s string) int { b := []byte(s); c := b[1:]; return len(c) }"},
		{name: "capacity taken", source: "func f(s string) int { b := []byte(s); return cap(b) }"},
		{name: "reassigned", source: "func f(s string) int { b := []byte(s); b = append(b, 'x'); return len(b) }"},
		{name: "appended to", source: "func f(s string) int { b := []byte(s); c := append(b, 'x'); return len(c) }"},
		{name: "captured by a closure", source: "func f(s string) int { b := []byte(s); g := func() int { return len(b) }; return g() }"},
		{name: "compared with nil", source: "func f(s string) bool { b := []byte(s); return b == nil }"},
		{name: "ranged into outer variables", source: "func f(s string) int { b := []byte(s); var i int; var c byte; for i, c = range b { }; return i + int(c) }"},
		{name: "converted back", source: "func f(s string) string { b := []byte(s); return string(b) }"},
		{name: "stored in a variable", source: "func f(s string) int { b := []byte(s); c := b; return len(c) }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")
			require.True(t, bodyContainsTier1SubOp(compiled, isa.SubOpConvertStringToBytes),
				"a []byte that may be written, aliased or escape keeps its own copy")
		})
	}
}

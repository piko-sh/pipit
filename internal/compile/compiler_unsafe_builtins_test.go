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
	"pipit.sh/pipit/internal/policy"
)

const unsafePreamble = "package main\n\nimport \"unsafe\"\n\ntype pair struct {\n\tA int\n\tB int\n}\n\n"

func TestUnsafeBuiltinsLowerToTheirOwnInstructions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{
			name:   "a string built over a byte slice",
			source: "func f(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }",
			want:   isa.OpUnsafeString,
		},
		{
			name:   "a slice built over a string",
			source: "func f(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }",
			want:   isa.OpUnsafeSlice,
		},
		{
			name:   "a pointer advanced by an offset",
			source: "func f(p *pair) unsafe.Pointer { return unsafe.Add(unsafe.Pointer(p), 8) }",
			want:   isa.OpUnsafeAdd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, unsafePreamble+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.want),
				"an unsafe builtin has its own instruction rather than a call, so %s must be emitted", tt.want)
		})
	}
}

func TestUnsafeDataAccessorsLowerToTheirTierOneSubOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.SubOpcode
	}{
		{
			name:   "the data pointer of a string",
			source: "func f(s string) *byte { return unsafe.StringData(s) }",
			want:   isa.SubOpUnsafeStringData,
		},
		{
			name:   "the data pointer of a slice",
			source: "func f(b []byte) *byte { return unsafe.SliceData(b) }",
			want:   isa.SubOpUnsafeSliceData,
		},
		{
			name:   "the data pointer of a slice of a wider element",
			source: "func f(xs []int) *int { return unsafe.SliceData(xs) }",
			want:   isa.SubOpUnsafeSliceData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, unsafePreamble+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsTier1SubOp(compiled, tt.want),
				"the data pointer is read with a single word, so %v must be emitted", tt.want)
		})
	}
}

func TestUnsafeLayoutQueriesAreFoldedAtCompileTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "the size of a value", source: "func f(p pair) uintptr { return unsafe.Sizeof(p) }"},
		{name: "the alignment of a value", source: "func f(p pair) uintptr { return unsafe.Alignof(p) }"},
		{name: "the offset of a field", source: "func f(p pair) uintptr { return unsafe.Offsetof(p.B) }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, unsafePreamble+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.False(t, bodyContainsOpcode(compiled, isa.OpUnsafeAdd),
				"a layout query is a constant the type-checker already folded, not a runtime operation")
			require.NotEmpty(t, compiled.Body,
				"the folded constant still has to be loaded into the result register")
		})
	}
}

func TestUnsafeBuiltinsAreRefusedWhenTheFeatureIsOff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a string built over a byte slice", source: "func f(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }"},
		{name: "the data pointer of a slice", source: "func f(b []byte) *byte { return unsafe.SliceData(b) }"},
		{name: "a pointer advanced by an offset", source: "func f(p *pair) unsafe.Pointer { return unsafe.Add(unsafe.Pointer(p), 8) }"},
	}

	withoutUnsafe := policy.InterpFeaturesAll &^ policy.InterpFeatureUnsafeOps

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tryCompileSnippetWithFeatures(t, unsafePreamble+tt.source+"\n", withoutUnsafe, 0, 0)

			require.Error(t, err,
				"a host that switched unsafe operations off must not be handed bytecode that performs them")
		})
	}
}

func TestALayoutQueryStillCompilesWithTheUnsafeFeatureOff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "the size of a value", source: "func f(p pair) uintptr { return unsafe.Sizeof(p) }"},
		{name: "the alignment of a value", source: "func f(p pair) uintptr { return unsafe.Alignof(p) }"},
		{name: "the offset of a field", source: "func f(p pair) uintptr { return unsafe.Offsetof(p.B) }"},
	}

	withoutUnsafe := policy.InterpFeaturesAll &^ policy.InterpFeatureUnsafeOps

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tryCompileSnippetWithFeatures(t, unsafePreamble+tt.source+"\n", withoutUnsafe, 0, 0)

			require.NoError(t, err,
				"a layout query is folded to a constant before it reaches the unsafe path, so the compiled body performs no unsafe operation for the feature gate to refuse")
		})
	}
}

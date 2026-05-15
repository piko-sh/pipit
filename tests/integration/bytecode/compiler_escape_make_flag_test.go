//go:build integration

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

package bytecode_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func makeSliceHeapFlagOf(t *testing.T, source, function string) uint8 {
	t.Helper()
	cfs := compileFileSource(t, source)
	compiled, err := cfs.FindFunction(function)
	require.NoError(t, err)
	body := compiled.Body
	for pc := 0; pc+1 < len(body); pc++ {
		if body[pc].Op == isa.OpMakeSlice && body[pc+1].Op == isa.OpExt {
			return body[pc+1].C & isa.MakeSliceExtHeapFlag
		}
	}
	t.Fatalf("no generic make in %s:\n%v", function, body)
	return 0
}

func TestEscapePassClearsHeapFlagForConfinedPointerSlice(t *testing.T) {
	t.Parallel()
	const confined = `package main

func grow(output *[]byte, n int) {
	if n == 0 {
		return
	}
	*output = append(*output, byte('a'+n))
	grow(output, n-1)
}

func build() string {
	output := make([]byte, 0, 16)
	grow(&output, 5)
	return string(output)
}

func main() { println(build()) }
`
	const kept = `package main

var saved *[]byte

func keep(output *[]byte) { saved = output }

func build() string {
	output := make([]byte, 0, 16)
	keep(&output)
	return string(output)
}

func main() { println(build()) }
`
	require.Zero(t, makeSliceHeapFlagOf(t, confined, "build"), "a confined pointer keeps the backing in the arena")
	require.Equal(t, isa.MakeSliceExtHeapFlag, makeSliceHeapFlagOf(t, kept, "build"), "a kept pointer leaves the backing on the heap")
}

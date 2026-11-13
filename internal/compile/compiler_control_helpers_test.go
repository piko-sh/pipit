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

const typedSliceResultSource = `package main

type T struct{}

func (T) two() ([]int, int) { return []int{3, 1, 2}, 0 }

func two() ([]int, int) { return []int{3, 1, 2}, 0 }

func uints() ([]uint, int) { return []uint{3, 1, 2}, 0 }
`

func TestAMethodCalledForSeveralResultsReceivesThemInTheCalleesBanks(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, typedSliceResultSource+"\nfunc f(t T) []int { a, _ := t.two(); return a }\n")
	caller := findCompiledFunction(t, root, "f")
	callee := findCompiledFunction(t, root, "T.two")

	require.Len(t, caller.CallSites, 1)
	site := caller.CallSites[0]
	require.Len(t, site.Returns, len(callee.ResultKinds))
	for index, location := range site.Returns {
		require.Equal(t, callee.ResultKinds[index], location.Kind,
			"result %d is received in the bank the method returns it in", index)
	}
}

func TestACapturedLocalDefinedFromSeveralResultsStaysInTheGeneralBank(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, typedSliceResultSource+"\nfunc f() []int { a, _ := two(); g := func() { a[0] = 9 }; g(); return a }\n")
	caller := findCompiledFunction(t, root, "f")

	require.Equal(t, isa.RegisterSliceInt64, caller.CallSites[0].Returns[0].Kind)
	require.True(t, bodyContainsTier1SubOp(caller, isa.SubOpBoxSliceInt64) || bodyContainsOpcode(caller, isa.OpBoxAsType),
		"a captured local is declared in the general bank, so the typed result is boxed into it")
}

func TestAStoreOfSeveralResultsBoxesATypedSliceAsItsDeclaredType(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, typedSliceResultSource+"\nvar sink any\n\nfunc f() { var d []uint; d, _ = uints(); g := func() { sink = d }; g() }\n")
	caller := findCompiledFunction(t, root, "f")

	require.True(t, bodyContainsOpcode(caller, isa.OpBoxAsType),
		"a []uint result boxed into the general bank records []uint rather than its []uint64 storage")
}

func TestAMultiValuedCallRecordsTheFunctionItCalls(t *testing.T) {
	t.Parallel()

	const declarations = `
type box struct{ limit int }

func split(n int) (int, bool) {
	if n > 0 {
		return split(n - 1)
	}
	return n, true
}

func (b *box) split(n int) (int, bool) {
	if n > b.limit {
		return b.split(n - 1)
	}
	return n, true
}
`
	tests := []struct {
		name   string
		source string
	}{
		{name: "a function", source: "func f(n int) int { v, ok := split(n); if ok { return v }; return 0 }"},
		{name: "a method", source: "func f(b *box, n int) int { v, ok := b.split(n); if ok { return v }; return 0 }"},
		{name: "an assignment to existing variables", source: "func f(n int) int { v, ok := 0, false; v, ok = split(n); if ok { return v }; return 0 }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+declarations+"\n"+tt.source+"\n")
			caller := findCompiledFunction(t, root, "f")

			require.Len(t, caller.CallSites, 1, "the callee recurses, so its one call stays a call")
			site := caller.CallSites[0]
			require.NotNil(t, site.CachedCallee,
				"the inliner passes over a site that names no callee, so a multi-valued call must name it as a single-valued one does")
			require.Contains(t, site.CachedCallee.Name, "split")
			require.Len(t, site.ArgCopyProgram, len(site.Arguments), "the argument copy plan is laid out when the call is compiled")
		})
	}
}

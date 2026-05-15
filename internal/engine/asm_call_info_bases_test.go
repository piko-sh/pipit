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

package engine

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func TestBuildASMCallInfoBasesShape(t *testing.T) {
	t.Parallel()
	withTable := &program.CompiledFunction{}
	withoutTable := &program.CompiledFunction{}
	table := make([]AsmCallInfo, 2)
	tables := map[*program.CompiledFunction][]AsmCallInfo{withTable: table, withoutTable: nil}
	tests := []struct {
		name      string
		functions []*program.CompiledFunction
		want      []uintptr
	}{
		{name: "no functions yields no array", functions: nil, want: nil},
		{
			name:      "entries follow the function list and end with the reserved zero slot",
			functions: []*program.CompiledFunction{withoutTable, withTable},
			want:      []uintptr{0, uintptr(unsafe.Pointer(&table[0])), 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, buildASMCallInfoBases(tc.functions, tables))
		})
	}
}

func TestEnsureASMCallInfoBasesReturnsOneSharedArray(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{}
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root, callee}

	first := EnsureASMCallInfoBases(root)
	second := EnsureASMCallInfoBases(root)
	require.Len(t, first, len(root.Functions)+1)
	require.Same(t, &first[0], &second[0], "repeated calls must hand out the root's one array")
	for i, function := range root.Functions {
		require.Equal(t, function.ASMCallInfoBase, first[i])
	}
	require.Zero(t, first[len(first)-1], "the trailing slot is the reserved zero for foreign callees")
}

func TestPublishCallInfoBaseForClonesTheSharedArrayOnce(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{}
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root, callee}
	shared := EnsureASMCallInfoBases(root)
	before := append([]uintptr(nil), shared...)

	vm := newTestVM(t)
	vm.functions = root.Functions
	vm.rootFunction = root
	vm.AsmCallInfoTables = EnsureASMCallInfoTables(root)
	vm.buildCallInfoBasesByFunction(root)
	require.Same(t, &shared[0], &vm.asmCallInfoBasesByFunction[0], "a fresh VM aliases the root's array")
	require.False(t, vm.asmCallInfoBasesPrivate)

	ctx := new(dispatchContext)
	vm.liveCtx = ctx
	vm.publishCallInfoBaseFor(callee, 0x1000)
	require.Equal(t, before, shared, "the root's array is never written")
	require.True(t, vm.asmCallInfoBasesPrivate)
	require.NotSame(t, &shared[0], &vm.asmCallInfoBasesByFunction[0], "the first publish clones")
	require.Equal(t, []uintptr{shared[0], 0x1000, 0}, vm.asmCallInfoBasesByFunction)
	require.Equal(t, uintptr(unsafe.Pointer(&vm.asmCallInfoBasesByFunction[0])), ctx.callInfoBasesByFunction,
		"the live dispatch context is repointed at the clone")
	require.EqualValues(t, 1, ctx.callInfoBasesRootMatch)

	clone := vm.asmCallInfoBasesByFunction
	vm.publishCallInfoBaseFor(root, 0x2000)
	require.Same(t, &clone[0], &vm.asmCallInfoBasesByFunction[0], "a second publish writes the clone in place")
	require.Equal(t, []uintptr{0x2000, 0x1000, 0}, vm.asmCallInfoBasesByFunction)
	require.Equal(t, before, shared)

	vm.dropRuntimeCallInfoFills()
	require.False(t, vm.asmCallInfoBasesPrivate)
	require.Nil(t, vm.asmCallInfoBasesByFunction)
}

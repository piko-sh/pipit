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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func registerOnlyHandlerNames(t *testing.T) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fileSet := token.NewFileSet()
	names := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, name, nil, parser.SkipObjectResolution)
		require.NoErrorf(t, parseErr, "parsing %s", name)

		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv != nil {
				continue
			}
			if declaresRegisterOnlyHandler(function) {
				names[function.Name.Name] = true
			}
		}
	}
	return names
}

func declaresRegisterOnlyHandler(function *ast.FuncDecl) bool {
	params := function.Type.Params
	results := function.Type.Results
	if params == nil || results == nil || len(params.List) != 4 || len(results.List) != 1 {
		return false
	}
	resultName, isIdent := results.List[0].Type.(*ast.Ident)
	if !isIdent || resultName.Name != "OpResult" {
		return false
	}
	for _, position := range []int{0, 1} {
		field := params.List[position]
		if len(field.Names) != 1 || field.Names[0].Name != "_" {
			return false
		}
	}
	return len(params.List[2].Names) == 1 && len(params.List[3].Names) == 1
}

func carriesACompilerProof(handler string) bool {
	return strings.HasSuffix(handler, "Unchecked")
}

func TestGeneratedDispatchReachesEveryRegisterOnlyHandler(t *testing.T) {
	t.Parallel()

	handlerNames := registerOnlyHandlerNames(t)
	require.NotEmpty(t, handlerNames, "the package must declare register-only handlers")

	dispatched := 0
	for _, row := range isa.AllSpecs() {
		if !handlerNames[row.Handler] || carriesACompilerProof(row.Handler) {
			continue
		}

		vm, frame, registers := newStandardVM(t)
		seedRegistersForDispatch(registers)

		got := flatDispatchSwitch(vm, frame, registers, dispatchSpecInstruction(row))

		require.Equalf(t, opContinue, got,
			"%s only touches register banks, so dispatching it must continue", row.Name)
		require.NoErrorf(t, vm.evalError,
			"%s must not record a fault when given well-formed operands", row.Name)
		dispatched++
	}

	require.Greaterf(t, dispatched, 100,
		"the dispatch switch should route well over a hundred register-only handlers; reached %d", dispatched)
}

func dispatchSpecInstruction(row isa.OpSpec) isa.Instruction {
	switch row.Tier {
	case isa.TierSub1:
		return isa.NewTier1Instruction(isa.SubOpcode(row.Code), 0, 0)
	case isa.TierSub2:
		return isa.NewTier2Instruction(isa.SubOpcodeTier2(row.Code), 0)
	case isa.TierSub3:
		return isa.NewTier3Instruction(isa.SubOpcodeTier3(row.Code))
	default:
		return isa.NewInstruction(isa.Opcode(row.Code), 0, 0, 0)
	}
}

func seedRegistersForDispatch(registers *Registers) {
	registers.Ints[0] = 1
	registers.Floats[0] = 1
	registers.Strings[0] = "pipit"
	registers.Bools[0] = true
	registers.Uints[0] = 1
	registers.Complex[0] = 1 + 1i

	registers.SlicesInt[0] = []int64{1, 2, 3, 4}
	registers.slicesFloat[0] = []float64{1, 2, 3, 4}
	registers.slicesString[0] = []string{"a", "b", "c", "d"}
	registers.slicesBool[0] = []bool{true, false, true, false}
	registers.slicesUint[0] = []uint64{1, 2, 3, 4}
	registers.slicesByte[0] = []byte("abcd")
}

func vmOnlyHandlerNames(t *testing.T) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fileSet := token.NewFileSet()
	names := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, name, nil, parser.SkipObjectResolution)
		require.NoErrorf(t, parseErr, "parsing %s", name)

		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv != nil {
				continue
			}
			if declaresVMOnlyHandler(function) {
				names[function.Name.Name] = true
			}
		}
	}
	return names
}

func declaresVMOnlyHandler(function *ast.FuncDecl) bool {
	params := function.Type.Params
	results := function.Type.Results
	if params == nil || results == nil || len(params.List) != 4 || len(results.List) != 1 {
		return false
	}
	resultName, isIdent := results.List[0].Type.(*ast.Ident)
	if !isIdent || resultName.Name != "OpResult" {
		return false
	}
	first := params.List[0]
	if len(first.Names) != 1 || first.Names[0].Name != "vm" {
		return false
	}
	second := params.List[1]
	return len(second.Names) == 1 && second.Names[0].Name == "_" &&
		len(params.List[2].Names) == 1 && len(params.List[3].Names) == 1
}

func TestGeneratedDispatchRoutesEveryFrameFreeHandler(t *testing.T) {
	t.Parallel()

	handlerNames := vmOnlyHandlerNames(t)
	require.NotEmpty(t, handlerNames, "the package must declare frame-free handlers")

	dispatched := 0
	for _, row := range isa.AllSpecs() {
		if !handlerNames[row.Handler] || carriesACompilerProof(row.Handler) {
			continue
		}

		vm, frame, registers := newStandardVM(t)
		seedRegistersForDispatch(registers)
		registers.General[0] = reflect.ValueOf([]int64{1, 2, 3, 4})

		func() {
			defer func() { _ = recover() }()
			_ = flatDispatchSwitch(vm, frame, registers, dispatchSpecInstruction(row))
		}()

		require.NotErrorIsf(t, vm.evalError, errInvalidOpcode,
			"%s has a specification row, so the dispatch switch must have an arm for it", row.Name)
		if vm.evalError != nil {
			require.NotContainsf(t, vm.evalError.Error(), "invalid umbrella sub-op",
				"%s has a specification row, so its tier-1 arm must exist", row.Name)
			require.NotContainsf(t, vm.evalError.Error(), "unknown tier",
				"%s has a specification row, so its sub-tier arm must exist", row.Name)
		}
		dispatched++
	}

	require.Greaterf(t, dispatched, 40,
		"the dispatch switch should route well over forty frame-free handlers; reached %d", dispatched)
}

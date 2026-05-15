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

//go:build integration

package bytecode_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const addressOfUpvalueSource = `package main

func spawn(sink *[]*int64) {
	var n int64
	*sink = append(*sink, &n)
	f := func() { *sink = append(*sink, &n) }
	f()
}

func EntrypointRun() int { return 0 }
`

func TestAddressOfUpvalueDoesNotReadTheValue(t *testing.T) {
	t.Parallel()

	compiled, err := app.NewService().CompileFileSet(context.Background(),
		map[string]string{"main.go": addressOfUpvalueSource})
	require.NoError(t, err)

	var closures []*program.CompiledFunction
	var collect func(fn *program.CompiledFunction)
	collect = func(fn *program.CompiledFunction) {
		if len(fn.UpvalueDescriptors) > 0 {
			closures = append(closures, fn)
		}
		for _, child := range program.ExportFunctions(fn) {
			collect(child)
		}
	}
	for _, fn := range program.ExportFunctions(compiled.Root()) {
		collect(fn)
	}
	require.NotEmpty(t, closures, "the source must produce a capturing closure")

	sawAsPointer := false
	for _, closure := range closures {
		byIndex := map[uint8]map[isa.RegisterKind]int{}
		for pc, instruction := range closure.Body {
			if instruction.Op != isa.OpGetUpvalue {
				continue
			}
			kind := isa.RegisterKind(instruction.C)
			if byIndex[instruction.B] == nil {
				byIndex[instruction.B] = map[isa.RegisterKind]int{}
			}
			byIndex[instruction.B][kind] = pc
			if kind == program.UpvalueKindAsPointer {
				sawAsPointer = true
			}
		}
		for index, kinds := range byIndex {
			pointerPC, tookAddress := kinds[program.UpvalueKindAsPointer]
			if !tookAddress {
				continue
			}
			for kind, valuePC := range kinds {
				require.Equalf(t, program.UpvalueKindAsPointer, kind,
					"%s: upvalue %d is loaded as a value at pc %d and by address at pc %d.\n"+
						"Nothing consumes the value load; it is left over from compiling the operand\n"+
						"before dispatching on the & operator. A discarded load of a shared cell\n"+
						"races with writes from any other goroutine holding it.",
					closure.Name, index, valuePC, pointerPC)
			}
		}
	}
	require.True(t, sawAsPointer, "the source must take the address of a captured variable")
}

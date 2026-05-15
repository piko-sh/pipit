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
	"fmt"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

const (
	deferAllocationProgram = `package main
var acc int
func add(n int, f float64, b bool, s string) {
	acc += n
	if b {
		acc += int(f)
	}
	acc += len(s)
}
func cleanup() {}
func work(n int) { %s }
func entrypoint() int {
	acc = 0
	for i := 0; i < 1000; i++ {
		work(i)
	}
	return acc
}
`
)

func executeEntrypointAllocs(t *testing.T, source string) (float64, any) {
	t.Helper()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	var result any
	allocs := testing.AllocsPerRun(5, func() {
		result, err = service.ExecuteEntrypoint(context.Background(), compiled, "entrypoint")
		require.NoError(t, err)
	})
	return allocs, result
}

func TestDeferTrivialScalarArgsZeroAlloc(t *testing.T) {
	deferAllocs, deferResult := executeEntrypointAllocs(t, fmt.Sprintf(deferAllocationProgram, `defer add(n, 2.5, true, "ab")`))
	plainAllocs, plainResult := executeEntrypointAllocs(t, fmt.Sprintf(deferAllocationProgram, `add(n, 2.5, true, "ab")`))
	emptyAllocs, _ := executeEntrypointAllocs(t, fmt.Sprintf(deferAllocationProgram, `defer cleanup()`))
	require.Equal(t, plainResult, deferResult)
	require.EqualValues(t, 1000*999/2+1000*2+1000*2, deferResult)
	t.Logf("allocations per execution: defer=%v plain=%v empty defer=%v", deferAllocs, plainAllocs, emptyAllocs)
	require.Lessf(t, deferAllocs-emptyAllocs, 8.0, "capturing scalar and constant string arguments must add no allocation over an argument-free trivial defer")
	require.Lessf(t, deferAllocs-plainAllocs, 8.0, "a trivial defer of a compiled closure must not allocate: its frame is pushed by the return handler rather than a nested dispatch")
}

func TestDeferTrivialStringArgSeesRegistrationValue(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `var seen string
func record(v string) { seen = v }
func work(prefix string) string {
	s := prefix + "-x"
	defer record(s)
	s = s + "-changed"
	return s
}
func prefixed(p string, i int) string { return p + string(rune('a'+i%26)) + "0123456789abcdef" }
func churn() int {
	total := 0
	for i := 0; i < 2048; i++ { total += len(prefixed("p", i)) }
	return total
}
work("p")
churn()
seen`)
	require.NoError(t, err)
	require.Equal(t, "p-x", result)
}

func TestDeferTrivialArgsCrossBankPlacement(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `var first, second any
var ints []int
func takeAny(v any, w any) { first = v; second = w }
func takeInt(v int, b bool) { ints = append(ints, v); if b { ints = append(ints, 1) } }
func viaBox(n int, f float64) { defer takeAny(n, f) }
func viaUnbox(v any, flag bool) { defer takeInt(v.(int), flag) }
viaBox(41, 2.5)
viaUnbox(7, true)
boxedInt := 0
switch v := first.(type) {
case int:
	boxedInt = v
case int64:
	boxedInt = int(v)
}
boxedInt + int(second.(float64)*10) + ints[0]*1000 + ints[1]*10000`)
	require.NoError(t, err)
	require.EqualValues(t, 41+25+7000+10000, result)
}

func TestDeferTrivialManySameKindUsesReflectPath(t *testing.T) {
	t.Parallel()
	record := &engine.SimpleDeferRecord{}
	body := make([]isa.Instruction, 5)
	for i := range body {
		body[i] = isa.Instruction{C: uint8(isa.RegisterInt), B: uint8(i)}
	}
	frame := &engine.CallFrame{Function: &program.CompiledFunction{Body: body}}
	registers := &engine.Registers{Ints: []int64{1, 2, 3, 4, 5}}
	require.True(t, record.TryCaptureDirect(&engine.VM{}, frame, registers, 4))
	require.EqualValues(t, 4, record.ArgumentCount)
	require.Equal(t, 4, frame.ProgramCounter)
	record.ClearDirect()
	frame.ProgramCounter = 0
	require.False(t, record.TryCaptureDirect(&engine.VM{}, frame, registers, 5))
	require.Zero(t, frame.ProgramCounter)
}

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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type typedSubSliceKind struct {
	bank  func(*Registers) reflect.Value
	elem  reflect.Type
	name  string
	subOp isa.SubOpcode
}

var typedSubSliceKinds = []typedSubSliceKind{
	{name: "int", subOp: isa.SubOpSliceSliceInt64, elem: reflect.TypeFor[int64](), bank: func(r *Registers) reflect.Value { return reflect.ValueOf(r.SlicesInt) }},
	{name: "float", subOp: isa.SubOpSliceSliceFloat64, elem: reflect.TypeFor[float64](), bank: func(r *Registers) reflect.Value { return reflect.ValueOf(r.slicesFloat) }},
	{name: "string", subOp: isa.SubOpSliceSliceString, elem: reflect.TypeFor[string](), bank: func(r *Registers) reflect.Value { return reflect.ValueOf(r.slicesString) }},
	{name: "bool", subOp: isa.SubOpSliceSliceBool, elem: reflect.TypeFor[bool](), bank: func(r *Registers) reflect.Value { return reflect.ValueOf(r.slicesBool) }},
	{name: "uint", subOp: isa.SubOpSliceSliceUint64, elem: reflect.TypeFor[uint64](), bank: func(r *Registers) reflect.Value { return reflect.ValueOf(r.slicesUint) }},
	{name: "byte", subOp: isa.SubOpSliceSliceByte, elem: reflect.TypeFor[byte](), bank: func(r *Registers) reflect.Value { return reflect.ValueOf(r.slicesByte) }},
}

func runTypedSubSliceOnce(t *testing.T, kind typedSubSliceKind, flags uint8, low, high int64, source reflect.Value) (*Registers, int64, int64) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, uint8(kind.subOp), 1, 0)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, flags, 2, 3))
	if flags&isa.SliceMaxBitFlag != 0 {
		builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, 4, 0, 0))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, _, registers := newFramedVM(t, builder.build())
	kind.bank(registers).Index(0).Set(source)
	registers.Ints[2] = low
	registers.Ints[3] = high
	registers.Ints[4] = int64(source.Cap())

	var ctx dispatchContext
	vm.buildDispatchContext(&ctx, &asmJumpTable)
	vm.saveCurrentDispatchRegisters(&ctx)
	ctx.pollBudget = dispatchPollBudget
	dispatchLoop(&ctx)
	return registers, ctx.exitReason, ctx.exitProgramCounter
}

func TestTypedSubSliceRunsInAssembly(t *testing.T) {
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	runtime.GC()

	shapes := []struct {
		name  string
		flags uint8
		low   int64
		high  int64
	}{
		{name: "s[low:high]", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 1, high: 3},
		{name: "s[low:]", flags: isa.SliceLowBoundFlag, low: 1},
		{name: "s[:high]", flags: isa.SliceHighBoundFlag, high: 3},
		{name: "s[:]", flags: 0},
		{name: "s[len:]", flags: isa.SliceLowBoundFlag, low: 4},
		{name: "s[low:high] beyond the length", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 2, high: 6},
		{name: "s[cap:cap] keeps the data pointer", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 6, high: 6},
	}
	outOfRange := []struct {
		name  string
		flags uint8
		low   int64
		high  int64
	}{
		{name: "low above high", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 3, high: 2},
		{name: "high above capacity", flags: isa.SliceHighBoundFlag, high: 7},
		{name: "negative low", flags: isa.SliceLowBoundFlag, low: -1},
	}

	for _, kind := range typedSubSliceKinds {
		source := reflect.MakeSlice(reflect.SliceOf(kind.elem), 4, 6)
		for _, shape := range shapes {
			t.Run(kind.name+" "+shape.name, func(t *testing.T) {
				high := shape.high
				if shape.flags&isa.SliceHighBoundFlag == 0 {
					high = int64(source.Len())
				}
				want := source.Slice(int(shape.low), int(high))

				registers, exit, _ := runTypedSubSliceOnce(t, kind, shape.flags, shape.low, shape.high, source)

				require.NotEqual(t, exitPathB, exit, "the handler must not fall back to Go")
				got := kind.bank(registers).Index(1)
				require.Equal(t, want.Len(), got.Len())
				require.Equal(t, want.Cap(), got.Cap())
				require.Equal(t, want.UnsafePointer(), got.UnsafePointer(), "the data pointer must match Go's, including its slicemask")
			})
		}
		for _, shape := range outOfRange {
			t.Run(kind.name+" "+shape.name+" falls back to Go", func(t *testing.T) {
				registers, exit, exitPC := runTypedSubSliceOnce(t, kind, shape.flags, shape.low, shape.high, source)

				require.Equal(t, exitPathB, exit, "the Go handler produces the panic")
				require.Zero(t, exitPC, "the fallback must rewind to the sub-slice")
				require.Zero(t, kind.bank(registers).Index(1).Len(), "a failed sub-slice writes nothing")
			})
		}
		t.Run(kind.name+" the three-index form falls back to Go", func(t *testing.T) {
			registers, exit, exitPC := runTypedSubSliceOnce(t, kind, isa.SliceLowBoundFlag|isa.SliceHighBoundFlag|isa.SliceMaxBitFlag, 1, 2, source)

			require.Equal(t, exitPathB, exit)
			require.Zero(t, exitPC, "the fallback must rewind to the sub-slice")
			require.Zero(t, kind.bank(registers).Index(1).Len(), "the declined form writes nothing")
		})
	}
}

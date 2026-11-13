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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/safeconv"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type tinyLeafNode struct {
	Count  uint32
	Slot   int
	Signed int16
}

const (
	tinyLeafCountField = iota
	tinyLeafSlotField
	tinyLeafSignedField
)

const (
	tinyLeafReceiverRegister    = 1
	tinyLeafEnvironmentRegister = 2
	tinyLeafResultRegister      = 3
	tinyLeafUntouched           = 0xDEAD
)

type tinyLeafReceiver struct {
	build   func() reflect.Value
	name    string
	ran     bool
	raises  bool
	written bool
}

func tinyLeafCallee(shape program.TinyLeafShape, field int) *program.CompiledFunction {
	described := reflect.TypeFor[tinyLeafNode]().Field(field)
	callee := program.NewNamedFunction("leaf")
	callee.TinyLeafShape = shape
	callee.TinyLeafLayout = program.StructFieldLayout{Offset: uint32(described.Offset), PathLength: 1, Kind: uint8(described.Type.Kind())}
	callee.TinyLeafLayout.Path[0] = uint8(field)
	return callee
}

func tinyLeafSite(environmentKind, resultKind isa.RegisterKind) *program.CallSite {
	return &program.CallSite{
		Arguments: []program.VarLocation{
			{Kind: isa.RegisterGeneral, Register: tinyLeafReceiverRegister},
			{Kind: environmentKind, Register: tinyLeafEnvironmentRegister},
		},
		Returns: []program.VarLocation{{Kind: resultKind, Register: tinyLeafResultRegister}},
	}
}

func tinyLeafReceivers() []tinyLeafReceiver {
	node := tinyLeafNode{Count: 42, Slot: 2, Signed: -7}
	held := struct{ Node any }{Node: node}
	heldPointer := struct{ Node any }{Node: &node}
	return []tinyLeafReceiver{
		{name: "a pointer to the struct", build: func() reflect.Value { return reflect.ValueOf(&node) }, ran: true, written: true},
		{name: "the struct where it can be addressed", build: func() reflect.Value { return reflect.ValueOf(&node).Elem() }, ran: true, written: true},
		{name: "the struct held by value", build: func() reflect.Value { return reflect.ValueOf(node) }, ran: true, written: true},
		{
			name:  "the struct held by value in an interface field",
			build: func() reflect.Value { return reflect.ValueOf(&held).Elem().Field(0) }, ran: true, written: true,
		},
		{name: "a nil pointer", build: func() reflect.Value { return reflect.ValueOf((*tinyLeafNode)(nil)) }, ran: true, raises: true},
		{name: "a pointer held in an interface field", build: func() reflect.Value { return reflect.ValueOf(&heldPointer).Elem().Field(0) }},
		{name: "no value at all", build: func() reflect.Value { return reflect.Value{} }},
		{name: "a value that is not a struct", build: func() reflect.Value { return reflect.ValueOf(7) }},
	}
}

func TestTinyLeafFieldReadsRunInPlaceOrLeaveTheCallToBeMade(t *testing.T) {
	t.Parallel()

	leaves := []struct {
		read       func(*Registers) uint64
		name       string
		shape      program.TinyLeafShape
		field      int
		resultKind isa.RegisterKind
		want       uint64
	}{
		{
			name: "an unsigned field of ", shape: program.TinyLeafReturnUintField, field: tinyLeafCountField,
			resultKind: isa.RegisterUint64, want: 42,
			read: func(registers *Registers) uint64 { return registers.Uints[tinyLeafResultRegister] },
		},
		{
			name: "a signed field of ", shape: program.TinyLeafReturnIntField, field: tinyLeafSignedField,
			resultKind: isa.RegisterInt64, want: safeconv.Int64ToUint64Reinterpret(-7),
			read: func(registers *Registers) uint64 {
				return safeconv.Int64ToUint64Reinterpret(registers.Ints[tinyLeafResultRegister])
			},
		},
		{
			name: "a slot of the environment named by ", shape: program.TinyLeafReturnEnvUintAtIntFieldSlot, field: tinyLeafSlotField,
			resultKind: isa.RegisterUint64, want: 30,
			read: func(registers *Registers) uint64 { return registers.Uints[tinyLeafResultRegister] },
		},
	}
	for _, receiver := range tinyLeafReceivers() {
		for _, leaf := range leaves {
			t.Run(leaf.name+receiver.name, func(t *testing.T) {
				t.Parallel()
				vm, _, registers := newStandardVM(t)
				registers.General[tinyLeafReceiverRegister] = receiver.build()
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint32{10, 20, 30})
				registers.Uints[tinyLeafResultRegister] = tinyLeafUntouched
				registers.Ints[tinyLeafResultRegister] = tinyLeafUntouched

				result, ran := runTinyLeafInline(vm, registers, tinyLeafSite(isa.RegisterGeneral, leaf.resultKind), tinyLeafCallee(leaf.shape, leaf.field))

				require.Equal(t, receiver.ran, ran)
				require.Equal(t, receiver.raises, result != opContinue)
				want := uint64(tinyLeafUntouched)
				if receiver.written {
					want = leaf.want
				}
				require.Equal(t, want, leaf.read(registers), "a leaf that did not run has to leave the result register alone")
			})
		}
	}
}

func TestTinyLeafDeclinesAFieldOfAnotherKindThanItsShapeReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		shape program.TinyLeafShape
		field int
	}{
		{name: "an unsigned read of a signed field", shape: program.TinyLeafReturnUintField, field: tinyLeafSignedField},
		{name: "a signed read of an unsigned field", shape: program.TinyLeafReturnIntField, field: tinyLeafCountField},
		{name: "an environment slot taken from an unsigned field", shape: program.TinyLeafReturnEnvUintAtIntFieldSlot, field: tinyLeafCountField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			registers.General[tinyLeafReceiverRegister] = reflect.ValueOf(tinyLeafNode{Count: 2, Slot: 2, Signed: 2})
			registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint32{10, 20, 30})
			registers.Uints[tinyLeafResultRegister] = tinyLeafUntouched
			registers.Ints[tinyLeafResultRegister] = tinyLeafUntouched

			result, ran := runTinyLeafInline(vm, registers, tinyLeafSite(isa.RegisterGeneral, isa.RegisterUint64), tinyLeafCallee(tt.shape, tt.field))

			require.False(t, ran, "a receiver held by value is read through reflect, which has no such field to give")
			require.Equal(t, opContinue, result)
			require.EqualValues(t, tinyLeafUntouched, registers.Uints[tinyLeafResultRegister])
			require.EqualValues(t, tinyLeafUntouched, registers.Ints[tinyLeafResultRegister])
		})
	}
}

func TestTinyLeafEnvironmentReadTakesOnlyASliceOfUnsignedIntegers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		load   func(*Registers)
		name   string
		kind   isa.RegisterKind
		want   uint64
		ran    bool
		raises bool
	}{
		{
			name: "the typed unsigned bank", kind: isa.RegisterSliceUint64, ran: true, want: 1 << 40,
			load: func(registers *Registers) {
				registers.slicesUint[tinyLeafEnvironmentRegister] = []uint64{1, 2, 1 << 40}
			},
		},
		{
			name: "the typed integer bank", kind: isa.RegisterSliceInt64, ran: true, want: 30,
			load: func(registers *Registers) { registers.SlicesInt[tinyLeafEnvironmentRegister] = []int64{10, 20, 30} },
		},
		{
			name: "a general slice of uint32", kind: isa.RegisterGeneral, ran: true, want: 30,
			load: func(registers *Registers) {
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint32{10, 20, 30})
			},
		},
		{
			name: "a general slice of uint64 keeps every bit", kind: isa.RegisterGeneral, ran: true, want: 1<<40 + 7,
			load: func(registers *Registers) {
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint64{1, 2, 1<<40 + 7})
			},
		},
		{
			name: "a slot past the end of the typed bank", kind: isa.RegisterSliceUint64, ran: true, raises: true, want: tinyLeafUntouched,
			load: func(registers *Registers) { registers.slicesUint[tinyLeafEnvironmentRegister] = []uint64{1, 2} },
		},
		{
			name: "a slot past the end of a general slice", kind: isa.RegisterGeneral, ran: true, raises: true, want: tinyLeafUntouched,
			load: func(registers *Registers) {
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint32{10})
			},
		},
		{
			name: "a general slice of signed integers", kind: isa.RegisterGeneral, want: tinyLeafUntouched,
			load: func(registers *Registers) {
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]int32{10, 20, 30})
			},
		},
		{
			name: "a general value that is not a slice", kind: isa.RegisterGeneral, want: tinyLeafUntouched,
			load: func(registers *Registers) { registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf("abc") },
		},
		{name: "no environment at all", kind: isa.RegisterGeneral, want: tinyLeafUntouched, load: func(*Registers) {}},
		{
			name: "an environment in a bank the leaf does not read", kind: isa.RegisterSliceByte, want: tinyLeafUntouched,
			load: func(registers *Registers) {
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint32{10, 20, 30})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			node := tinyLeafNode{Slot: 2}
			registers.General[tinyLeafReceiverRegister] = reflect.ValueOf(&node)
			registers.Uints[tinyLeafResultRegister] = tinyLeafUntouched
			tt.load(registers)

			result, ran := runTinyLeafInline(vm, registers, tinyLeafSite(tt.kind, isa.RegisterUint64),
				tinyLeafCallee(program.TinyLeafReturnEnvUintAtIntFieldSlot, tinyLeafSlotField))

			require.Equal(t, tt.ran, ran)
			require.Equal(t, tt.raises, result != opContinue)
			require.Equal(t, tt.want, registers.Uints[tinyLeafResultRegister])
		})
	}
}

func TestTinyLeafEnvironmentReadRaisesForASlotBelowZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		load func(*Registers)
		name string
		kind isa.RegisterKind
	}{
		{
			name: "the typed unsigned bank", kind: isa.RegisterSliceUint64,
			load: func(registers *Registers) { registers.slicesUint[tinyLeafEnvironmentRegister] = []uint64{1, 2, 3} },
		},
		{
			name: "the typed integer bank", kind: isa.RegisterSliceInt64,
			load: func(registers *Registers) { registers.SlicesInt[tinyLeafEnvironmentRegister] = []int64{1, 2, 3} },
		},
		{
			name: "a general slice", kind: isa.RegisterGeneral,
			load: func(registers *Registers) {
				registers.General[tinyLeafEnvironmentRegister] = reflect.ValueOf([]uint32{1, 2, 3})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			node := tinyLeafNode{Slot: -1}
			registers.General[tinyLeafReceiverRegister] = reflect.ValueOf(&node)
			registers.Uints[tinyLeafResultRegister] = tinyLeafUntouched
			tt.load(registers)

			result, ran := runTinyLeafInline(vm, registers, tinyLeafSite(tt.kind, isa.RegisterUint64),
				tinyLeafCallee(program.TinyLeafReturnEnvUintAtIntFieldSlot, tinyLeafSlotField))

			require.True(t, ran, "the leaf ran and raised, as the framed call would have")
			require.NotEqual(t, opContinue, result)
			require.EqualValues(t, tinyLeafUntouched, registers.Uints[tinyLeafResultRegister])
		})
	}
}

func TestTinyLeafInlineDeclinesAShapeItDoesNotKnow(t *testing.T) {
	t.Parallel()
	vm, _, registers := newStandardVM(t)
	node := tinyLeafNode{Count: 42}
	registers.General[tinyLeafReceiverRegister] = reflect.ValueOf(&node)
	callee := tinyLeafCallee(program.TinyLeafNone, tinyLeafCountField)

	result, ran := runTinyLeafInline(vm, registers, tinyLeafSite(isa.RegisterGeneral, isa.RegisterUint64), callee)

	require.False(t, ran)
	require.Equal(t, opContinue, result)
}

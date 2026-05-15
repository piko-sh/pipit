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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestFastPathArgumentReadersPreferTheirOwnBank(t *testing.T) {
	t.Parallel()

	t.Run("a byte-slice argument", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.slicesByte[1] = []byte("typed")
		registers.General[2] = reflect.ValueOf([]byte("boxed"))

		require.Equal(t, []byte("typed"), readBytesArg(&registers, program.VarLocation{Kind: isa.RegisterSliceByte, Register: 1}))
		require.Equal(t, []byte("boxed"), readBytesArg(&registers, program.VarLocation{Kind: isa.RegisterGeneral, Register: 2}))
		require.Nil(t, readBytesArg(&registers, program.VarLocation{Kind: isa.RegisterGeneral, Register: 3}),
			"an unset general register holds no bytes")
	})

	t.Run("a bool argument", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.Bools[1] = true
		registers.General[2] = reflect.ValueOf(true)

		require.True(t, readBoolArg(&registers, program.VarLocation{Kind: isa.RegisterBool, Register: 1}))
		require.True(t, readBoolArg(&registers, program.VarLocation{Kind: isa.RegisterGeneral, Register: 2}))
	})

	t.Run("a string argument", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.Strings[1] = "typed"
		registers.General[2] = reflect.ValueOf("boxed")

		require.Equal(t, "typed", readStringArg(&registers, program.VarLocation{Kind: isa.RegisterString, Register: 1}))
		require.Equal(t, "boxed", readStringArg(&registers, program.VarLocation{Kind: isa.RegisterGeneral, Register: 2}))
	})

	t.Run("an int argument", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.Ints[1] = 4
		registers.General[2] = reflect.ValueOf(5)

		require.Equal(t, int64(4), readIntArg(&registers, program.VarLocation{Kind: isa.RegisterInt, Register: 1}))
		require.Equal(t, int64(5), readIntArg(&registers, program.VarLocation{Kind: isa.RegisterGeneral, Register: 2}))
	})

	t.Run("a float argument", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.Floats[1] = 1.5
		registers.General[2] = reflect.ValueOf(2.5)

		require.InDelta(t, 1.5, readFloatArg(&registers, program.VarLocation{Kind: isa.RegisterFloat, Register: 1}), 0)
		require.InDelta(t, 2.5, readFloatArg(&registers, program.VarLocation{Kind: isa.RegisterGeneral, Register: 2}), 0)
	})
}

func TestReadAnyArgBoxesEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		load func(*Registers)
		want any
	}{
		{name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 4 }, want: 4},
		{name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.5 }, want: 1.5},
		{name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" }, want: "s"},
		{name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true }, want: true},
		{name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 6 }, want: uint64(6)},
		{name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 2i }, want: 2i},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load: func(r *Registers) { r.SlicesInt[1] = []int64{1} }, want: []int64{1},
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat,
			load: func(r *Registers) { r.slicesFloat[1] = []float64{1.5} }, want: []float64{1.5},
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString,
			load: func(r *Registers) { r.slicesString[1] = []string{"s"} }, want: []string{"s"},
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool,
			load: func(r *Registers) { r.slicesBool[1] = []bool{true} }, want: []bool{true},
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint,
			load: func(r *Registers) { r.slicesUint[1] = []uint64{2} }, want: []uint64{2},
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte,
			load: func(r *Registers) { r.slicesByte[1] = []byte{3} }, want: []byte{3},
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load: func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") }, want: "boxed",
		},
		{name: "an unset general register", kind: isa.RegisterGeneral, load: func(*Registers) {}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)

			require.Equal(t, tt.want, readAnyArg(&registers, program.VarLocation{Kind: tt.kind, Register: 1}))
		})
	}
}

func TestArgumentIsPipitSynthesisedPeelsOneInterfaceLayer(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")
	boxed := reflect.ValueOf(map[string]any{"k": reflect.New(script).Elem().Interface()}).MapIndex(reflect.ValueOf("k"))
	nilBoxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

	tests := []struct {
		name  string
		value reflect.Value
		want  bool
	}{
		{name: "a synthesised struct", value: reflect.New(script).Elem(), want: true},
		{name: "a pointer to one", value: reflect.New(script), want: true},
		{name: "an interface holding one", value: boxed, want: true},
		{name: "an ordinary value", value: reflect.ValueOf(3), want: false},
		{name: "an invalid value", value: reflect.Value{}, want: false},
		{name: "a nil interface", value: nilBoxed, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, argumentIsPipitSynthesised(tt.value))
		})
	}
}

func TestTypeNameHasInterfaceAdapterConsultsTheMethodTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		key    string
		lookup string
		want   bool
	}{
		{name: "a registered Error method", key: "Fault.Error", lookup: "Fault", want: true},
		{name: "a registered String method", key: "Score.String", lookup: "Score", want: true},
		{name: "a registered Format method", key: "Score.Format", lookup: "Score", want: true},
		{name: "a registered MarshalJSON method", key: "Score.MarshalJSON", lookup: "Score", want: true},
		{name: "a method that bridges nothing", key: "Score.Other", lookup: "Score", want: false},
		{name: "a type the table does not hold", key: "Score.String", lookup: "Absent", want: false},
		{name: "an empty type name", key: "Score.String", lookup: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := adapterHostVM(t, tt.key, stringReturningMethod(t, "m", "x"))

			require.Equal(t, tt.want, typeNameHasInterfaceAdapter(vm, tt.lookup))
		})
	}

	t.Run("no VM has no method table", func(t *testing.T) {
		t.Parallel()

		require.False(t, typeNameHasInterfaceAdapter(nil, "Score"))
	})
}

func TestTypeNameHasUsableInterfaceAdapterAppliesTheMethodSetRule(t *testing.T) {
	t.Parallel()

	t.Run("a value method is usable from a value", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "x"))

		require.True(t, typeNameHasUsableInterfaceAdapter(vm, "Score", false))
		require.True(t, typeNameHasUsableInterfaceAdapter(vm, "Score", true))
	})

	t.Run("a pointer method is usable only from a pointer", func(t *testing.T) {
		t.Parallel()

		method := stringReturningMethod(t, "String", "x")
		method.IsPointerReceiver = true
		vm := adapterHostVM(t, "Score.String", method)

		require.False(t, typeNameHasUsableInterfaceAdapter(vm, "Score", false),
			"a pointer-receiver method is not in a value's method set")
		require.True(t, typeNameHasUsableInterfaceAdapter(vm, "Score", true))
	})

	t.Run("the Format method is not one of the usable three", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Score.Format", stringReturningMethod(t, "Format", "x"))

		require.False(t, typeNameHasUsableInterfaceAdapter(vm, "Score", true))
	})

	t.Run("no VM and no name resolve to nothing", func(t *testing.T) {
		t.Parallel()

		require.False(t, typeNameHasUsableInterfaceAdapter(nil, "Score", true))
		require.False(t, typeNameHasUsableInterfaceAdapter(newTestVM(t), "", true))
	})
}

func TestArgStaticTypeNameRequiresAdapterReadsTheSite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		site  *program.CallSite
		index int
		want  bool
	}{
		{
			name: "a recorded name with an adapter", site: &program.CallSite{ArgumentStaticTypeNames: []string{"Score"}},
			want: true,
		},
		{
			name: "a recorded name with no adapter", site: &program.CallSite{ArgumentStaticTypeNames: []string{"Absent"}},
		},
		{name: "an empty recorded name", site: &program.CallSite{ArgumentStaticTypeNames: []string{""}}},
		{name: "an index past the recorded names", site: &program.CallSite{}, index: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "x"))

			require.Equal(t, tt.want, argStaticTypeNameRequiresAdapter(vm, tt.site, tt.index))
		})
	}
}

func TestArgDynamicValueRequiresAdapterInspectsTheLiveRegister(t *testing.T) {
	t.Parallel()

	t.Run("a non-general argument never needs one", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "at.String", stringReturningMethod(t, "String", "x"))
		registers := standardRegisters()

		require.False(t, argDynamicValueRequiresAdapter(vm, program.VarLocation{Kind: isa.RegisterInt}, &registers))
	})

	t.Run("an unset register never needs one", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "at.String", stringReturningMethod(t, "String", "x"))
		registers := standardRegisters()

		require.False(t, argDynamicValueRequiresAdapter(vm, program.VarLocation{Kind: isa.RegisterGeneral}, &registers))
	})

	t.Run("an ordinary value never needs one", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "at.String", stringReturningMethod(t, "String", "x"))
		registers := standardRegisters()
		registers.General[0] = reflect.ValueOf(3)

		require.False(t, argDynamicValueRequiresAdapter(vm, program.VarLocation{Kind: isa.RegisterGeneral}, &registers))
	})

	t.Run("a synthesised value with a registered method needs one", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "at.String", stringReturningMethod(t, "String", "x"))
		registers := standardRegisters()
		registers.General[0] = reflect.ValueOf(identityTrailing{})

		require.True(t, argDynamicValueRequiresAdapter(vm, program.VarLocation{Kind: isa.RegisterGeneral}, &registers))
	})

	t.Run("a synthesised value with no registered method does not", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Other.String", stringReturningMethod(t, "String", "x"))
		registers := standardRegisters()
		registers.General[0] = reflect.ValueOf(identityTrailing{})

		require.False(t, argDynamicValueRequiresAdapter(vm, program.VarLocation{Kind: isa.RegisterGeneral}, &registers))
	})
}

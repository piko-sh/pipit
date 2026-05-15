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

type methodCarrier struct{ Count int }

func (m methodCarrier) Value() int { return m.Count }

func (m *methodCarrier) Bump() { m.Count++ }

type embedCarrier struct {
	methodCarrier
	Name string
}

type pointerEmbedCarrier struct {
	*methodCarrier
	Name string
}

type plainEmbedded struct{ X int }

type embedWithoutMethods struct {
	plainEmbedded
	Name string
}

func TestLookupCachedMethodServesTheSecondCall(t *testing.T) {
	t.Parallel()

	t.Run("a value method resolves and caches", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		receiver := reflect.ValueOf(methodCarrier{Count: 4})

		first := lookupCachedMethod(vm, receiver, "Value")
		require.True(t, first.IsValid())
		require.Len(t, vm.methodCache, 1, "the first lookup populates the cache")

		second := lookupCachedMethod(vm, receiver, "Value")
		require.True(t, second.IsValid())
		require.Equal(t, 4, second.Call(nil)[0].Interface())
	})

	t.Run("a pointer method resolves on the pointer type", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		carrier := &methodCarrier{}

		bound := lookupCachedMethod(vm, reflect.ValueOf(carrier), "Bump")
		require.True(t, bound.IsValid())
		bound.Call(nil)
		require.Equal(t, 1, carrier.Count)
	})

	t.Run("a method the type does not declare resolves to nothing", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		require.False(t, lookupCachedMethod(vm, reflect.ValueOf(methodCarrier{}), "Absent").IsValid())
	})

	t.Run("a value receiver cannot reach a pointer method", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		require.False(t, lookupCachedMethod(vm, reflect.ValueOf(methodCarrier{}), "Bump").IsValid(),
			"a pointer-receiver method is not in the value's method set")
	})
}

func TestAddressableMethodReceiverPromotesOnlyWhenItMust(t *testing.T) {
	t.Parallel()

	t.Run("a type that already has methods is left alone", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		receiver := reflect.ValueOf(methodCarrier{})

		require.Equal(t, reflect.Struct, addressableMethodReceiver(receiver, &registers, 1).Kind())
	})

	t.Run("a method-less struct is promoted to a pointer", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		receiver := reflect.ValueOf(plainEmbedded{X: 3})

		promoted := addressableMethodReceiver(receiver, &registers, 1)
		require.Equal(t, reflect.Pointer, promoted.Kind())
		require.Equal(t, plainEmbedded{X: 3}, promoted.Elem().Interface())
		require.Equal(t, plainEmbedded{X: 3}, registers.General[1].Interface(),
			"the register is rehomed so later writes reach the same storage")
	})

	t.Run("an addressable struct takes its own address", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		cell := reflect.New(reflect.TypeFor[plainEmbedded]()).Elem()

		promoted := addressableMethodReceiver(cell, &registers, 1)
		require.Equal(t, cell.Addr().Pointer(), promoted.Pointer())
	})

	t.Run("a non-struct is left alone", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()

		require.Equal(t, reflect.Int, addressableMethodReceiver(reflect.ValueOf(3), &registers, 1).Kind())
	})
}

func TestResolveNativeMethodOnEmbedsWalksTheEmbeddedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		receiver reflect.Value
		method   string
		want     bool
	}{
		{name: "a value embed carries its method", receiver: reflect.ValueOf(embedCarrier{}), method: "Value", want: true},
		{
			name: "a pointer receiver reaches the embed too", receiver: reflect.ValueOf(&embedCarrier{}),
			method: "Value", want: true,
		},
		{
			name: "a pointer embed is followed", receiver: reflect.ValueOf(pointerEmbedCarrier{methodCarrier: &methodCarrier{}}),
			method: "Bump", want: true,
		},
		{name: "a method no embed carries", receiver: reflect.ValueOf(embedCarrier{}), method: "Absent"},
		{name: "an embed with no methods", receiver: reflect.ValueOf(embedWithoutMethods{}), method: "Value"},
		{name: "a nil pointer receiver", receiver: reflect.ValueOf((*embedCarrier)(nil)), method: "Value"},
		{name: "a receiver that is not a struct", receiver: reflect.ValueOf(3), method: "Value"},
		{name: "a struct with no embedded field", receiver: reflect.ValueOf(methodCarrier{}), method: "Absent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, resolveNativeMethodOnEmbeds(tt.receiver, tt.method).IsValid())
		})
	}
}

func TestResolveMethodOnEmbeddedFieldTriesEveryShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		field  reflect.Value
		method string
		want   bool
	}{
		{name: "a value field with a value method", field: reflect.ValueOf(methodCarrier{}), method: "Value", want: true},
		{
			name:  "an addressable field reaches the pointer method",
			field: reflect.New(reflect.TypeFor[methodCarrier]()).Elem(), method: "Bump", want: true,
		},
		{name: "a non-nil pointer field", field: reflect.ValueOf(&methodCarrier{}), method: "Value", want: true},
		{
			name:  "a nil pointer field still exposes the promoted method",
			field: reflect.ValueOf((*methodCarrier)(nil)), method: "Value", want: true,
		},
		{name: "a field with no such method", field: reflect.ValueOf(plainEmbedded{}), method: "Value"},
		{name: "a nil pointer field with no such method", field: reflect.ValueOf((*methodCarrier)(nil)), method: "Absent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, resolveMethodOnEmbeddedField(tt.field, tt.method).IsValid())
		})
	}
}

func TestEnsureAddressableStructReceiverCopiesOnlyWhatItMust(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		receiver reflect.Value
		wantCopy bool
	}{
		{name: "a non-addressable struct is copied", receiver: reflect.ValueOf(methodCarrier{Count: 1}), wantCopy: true},
		{name: "an addressable struct is left alone", receiver: reflect.New(reflect.TypeFor[methodCarrier]()).Elem()},
		{name: "a non-struct is left alone", receiver: reflect.ValueOf(3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ensureAddressableStructReceiver(tt.receiver)
			require.Equal(t, tt.receiver.Interface(), got.Interface())
			if tt.wantCopy {
				require.True(t, got.CanAddr(), "a pointer-receiver method inside the callee needs an address to take")
			}
		})
	}
}

func TestArgumentExtractorsUnwrapOnlyInterfaces(t *testing.T) {
	t.Parallel()

	boxed := reflect.ValueOf(map[string]any{"k": 4}).MapIndex(reflect.ValueOf("k"))

	require.Equal(t, reflect.Interface, identityArg(boxed).Kind(), "the identity extractor keeps the wrapper")
	require.Equal(t, reflect.Int, elemArg(boxed).Kind(), "the method-expression extractor unwraps it")
	require.Equal(t, reflect.Int, elemArg(reflect.ValueOf(4)).Kind())
}

func TestSetMethodArgsPlacesArgumentsByKind(t *testing.T) {
	t.Parallel()

	t.Run("banked parameters fill their own banks in order", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		callee := program.NewNamedFunction("m")
		callee.ParameterKinds = []isa.RegisterKind{
			isa.RegisterGeneral, isa.RegisterInt, isa.RegisterString, isa.RegisterInt,
		}

		setMethodArgs(&registers, callee, []reflect.Value{
			reflect.ValueOf(int64(1)), reflect.ValueOf("s"), reflect.ValueOf(int64(2)),
		}, identityArg)

		require.Equal(t, int64(1), registers.Ints[0])
		require.Equal(t, int64(2), registers.Ints[1])
		require.Equal(t, "s", registers.Strings[0])
	})

	t.Run("a scattered layout places each argument at its own register", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		callee := program.NewNamedFunction("m")
		callee.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}
		callee.ParameterRegisters = []uint8{0, 5}

		setMethodArgs(&registers, callee, []reflect.Value{reflect.ValueOf(int64(9))}, identityArg)

		require.Equal(t, int64(9), registers.Ints[5])
	})

	t.Run("the extractor is applied to every argument", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		callee := program.NewNamedFunction("m")
		callee.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}
		boxed := reflect.ValueOf(map[string]any{"k": int64(3)}).MapIndex(reflect.ValueOf("k"))

		setMethodArgs(&registers, callee, []reflect.Value{boxed}, elemArg)

		require.Equal(t, int64(3), registers.Ints[0])
	})
}

func TestBindMethodRefusesBrokenOperands(t *testing.T) {
	t.Parallel()

	t.Run("an unset receiver raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))

		got := handleBindMethod(vm, frame, registers, op(0, 1, 0))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a function index past the table is a bounds fault", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(9, 0, 0))
		registers.General[1] = reflect.ValueOf(methodCarrier{})

		require.Equal(t, opPanicError, handleBindMethod(vm, frame, registers, op(0, 1, 0)))
		require.Error(t, vm.evalError)
	})

	t.Run("a callee with no recorded signature is refused", func(t *testing.T) {
		t.Parallel()

		callee := program.NewNamedFunction("m")
		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(8)
		builder.functions = []*program.CompiledFunction{callee}
		builder.Emit(isa.OpDrillTier1, 0, 0, 0)
		builder.body = append(builder.body, op(0, 0, 0))
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

		vm, frame, registers := newFramedVM(t, builder.build())
		frame.ProgramCounter = 1
		registers.General[1] = reflect.ValueOf(methodCarrier{})

		require.Equal(t, opPanicError, handleBindMethod(vm, frame, registers, op(0, 1, 0)))
		require.ErrorContains(t, vm.evalError, "no type info")
	})
}

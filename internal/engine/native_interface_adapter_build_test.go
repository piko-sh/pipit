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
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func stringReturningMethod(t *testing.T, name, result string) *program.CompiledFunction {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	constant := builder.addStringConst(result)
	builder.returnString()
	builder.parameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
	builder.Emit(isa.OpLoadStringConst, 0, constant, 0)
	builder.body = append(builder.body, tier2Return(1))

	method := builder.build()
	method.Name = name
	method.HasReceiver = true
	method.ParameterRegisters = []uint8{0}
	return method
}

func adapterHostVM(t *testing.T, key string, method *program.CompiledFunction) *VM {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	builder.functions = []*program.CompiledFunction{method}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	root := builder.build()
	root.SetMethodTable(map[string]uint16{key: 0})

	vm := newTestVM(t)
	vm.prepareForExecution(root)
	t.Cleanup(vm.ReleaseArena)
	return vm
}

func TestStringerAdapterDispatchesTheCompiledMethod(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "ninety"))

	adapter := buildStringerAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Score")
	require.True(t, adapter.IsValid())

	stringer, ok := reflect.TypeAssert[fmt.Stringer](adapter)
	require.True(t, ok, "the adapter is built so native code sees a real Stringer")
	require.Equal(t, "ninety", stringer.String())
}

func TestErrorAdapterDispatchesTheCompiledMethod(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "Fault.Error", stringReturningMethod(t, "Error", "boom"))

	adapter := buildErrorAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Fault")
	require.True(t, adapter.IsValid())

	failure, ok := reflect.TypeAssert[error](adapter)
	require.True(t, ok)
	require.Equal(t, "boom", failure.Error())
}

func TestAdapterBuildersDeclineWithoutARegisteredMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		builder func(*VM, reflect.Value, string) reflect.Value
	}{
		{name: "the stringer adapter", builder: buildStringerAdapterIfRegistered},
		{name: "the error adapter", builder: buildErrorAdapterIfRegistered},
		{name: "the formatter adapter", builder: buildFormatterAdapterIfRegistered},
		{name: "the writer adapter", builder: buildWriterAdapterIfRegistered},
		{name: "the reader adapter", builder: buildReaderAdapterIfRegistered},
		{name: "the scanner adapter", builder: buildScannerAdapterIfRegistered},
		{name: "the marshaler adapter", builder: buildMarshalerAdapterIfRegistered},
		{name: "the unmarshaler adapter", builder: buildUnmarshalerAdapterIfRegistered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := adapterHostVM(t, "Other.Absent", stringReturningMethod(t, "Absent", ""))

			require.False(t, tt.builder(vm, reflect.ValueOf(identityTrailing{}), "Score").IsValid(),
				"an unregistered method name must not produce an adapter")
		})
	}
}

func TestAdapterBuildersRefuseAMethodOfTheWrongShape(t *testing.T) {
	t.Parallel()

	method := stringReturningMethod(t, "String", "ninety")
	method.ResultKinds = []isa.RegisterKind{isa.RegisterString, isa.RegisterString}
	vm := adapterHostVM(t, "Score.String", method)

	require.False(t, buildStringerAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Score").IsValid(),
		"a two-result String is not the method fmt.Stringer declares")
}

func TestAdapterBuildersRefuseAPointerMethodOnAValue(t *testing.T) {
	t.Parallel()

	method := stringReturningMethod(t, "String", "ninety")
	method.IsPointerReceiver = true
	vm := adapterHostVM(t, "Score.String", method)

	require.False(t, buildStringerAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Score").IsValid(),
		"a pointer-receiver method is not in a value's method set")
	require.True(t, buildStringerAdapterIfRegistered(vm, reflect.ValueOf(&identityTrailing{}), "Score").IsValid())
}

func TestLookupAdapterMethodPrefersTheLocalMethodTable(t *testing.T) {
	t.Parallel()

	t.Run("a local method resolves with no root override", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "x"))

		root, index, ok := lookupAdapterMethod(vm, "Score.String")
		require.True(t, ok)
		require.Nil(t, root, "an in-package method lives in the VM's own root")
		require.Zero(t, index)
	})

	t.Run("an unknown key resolves to nothing", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "x"))

		_, _, ok := lookupAdapterMethod(vm, "Score.Absent")
		require.False(t, ok)
	})

	t.Run("no VM resolves to nothing", func(t *testing.T) {
		t.Parallel()

		_, _, ok := lookupAdapterMethod(nil, "Score.String")
		require.False(t, ok)
	})
}

func TestResolveAdapterCalleeGuardsItsIndices(t *testing.T) {
	t.Parallel()

	t.Run("an in-range index resolves against the VM's root", func(t *testing.T) {
		t.Parallel()

		method := stringReturningMethod(t, "String", "x")
		vm := adapterHostVM(t, "Score.String", method)

		callee, root, ok := resolveAdapterCallee(vm, nil, 0)
		require.True(t, ok)
		require.Same(t, method, callee)
		require.Same(t, vm.rootFunction, root)
	})

	t.Run("an index past the table resolves to nothing", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "x"))

		_, _, ok := resolveAdapterCallee(vm, nil, 9)
		require.False(t, ok)
	})

	t.Run("no VM resolves to nothing", func(t *testing.T) {
		t.Parallel()

		_, _, ok := resolveAdapterCallee(nil, nil, 0)
		require.False(t, ok)
	})

	t.Run("a VM with no root resolves to nothing", func(t *testing.T) {
		t.Parallel()

		_, _, ok := resolveAdapterCallee(newTestVM(t), nil, 0)
		require.False(t, ok)
	})
}

func TestInvokeStringReturnMethodIsEmptyWithoutACallee(t *testing.T) {
	t.Parallel()

	require.Empty(t, invokeStringReturnMethod(nil, nil, 0, reflect.ValueOf(identityTrailing{})))
	require.Empty(t, invokeStringReturnMethod(newTestVM(t), nil, 9, reflect.ValueOf(identityTrailing{})))
}

func TestPipitTypeNameReadsTheSentinelOrTheRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value reflect.Value
		want  string
		found bool
	}{
		{name: "a synthesised struct", value: reflect.ValueOf(identityTrailing{}), want: "at", found: true},
		{name: "a pointer to one", value: reflect.ValueOf(&identityTrailing{}), want: "at", found: true},
		{name: "an ordinary type", value: reflect.ValueOf(3)},
		{name: "an invalid value", value: reflect.Value{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := adapterHostVM(t, "Score.String", stringReturningMethod(t, "String", "x"))

			name, ok := pipitTypeName(vm, tt.value)
			require.Equal(t, tt.found, ok)
			require.Equal(t, tt.want, name)
		})
	}

	t.Run("a VM with no root function names nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := pipitTypeName(newTestVM(t), reflect.ValueOf(identityTrailing{}))
		require.False(t, ok)
	})
}

func TestReceiverValueForFollowsTheDeclaredReceiverKind(t *testing.T) {
	t.Parallel()

	value := identityTrailing{}

	t.Run("a value receiver takes the value itself", func(t *testing.T) {
		t.Parallel()

		callee := stringReturningMethod(t, "String", "x")

		require.Equal(t, reflect.Struct, receiverValueFor(callee, reflect.ValueOf(value)).Kind())
	})

	t.Run("a pointer receiver takes an address", func(t *testing.T) {
		t.Parallel()

		callee := stringReturningMethod(t, "String", "x")
		callee.IsPointerReceiver = true

		require.Equal(t, reflect.Pointer, receiverValueFor(callee, reflect.ValueOf(value)).Kind(),
			"an unaddressable value is copied into a fresh cell so the callee can take its address")
	})

	t.Run("a pointer receiver leaves a pointer alone", func(t *testing.T) {
		t.Parallel()

		callee := stringReturningMethod(t, "String", "x")
		callee.IsPointerReceiver = true
		pointer := reflect.ValueOf(&value)

		require.Equal(t, pointer.Pointer(), receiverValueFor(callee, pointer).Pointer())
	})
}

func countingMethod(t *testing.T, count int64) *program.CompiledFunction {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	constant := builder.addIntConst(count)
	builder.resultKinds = []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral}
	builder.parameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral}
	builder.Emit(isa.OpLoadIntConst, 0, constant, 0)
	builder.body = append(builder.body, isa.NewTier2Instruction(isa.SubOpTier2LoadNil, 0))
	builder.body = append(builder.body, tier2Return(2))

	method := builder.build()
	method.Name = "Write"
	method.HasReceiver = true
	method.ParameterRegisters = []uint8{0, 1}
	return method
}

func TestWriterAdapterReportsWhatTheMethodAccepted(t *testing.T) {
	t.Parallel()

	t.Run("a full write reports no error", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Sink.Write", countingMethod(t, 3))
		adapter := buildWriterAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Sink")
		require.True(t, adapter.IsValid())

		writer, ok := reflect.TypeAssert[io.Writer](adapter)
		require.True(t, ok)

		count, err := writer.Write([]byte("abc"))
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("a short write is reported as such", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Sink.Write", countingMethod(t, 1))
		adapter := buildWriterAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Sink")
		writer, ok := reflect.TypeAssert[io.Writer](adapter)
		require.True(t, ok)

		count, err := writer.Write([]byte("abc"))
		require.ErrorIs(t, err, io.ErrShortWrite, "Go's io.Writer contract requires an error for a short write")
		require.Equal(t, 1, count)
	})

	t.Run("a count past the buffer is clamped", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Sink.Write", countingMethod(t, 99))
		adapter := buildWriterAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Sink")
		writer, ok := reflect.TypeAssert[io.Writer](adapter)
		require.True(t, ok)

		count, err := writer.Write([]byte("abc"))
		require.NoError(t, err)
		require.Equal(t, 3, count, "a method cannot claim to have taken more than it was given")
	})

	t.Run("a negative count is clamped to zero", func(t *testing.T) {
		t.Parallel()

		vm := adapterHostVM(t, "Sink.Write", countingMethod(t, -5))
		adapter := buildWriterAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Sink")
		writer, ok := reflect.TypeAssert[io.Writer](adapter)
		require.True(t, ok)

		count, err := writer.Write([]byte("abc"))
		require.ErrorIs(t, err, io.ErrShortWrite)
		require.Zero(t, count)
	})
}

func TestReaderAdapterStopsWhenTheMethodNeverAdvances(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "Source.Read", countingMethod(t, 0))
	adapter := buildReaderAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Source")
	require.True(t, adapter.IsValid())

	reader, ok := reflect.TypeAssert[io.Reader](adapter)
	require.True(t, ok)

	buffer := make([]byte, 4)
	for range pipitReaderMaxNoProgressCalls {
		count, err := reader.Read(buffer)
		require.ErrorIs(t, err, io.ErrNoProgress, "a read of nothing with no error breaks io.Reader's contract")
		require.Zero(t, count)
	}

	_, err := reader.Read(buffer)
	require.ErrorIs(t, err, io.EOF, "a Read that never advances must not spin forever")
}

func TestReaderAdapterReportsWhatTheMethodFilled(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "Source.Read", countingMethod(t, 2))
	adapter := buildReaderAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Source")
	reader, ok := reflect.TypeAssert[io.Reader](adapter)
	require.True(t, ok)

	count, err := reader.Read(make([]byte, 4))
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestAdapterMethodsReportAnUnavailableCallee(t *testing.T) {
	t.Parallel()

	t.Run("the stringer adapter yields the empty string", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		adapter := &pipitStringerAdapter{vm: vm, methodIndex: 9}
		require.Empty(t, adapter.String())
		require.False(t, adapter.adapterUnderlying().IsValid())
	})

	t.Run("the error adapter yields the empty string", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		adapter := &pipitErrorAdapter{vm: vm, methodIndex: 9}
		require.Empty(t, adapter.Error())
		require.False(t, adapter.adapterUnderlying().IsValid())
	})

	t.Run("the writer adapter reports the method is gone", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		count, err := (&pipitWriterAdapter{vm: vm, methodIndex: 9}).Write([]byte("abc"))
		require.Error(t, err)
		require.Zero(t, count)
	})

	t.Run("the reader adapter reports end of file", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		count, err := (&pipitReaderAdapter{vm: vm, methodIndex: 9}).Read(make([]byte, 4))
		require.ErrorIs(t, err, io.EOF)
		require.Zero(t, count)
	})

	t.Run("the marshaler adapter yields nothing", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		raw, err := (&pipitMarshalerAdapter{vm: vm, methodIndex: 9}).MarshalJSON()
		require.NoError(t, err)
		require.Nil(t, raw)
	})

	t.Run("the unmarshaler adapter yields no error", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		require.NoError(t, (&pipitUnmarshalerAdapter{vm: vm, methodIndex: 9}).UnmarshalJSON([]byte("{}")))
	})

	t.Run("the scanner adapter reports the method is gone", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		require.Error(t, (&pipitScannerAdapter{vm: vm, methodIndex: 9}).Scan(nil, 'v'))
	})

	t.Run("the formatter adapter writes nothing", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		adapter := &pipitFormatterAdapter{vm: vm, methodIndex: 9}
		require.NotPanics(t, func() { adapter.Format(nil, 'v') })
		require.False(t, adapter.adapterUnderlying().IsValid())
	})

	t.Run("the sort adapter reports an empty, unordered collection", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)

		adapter := &pipitSortInterfaceAdapter{vm: vm, lenIndex: 9, lessIndex: 9, swapIndex: 9}
		require.Zero(t, adapter.Len())
		require.False(t, adapter.Less(0, 1))
		require.NotPanics(t, func() { adapter.Swap(0, 1) })
	})
}

func TestSortInterfaceAdapterNeedsAllThreeMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		keys []string
	}{
		{name: "only Len", keys: []string{"Items.Len"}},
		{name: "Len and Less but no Swap", keys: []string{"Items.Len", "Items.Less"}},
		{name: "no sort methods at all", keys: []string{"Items.Other"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := newBytecodeBuilder()
			builder.numRegisters = wideRegCounts(4)
			builder.functions = []*program.CompiledFunction{stringReturningMethod(t, "m", "x")}
			builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
			root := builder.build()
			table := map[string]uint16{}
			for _, key := range tt.keys {
				table[key] = 0
			}
			root.SetMethodTable(table)

			vm := newTestVM(t)
			vm.prepareForExecution(root)
			t.Cleanup(vm.ReleaseArena)

			require.False(t, buildSortInterfaceAdapterIfRegistered(vm, reflect.ValueOf(identityTrailing{}), "Items").IsValid(),
				"sort.Interface is all three methods or none")
		})
	}
}

func TestUnwrapPipitNamedTypeReachesTheConcreteType(t *testing.T) {
	t.Parallel()

	named := pipitNamedType{Type: reflect.TypeFor[int64](), sourceName: "Score", sourcePackage: "main"}

	t.Run("a wrapped named type unwraps to its concrete type", func(t *testing.T) {
		t.Parallel()

		unwrapped := unwrapPipitNamedType(reflect.ValueOf(named))
		require.Equal(t, reflect.TypeFor[int64](), unwrapped.Interface())
	})

	t.Run("a slice of wrapped types unwraps element by element", func(t *testing.T) {
		t.Parallel()

		slice := []reflect.Type{named, reflect.TypeFor[string]()}

		unwrapped := unwrapPipitNamedType(reflect.ValueOf(slice))
		require.Len(t, unwrapped.Interface(), 2)
		require.Equal(t, reflect.TypeFor[int64](), unwrapped.Index(0).Interface())
		require.Equal(t, reflect.TypeFor[string](), unwrapped.Index(1).Interface())
	})

	t.Run("an invalid value is handed back", func(t *testing.T) {
		t.Parallel()

		require.False(t, unwrapPipitNamedType(reflect.Value{}).IsValid())
	})

	t.Run("an ordinary value is handed back", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, 3, unwrapPipitNamedType(reflect.ValueOf(3)).Interface())
	})
}

func TestExtractResultHelpersReadTheirSlot(t *testing.T) {
	t.Parallel()

	failure := errors.New("failed")

	t.Run("bytes come from the first result", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, []byte("raw"), extractBytesResult([]reflect.Value{reflect.ValueOf([]byte("raw"))}))
		require.Nil(t, extractBytesResult(nil))
		require.Nil(t, extractBytesResult([]reflect.Value{reflect.Value{}}))
		require.Nil(t, extractBytesResult([]reflect.Value{reflect.ValueOf(3)}))
	})

	t.Run("the error comes from the second result", func(t *testing.T) {
		t.Parallel()

		results := []reflect.Value{reflect.ValueOf([]byte("raw")), reflect.ValueOf(failure)}
		require.ErrorIs(t, extractErrorResult(results), failure)
		require.NoError(t, extractErrorResult([]reflect.Value{reflect.ValueOf(3)}))
		require.NoError(t, extractErrorResult([]reflect.Value{reflect.ValueOf(3), reflect.Value{}}))
		require.NoError(t, extractErrorResult([]reflect.Value{reflect.ValueOf(3), reflect.ValueOf(4)}))
	})
}

func TestTryWrapNamedInterfacePointerNeedsTheRegistryEntry(t *testing.T) {
	t.Parallel()

	pointerToInterface := reflect.TypeFor[*any]()

	t.Run("no VM wraps nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := tryWrapNamedInterfacePointer(nil, &program.CallSite{}, "Shape", "main", pointerToInterface)
		require.False(t, ok)
	})

	t.Run("a type that is not a pointer to an interface wraps nothing", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		_, ok := tryWrapNamedInterfacePointer(vm, &program.CallSite{}, "Shape", "main", reflect.TypeFor[*int]())
		require.False(t, ok)
	})

	t.Run("no package qualifier wraps nothing", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		_, ok := tryWrapNamedInterfacePointer(vm, &program.CallSite{}, "Shape", "", pointerToInterface)
		require.False(t, ok)
	})

	t.Run("a name the registry does not hold wraps nothing", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		_, ok := tryWrapNamedInterfacePointer(vm, &program.CallSite{}, "Absent", "main", pointerToInterface)
		require.False(t, ok)
	})

	t.Run("a registered name wraps the pointer", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Globals.RegisterUserNamedInterface("main.Shape", typemodel.NewNamedInterfaceType("main", "Shape", nil))

		wrapper, ok := tryWrapNamedInterfacePointer(vm, &program.CallSite{}, "Shape", "main", pointerToInterface)
		require.True(t, ok)
		require.Equal(t, pointerToInterface, wrapper.Type)
	})

	t.Run("a static type naming something else wraps nothing", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Globals.RegisterUserNamedInterface("main.Shape", typemodel.NewNamedInterfaceType("main", "Shape", nil))
		site := &program.CallSite{ArgumentStaticTypeStrings: []string{"*main.Other"}}

		_, ok := tryWrapNamedInterfacePointer(vm, site, "Shape", "main", pointerToInterface)
		require.False(t, ok)
	})
}

func TestMakeFuncImplConvertingShimKeepsTheImplSignature(t *testing.T) {
	t.Parallel()

	inner := reflect.ValueOf(func(in []reflect.Value) []reflect.Value { return in })

	shim := makeFuncImplConvertingShim(reflect.TypeFor[func() int32](), inner)
	require.Equal(t, inner.Type(), shim.Type(), "the shim stands in for the impl, so it keeps its own type")

	require.NotPanics(t, func() { shim.Call([]reflect.Value{reflect.ValueOf([]reflect.Value(nil))}) })
}

func TestConvertMakeFuncResultsLeavesWhatItCannotFit(t *testing.T) {
	t.Parallel()

	functionType := reflect.TypeFor[func() (int32, string)]()

	results := []reflect.Value{reflect.ValueOf(int64(5)), reflect.ValueOf("s"), reflect.ValueOf(true)}
	convertMakeFuncResults(functionType, results)

	require.Equal(t, int32(5), results[0].Interface())
	require.Equal(t, "s", results[1].Interface())
	require.Equal(t, true, results[2].Interface(), "a result past the declared outputs is left alone")
}

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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type nativeSlot struct {
	kind     isa.RegisterKind
	register uint8
}

func nativeLocation(slot nativeSlot) program.VarLocation {
	return program.VarLocation{Kind: slot.kind, Register: slot.register}
}

func nativeCallFixture(t *testing.T, function any, arguments, returns []nativeSlot) (*VM, *CallFrame, *Registers) {
	t.Helper()

	site := program.CallSite{IsNative: true, NativeRegister: 1}
	for _, slot := range arguments {
		site.Arguments = append(site.Arguments, nativeLocation(slot))
	}
	for _, slot := range returns {
		site.Returns = append(site.Returns, nativeLocation(slot))
	}

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.AddCallSite(&site)
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	registers.General[1] = reflect.ValueOf(function)
	return vm, frame, registers
}

func TestNativeCallDispatchesToTheGoFunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		function  any
		seed      func(registers *Registers)
		check     func(t *testing.T, registers *Registers)
		name      string
		arguments []nativeSlot
		returns   []nativeSlot
	}{
		{
			name:      "one integer argument and one integer result",
			function:  func(n int) int { return n * 2 },
			arguments: []nativeSlot{{kind: isa.RegisterInt64, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterInt64, register: 0}},
			seed:      func(r *Registers) { r.Ints[2] = 21 },
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.Equal(t, int64(42), r.Ints[0])
			},
		},
		{
			name:     "no arguments and a string result",
			function: func() string { return "pipit" },
			returns:  []nativeSlot{{kind: isa.RegisterString, register: 0}},
			seed:     func(*Registers) {},
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.Equal(t, "pipit", r.Strings[0])
			},
		},
		{
			name:     "several integer arguments",
			function: func(a, b, c int) int { return a + b + c },
			arguments: []nativeSlot{
				{kind: isa.RegisterInt64, register: 2},
				{kind: isa.RegisterInt64, register: 3},
				{kind: isa.RegisterInt64, register: 4},
			},
			returns: []nativeSlot{{kind: isa.RegisterInt64, register: 0}},
			seed:    func(r *Registers) { r.Ints[2], r.Ints[3], r.Ints[4] = 1, 2, 3 },
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.Equal(t, int64(6), r.Ints[0])
			},
		},
		{
			name:     "several results land in their own banks",
			function: func() (int, string) { return 1, "a" },
			returns: []nativeSlot{
				{kind: isa.RegisterInt64, register: 0},
				{kind: isa.RegisterString, register: 5},
			},
			seed: func(*Registers) {},
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.Equal(t, int64(1), r.Ints[0])
				require.Equal(t, "a", r.Strings[5])
			},
		},
		{
			name:      "a string argument and a string result",
			function:  func(s string) string { return s + "!" },
			arguments: []nativeSlot{{kind: isa.RegisterString, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterString, register: 0}},
			seed:      func(r *Registers) { r.Strings[2] = "pipit" },
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.Equal(t, "pipit!", r.Strings[0])
			},
		},
		{
			name:      "a slice argument through the general bank",
			function:  func(xs []int) int { return len(xs) },
			arguments: []nativeSlot{{kind: isa.RegisterGeneral, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterInt64, register: 0}},
			seed:      func(r *Registers) { r.General[2] = reflect.ValueOf([]int{1, 2, 3}) },
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.Equal(t, int64(3), r.Ints[0])
			},
		},
		{
			name:      "a float argument and a float result",
			function:  func(f float64) float64 { return f * 2 },
			arguments: []nativeSlot{{kind: isa.RegisterFloat64, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterFloat64, register: 0}},
			seed:      func(r *Registers) { r.Floats[2] = 1.5 },
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.InDelta(t, 3.0, r.Floats[0], 0)
			},
		},
		{
			name:      "a boolean result",
			function:  func(n int) bool { return n > 0 },
			arguments: []nativeSlot{{kind: isa.RegisterInt64, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterBool, register: 0}},
			seed:      func(r *Registers) { r.Ints[2] = 1 },
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.True(t, r.Bools[0])
			},
		},
		{
			name:     "an error result lands in the general bank",
			function: func() error { return errors.New("boom") },
			returns:  []nativeSlot{{kind: isa.RegisterGeneral, register: 0}},
			seed:     func(*Registers) {},
			check: func(t *testing.T, r *Registers) {
				t.Helper()
				require.True(t, r.General[0].IsValid())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := nativeCallFixture(t, tt.function, tt.arguments, tt.returns)
			tt.seed(registers)

			require.Equal(t, opContinue, handleCallNative(vm, frame, registers, op(0, 0, 0)))
			tt.check(t, registers)
		})
	}
}

func TestNativeCallRunsAFunctionWithNoResults(t *testing.T) {
	t.Parallel()

	called := 0
	vm, frame, registers := nativeCallFixture(t,
		func(n int) { called = n },
		[]nativeSlot{{kind: isa.RegisterInt64, register: 2}},
		nil,
	)
	registers.Ints[2] = 7

	require.Equal(t, opContinue, handleCallNative(vm, frame, registers, op(0, 0, 0)))
	require.Equal(t, 7, called, "the native function must actually run")
}

func TestNativeCallSurfacesAPanicFromTheGoFunction(t *testing.T) {
	t.Parallel()

	vm, frame, registers := nativeCallFixture(t, func() { panic("boom") }, nil, nil)

	got := handleCallNative(vm, frame, registers, op(0, 0, 0))

	require.NotEqual(t, opContinue, got,
		"a panic inside a native function must not unwind into the host")
	require.Error(t, vm.evalError)
}

func TestNativeCallRefusesASiteIndexPastTheTable(t *testing.T) {
	t.Parallel()

	vm, frame, registers := nativeCallFixture(t, func() {}, nil, nil)

	low, high := isa.SplitWide(99)
	got := handleCallNative(vm, frame, registers, op(0, low, high))

	require.Equal(t, opPanicError, got)
	require.Error(t, vm.evalError)
}

func TestNativeCallOnAnEmptyFunctionRegisterRaises(t *testing.T) {
	t.Parallel()

	vm, frame, registers := nativeCallFixture(t, func() {}, nil, nil)
	registers.General[1] = reflect.Value{}

	defer func() {
		require.NotNil(t, recover(),
			"calling through a register that holds no function must raise rather than continue")
	}()

	handleCallNative(vm, frame, registers, op(0, 0, 0))
}

func TestCacheParamTypesRecordsTheCalleeSignature(t *testing.T) {
	t.Parallel()

	argumentSlots := func(n int) []program.VarLocation {
		slots := make([]program.VarLocation, 0, n)
		for index := range n {
			slots = append(slots, program.VarLocation{Kind: isa.RegisterGeneral, Register: uint8(index)})
		}
		return slots
	}

	tests := []struct {
		function     any
		name         string
		siteArgs     int
		wantIn       int
		wantVariadic bool
	}{
		{name: "a fixed-arity function", function: func(a int, b string) {}, siteArgs: 2, wantIn: 2},
		{name: "a one-argument function", function: func(a int) {}, siteArgs: 1, wantIn: 1},
		{name: "a variadic function", function: func(a int, rest ...string) {}, siteArgs: 2, wantIn: 2, wantVariadic: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			site := &program.CallSite{Arguments: argumentSlots(tt.siteArgs)}

			cacheParamTypes(site, reflect.ValueOf(tt.function))

			cache := site.NativeParamCacheLoad()
			require.NotNil(t, cache, "the signature must be published for later calls")
			require.Len(t, site.NativeParamTypes(), tt.wantIn)
			require.Equal(t, tt.wantVariadic, cache.IsVariadic)
		})
	}

	t.Run("a site that passes no arguments caches nothing", func(t *testing.T) {
		t.Parallel()
		site := &program.CallSite{}

		cacheParamTypes(site, reflect.ValueOf(func() {}))

		require.Nil(t, site.NativeParamCacheLoad(),
			"a call with no arguments has no parameter shaping to remember")
	})

	t.Run("the cache is published once and reused", func(t *testing.T) {
		t.Parallel()
		site := &program.CallSite{Arguments: argumentSlots(1)}
		function := reflect.ValueOf(func(a int) {})

		cacheParamTypes(site, function)
		first := site.NativeParamCacheLoad()
		cacheParamTypes(site, function)

		require.Same(t, first, site.NativeParamCacheLoad(),
			"a second call must reuse the published cache rather than rebuild it")
	})
}

type uncachedMethodProbe struct{ base int }

func (p uncachedMethodProbe) Combine(a, b, c, d int, s string) int {
	return p.base + a + b + c + d + len(s)
}

func uncachedMethodValueFixture(t *testing.T, callee reflect.Value) (*VM, *CallFrame, *Registers) {
	t.Helper()
	arguments := []nativeSlot{
		{kind: isa.RegisterInt64, register: 2}, {kind: isa.RegisterInt64, register: 3},
		{kind: isa.RegisterInt64, register: 4}, {kind: isa.RegisterInt64, register: 5},
		{kind: isa.RegisterString, register: 0},
	}
	vm, frame, registers := nativeCallFixture(t, func() {}, arguments, []nativeSlot{{kind: isa.RegisterInt64, register: 0}})
	registers.General[1] = callee
	registers.Ints[2], registers.Ints[3], registers.Ints[4], registers.Ints[5] = 1, 2, 3, 4
	registers.Strings[0] = "xyz"
	require.Equal(t, opContinue, handleCallNative(vm, frame, registers, op(0, 0, 0)))
	require.Equal(t, int64(21), registers.Ints[0])
	require.Equal(t, nativeFastPathNoneEntry, loadNativeFastPath(&frame.Function.CallSites[0]),
		"the site must be classified as having no fast path, so later calls take callNativeUncached")
	return vm, frame, registers
}

func TestAnUncachedMethodValueCallDoesNotConvertTheMethodValueToAnInterface(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	probe := uncachedMethodProbe{base: 8}
	reflectMethod := reflect.ValueOf(probe).MethodByName("Combine")
	goMethodValue := reflect.ValueOf(probe.Combine)

	measure := func(callee reflect.Value) float64 {
		t.Helper()
		vm, frame, registers := uncachedMethodValueFixture(t, callee)
		return testing.AllocsPerRun(200, func() {
			if handleCallNative(vm, frame, registers, op(0, 0, 0)) != opContinue {
				t.Fatal("the call must continue")
			}
		})
	}
	viaReflect, viaGo := measure(reflectMethod), measure(goMethodValue)

	require.LessOrEqual(t, viaReflect, viaGo+1,
		"an uncached call of a reflect method value allocates %v objects against %v for the same Go method value", viaReflect, viaGo)
}

type unexportedMethodHolder struct{ hidden func() int }

func TestAnUncachedCallOfAFunctionReadFromAnUnexportedFieldStillPanics(t *testing.T) {
	holder := unexportedMethodHolder{hidden: func() int { return 1 }}
	hidden := reflect.ValueOf(holder).Field(0)
	require.False(t, hidden.CanInterface())
	for _, classify := range []bool{true, false} {
		vm, frame, registers := nativeCallFixture(t, func() {}, nil, []nativeSlot{{kind: isa.RegisterInt64, register: 0}})
		registers.General[1] = hidden
		site := &frame.Function.CallSites[0]
		require.PanicsWithValue(t, "reflect.Value.Interface: cannot return value obtained from unexported field or method",
			func() { callNativeUncached(vm, frame, registers, site, 0, classify) },
			"classify=%v: the call must refuse the value as reflect.Value.Interface does", classify)
	}
}

type methodValueCallProbe struct {
	base int
}

func (p methodValueCallProbe) Add(n int) int { return p.base + n }

func (p *methodValueCallProbe) Scale(n int) int {
	p.base *= n
	return p.base
}

func (p methodValueCallProbe) Sum(values ...int) int {
	total := p.base
	for _, value := range values {
		total += value
	}
	return total
}

func TestAMethodValueIsCalledAsTheMethodFunctionWithItsReceiver(t *testing.T) {
	t.Parallel()

	probe := &methodValueCallProbe{base: 3}
	add := reflect.ValueOf(*probe).MethodByName("Add")
	require.Equal(t, 10, callNativeFunction(add, []reflect.Value{reflect.ValueOf(7)}, false)[0].Interface())
	scale := reflect.ValueOf(probe).MethodByName("Scale")
	require.Equal(t, 6, callNativeFunction(scale, []reflect.Value{reflect.ValueOf(2)}, false)[0].Interface())
	require.Equal(t, 6, probe.base, "a pointer receiver is the same pointer")
	sum := reflect.ValueOf(*probe).MethodByName("Sum")
	require.Equal(t, 9, callNativeFunction(sum, []reflect.Value{reflect.ValueOf([]int{1, 2})}, true)[0].Interface())
	require.Equal(t, 9, callNativeFunction(sum, []reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)}, false)[0].Interface())

	var boxed fmt.Stringer = time.Duration(5)
	viaInterface := reflect.ValueOf(&boxed).Elem().MethodByName("String")
	require.Equal(t, "5ns", callNativeFunction(viaInterface, nil, false)[0].Interface(), "an interface receiver keeps the method value")
	plain := reflect.ValueOf(strings.ToUpper)
	require.Equal(t, "A", callNativeFunction(plain, []reflect.Value{reflect.ValueOf("a")}, false)[0].Interface())
}

func TestAMethodValueOnANilReceiverPanicsAsTheMethodValueDoes(t *testing.T) {
	t.Parallel()

	var missing *methodValueCallProbe
	viaValue := reflect.ValueOf(missing).MethodByName("Add")
	want := recoveredPanic(func() { viaValue.Call([]reflect.Value{reflect.ValueOf(1)}) })
	got := recoveredPanic(func() { callNativeFunction(viaValue, []reflect.Value{reflect.ValueOf(1)}, false) })
	require.NotNil(t, want)
	require.Equal(t, fmt.Sprint(want), fmt.Sprint(got), "a value method called through a nil pointer panics the same way")

	scale := reflect.ValueOf(missing).MethodByName("Scale")
	want = recoveredPanic(func() { scale.Call([]reflect.Value{reflect.ValueOf(2)}) })
	got = recoveredPanic(func() { callNativeFunction(scale, []reflect.Value{reflect.ValueOf(2)}, false) })
	require.NotNil(t, want)
	require.Equal(t, fmt.Sprint(want), fmt.Sprint(got), "a pointer method dereferencing a nil receiver panics the same way")
}

func TestAMethodValueCallAllocatesLessThanTheMethodValue(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	if _, _, split := concreteMethodValue(reflect.ValueOf(methodValueCallProbe{}).MethodByName("Add")); !split {
		t.Skip("this build calls method values as they are")
	}
	add := reflect.ValueOf(methodValueCallProbe{base: 1}).MethodByName("Add")
	arguments := []reflect.Value{reflect.ValueOf(2)}
	callNativeFunction(add, arguments, false)
	direct := testing.AllocsPerRun(100, func() { add.Call(arguments) })
	split := testing.AllocsPerRun(100, func() { callNativeFunction(add, arguments, false) })
	t.Logf("method value %v, method function %v objects per call", direct, split)
	require.Less(t, split, direct)
}

func recoveredPanic(call func()) (recovered any) {
	defer func() { recovered = recover() }()
	call()
	return nil
}

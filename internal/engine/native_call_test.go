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
	"reflect"
	"testing"

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
			arguments: []nativeSlot{{kind: isa.RegisterInt, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterInt, register: 0}},
			seed:      func(r *Registers) { r.Ints[2] = 21 },
			check:     func(t *testing.T, r *Registers) { require.Equal(t, int64(42), r.Ints[0]) },
		},
		{
			name:     "no arguments and a string result",
			function: func() string { return "pipit" },
			returns:  []nativeSlot{{kind: isa.RegisterString, register: 0}},
			seed:     func(*Registers) {},
			check:    func(t *testing.T, r *Registers) { require.Equal(t, "pipit", r.Strings[0]) },
		},
		{
			name:     "several integer arguments",
			function: func(a, b, c int) int { return a + b + c },
			arguments: []nativeSlot{
				{kind: isa.RegisterInt, register: 2},
				{kind: isa.RegisterInt, register: 3},
				{kind: isa.RegisterInt, register: 4},
			},
			returns: []nativeSlot{{kind: isa.RegisterInt, register: 0}},
			seed:    func(r *Registers) { r.Ints[2], r.Ints[3], r.Ints[4] = 1, 2, 3 },
			check:   func(t *testing.T, r *Registers) { require.Equal(t, int64(6), r.Ints[0]) },
		},
		{
			name:     "several results land in their own banks",
			function: func() (int, string) { return 1, "a" },
			returns: []nativeSlot{
				{kind: isa.RegisterInt, register: 0},
				{kind: isa.RegisterString, register: 5},
			},
			seed: func(*Registers) {},
			check: func(t *testing.T, r *Registers) {
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
			check:     func(t *testing.T, r *Registers) { require.Equal(t, "pipit!", r.Strings[0]) },
		},
		{
			name:      "a slice argument through the general bank",
			function:  func(xs []int) int { return len(xs) },
			arguments: []nativeSlot{{kind: isa.RegisterGeneral, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterInt, register: 0}},
			seed:      func(r *Registers) { r.General[2] = reflect.ValueOf([]int{1, 2, 3}) },
			check:     func(t *testing.T, r *Registers) { require.Equal(t, int64(3), r.Ints[0]) },
		},
		{
			name:      "a float argument and a float result",
			function:  func(f float64) float64 { return f * 2 },
			arguments: []nativeSlot{{kind: isa.RegisterFloat, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterFloat, register: 0}},
			seed:      func(r *Registers) { r.Floats[2] = 1.5 },
			check:     func(t *testing.T, r *Registers) { require.InDelta(t, 3.0, r.Floats[0], 0) },
		},
		{
			name:      "a boolean result",
			function:  func(n int) bool { return n > 0 },
			arguments: []nativeSlot{{kind: isa.RegisterInt, register: 2}},
			returns:   []nativeSlot{{kind: isa.RegisterBool, register: 0}},
			seed:      func(r *Registers) { r.Ints[2] = 1 },
			check:     func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name:     "an error result lands in the general bank",
			function: func() error { return errors.New("boom") },
			returns:  []nativeSlot{{kind: isa.RegisterGeneral, register: 0}},
			seed:     func(*Registers) {},
			check:    func(t *testing.T, r *Registers) { require.True(t, r.General[0].IsValid()) },
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
		[]nativeSlot{{kind: isa.RegisterInt, register: 2}},
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

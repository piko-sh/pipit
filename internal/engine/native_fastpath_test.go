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
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func makeRegisters() Registers {
	return Registers{
		Ints:    make([]int64, 8),
		Floats:  make([]float64, 8),
		Strings: make([]string, 8),
		General: make([]reflect.Value, 8),
		Bools:   make([]bool, 8),
		Uints:   make([]uint64, 8),
		Complex: make([]complex128, 8),
	}
}

func TestNativeFastPath_StringToString(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "hello"

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 1, Kind: isa.RegisterString}},
	}
	reflectedFunction := reflect.ValueOf(strings.ToUpper)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "HELLO", regs.Strings[1])
}

func TestNativeFastPath_StringStringToBool(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "hello world"
	regs.Strings[1] = "world"

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterString},
			{Register: 1, Kind: isa.RegisterString},
		},
		Returns: []program.VarLocation{{Register: 0, Kind: isa.RegisterBool}},
	}
	reflectedFunction := reflect.ValueOf(strings.Contains)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.True(t, regs.Bools[0])
}

func TestNativeFastPath_IntToString(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Ints[0] = 42

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
	}
	reflectedFunction := reflect.ValueOf(strconv.Itoa)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "42", regs.Strings[0])
}

func TestNativeFastPath_FormatInt(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Ints[0] = 255
	regs.Ints[1] = 16

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterInt64},
			{Register: 1, Kind: isa.RegisterInt64},
		},
		Returns: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
	}
	reflectedFunction := reflect.ValueOf(strconv.FormatInt)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "ff", regs.Strings[0])
}

func TestNativeFastPath_Variadic_Sprintf(t *testing.T) {
	t.Parallel()

	t.Run("no varargs", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()
		regs.Strings[0] = "hello"

		site := &program.CallSite{
			Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
			Returns:   []program.VarLocation{{Register: 1, Kind: isa.RegisterString}},
		}
		reflectedFunction := reflect.ValueOf(fmt.Sprintf)

		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.Equal(t, "hello", regs.Strings[1])
	})

	t.Run("one string argument", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()
		regs.Strings[0] = "hello %s"
		regs.Strings[1] = "world"

		site := &program.CallSite{
			Arguments: []program.VarLocation{
				{Register: 0, Kind: isa.RegisterString},
				{Register: 1, Kind: isa.RegisterString},
			},
			Returns: []program.VarLocation{{Register: 2, Kind: isa.RegisterString}},
		}
		reflectedFunction := reflect.ValueOf(fmt.Sprintf)

		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.Equal(t, "hello world", regs.Strings[2])
	})

	t.Run("one int argument", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()
		regs.Strings[0] = "value: %d"
		regs.Ints[0] = 42

		site := &program.CallSite{
			Arguments: []program.VarLocation{
				{Register: 0, Kind: isa.RegisterString},
				{Register: 0, Kind: isa.RegisterInt64},
			},
			Returns: []program.VarLocation{{Register: 1, Kind: isa.RegisterString}},
		}
		reflectedFunction := reflect.ValueOf(fmt.Sprintf)

		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.Equal(t, "value: 42", regs.Strings[1])
	})

	t.Run("mixed arguments", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()
		regs.Strings[0] = "%s=%d (%.1f)"
		regs.Strings[1] = "x"
		regs.Ints[0] = 10
		regs.Floats[0] = 3.14

		site := &program.CallSite{
			Arguments: []program.VarLocation{
				{Register: 0, Kind: isa.RegisterString},
				{Register: 1, Kind: isa.RegisterString},
				{Register: 0, Kind: isa.RegisterInt64},
				{Register: 0, Kind: isa.RegisterFloat64},
			},
			Returns: []program.VarLocation{{Register: 2, Kind: isa.RegisterString}},
		}
		reflectedFunction := reflect.ValueOf(fmt.Sprintf)

		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.Equal(t, "x=10 (3.1)", regs.Strings[2])
	})

	t.Run("buffer reuse", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()
		site := &program.CallSite{
			Arguments: []program.VarLocation{
				{Register: 0, Kind: isa.RegisterString},
				{Register: 0, Kind: isa.RegisterInt64},
			},
			Returns: []program.VarLocation{{Register: 1, Kind: isa.RegisterString}},
		}
		reflectedFunction := reflect.ValueOf(fmt.Sprintf)

		regs.Strings[0] = "v=%d"
		regs.Ints[0] = 1
		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.Equal(t, "v=1", regs.Strings[1])

		regs.Strings[0] = "v=%d"
		regs.Ints[0] = 2
		ok, _, _ = tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.Equal(t, "v=2", regs.Strings[1])
	})
}

func TestNativeFastPath_FormattingShapeServesOnlyFmt(t *testing.T) {
	t.Parallel()

	regs := makeRegisters()
	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterString},
			{Register: 0, Kind: isa.RegisterInt64},
		},
		Returns: []program.VarLocation{{Register: 1, Kind: isa.RegisterString}},
	}
	keep := func(format string, _ ...any) string { return format }

	regs.Strings[0] = "v=%d"
	regs.Ints[0] = 1
	ok, tag, _ := tryNativeFastPath(nil, site, keep, &regs)
	require.False(t, ok, "a non-fmt function of fmt's shape must take the ordinary path, which passes values as themselves")
	require.Equal(t, fastPathTagNone, tag)
	require.Equal(t, fastPathTagSprintfString, classifyNativeFastPath(fmt.Sprintf))
	require.Equal(t, fastPathTagNone, classifyNativeFastPath(keep))
}

func TestNativeFastPath_Variadic_Errorf(t *testing.T) {
	t.Parallel()

	regs := makeRegisters()
	regs.Strings[0] = "bad: %s"
	regs.Strings[1] = "reason"

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterString},
			{Register: 1, Kind: isa.RegisterString},
		},
		Returns: []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}},
	}
	reflectedFunction := reflect.ValueOf(fmt.Errorf)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.True(t, regs.General[0].IsValid())
	err, ok := reflect.TypeAssert[error](regs.General[0])
	require.True(t, ok)
	require.Equal(t, "bad: reason", err.Error())
}

func TestNativeFastPath_Variadic_Sprint(t *testing.T) {
	t.Parallel()

	regs := makeRegisters()
	regs.Strings[0] = "hello"
	regs.Strings[1] = " "
	regs.Strings[2] = "world"

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterString},
			{Register: 1, Kind: isa.RegisterString},
			{Register: 2, Kind: isa.RegisterString},
		},
		Returns: []program.VarLocation{{Register: 3, Kind: isa.RegisterString}},
	}
	reflectedFunction := reflect.ValueOf(fmt.Sprint)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "hello world", regs.Strings[3])
}

func TestNativeFastPath_ZeroArgString(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()

	called := false
	reflectedFunction := reflect.ValueOf(func() string { called = true; return "result" })

	site := &program.CallSite{
		Arguments: nil,
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.True(t, called)
	require.Equal(t, "result", regs.Strings[0])
}

func TestNativeFastPath_ZeroArgBool(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()

	reflectedFunction := reflect.ValueOf(func() bool { return true })

	site := &program.CallSite{
		Arguments: nil,
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterBool}},
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.True(t, regs.Bools[0])
}

func TestNativeFastPath_ZeroArgInt(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()

	reflectedFunction := reflect.ValueOf(func() int { return 99 })

	site := &program.CallSite{
		Arguments: nil,
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, int64(99), regs.Ints[0])
}

func TestNativeFastPath_VoidString(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "hello"

	var received string
	reflectedFunction := reflect.ValueOf(func(s string) { received = s })

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   nil,
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "hello", received)
}

func TestNativeFastPath_VoidTwoStrings(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "key"
	regs.Strings[1] = "value"

	var a, b string
	reflectedFunction := reflect.ValueOf(func(k, v string) { a = k; b = v })

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterString},
			{Register: 1, Kind: isa.RegisterString},
		},
		Returns: nil,
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "key", a)
	require.Equal(t, "value", b)
}

func TestNativeFastPath_StringToError(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "test input"

	reflectedFunction := reflect.ValueOf(func(s string) error {
		return fmt.Errorf("err: %s", s)
	})

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}},
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.True(t, regs.General[0].IsValid())
	err, ok := reflect.TypeAssert[error](regs.General[0])
	require.True(t, ok)
	require.Equal(t, "err: test input", err.Error())
}

func TestNativeFastPath_StringToError_Nil(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "ok"

	reflectedFunction := reflect.ValueOf(func(s string) error { return nil })

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}},
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.False(t, regs.General[0].IsValid())
}

func TestNativeFastPath_AnyToBool(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "hello"

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterBool}},
	}
	reflectedFunction := reflect.ValueOf(func(v any) bool { return v != nil && v != "" })

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.True(t, regs.Bools[0])
}

func TestNativeFastPath_AnyToString(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Ints[0] = 42

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
	}
	reflectedFunction := reflect.ValueOf(func(v any) string { return fmt.Sprint(v) })

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "42", regs.Strings[0])
}

func TestNativeFastPath_AnyToInt(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Strings[0] = "ignored"

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
	}
	reflectedFunction := reflect.ValueOf(func(v any) int { return 99 })

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, int64(99), regs.Ints[0])
}

func TestNativeFastPath_AnyToInt64(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Floats[0] = 3.14

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterFloat64}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
	}
	reflectedFunction := reflect.ValueOf(func(v any) int64 { return 123 })

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, int64(123), regs.Ints[0])
}

func TestNativeFastPath_AnyToFloat64(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Ints[0] = 10

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterFloat64}},
	}
	reflectedFunction := reflect.ValueOf(func(v any) float64 { return 2.5 })

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.InDelta(t, 2.5, regs.Floats[0], 0)
}

func TestNativeFastPath_AnyAnyToAny(t *testing.T) {
	t.Parallel()

	t.Run("returns first truthy", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()
		regs.Strings[0] = ""
		regs.Strings[1] = "fallback"

		site := &program.CallSite{
			Arguments: []program.VarLocation{
				{Register: 0, Kind: isa.RegisterString},
				{Register: 1, Kind: isa.RegisterString},
			},
			Returns: []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}},
		}
		reflectedFunction := reflect.ValueOf(func(a, b any) any {
			if a != nil && a != "" {
				return a
			}
			return b
		})

		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.True(t, regs.General[0].IsValid())
		require.Equal(t, "fallback", regs.General[0].Interface())
	})

	t.Run("returns nil", func(t *testing.T) {
		t.Parallel()
		regs := makeRegisters()

		site := &program.CallSite{
			Arguments: []program.VarLocation{
				{Register: 0, Kind: isa.RegisterString},
				{Register: 1, Kind: isa.RegisterString},
			},
			Returns: []program.VarLocation{{Register: 0, Kind: isa.RegisterGeneral}},
		}
		reflectedFunction := reflect.ValueOf(func(a, b any) any { return nil })

		ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
		require.True(t, ok)
		require.False(t, regs.General[0].IsValid())
	})
}

func TestNativeFastPath_ZeroArgVoid(t *testing.T) {
	t.Parallel()
	called := false
	reflectedFunction := reflect.ValueOf(func() { called = true })

	site := &program.CallSite{
		Arguments: nil,
		Returns:   nil,
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), new(makeRegisters()))
	require.True(t, ok)
	require.True(t, called)
}

func TestNativeFastPath_Int64Void(t *testing.T) {
	t.Parallel()
	regs := makeRegisters()
	regs.Ints[0] = 42

	var received int64
	reflectedFunction := reflect.ValueOf(func(v int64) { received = v })

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
		Returns:   nil,
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, int64(42), received)
}

func TestNativeFastPath_NoMatch(t *testing.T) {
	t.Parallel()

	reflectedFunction := reflect.ValueOf(func(a, b, c, d, e int) int { return a })

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
		Returns:   []program.VarLocation{{Register: 1, Kind: isa.RegisterInt64}},
	}

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), new(makeRegisters()))
	require.False(t, ok)
	require.Equal(t, nativeFastPathNoneEntry, loadNativeFastPath(site))
}

func TestNativeFastPath_RepeatedCalls(t *testing.T) {
	t.Parallel()

	regs := makeRegisters()
	regs.Strings[0] = "test"

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 1, Kind: isa.RegisterString}},
	}
	reflectedFunction := reflect.ValueOf(strings.ToUpper)

	ok, _, _ := tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "TEST", regs.Strings[1])

	regs.Strings[0] = "again"
	ok, _, _ = tryNativeFastPath(nil, site, reflectedFunction.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "AGAIN", regs.Strings[1])
}

func TestNativeFastPath_BoundMethodNotStale(t *testing.T) {
	t.Parallel()

	regs := makeRegisters()

	site := &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns: []program.VarLocation{
			{Register: 0, Kind: isa.RegisterInt64},
			{Register: 0, Kind: isa.RegisterGeneral},
		},
	}

	b1 := &strings.Builder{}
	b2 := &strings.Builder{}

	fn1 := reflect.ValueOf(b1).MethodByName("WriteString")
	fn2 := reflect.ValueOf(b2).MethodByName("WriteString")

	regs.Strings[0] = "hello"
	ok, _, _ := tryNativeFastPath(nil, site, fn1.Interface(), &regs)
	require.True(t, ok)
	require.Equal(t, "hello", b1.String())

	regs.Strings[0] = "world"
	ok, _, _ = tryNativeFastPath(nil, site, fn2.Interface(), &regs)
	require.True(t, ok)

	require.Equal(t, "hello", b1.String(), "b1 should not have been modified by second call")
	require.Equal(t, "world", b2.String(), "b2 should have received the second write")
}

func TestCachedNativeFastPathCallsTheFunctionInTheRegister(t *testing.T) {
	t.Parallel()

	cached := func() int { return 1 }
	tests := []struct {
		name           string
		register       reflect.Value
		wantDispatched bool
		wantResult     int64
	}{
		{name: "the cached function", register: reflect.ValueOf(cached), wantDispatched: true, wantResult: 1},
		{name: "another function of the same type", register: reflect.ValueOf(func() int { return 2 }), wantDispatched: true, wantResult: 2},
		{name: "a function of another type falls back", register: reflect.ValueOf(func() string { return "x" }), wantDispatched: false, wantResult: 0},
		{name: "an empty register falls back", register: reflect.Value{}, wantDispatched: false, wantResult: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			regs := makeRegisters()
			site := &program.CallSite{
				NativeRegister: 0,
				Arguments:      nil,
				Returns:        []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
			}
			ok, tag, _ := tryNativeFastPath(nil, site, cached, &regs)
			require.True(t, ok)

			regs.Ints[0] = 0
			regs.General[0] = tt.register
			_, dispatched := dispatchCachedNativeFastPath(nil, site, &regs, &nativeFastPathEntry{fn: cached, tag: tag})

			require.Equal(t, tt.wantDispatched, dispatched)
			require.Equal(t, tt.wantResult, regs.Ints[0])
		})
	}
}

func TestFmtArgsScratchReusesOnlyForFmtCallees(t *testing.T) {
	t.Parallel()
	vm := &VM{}
	scratch := fmtArgsScratchFor(vm, fmt.Sprintf)
	first := scratch.take(3)
	first[0], first[1], first[2] = "a", 1, 2.5
	scratch.give(first)
	require.Equal(t, 1, vm.fmtArgsIdleCount)
	require.Equal(t, []any{nil, nil, nil}, first[:3], "a returned buffer keeps none of the call's arguments")

	second := scratch.take(2)
	require.Equal(t, reflect.ValueOf(first).Pointer(), reflect.ValueOf(second).Pointer(), "the idle buffer is lent again")
	nested := fmtArgsScratchFor(vm, fmt.Errorf).take(2)
	require.NotEqual(t, reflect.ValueOf(second).Pointer(), reflect.ValueOf(nested).Pointer(), "a nested call gets its own buffer")

	var kept []any
	retaining := func(format string, args ...any) string { kept = args; return format }
	other := fmtArgsScratchFor(vm, retaining)
	buffer := other.take(1)
	buffer[0] = "held"
	other.give(buffer)
	_ = retaining("x", buffer...)
	require.Equal(t, []any{"held"}, kept, "a callee that may keep its slice is never lent a reused buffer")
	require.Nil(t, fmtArgsScratchFor(nil, fmt.Sprintf).vm)
}

func TestFmtArgsScratchBoundsWhatItKeeps(t *testing.T) {
	t.Parallel()
	vm := &VM{}
	scratch := fmtArgsScratchFor(vm, fmt.Sprint)
	scratch.give(make([]any, fmtArgsScratchMaxCap+1))
	require.Zero(t, vm.fmtArgsIdleCount, "an oversized buffer is dropped")
	for range fmtArgsScratchDepth + 2 {
		scratch.give(make([]any, 1))
	}
	require.Equal(t, fmtArgsScratchDepth, vm.fmtArgsIdleCount)
}

func TestFmtFastPathFormatsWithoutAllocatingItsArguments(t *testing.T) {
	registers := makeRegisters()
	site := newSprintfSiteForScratchTest()
	registers.Strings[0] = "%s-%s"
	registers.Strings[1] = "a"
	registers.Strings[2] = "b"
	vm := &VM{}
	fpDispatchSprintfString(vm, fmt.Sprintf, site, &registers)
	require.Equal(t, "a-b", registers.Strings[3])
	require.Equal(t, 1, vm.fmtArgsIdleCount, "the variadic buffer is kept for the next call")
	require.Equal(t, []any{nil, nil}, vm.fmtArgsIdle[0][:2])
}

func TestTypeContainsPipitSynthesisedAnswersACachedTypeWithoutAllocating(t *testing.T) {
	type plain struct{ A []int }
	target := reflect.TypeFor[plain]()
	require.False(t, typeContainsPipitSynthesised(target))
	allocations := testing.AllocsPerRun(100, func() { _ = typeContainsPipitSynthesised(target) })
	require.Zero(t, allocations)
}

func newSprintfSiteForScratchTest() *program.CallSite {
	return &program.CallSite{
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}, {Register: 1, Kind: isa.RegisterString}, {Register: 2, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 3, Kind: isa.RegisterString}},
	}
}

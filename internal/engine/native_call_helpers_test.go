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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func TestSafeReflectCallReturnsResultsAndCatchesPanics(t *testing.T) {
	t.Parallel()

	t.Run("a call that returns cleanly hands back its results", func(t *testing.T) {
		t.Parallel()
		double := reflect.ValueOf(func(n int) int { return n * 2 })

		results, panicValue, err := safeReflectCallWithPanic(double, []reflect.Value{reflect.ValueOf(21)})

		require.NoError(t, err)
		require.Nil(t, panicValue)
		require.Len(t, results, 1)
		require.Equal(t, int64(42), results[0].Int())
	})

	t.Run("a call with no results hands back none", func(t *testing.T) {
		t.Parallel()
		noop := reflect.ValueOf(func() {})

		results, panicValue, err := safeReflectCallWithPanic(noop, nil)

		require.NoError(t, err)
		require.Nil(t, panicValue)
		require.Empty(t, results)
	})

	t.Run("a call with several results keeps their order", func(t *testing.T) {
		t.Parallel()
		pair := reflect.ValueOf(func() (int, string) { return 1, "a" })

		results, _, err := safeReflectCallWithPanic(pair, nil)

		require.NoError(t, err)
		require.Len(t, results, 2)
		require.Equal(t, int64(1), results[0].Int())
		require.Equal(t, "a", results[1].String())
	})

	t.Run("a panicking call is caught rather than unwound into the host", func(t *testing.T) {
		t.Parallel()
		boom := reflect.ValueOf(func() { panic("boom") })

		results, panicValue, err := safeReflectCallWithPanic(boom, nil)

		require.Error(t, err, "the panic must surface as an error")
		require.Equal(t, "boom", panicValue)
		require.Nil(t, results)
	})

	t.Run("a runtime panic is caught too", func(t *testing.T) {
		t.Parallel()
		divide := reflect.ValueOf(func(a, b int) int { return a / b })

		_, panicValue, err := safeReflectCallWithPanic(divide,
			[]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(0)})

		require.Error(t, err)
		require.NotNil(t, panicValue)
	})
}

func TestSafeReflectCallOrCallSliceHonoursTheSpreadFlag(t *testing.T) {
	t.Parallel()

	sum := reflect.ValueOf(func(values ...int) int {
		total := 0
		for _, value := range values {
			total += value
		}
		return total
	})

	t.Run("a spread call passes the slice as the variadic tail", func(t *testing.T) {
		t.Parallel()
		results, _, err := safeReflectCallOrCallSlice(sum,
			[]reflect.Value{reflect.ValueOf([]int{1, 2, 3})}, true)

		require.NoError(t, err)
		require.Equal(t, int64(6), results[0].Int())
	})

	t.Run("an ordinary call passes each element separately", func(t *testing.T) {
		t.Parallel()
		results, _, err := safeReflectCallOrCallSlice(sum,
			[]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)}, false)

		require.NoError(t, err)
		require.Equal(t, int64(3), results[0].Int())
	})

	t.Run("a mismatched spread is caught rather than crashing the host", func(t *testing.T) {
		t.Parallel()
		_, panicValue, err := safeReflectCallOrCallSlice(sum,
			[]reflect.Value{reflect.ValueOf(1)}, true)

		require.Error(t, err, "CallSlice needs a slice for its final argument")
		require.NotNil(t, panicValue)
	})
}

func TestInterfaceOrNilUnwrapsOnlyWhatItCan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() reflect.Value
		want  any
		name  string
	}{
		{name: "an integer unwraps", build: func() reflect.Value { return reflect.ValueOf(42) }, want: 42},
		{name: "a string unwraps", build: func() reflect.Value { return reflect.ValueOf("a") }, want: "a"},
		{name: "an invalid value yields nothing", build: func() reflect.Value { return reflect.Value{} }, want: nil},
		{
			name: "an inaccessible field yields nothing",
			build: func() reflect.Value {
				type hidden struct{ unexported int }
				return reflect.ValueOf(hidden{unexported: 1}).Field(0)
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, interfaceOrNil(tt.build()))
		})
	}
}

func TestNativeFunctionClassifiersRecogniseTheirOwnFamily(t *testing.T) {
	t.Parallel()

	t.Run("the wall-clock sleep is recognised by name and signature", func(t *testing.T) {
		t.Parallel()
		sleep := reflect.ValueOf(time.Sleep)

		require.True(t, isWallClockSleep(sleep, sleep.Type()))
	})

	t.Run("a function with the same shape but another name is not a sleep", func(t *testing.T) {
		t.Parallel()
		lookalike := reflect.ValueOf(func(time.Duration) {})

		require.False(t, isWallClockSleep(lookalike, lookalike.Type()),
			"the classifier must key on the symbol, not just the signature")
	})

	t.Run("a function with the wrong shape is not a sleep", func(t *testing.T) {
		t.Parallel()
		wrongShape := reflect.ValueOf(func(int) {})

		require.False(t, isWallClockSleep(wrongShape, wrongShape.Type()))
	})

	t.Run("the encoding marshallers are recognised", func(t *testing.T) {
		t.Parallel()
		require.True(t, isEncodingMarshaller(reflect.ValueOf(json.Marshal)))
		require.True(t, isEncodingMarshaller(reflect.ValueOf(json.MarshalIndent)))
	})

	t.Run("an unrelated function is not a marshaller", func(t *testing.T) {
		t.Parallel()
		require.False(t, isEncodingMarshaller(reflect.ValueOf(json.Unmarshal)))
		require.False(t, isEncodingMarshaller(reflect.ValueOf(func() {})))
	})

	t.Run("the scan family is recognised by its prefix", func(t *testing.T) {
		t.Parallel()
		require.True(t, isFmtScanFunction(reflect.ValueOf(fmt.Sscan)))
		require.True(t, isFmtScanFunction(reflect.ValueOf(fmt.Sscanf)))
		require.False(t, isFmtScanFunction(reflect.ValueOf(fmt.Sprint)))
	})

	t.Run("a variadic formatting function is recognised", func(t *testing.T) {
		t.Parallel()
		sprint := reflect.ValueOf(fmt.Sprint)

		require.True(t, isFmtVariadicAny(sprint, sprint.Type()))
	})

	t.Run("a variadic function outside the formatting package is not", func(t *testing.T) {
		t.Parallel()
		other := reflect.ValueOf(func(...any) {})

		require.False(t, isFmtVariadicAny(other, other.Type()))
	})

	t.Run("a non-variadic function is never a variadic formatter", func(t *testing.T) {
		t.Parallel()
		fixed := reflect.ValueOf(func(a any) {})

		require.False(t, isFmtVariadicAny(fixed, fixed.Type()))
	})

	t.Run("a variadic function over a non-empty interface is not", func(t *testing.T) {
		t.Parallel()
		typed := reflect.ValueOf(func(...error) {})

		require.False(t, isFmtVariadicAny(typed, typed.Type()),
			"only a variadic tail of the empty interface carries formatting arguments")
	})
}

func TestLoadNativeFastPathStartsUnprobed(t *testing.T) {
	t.Parallel()

	site := &program.CallSite{}

	require.Nil(t, loadNativeFastPath(site), "a fresh call site has no classification yet")
}

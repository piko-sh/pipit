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
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
)

func TestReflectInterfaceEqualDynamicType(t *testing.T) {
	t.Parallel()

	int32One := reflect.ValueOf(int32(1))
	int64One := reflect.ValueOf(int64(1))
	equal, uncomparable := engine.ReflectInterfaceEqual(int32One, int64One)
	require.False(t, uncomparable)
	require.False(t, equal, "int32(1) and int64(1) have distinct dynamic types")

	equal, uncomparable = engine.ReflectInterfaceEqual(int64One, int64One)
	require.False(t, uncomparable)
	require.True(t, equal)

	var pInt *int
	var pStr *string
	equal, uncomparable = engine.ReflectInterfaceEqual(reflect.ValueOf(pInt), reflect.ValueOf(pStr))
	require.False(t, uncomparable)
	require.False(t, equal, "(*int)(nil) and (*string)(nil) have distinct dynamic types")

	_, uncomparable = engine.ReflectInterfaceEqual(reflect.ValueOf([]int{1}), reflect.ValueOf([]int{1}))
	require.True(t, uncomparable, "comparing two []int is uncomparable and must panic at runtime")
}

func TestEvalInterfaceEqualityUncomparablePanics(t *testing.T) {
	t.Parallel()

	code := `func cmp() (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = "panicked"
		}
	}()
	var a any = []int{1}
	var b any = []int{1}
	if a == b {
		return "equal"
	}
	return "notequal"
}
cmp()`
	service := app.NewService()
	result, err := service.Eval(context.Background(), code)
	require.NoError(t, err)
	require.Equal(t, "panicked", result)
}

func TestReflectBinaryOpUintDivRem(t *testing.T) {
	t.Parallel()

	const big = uint64(1)<<63 + 10
	a := reflect.ValueOf(big)
	b := reflect.ValueOf(uint64(3))

	quotient := engine.ReflectBinaryOp(a, b,
		func(x, y int64) int64 { return x / y }, nil, nil,
		func(x, y uint64) uint64 { return x / y })
	require.Equal(t, big/3, quotient.Interface())

	remainder := engine.ReflectBinaryOp(a, b,
		func(x, y int64) int64 { return x % y }, nil, nil,
		func(x, y uint64) uint64 { return x % y })
	require.Equal(t, big%3, remainder.Interface())
}

func TestEvalSelectReceiveClosedChannelZeroValue(t *testing.T) {
	t.Parallel()

	code := `ch := make(chan int, 1)
ch <- 7
<-ch
close(ch)
v := 99
select {
case v = <-ch:
}
v`
	service := app.NewService()
	result, err := service.Eval(context.Background(), code)
	require.NoError(t, err)
	require.EqualValues(t, 0, result, "receive from a closed channel yields the zero value")
}

func TestEvalRuntimePanicsAreRecoverable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
	}{
		{
			name: "typed_slice_index_out_of_range",
			code: `func f() (r string) {
	defer func() {
		if recover() != nil {
			r = "recovered"
		}
	}()
	s := []int{1, 2, 3}
	i := 5
	_ = s[i]
	return "no-panic"
}
f()`,
		},
		{
			name: "failed_type_assertion",
			code: `func f() (r string) {
	defer func() {
		if recover() != nil {
			r = "recovered"
		}
	}()
	var a any = "hello"
	_ = a.(int)
	return "no-panic"
}
f()`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.Equal(t, "recovered", result)
		})
	}
}

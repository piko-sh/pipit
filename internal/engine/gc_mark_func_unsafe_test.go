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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFunctionCapturesNothingByName(t *testing.T) {
	captured := 3
	closure := func() int { return captured }
	builder := &strings.Builder{}
	require.True(t, functionCapturesNothing(reflect.ValueOf(strings.ToUpper).Pointer()), "top-level function")
	require.True(t, functionCapturesNothing(reflect.ValueOf((*strings.Builder).WriteString).Pointer()), "method expression")
	require.True(t, functionCapturesNothing(reflect.ValueOf(slices.Max[[]int]).Pointer()), "generic instantiation")
	require.False(t, functionCapturesNothing(reflect.ValueOf(closure).Pointer()), "closure")
	require.False(t, functionCapturesNothing(reflect.ValueOf(builder.WriteString).Pointer()), "method value")
	madeFunc := reflect.MakeFunc(reflect.TypeFor[func()](), func([]reflect.Value) []reflect.Value { return nil })
	require.False(t, functionCapturesNothing(madeFunc.Pointer()), "reflect.MakeFunc")
	require.False(t, functionCapturesNothing(reflect.ValueOf(strings.ToUpper).Pointer()+1), "not a function's entry")
}

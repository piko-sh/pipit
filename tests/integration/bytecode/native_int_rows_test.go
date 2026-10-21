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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestNativeFunctionsOfOneIntAndCallbacksOfOneIntGiveGosResults(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	service.RegisterPackage("inthost", map[string]reflect.Value{
		"Narrow16": reflect.ValueOf(func(v int) int16 { return int16(v) }),
		"Narrow32": reflect.ValueOf(func(v int) int32 { return int32(v) }),
		"Byte":     reflect.ValueOf(func(v int) uint8 { return uint8(v) }),
		"Word":     reflect.ValueOf(func(v int) uint16 { return uint16(v) }),
		"Each": reflect.ValueOf(func(n int, visit func(int)) {
			for i := range n {
				visit(i - 2)
			}
		}),
	})
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": `package main

import "inthost"

func entrypoint() int {
	total := 0
	for round := 0; round < 50; round++ {
		inthost.Each(5, func(i int) { total += i * 10 })
		total += int(inthost.Narrow16(-7-round)) + int(inthost.Narrow32(-70000)) + int(inthost.Byte(200+round)) + int(inthost.Word(60000))
	}
	return total
}
`})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), compiled, "entrypoint")
	require.NoError(t, err)
	want := 0
	for round := range 50 {
		for i := range 5 {
			want += (i - 2) * 10
		}
		want += int(int16(-7-round)) + int(int32(-70000)) + int(uint8(200+round)) + int(uint16(60000))
	}
	require.EqualValues(t, want, result)
}

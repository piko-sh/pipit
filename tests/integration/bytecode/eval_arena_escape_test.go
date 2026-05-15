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

func TestArenaEscapeStructStringFieldSurvivesReuse(t *testing.T) {
	service := app.NewService()

	code := `type Box struct{ S string }
func build(prefix string) Box { return Box{S: prefix + "-world-" + prefix} }
build("hello")`
	result, err := service.Eval(context.Background(), code)
	require.NoError(t, err)

	field := reflect.ValueOf(result).FieldByName("S")
	require.True(t, field.IsValid(), "expected a Box struct with an S field, got %T", result)
	require.Equal(t, "hello-world-hello", field.String())

	churn := `s := ""
for i := 0; i < 4096; i++ {
	s = s + "0123456789abcdef"
}
len(s)`
	_, err = service.Eval(context.Background(), churn)
	require.NoError(t, err)

	require.Equal(t, "hello-world-hello", field.String(),
		"struct string field corrupted after the pooled arena was reused")
}

func TestArenaEscapeNamedStringStructFieldSurvivesReuse(t *testing.T) {
	service := app.NewService()

	code := `type Name string
type Person struct{ N Name; Age int }
func build(prefix string) Person { return Person{N: Name(prefix + "-" + prefix), Age: 7} }
build("hi")`
	result, err := service.Eval(context.Background(), code)
	require.NoError(t, err)

	value := reflect.ValueOf(result)
	name := value.FieldByName("N")
	require.True(t, name.IsValid(), "expected a Person struct with an N field, got %T", result)
	require.Equal(t, "hi-hi", name.String())

	churn := `s := ""
for i := 0; i < 4096; i++ {
	s = s + "0123456789abcdef"
}
len(s)`
	_, err = service.Eval(context.Background(), churn)
	require.NoError(t, err)

	require.Equal(t, "hi-hi", name.String(),
		"named-string field corrupted after the pooled arena was reused")
}

func TestArenaEscapeNestedStructStringFieldSurvivesReuse(t *testing.T) {
	service := app.NewService()

	code := `type Inner struct{ Name string }
type Outer struct{ In Inner }
func build(part string) Outer { return Outer{In: Inner{Name: "n-" + part + "-n"}} }
build("deep")`
	result, err := service.Eval(context.Background(), code)
	require.NoError(t, err)

	name := reflect.ValueOf(result).FieldByName("In").FieldByName("Name")
	require.True(t, name.IsValid(), "expected Outer{Inner{Name}}, got %T", result)
	require.Equal(t, "n-deep-n", name.String())

	churn := `s := ""
for i := 0; i < 4096; i++ {
	s = s + "ABCDEFGHIJKLMNOP"
}
len(s)`
	_, err = service.Eval(context.Background(), churn)
	require.NoError(t, err)

	require.Equal(t, "n-deep-n", name.String(),
		"nested struct string field corrupted after the pooled arena was reused")
}

func TestArenaBackingAllocLimit(t *testing.T) {
	alloc := []struct {
		name string
		call func(a *engine.RegisterArena, n int)
	}{
		{"byte", func(a *engine.RegisterArena, n int) { a.AllocByteBacking(n) }},
		{"int", func(a *engine.RegisterArena, n int) { a.AllocIntBacking(n) }},
		{"float", func(a *engine.RegisterArena, n int) { a.AllocFloatBacking(n) }},
		{"string", func(a *engine.RegisterArena, n int) { a.AllocStringBacking(n) }},
		{"bool", func(a *engine.RegisterArena, n int) { a.AllocBoolBacking(n) }},
		{"uint", func(a *engine.RegisterArena, n int) { a.AllocUintBacking(n) }},
	}
	for _, tt := range alloc {
		t.Run(tt.name+"_over_limit_panics", func(t *testing.T) {
			arena := engine.GetRegisterArena()
			defer engine.PutRegisterArena(arena)
			arena.MaxAllocSize = 100

			require.PanicsWithError(t, "arena: single allocation of 1000 elements exceeds limit 100: allocation size limit exceeded",
				func() { tt.call(arena, 1000) })
		})
		t.Run(tt.name+"_within_limit_ok", func(t *testing.T) {
			arena := engine.GetRegisterArena()
			defer engine.PutRegisterArena(arena)
			arena.MaxAllocSize = 100

			require.NotPanics(t, func() { tt.call(arena, 50) })
		})
		t.Run(tt.name+"_unlimited_ok", func(t *testing.T) {
			arena := engine.GetRegisterArena()
			defer engine.PutRegisterArena(arena)
			arena.MaxAllocSize = 0

			require.NotPanics(t, func() { tt.call(arena, 100000) })
		})
	}
}

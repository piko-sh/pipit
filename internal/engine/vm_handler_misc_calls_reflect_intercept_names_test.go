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

	"pipit.sh/pipit/internal/isa"
)

func embeddingScriptType(t *testing.T) reflect.Type {
	t.Helper()

	return reflect.StructOf([]reflect.StructField{
		{Name: isa.EmbeddedUnexportedPrefix + "base", Type: reflect.TypeFor[int](), PkgPath: "main"},
		{Name: "Name", Type: reflect.TypeFor[string]()},
	})
}

func TestPipitStructFieldLookupAnswersToSourceNames(t *testing.T) {
	t.Parallel()

	embedding := embeddingScriptType(t)

	t.Run("a renamed embedded field answers only to its source name", func(t *testing.T) {
		t.Parallel()

		require.True(t, pipitStructFieldMatches(embedding.Field(0), "base"))
		require.False(t, pipitStructFieldMatches(embedding.Field(0), isa.EmbeddedUnexportedPrefix+"base"),
			"no such field exists in the program being run")
	})

	t.Run("an ordinary field answers to its own name", func(t *testing.T) {
		t.Parallel()

		require.True(t, pipitStructFieldMatches(embedding.Field(1), "Name"))
		require.False(t, pipitStructFieldMatches(embedding.Field(1), "Other"))
	})

	t.Run("a lookup by source name finds the field value", func(t *testing.T) {
		t.Parallel()

		value := reflect.New(embedding).Elem()
		value.Field(1).SetString("named")

		require.Equal(t, "named", pipitFieldByName(value, "Name").String())
		require.True(t, pipitFieldByName(value, "base").IsValid(), "an embedded field is still reachable")
		require.False(t, pipitFieldByName(value, "absent").IsValid())
	})

	t.Run("the sentinel field is not reachable by name", func(t *testing.T) {
		t.Parallel()

		script := syntheticScriptType(t, "Point")
		value := reflect.New(script).Elem()

		require.False(t, pipitFieldByName(value, pipitIDFieldPrefix+"Point").IsValid(),
			"the identity field is pipit's, not the program's")
	})
}

func TestPipitStructTypeForValueFollowsPointers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value reflect.Value
		want  bool
	}{
		{name: "a struct value", value: reflect.ValueOf(identityNone{}), want: true},
		{name: "a pointer to a struct", value: reflect.ValueOf(&identityNone{}), want: true},
		{name: "a scalar", value: reflect.ValueOf(4), want: false},
		{name: "a slice", value: reflect.ValueOf([]int{1}), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reflectType, ok := pipitStructTypeForValue(tt.value)
			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, reflect.Struct, reflectType.Kind())
			}
		})
	}
}

func TestSafeMethodByNameTurnsAPanicIntoAnError(t *testing.T) {
	t.Parallel()

	t.Run("an existing method is returned", func(t *testing.T) {
		t.Parallel()

		method, err := safeMethodByName(reflect.ValueOf(assertStringer{}), "String")
		require.NoError(t, err)
		require.True(t, method.IsValid())
	})

	t.Run("an absent method is an invalid value, not an error", func(t *testing.T) {
		t.Parallel()

		method, err := safeMethodByName(reflect.ValueOf(assertStringer{}), "Absent")
		require.NoError(t, err)
		require.False(t, method.IsValid())
	})

	t.Run("an invalid receiver is reported as an error", func(t *testing.T) {
		t.Parallel()

		_, err := safeMethodByName(reflect.Value{}, "String")
		require.Error(t, err)
	})
}

func TestUnwrapInterfaceArgumentsReachesTheConcreteValue(t *testing.T) {
	t.Parallel()

	boxed := reflect.ValueOf(map[string]any{"k": 4}).MapIndex(reflect.ValueOf("k"))
	nilBoxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

	unwrapped := unwrapInterfaceArguments([]reflect.Value{boxed, nilBoxed, reflect.ValueOf("plain")})

	require.Equal(t, reflect.Int, unwrapped[0].Kind(), "an interface argument is unwrapped to its dynamic value")
	require.Equal(t, reflect.Interface, unwrapped[1].Kind(), "a nil interface has nothing to unwrap to")
	require.Equal(t, "plain", unwrapped[2].Interface())
}

func TestShapeBoundMethodResultsFitTheDeclaredTypes(t *testing.T) {
	t.Parallel()

	outTypes := []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[string]()}

	tests := []struct {
		name    string
		results []reflect.Value
		want    []any
	}{
		{
			name:    "exact results pass straight through",
			results: []reflect.Value{reflect.ValueOf(4), reflect.ValueOf("s")},
			want:    []any{4, "s"},
		},
		{
			name:    "a convertible result is converted",
			results: []reflect.Value{reflect.ValueOf(int64(4)), reflect.ValueOf("s")},
			want:    []any{4, "s"},
		},
		{
			name:    "a missing result becomes the zero value",
			results: []reflect.Value{reflect.ValueOf(4)},
			want:    []any{4, ""},
		},
		{
			name:    "an invalid result becomes the zero value",
			results: []reflect.Value{reflect.Value{}, reflect.ValueOf("s")},
			want:    []any{0, "s"},
		},
		{
			name:    "an unrelated result becomes the zero value",
			results: []reflect.Value{reflect.ValueOf([]int{1}), reflect.ValueOf("s")},
			want:    []any{0, "s"},
		},
		{
			name:    "surplus results are dropped",
			results: []reflect.Value{reflect.ValueOf(4), reflect.ValueOf("s"), reflect.ValueOf(true)},
			want:    []any{4, "s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shaped := shapeBoundMethodResults(tt.results, outTypes)
			require.Len(t, shaped, len(outTypes))
			for i, want := range tt.want {
				require.Equal(t, want, shaped[i].Interface())
			}
		})
	}
}

func TestShapeBoundMethodResultUnwrapsInterfaceResults(t *testing.T) {
	t.Parallel()

	boxed := reflect.ValueOf(map[string]any{"k": 4}).MapIndex(reflect.ValueOf("k"))
	nilBoxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

	require.Equal(t, 4, shapeBoundMethodResult(boxed, reflect.TypeFor[int]()).Interface(),
		"a result that left the general bank as an interface is unwrapped first")
	require.Equal(t, 0, shapeBoundMethodResult(nilBoxed, reflect.TypeFor[int]()).Interface(),
		"a nil interface becomes the zero of the promised type")
	require.Equal(t, 4, shapeBoundMethodResult(boxed, reflect.TypeFor[any]()).Interface(),
		"an interface result promised as any keeps its wrapper")
}

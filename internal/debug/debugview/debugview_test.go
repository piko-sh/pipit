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

package debugview

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type point struct {
	X int
	Y int
}

func TestLeaf(t *testing.T) {
	t.Parallel()
	var nilPointer *point
	var nilSlice []int
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "string", value: "hi", want: `"hi"`},
		{name: "int", value: 42, want: "42"},
		{name: "struct", value: point{X: 1}, want: "point{...}"},
		{name: "pointer", value: &point{}, want: "&point{...}"},
		{name: "nil pointer", value: nilPointer, want: "nil"},
		{name: "map", value: map[string]int{"a": 1}, want: "map[string]int (1 entries)"},
		{name: "slice", value: []int{1, 2}, want: "[]int (len=2)"},
		{name: "nil slice", value: nilSlice, want: "nil"},
		{name: "array", value: [3]int{}, want: "[3]int (len=3)"},
		{name: "invalid", value: nil, want: "<invalid>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Leaf(reflect.ValueOf(tt.value)))
		})
	}
}

func TestChildren(t *testing.T) {
	t.Parallel()
	fields := Children(reflect.ValueOf(&point{X: 1, Y: 2}))
	require.Len(t, fields, 2, "struct children")
	require.Equal(t, "X", fields[0].Name)
	require.Equal(t, "Y", fields[1].Name)
	require.Equal(t, int64(2), fields[1].Value.Int())
	require.Equal(t, "int", fields[0].Type)

	entries := Children(reflect.ValueOf(map[string]int{"b": 2, "a": 1}))
	require.Len(t, entries, 2, "map children are sorted by key")
	require.Equal(t, "a", entries[0].Name)
	require.Equal(t, "b", entries[1].Name)

	elements := Children(reflect.ValueOf([]string{"x", "y"}))
	require.Len(t, elements, 2, "slice children")
	require.Equal(t, "[1]", elements[1].Name)
	require.Equal(t, "y", elements[1].Value.String())

	require.Nil(t, Children(reflect.ValueOf(7)), "a leaf has no children")
}

func TestSynthesisedStructs(t *testing.T) {
	t.Parallel()
	synthesised := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[int]()},
		{Name: "_pipitID_pair", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
	value := reflect.New(synthesised).Elem()
	value.Field(0).SetInt(7)

	require.Equal(t, "pair", TypeName(synthesised))
	require.Equal(t, "[]*pair", TypeName(reflect.SliceOf(reflect.PointerTo(synthesised))))
	require.Equal(t, "pair{...}", Leaf(value))
	children := Children(value)
	require.Equal(t, 1, ChildCount(value), "the sentinel field is not a child")
	require.Len(t, children, 1)
	require.Equal(t, "A", children[0].Name)
}

func TestExpandable(t *testing.T) {
	t.Parallel()
	var nilPointer *point
	var boxed any = []int{1}
	tests := []struct {
		name  string
		value reflect.Value
		want  bool
	}{
		{name: "struct", value: reflect.ValueOf(point{}), want: true},
		{name: "empty struct", value: reflect.ValueOf(struct{}{}), want: false},
		{name: "empty slice", value: reflect.ValueOf([]int{}), want: false},
		{name: "nil pointer", value: reflect.ValueOf(nilPointer), want: false},
		{name: "interface to slice", value: reflect.ValueOf(&boxed).Elem(), want: true},
		{name: "int", value: reflect.ValueOf(3), want: false},
		{name: "invalid", value: reflect.Value{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Expandable(tt.value))
		})
	}
}

func TestLeafNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    any
		typeName string
		want     string
	}{
		{name: "slice under its source type name", value: []int64{3, 8}, typeName: "[]int", want: "[]int (len=2)"},
		{name: "map under its source type name", value: map[string]int64{"a": 1}, typeName: "map[string]int", want: "map[string]int (1 entries)"},
		{name: "scalar", value: int64(7), typeName: "int", want: "7"},
		{name: "slice without a name", value: []int64{1}, typeName: "", want: "[]int64 (len=1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, LeafNamed(reflect.ValueOf(tt.value), tt.typeName))
		})
	}
}

type lowercaseFields struct {
	count   int
	enabled bool
	ratio   float64
	label   string
	inner   point
}

func TestLeafRendersUnexportedFields(t *testing.T) {
	t.Parallel()
	value := reflect.ValueOf(lowercaseFields{count: 3, enabled: true, ratio: 0.5, label: "x", inner: point{X: 1}})
	want := map[string]string{"count": "3", "enabled": "true", "ratio": "0.5", "label": `"x"`, "inner": "point{...}"}

	children := Children(value)
	require.Len(t, children, len(want))
	for _, child := range children {
		t.Run(child.Name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, want[child.Name], Leaf(child.Value))
		})
	}
}

func TestLeafRendersNativeValues(t *testing.T) {
	t.Parallel()
	hidden := reflect.ValueOf(struct{ m map[int]string }{m: map[int]string{4: "d"}}).Field(0)
	children := Children(hidden)
	require.Len(t, children, 1, "children of a map behind an unexported field")
	require.Equal(t, "4", children[0].Name)
	require.Equal(t, `"d"`, Leaf(children[0].Value))
	for _, child := range Children(reflect.ValueOf(time.Unix(0, 0))) {
		_ = Leaf(child.Value)
	}

	tests := []struct {
		name  string
		value reflect.Value
		want  string
	}{
		{name: "a Stringer keeps its String method", value: reflect.ValueOf(time.Second), want: "1s"},
		{name: "an unexported field of a native struct", value: reflect.ValueOf(time.Unix(0, 0)).FieldByName("ext"), want: "62135596800"},
		{name: "a map key behind an unexported field", value: reflect.ValueOf(struct{ m map[int]string }{m: map[int]string{4: "d"}}).Field(0), want: "map[int]string (1 entries)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Leaf(tt.value))
		})
	}
}

func TestChildrenRange(t *testing.T) {
	t.Parallel()
	slice := reflect.ValueOf([]int{10, 11, 12, 13, 14})
	numericKeys := reflect.ValueOf(map[int]string{10: "j", 2: "b", 1: "a", -3: "m"})
	tests := []struct {
		name  string
		value reflect.Value
		start int
		count int
		want  []string
	}{
		{name: "a slice in full", value: slice, start: 0, count: 0, want: []string{"[0]", "[1]", "[2]", "[3]", "[4]"}},
		{name: "a slice window", value: slice, start: 1, count: 2, want: []string{"[1]", "[2]"}},
		{name: "a window running past the end", value: slice, start: 3, count: 10, want: []string{"[3]", "[4]"}},
		{name: "a start past the end", value: slice, start: 9, count: 2, want: []string{}},
		{name: "a negative start", value: slice, start: -4, count: 1, want: []string{"[0]"}},
		{name: "numeric map keys in numeric order", value: numericKeys, start: 0, count: 0, want: []string{"-3", "1", "2", "10"}},
		{name: "a map window", value: numericKeys, start: 2, count: 1, want: []string{"2"}},
		{name: "a struct window", value: reflect.ValueOf(point{X: 1, Y: 2}), start: 1, count: 1, want: []string{"Y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			children := ChildrenRange(tt.value, tt.start, tt.count)
			got := make([]string, 0, len(children))
			for _, child := range children {
				got = append(got, child.Name)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSelfReferentialValuesStayBounded(t *testing.T) {
	t.Parallel()
	var loop any
	loop = &loop
	type node struct{ Next *node }
	cycle := &node{}
	cycle.Next = cycle

	tests := []struct {
		name  string
		value reflect.Value
	}{
		{name: "an interface holding a pointer to itself", value: reflect.ValueOf(&loop)},
		{name: "a struct pointing at itself", value: reflect.ValueOf(cycle)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, Leaf(tt.value), "every value renders a leaf")
			_ = Deref(tt.value)
			_ = ChildCount(tt.value)
			_ = Children(tt.value)
		})
	}
}

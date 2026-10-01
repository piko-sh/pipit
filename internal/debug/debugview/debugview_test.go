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
	"slices"
	"testing"
	"time"
)

type point struct {
	X, Y int
}

func TestLeaf(t *testing.T) {
	var nilPointer *point
	var nilSlice []int
	testCases := []struct {
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
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Leaf(reflect.ValueOf(tc.value)); got != tc.want {
				t.Fatalf("Leaf(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
	if got := Leaf(reflect.Value{}); got != "<invalid>" {
		t.Fatalf("Leaf(invalid) = %q", got)
	}
}

func TestChildren(t *testing.T) {
	fields := Children(reflect.ValueOf(&point{X: 1, Y: 2}))
	if len(fields) != 2 || fields[0].Name != "X" || fields[1].Name != "Y" || fields[1].Value.Int() != 2 || fields[0].Type != "int" {
		t.Fatalf("struct children = %+v", fields)
	}

	entries := Children(reflect.ValueOf(map[string]int{"b": 2, "a": 1}))
	if len(entries) != 2 || entries[0].Name != "a" || entries[1].Name != "b" {
		t.Fatalf("map children not sorted: %+v", entries)
	}

	elements := Children(reflect.ValueOf([]string{"x", "y"}))
	if len(elements) != 2 || elements[1].Name != "[1]" || elements[1].Value.String() != "y" {
		t.Fatalf("slice children = %+v", elements)
	}

	if Children(reflect.ValueOf(7)) != nil {
		t.Fatal("leaf has children")
	}
}

func TestSynthesisedStructs(t *testing.T) {
	synthesised := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[int]()},
		{Name: "_pipitID_pair", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
	value := reflect.New(synthesised).Elem()
	value.Field(0).SetInt(7)

	if got := TypeName(synthesised); got != "pair" {
		t.Fatalf("TypeName = %q, want pair", got)
	}
	if got := TypeName(reflect.SliceOf(reflect.PointerTo(synthesised))); got != "[]*pair" {
		t.Fatalf("TypeName of slice = %q, want []*pair", got)
	}
	if got := Leaf(value); got != "pair{...}" {
		t.Fatalf("Leaf = %q, want pair{...}", got)
	}
	children := Children(value)
	if ChildCount(value) != 1 || len(children) != 1 || children[0].Name != "A" {
		t.Fatalf("sentinel field listed as a child: %+v", children)
	}
}

func TestExpandable(t *testing.T) {
	var nilPointer *point
	var boxed any = []int{1}
	testCases := []struct {
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
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Expandable(tc.value); got != tc.want {
				t.Fatalf("Expandable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLeafNamed(t *testing.T) {
	wide := []int64{3, 8}
	if got := LeafNamed(reflect.ValueOf(wide), "[]int"); got != "[]int (len=2)" {
		t.Fatalf("LeafNamed slice = %q", got)
	}
	if got := LeafNamed(reflect.ValueOf(map[string]int64{"a": 1}), "map[string]int"); got != "map[string]int (1 entries)" {
		t.Fatalf("LeafNamed map = %q", got)
	}
	if got := LeafNamed(reflect.ValueOf(int64(7)), "int"); got != "7" {
		t.Fatalf("LeafNamed scalar = %q", got)
	}
	if got := LeafNamed(reflect.ValueOf([]int64{1}), ""); got != "[]int64 (len=1)" {
		t.Fatalf("LeafNamed without a name = %q", got)
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
	value := reflect.ValueOf(lowercaseFields{count: 3, enabled: true, ratio: 0.5, label: "x", inner: point{X: 1}})
	want := map[string]string{"count": "3", "enabled": "true", "ratio": "0.5", "label": `"x"`, "inner": "point{...}"}

	children := Children(value)
	if len(children) != len(want) {
		t.Fatalf("children = %+v, want %d fields", children, len(want))
	}
	for _, child := range children {
		t.Run(child.Name, func(t *testing.T) {
			if got := Leaf(child.Value); got != want[child.Name] {
				t.Fatalf("Leaf(%s) = %q, want %q", child.Name, got, want[child.Name])
			}
		})
	}
}

func TestLeafRendersNativeValues(t *testing.T) {
	testCases := []struct {
		name  string
		value reflect.Value
		want  string
	}{
		{name: "a Stringer keeps its String method", value: reflect.ValueOf(time.Second), want: "1s"},
		{name: "an unexported field of a native struct", value: reflect.ValueOf(time.Unix(0, 0)).FieldByName("ext"), want: "62135596800"},
		{name: "a map key behind an unexported field", value: reflect.ValueOf(struct{ m map[int]string }{m: map[int]string{4: "d"}}).Field(0), want: "map[int]string (1 entries)"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Leaf(tc.value); got != tc.want {
				t.Fatalf("Leaf = %q, want %q", got, tc.want)
			}
		})
	}

	hidden := reflect.ValueOf(struct{ m map[int]string }{m: map[int]string{4: "d"}}).Field(0)
	if children := Children(hidden); len(children) != 1 || children[0].Name != "4" || Leaf(children[0].Value) != `"d"` {
		t.Fatalf("children of a map behind an unexported field = %+v", children)
	}
	for _, child := range Children(reflect.ValueOf(time.Unix(0, 0))) {
		_ = Leaf(child.Value)
	}
}

func TestChildrenRange(t *testing.T) {
	slice := reflect.ValueOf([]int{10, 11, 12, 13, 14})
	numericKeys := reflect.ValueOf(map[int]string{10: "j", 2: "b", 1: "a", -3: "m"})
	testCases := []struct {
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
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			children := ChildrenRange(tc.value, tc.start, tc.count)
			got := make([]string, 0, len(children))
			for _, child := range children {
				got = append(got, child.Name)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ChildrenRange names = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelfReferentialValuesStayBounded(t *testing.T) {
	var loop any
	loop = &loop
	type node struct{ Next *node }
	cycle := &node{}
	cycle.Next = cycle

	testCases := []struct {
		name  string
		value reflect.Value
	}{
		{name: "an interface holding a pointer to itself", value: reflect.ValueOf(&loop)},
		{name: "a struct pointing at itself", value: reflect.ValueOf(cycle)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Leaf(tc.value); got == "" {
				t.Fatal("Leaf returned nothing")
			}
			_ = Deref(tc.value)
			_ = ChildCount(tc.value)
			_ = Children(tc.value)
		})
	}
}

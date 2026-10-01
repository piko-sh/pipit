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

// Package debugview renders interpreted values for debugger front ends.
//
// Values returned by the debugger are arbitrary Go values reached through reflection.
// Front ends (the DAP server, the browser playground) need the same short display
// strings, type names, and child listings for drilling into composites, so the rendering
// lives here rather than in each client.
package debugview

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/isa"
)

const (
	// nilText is how a nil pointer, interface, slice or map is shown.
	nilText = "nil"

	// lengthFormat summarises a slice or array by type and length.
	lengthFormat = "%s (len=%d)"

	// cycleText stands for the rest of a pointer or interface chain cut off at
	// maxIndirections.
	cycleText = "…"

	// maxIndirections bounds how many pointers and interfaces Deref and Leaf follow, so a
	// value that refers to itself, such as an interface holding a pointer to itself, renders
	// instead of looping.
	maxIndirections = 64
)

// Child is one named element of a composite value: a struct field, a map entry or a
// slice/array element.
type Child struct {
	// Name labels the child: the field name, the stringified map key, or "[i]".
	Name string

	// Type is the display type name of the child's declared type.
	Type string

	// Value holds the child value for further rendering or expansion.
	Value reflect.Value
}

// Deref follows non-nil interfaces and pointers to the value they hold.
//
// Takes value (reflect.Value) which is the value to unwrap.
//
// Returns reflect.Value which is the innermost non-interface, non-pointer value, the
// first nil interface or pointer met on the way, or the value reached after
// maxIndirections steps when the chain refers back to itself.
func Deref(value reflect.Value) reflect.Value {
	for range maxIndirections {
		if !value.IsValid() {
			return value
		}
		switch value.Kind() {
		case reflect.Interface, reflect.Pointer:
			if value.IsNil() {
				return value
			}
			value = value.Elem()
		default:
			return value
		}
	}
	return value
}

// Expandable reports whether value has at least one child a user could drill into.
//
// Takes value (reflect.Value) which is inspected for children.
//
// Returns bool which is true when the value has expandable children.
func Expandable(value reflect.Value) bool {
	return ChildCount(value) > 0
}

// ChildCount returns how many children Children would list for value.
//
// Takes value (reflect.Value) which is inspected for children.
//
// Returns int which is the number of fields, entries or elements.
func ChildCount(value reflect.Value) int {
	current := Deref(value)
	if !current.IsValid() {
		return 0
	}
	switch current.Kind() {
	case reflect.Struct:
		count := 0
		for field := range current.Type().Fields() {
			if !isSentinel(field) {
				count++
			}
		}
		return count
	case reflect.Map, reflect.Slice, reflect.Array:
		return current.Len()
	default:
		return 0
	}
}

// Children lists every child of a composite value; see ChildrenRange for the order.
//
// Takes value (reflect.Value) which holds the composite to expand.
//
// Returns []Child which lists the children, or nil for leaves and nil values.
func Children(value reflect.Value) []Child {
	return ChildrenRange(value, 0, 0)
}

// ChildrenRange lists a window of the children of a composite value, building only the
// children inside it.
//
// Struct fields keep their declaration order, slice and array elements their index order,
// and map entries are sorted by key (numerically for numeric keys) so repeated expansions
// and successive pages are stable.
//
// Takes value (reflect.Value) which holds the composite to expand.
// Takes start (int) which is the index of the first child to list, clamped to the
// children there are.
// Takes count (int) which is the most children to list; zero or less lists the rest.
//
// Returns []Child which lists the window, or nil for leaves and nil values.
func ChildrenRange(value reflect.Value, start, count int) []Child {
	current := Deref(value)
	if !current.IsValid() {
		return nil
	}
	switch current.Kind() {
	case reflect.Struct:
		fields := structChildren(current)
		low, high := windowBounds(len(fields), start, count)
		return fields[low:high]
	case reflect.Map:
		return mapChildren(current, start, count)
	case reflect.Slice, reflect.Array:
		return indexedChildren(current, start, count)
	default:
		return nil
	}
}

// structChildren lists a struct's fields, leaving out the interpreter's name marker.
//
// Takes current (reflect.Value) which is a struct value.
//
// Returns []Child which holds one child per user field.
func structChildren(current reflect.Value) []Child {
	typ := current.Type()
	out := make([]Child, 0, current.NumField())
	for index := range current.NumField() {
		field := typ.Field(index)
		if isSentinel(field) {
			continue
		}
		out = append(out, Child{Name: field.Name, Type: TypeName(field.Type), Value: current.Field(index)})
	}
	return out
}

// mapChildren lists a window of a map's entries in key order.
//
// Takes current (reflect.Value) which is a map value.
// Takes start (int) which is the index of the first entry to list.
// Takes count (int) which is the most entries to list; zero or less lists the rest.
//
// Returns []Child which holds the entries in the window, named by their keys.
func mapChildren(current reflect.Value, start, count int) []Child {
	keys := current.MapKeys()
	slices.SortFunc(keys, compareMapKeys)
	low, high := windowBounds(len(keys), start, count)
	elemType := TypeName(current.Type().Elem())
	out := make([]Child, 0, high-low)
	for _, key := range keys[low:high] {
		out = append(out, Child{Name: fmt.Sprint(key), Type: elemType, Value: current.MapIndex(key)})
	}
	return out
}

// indexedChildren lists a window of a slice's or array's elements.
//
// Takes current (reflect.Value) which is a slice or array value.
// Takes start (int) which is the index of the first element to list.
// Takes count (int) which is the most elements to list; zero or less lists the rest.
//
// Returns []Child which holds the elements in the window, named by their index.
func indexedChildren(current reflect.Value, start, count int) []Child {
	low, high := windowBounds(current.Len(), start, count)
	elemType := TypeName(current.Type().Elem())
	out := make([]Child, 0, high-low)
	for index := low; index < high; index++ {
		out = append(out, Child{Name: "[" + strconv.Itoa(index) + "]", Type: elemType, Value: current.Index(index)})
	}
	return out
}

// windowBounds clamps a start and count to a list of length items.
//
// Takes length (int) which is how many items the list holds.
// Takes start (int) which is the first item wanted.
// Takes count (int) which is the most items wanted; zero or less means the rest.
//
// Returns low (int) which is the index of the first item in the window.
// Returns high (int) which is one past the index of the last item in the window.
func windowBounds(length, start, count int) (low, high int) {
	low = min(max(start, 0), length)
	high = length
	if count > 0 {
		high = min(high, low+count)
	}
	return low, high
}

// compareMapKeys orders two map keys: numbers, strings and booleans by value, anything
// else by its printed form, and keys of different kinds (behind an interface) by kind.
//
// Takes a (reflect.Value) which is the first key.
// Takes b (reflect.Value) which is the second key.
//
// Returns int which is negative, zero or positive as a sorts before, with or after b.
func compareMapKeys(a, b reflect.Value) int {
	a, b = unwrapInterface(a), unwrapInterface(b)
	if a.Kind() != b.Kind() {
		return cmp.Compare(a.Kind(), b.Kind())
	}
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmp.Compare(a.Int(), b.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cmp.Compare(a.Uint(), b.Uint())
	case reflect.Float32, reflect.Float64:
		return cmp.Compare(a.Float(), b.Float())
	case reflect.String:
		return cmp.Compare(a.String(), b.String())
	case reflect.Bool:
		return cmp.Compare(boolRank(a.Bool()), boolRank(b.Bool()))
	default:
		return cmp.Compare(fmt.Sprint(a), fmt.Sprint(b))
	}
}

// unwrapInterface returns the value a non-nil interface holds, or value itself.
//
// Takes value (reflect.Value) which may be an interface.
//
// Returns reflect.Value which is the dynamic value behind a non-nil interface.
func unwrapInterface(value reflect.Value) reflect.Value {
	if value.Kind() == reflect.Interface && !value.IsNil() {
		return value.Elem()
	}
	return value
}

// boolRank orders false before true.
//
// Takes value (bool) which is the boolean to rank.
//
// Returns int which is 0 for false and 1 for true.
func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

// Leaf returns the one-line display string for a value. Composites render as a summary
// (type and length) rather than their contents; use Children to expand them.
//
// Takes value (reflect.Value) which holds the value to render.
//
// Returns string which is the display text for the value.
func Leaf(value reflect.Value) string {
	return leaf(value, 0)
}

// leaf renders a value for Leaf, counting the pointers and interfaces already followed.
// Values are formatted through their reflect.Value, which works for unexported fields too
// and still uses a String or Error method when the value exposes one.
//
// Takes value (reflect.Value) which holds the value to render.
// Takes depth (int) which counts the indirections followed to reach value.
//
// Returns string which is the display text for the value.
func leaf(value reflect.Value, depth int) string {
	if !value.IsValid() {
		return "<invalid>"
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		return leafIndirect(value, depth)
	case reflect.Struct:
		return fmt.Sprintf("%s{...}", TypeName(value.Type()))
	case reflect.Map:
		return fmt.Sprintf("%s (%d entries)", TypeName(value.Type()), value.Len())
	case reflect.Slice:
		if value.IsNil() {
			return nilText
		}
		return fmt.Sprintf(lengthFormat, TypeName(value.Type()), value.Len())
	case reflect.Array:
		return fmt.Sprintf(lengthFormat, TypeName(value.Type()), value.Len())
	case reflect.Chan, reflect.Func:
		return TypeName(value.Type())
	case reflect.String:
		return strconv.Quote(value.String())
	default:
		return fmt.Sprint(value)
	}
}

// leafIndirect renders a pointer as & and its target, and an interface as the value it
// holds, stopping after maxIndirections so a self-referential chain still renders.
//
// Takes value (reflect.Value) which is a pointer or interface.
// Takes depth (int) which counts the indirections followed to reach value.
//
// Returns string which is the display text for the value.
func leafIndirect(value reflect.Value, depth int) string {
	if value.IsNil() {
		return nilText
	}
	if depth >= maxIndirections {
		return cycleText
	}
	target := leaf(value.Elem(), depth+1)
	if value.Kind() == reflect.Pointer {
		return "&" + target
	}
	return target
}

// LeafNamed is Leaf with the value's declared type name used in composite summaries. The
// interpreter stores some values in a wider runtime representation (an int slice is held
// as []int64), so the declared name reads better than the reflect type.
//
// Takes value (reflect.Value) which holds the value to render.
// Takes name (string) which is the declared type name; empty falls back to Leaf.
//
// Returns string which is the display text for the value.
func LeafNamed(value reflect.Value, name string) string {
	if name == "" || !value.IsValid() {
		return Leaf(value)
	}
	switch value.Kind() {
	case reflect.Struct:
		return name + "{...}"
	case reflect.Map:
		return fmt.Sprintf("%s (%d entries)", name, value.Len())
	case reflect.Slice:
		if value.IsNil() {
			return nilText
		}
		return fmt.Sprintf(lengthFormat, name, value.Len())
	case reflect.Array:
		return fmt.Sprintf(lengthFormat, name, value.Len())
	default:
		return Leaf(value)
	}
}

// TypeName returns a short, human-friendly type name. Structs the interpreter synthesises
// for declared types are named after the declaration rather than their reflect layout,
// including inside pointer, slice, array and map types.
//
// Takes typ (reflect.Type) which is the type to name.
//
// Returns string which is the display name for the type.
func TypeName(typ reflect.Type) string {
	if typ == nil {
		return ""
	}
	if typ.Name() != "" {
		return typ.Name()
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return "*" + TypeName(typ.Elem())
	case reflect.Slice:
		return "[]" + TypeName(typ.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(typ.Len()) + "]" + TypeName(typ.Elem())
	case reflect.Map:
		return "map[" + TypeName(typ.Key()) + "]" + TypeName(typ.Elem())
	case reflect.Struct:
		for field := range typ.Fields() {
			if isSentinel(field) {
				return field.Name[len(isa.SynthesisedIDFieldPrefix):]
			}
		}
		return typ.String()
	default:
		return typ.String()
	}
}

// isSentinel reports whether field is the marker the interpreter adds to synthesised
// struct types to carry their declared name. It is not user data.
//
// Takes field (reflect.StructField) which is the field to inspect.
//
// Returns bool which is true for the marker field.
func isSentinel(field reflect.StructField) bool {
	return strings.HasPrefix(field.Name, isa.SynthesisedIDFieldPrefix)
}

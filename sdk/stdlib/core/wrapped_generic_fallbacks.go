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

package core

import (
	"fmt"
	"math"
	"reflect"
	"slices"
)

// reflectLessOrdered reports whether a is strictly less than b, where a and b are values
// of the same cmp.Ordered kind (integer, float, or string). Both must be valid and have
// the same Kind.
//
// Takes a (reflect.Value) which is the left operand.
// Takes b (reflect.Value) which is the right operand.
//
// Returns true if a < b under natural ordering.
//
// Panics if the kind is not ordered.
func reflectLessOrdered(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() < b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() < b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() < b.Float()
	case reflect.String:
		return a.String() < b.String()
	default:
		panic(fmt.Sprintf("ordered comparison: unsupported kind %s", a.Kind()))
	}
}

// reflectCompareOrdered returns -1/0/+1 comparing a and b under natural ordering,
// matching the contract of cmp.Compare.
//
// Takes a (reflect.Value) which is the left operand.
// Takes b (reflect.Value) which is the right operand.
//
// Returns -1 if a < b, +1 if a > b, 0 if equal. NaN handling matches cmp.Compare: NaN
// sorts before non-NaN.
//
// Panics if the kind is not ordered.
func reflectCompareOrdered(a, b reflect.Value) int {
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmpThreeWay(a.Int(), b.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cmpThreeWay(a.Uint(), b.Uint())
	case reflect.Float32, reflect.Float64:
		return cmpFloatThreeWay(a.Float(), b.Float())
	case reflect.String:
		return cmpThreeWay(a.String(), b.String())
	default:
		panic(fmt.Sprintf("cmp.Compare: unsupported kind %s", a.Kind()))
	}
}

// cmpThreeWay returns -1/0/+1 for the natural ordering of two values of the same
// totally-ordered type. NaN is not a concern here because this helper is only used for
// integer and string kinds.
//
// Takes av (T) which is the first value.
// Takes bv (T) which is the second value.
//
// Returns -1 if av < bv, +1 if av > bv, 0 if equal.
func cmpThreeWay[T int64 | uint64 | string](av, bv T) int {
	switch {
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

// cmpFloatThreeWay returns -1/0/+1 comparing two floats under the cmp.Compare contract,
// where NaN sorts before every non-NaN value and equal to itself.
//
// Takes av (float64) which is the first value.
// Takes bv (float64) which is the second value.
//
// Returns the comparison result with NaN ordered first.
func cmpFloatThreeWay(av, bv float64) int {
	aNaN := math.IsNaN(av)
	bNaN := math.IsNaN(bv)
	switch {
	case aNaN && bNaN:
		return 0
	case aNaN:
		return -1
	case bNaN:
		return 1
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

// requireSlice asserts that x is a slice value and returns its reflect.Value.
//
// Takes functionName (string) which is the panic message prefix.
// Takes x (any) which is the candidate value.
//
// Returns reflect.Value of x.
//
// Panics when x is not a slice; the panic message is prefixed with functionName.
func requireSlice(functionName string, x any) reflect.Value {
	rv := reflect.ValueOf(x)
	if rv.Kind() != reflect.Slice {
		panic(fmt.Sprintf("%s: unsupported type %T", functionName, x))
	}
	return rv
}

// reflectSlicesSort sorts a slice in place under the natural ordering of its element
// kind. Equivalent to slices.Sort but type-agnostic.
//
// Takes x (any) which must be a slice.
//
// Uses slices.SortFunc (pattern-defeating quicksort, unstable). The stdlib slices.Sort is
// also pdqsort but with type-specific inlining. For non-fast-path types interpreted code
// accepts the unstable guarantee (matches slices.Sort which is documented unstable).
func reflectSlicesSort(x any) {
	rv := requireSlice("slices.Sort", x)

	length := rv.Len()
	indices := make([]int, length)
	for i := range indices {
		indices[i] = i
	}
	slices.SortFunc(indices, func(i, j int) int {
		return reflectCompareOrdered(rv.Index(i), rv.Index(j))
	})
	sorted := reflect.MakeSlice(rv.Type(), length, length)
	for destination, source := range indices {
		sorted.Index(destination).Set(rv.Index(source))
	}
	reflect.Copy(rv, sorted)
}

// reflectSlicesMin returns the minimum element of a non-empty slice under natural
// ordering.
//
// Takes x (any) which must be a non-empty slice.
//
// Returns the minimum element.
//
// Panics if x is empty or element kind is not ordered.
func reflectSlicesMin(x any) any {
	rv := requireSlice("slices.Min", x)
	if rv.Len() == 0 {
		panic("slices.Min: empty slice")
	}
	best := rv.Index(0)
	for i := 1; i < rv.Len(); i++ {
		candidate := rv.Index(i)
		if reflectLessOrdered(candidate, best) {
			best = candidate
		}
	}
	return best.Interface()
}

// reflectSlicesMax returns the maximum element of a non-empty slice under natural
// ordering.
//
// Takes x (any) which must be a non-empty slice.
//
// Returns the maximum element.
//
// Panics if x is empty or element kind is not ordered.
func reflectSlicesMax(x any) any {
	rv := requireSlice("slices.Max", x)
	if rv.Len() == 0 {
		panic("slices.Max: empty slice")
	}
	best := rv.Index(0)
	for i := 1; i < rv.Len(); i++ {
		candidate := rv.Index(i)
		if reflectLessOrdered(best, candidate) {
			best = candidate
		}
	}
	return best.Interface()
}

// reflectSlicesBinarySearch searches a sorted slice for target, returning the insertion
// index and whether the value was found. Equivalent to slices.BinarySearch under natural
// ordering.
//
// Takes x (any) which must be a sorted slice.
// Takes target (any) which must match the slice element kind.
//
// Returns (index, found).
func reflectSlicesBinarySearch(x any, target any) (int, bool) {
	rv := requireSlice("slices.BinarySearch", x)
	targetVal := reflect.ValueOf(target)
	lo, hi := 0, rv.Len()
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if reflectLessOrdered(rv.Index(mid), targetVal) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < rv.Len() && reflectCompareOrdered(rv.Index(lo), targetVal) == 0 {
		return lo, true
	}
	return lo, false
}

// reflectSlicesIsSorted reports whether a slice is sorted in non-decreasing order under
// natural ordering.
//
// Takes x (any) which must be a slice.
//
// Returns true if x is sorted.
func reflectSlicesIsSorted(x any) bool {
	rv := requireSlice("slices.IsSorted", x)
	for i := 1; i < rv.Len(); i++ {
		if reflectLessOrdered(rv.Index(i), rv.Index(i-1)) {
			return false
		}
	}
	return true
}

// reflectSlicesCompare lexicographically compares two slices, returning -1/0/+1.
// Equivalent to slices.Compare under natural ordering.
//
// Takes a (any) which must be a slice.
// Takes b (any) which must be a slice of the same element kind.
//
// Returns the lexicographic comparison result.
func reflectSlicesCompare(a, b any) int {
	ra := requireSlice("slices.Compare", a)
	rb := requireSlice("slices.Compare", b)
	minLen := min(rb.Len(), ra.Len())
	for i := range minLen {
		if c := reflectCompareOrdered(ra.Index(i), rb.Index(i)); c != 0 {
			return c
		}
	}
	switch {
	case ra.Len() < rb.Len():
		return -1
	case ra.Len() > rb.Len():
		return 1
	default:
		return 0
	}
}

// reflectSlicesContains reports whether target is present in x using Go equality (==)
// semantics. Equivalent to slices.Contains.
//
// Takes x (any) which must be a slice.
// Takes target (any) which is the value to search for.
//
// Returns true if x contains target.
func reflectSlicesContains(x, target any) bool {
	rv := requireSlice("slices.Contains", x)
	for i := range rv.Len() {
		if rv.Index(i).Interface() == target {
			return true
		}
	}
	return false
}

// reflectSlicesEqual reports whether two slices are element-wise equal (Go ==).
//
// Takes a (any) which must be a slice.
// Takes b (any) which must be a slice of the same type.
//
// Returns true if a and b have the same length and equal elements.
func reflectSlicesEqual(a, b any) bool {
	ra := requireSlice("slices.Equal", a)
	rb := requireSlice("slices.Equal", b)
	if ra.Len() != rb.Len() {
		return false
	}
	for i := range ra.Len() {
		if ra.Index(i).Interface() != rb.Index(i).Interface() {
			return false
		}
	}
	return true
}

// reflectSlicesIndex returns the first index of target in x, or -1 when absent.
// Equivalent to slices.Index.
//
// Takes x (any) which must be a slice.
// Takes target (any) which is the value to search for.
//
// Returns the index of target, or -1 if not found.
func reflectSlicesIndex(x, target any) int {
	rv := requireSlice("slices.Index", x)
	for i := range rv.Len() {
		if rv.Index(i).Interface() == target {
			return i
		}
	}
	return -1
}

// reflectCmpCompare returns -1/0/+1 comparing a and b under natural ordering. Equivalent
// to cmp.Compare for any cmp.Ordered type.
//
// Takes a (any) which is the left operand.
// Takes b (any) which is the right operand.
//
// Returns the comparison result.
func reflectCmpCompare(a, b any) int {
	return reflectCompareOrdered(reflect.ValueOf(a), reflect.ValueOf(b))
}

// reflectCmpLess reports whether a is less than b under natural ordering. Equivalent to
// cmp.Less.
//
// Takes a (any) which is the left operand.
// Takes b (any) which is the right operand.
//
// Returns true if a < b.
func reflectCmpLess(a, b any) bool {
	return reflectLessOrdered(reflect.ValueOf(a), reflect.ValueOf(b))
}

// reflectCmpOr returns the first non-zero value, or the zero value of the element type if
// all are zero. Equivalent to cmp.Or for any comparable type.
//
// Takes vals (...any) any number of comparable values.
//
// Returns the first non-zero value, or the zero value when all are zero (or when vals is
// empty).
func reflectCmpOr(vals ...any) any {
	for _, v := range vals {
		if v == nil {
			continue
		}
		rv := reflect.ValueOf(v)
		if !rv.IsZero() {
			return v
		}
	}
	if len(vals) == 0 {
		return nil
	}
	return reflect.Zero(reflect.TypeOf(vals[0])).Interface()
}

// reflectSlicesClip returns a slice with the same length as s but with capacity equal to
// the length, so that subsequent appends will always allocate. Equivalent to slices.Clip.
//
// Takes x (any) which must be a slice.
//
// Returns the clipped slice.
func reflectSlicesClip(x any) any {
	rv := requireSlice("slices.Clip", x)
	return rv.Slice3(0, rv.Len(), rv.Len()).Interface()
}

// reflectSlicesClone returns a fresh copy of the input slice with the same backing-store
// contents but no shared identity. Equivalent to slices.Clone.
//
// Takes x (any) which must be a slice.
//
// Returns the cloned slice.
func reflectSlicesClone(x any) any {
	rv := requireSlice("slices.Clone", x)
	result := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	reflect.Copy(result, rv)
	return result.Interface()
}

// reflectSlicesReverse reverses x in place. Equivalent to slices.Reverse.
//
// Takes x (any) which must be a slice.
func reflectSlicesReverse(x any) {
	rv := requireSlice("slices.Reverse", x)
	swap := reflect.Swapper(x)
	for i, j := 0, rv.Len()-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}

// reflectSlicesConcat concatenates one or more slices into a new slice.
//
// All input slices must share the same element type. Equivalent to slices.Concat.
//
// Takes parts (...any) one or more slices.
//
// Returns the concatenated slice (or an untyped nil when no slices are supplied).
//
// Panics when any argument is not a slice, or when the element types disagree.
func reflectSlicesConcat(parts ...any) any {
	if len(parts) == 0 {
		return nil
	}
	first := requireSlice("slices.Concat", parts[0])
	sliceType := first.Type()
	total := first.Len()
	for i := 1; i < len(parts); i++ {
		rv := requireSlice("slices.Concat", parts[i])
		if rv.Type() != sliceType {
			panic(fmt.Sprintf("slices.Concat: mixed slice types %s and %s", sliceType, rv.Type()))
		}
		total += rv.Len()
	}
	result := reflect.MakeSlice(sliceType, 0, total)
	for _, p := range parts {
		result = reflect.AppendSlice(result, reflect.ValueOf(p))
	}
	return result.Interface()
}

// reflectMapsClone returns a fresh copy of the input map with the same key-value pairs
// but no shared identity. Equivalent to maps.Clone.
//
// Takes m (any) which must be a map.
//
// Returns the cloned map.
//
// Panics if m is not a map.
func reflectMapsClone(m any) any {
	rv := reflect.ValueOf(m)
	if rv.Kind() != reflect.Map {
		panic(fmt.Sprintf("maps.Clone: unsupported type %T", m))
	}
	result := reflect.MakeMapWithSize(rv.Type(), rv.Len())
	mapIter := rv.MapRange()
	for mapIter.Next() {
		result.SetMapIndex(mapIter.Key(), mapIter.Value())
	}
	return result.Interface()
}

// reflectMapsCopy copies all key-value pairs from source into destination, overwriting
// any pre-existing keys. Equivalent to maps.Copy.
//
// Takes destination (any) which must be a map to copy into.
// Takes source (any) which must be a map of the same key/value types.
//
// Panics if destination is not a map or source's type doesn't match destination's.
func reflectMapsCopy(destination, source any) {
	dstRV := reflect.ValueOf(destination)
	srcRV := reflect.ValueOf(source)
	if !srcRV.IsValid() || (srcRV.Kind() == reflect.Map && srcRV.Len() == 0) {
		return
	}
	if dstRV.Kind() != reflect.Map {
		panic(fmt.Sprintf("maps.Copy: destination is not a map: %T", destination))
	}
	if srcRV.Kind() != reflect.Map {
		panic(fmt.Sprintf("maps.Copy: source is not a map: %T", source))
	}
	mapIter := srcRV.MapRange()
	for mapIter.Next() {
		dstRV.SetMapIndex(mapIter.Key(), mapIter.Value())
	}
}

// reflectMapsEqual reports whether two maps have the same key set and same values per key
// compared with Go equality (==), matching maps.Equal.
//
// Takes a (any) which must be a map.
// Takes b (any) which must be a map of the same type.
//
// Returns true if the maps are key-value equal.
//
// Panics if either argument is not a map.
func reflectMapsEqual(a, b any) bool {
	ra := reflect.ValueOf(a)
	rb := reflect.ValueOf(b)
	if ra.Kind() != reflect.Map || rb.Kind() != reflect.Map {
		panic(fmt.Sprintf("maps.Equal: both arguments must be maps, got %T and %T", a, b))
	}
	if ra.Len() != rb.Len() {
		return false
	}
	mapIter := ra.MapRange()
	for mapIter.Next() {
		k := mapIter.Key()
		v := mapIter.Value()
		bv := rb.MapIndex(k)
		if !bv.IsValid() {
			return false
		}
		if v.Interface() != bv.Interface() {
			return false
		}
	}
	return true
}

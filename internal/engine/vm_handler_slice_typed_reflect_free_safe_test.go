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

//go:build safe

package engine

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type (
	plainSliceNamedElement int32
	plainSliceNamedBytes   []byte
	plainSliceHolder       struct {
		hidden []uint16
	}
)

type plainSliceRoute struct {
	name       string
	kinds      func(reflect.Kind) bool
	get        func(reflect.Value, int64) (any, bool)
	reflectGet func(reflect.Value) any
	set        func(reflect.Value, int64, int) bool
	reflectSet func(reflect.Value, int)
	writes     bool
}

type plainSliceCollection struct {
	name      string
	makeValue func() reflect.Value
	writable  bool
	declined  bool
}

func plainSliceRoutes() []plainSliceRoute {
	integer := func(kind reflect.Kind) bool { return isSignedIntKind(kind) || isUnsignedIntKind(kind) }
	float := func(kind reflect.Kind) bool { return kind == reflect.Float32 || kind == reflect.Float64 }
	return []plainSliceRoute{
		{
			name: "int", kinds: integer, writes: true,
			get: func(v reflect.Value, i int64) (any, bool) { return getElementInt64WithoutReflect(v, i) },
			reflectGet: func(e reflect.Value) any {
				if isUnsignedIntKind(e.Kind()) {
					return int64(e.Uint())
				}
				return e.Int()
			},
			set: func(v reflect.Value, i int64, seed int) bool {
				return setElementInt64WithoutReflect(nil, v, i, int64(seed)*-0x0123456789ABCDE)
			},
			reflectSet: func(e reflect.Value, seed int) {
				if isUnsignedIntKind(e.Kind()) {
					e.SetUint(uint64(int64(seed) * -0x0123456789ABCDE))
					return
				}
				e.SetInt(int64(seed) * -0x0123456789ABCDE)
			},
		},
		{
			name: "uint", kinds: isUnsignedIntKind, writes: true,
			get:        func(v reflect.Value, i int64) (any, bool) { return getElementUint64WithoutReflect(v, i) },
			reflectGet: func(e reflect.Value) any { return e.Uint() },
			set: func(v reflect.Value, i int64, seed int) bool {
				return setElementUint64WithoutReflect(nil, v, i, uint64(seed)*0xFEDCBA987654321)
			},
			reflectSet: func(e reflect.Value, seed int) { e.SetUint(uint64(seed) * 0xFEDCBA987654321) },
		},
		{
			name: "float", kinds: float, writes: true,
			get:        func(v reflect.Value, i int64) (any, bool) { return getElementFloat64WithoutReflect(v, i) },
			reflectGet: func(e reflect.Value) any { return e.Float() },
			set: func(v reflect.Value, i int64, seed int) bool {
				return setElementFloat64WithoutReflect(nil, v, i, float64(seed)/3)
			},
			reflectSet: func(e reflect.Value, seed int) { e.SetFloat(float64(seed) / 3) },
		},
		{
			name: "bool", kinds: func(kind reflect.Kind) bool { return kind == reflect.Bool }, writes: true,
			get:        func(v reflect.Value, i int64) (any, bool) { return getElementBoolWithoutReflect(v, i) },
			reflectGet: func(e reflect.Value) any { return e.Bool() },
			set: func(v reflect.Value, i int64, seed int) bool {
				return setElementBoolWithoutReflect(nil, v, i, seed%2 == 1)
			},
			reflectSet: func(e reflect.Value, seed int) { e.SetBool(seed%2 == 1) },
		},
		{
			name: "string", kinds: func(kind reflect.Kind) bool { return kind == reflect.String }, writes: false,
			get:        func(v reflect.Value, i int64) (any, bool) { return getElementStringWithoutReflect(v, i) },
			reflectGet: func(e reflect.Value) any { return e.String() },
			set: func(v reflect.Value, i int64, seed int) bool {
				return setElementStringWithoutReflect(nil, v, i, fmt.Sprint("element ", seed))
			},
			reflectSet: func(e reflect.Value, seed int) { e.SetString(fmt.Sprint("element ", seed)) },
		},
	}
}

func plainSliceCollections() []plainSliceCollection {
	values := func(v any) func() reflect.Value {
		return func() reflect.Value { return reflect.ValueOf(v).Elem() }
	}
	return []plainSliceCollection{
		{name: "[]int", makeValue: values(&[]int{0, -1, 1<<63 - 1, -1 << 63}), writable: true},
		{name: "[]int8", makeValue: values(&[]int8{0, -1, 0x7F, -0x80}), writable: true},
		{name: "[]int16", makeValue: values(&[]int16{0, -1, 0x7FFF, -0x8000}), writable: true},
		{name: "[]int32", makeValue: values(&[]int32{0, -1, 0x7FFFFFFF, -0x80000000}), writable: true},
		{name: "[]int64", makeValue: values(&[]int64{0, -1, 1<<63 - 1, -1 << 63}), writable: true},
		{name: "[]uint", makeValue: values(&[]uint{0, 1, ^uint(0)}), writable: true},
		{name: "[]uint8", makeValue: values(&[]uint8{0, 1, 0x7F, 0x80, 0xFF}), writable: true},
		{name: "[]uint16", makeValue: values(&[]uint16{0, 1, 0x8000, 0xFFFF}), writable: true},
		{name: "[]uint32", makeValue: values(&[]uint32{0, 1, 0x80000000, 0xFFFFFFFF}), writable: true},
		{name: "[]uint64", makeValue: values(&[]uint64{0, 1, 1 << 63, ^uint64(0)}), writable: true},
		{name: "[]uintptr", makeValue: values(&[]uintptr{0, 1, ^uintptr(0)}), writable: true},
		{name: "[]float32", makeValue: values(&[]float32{0, -1.5, 3.25}), writable: true},
		{name: "[]float64", makeValue: values(&[]float64{0, -1.5, 1e300}), writable: true},
		{name: "[]bool", makeValue: values(&[]bool{true, false, true}), writable: true},
		{name: "[]string", makeValue: values(&[]string{"", "one", "two"}), writable: true},
		{name: "non-addressable slice", makeValue: func() reflect.Value { return reflect.ValueOf([]uint8{1, 2, 3}) }, writable: true},
		{name: "named element", makeValue: values(&[]plainSliceNamedElement{-7, 8}), writable: true},
		{name: "named slice", makeValue: values(&plainSliceNamedBytes{9, 10}), writable: true},
		{name: "array of uint8", makeValue: values(&[3]uint8{1, 2, 3}), writable: true},
		{name: "array of strings", makeValue: values(&[2]string{"a", "b"}), writable: true},
		{name: "pointer to array", makeValue: func() reflect.Value { return reflect.ValueOf(&[3]float64{1, 2, 3}) }, writable: true},
		{name: "non-addressable array", makeValue: func() reflect.Value { return reflect.ValueOf([3]int16{4, 5, 6}) }, writable: false},
		{name: "read-only slice", makeValue: func() reflect.Value {
			return reflect.ValueOf(plainSliceHolder{hidden: []uint16{7, 8}}).Field(0)
		}, writable: false},
		{name: "interface elements", makeValue: values(&[]any{uint8(1), "x"}), declined: true},
		{name: "pointer elements", makeValue: values(&[]*int{nil}), declined: true},
		{name: "complex elements", makeValue: values(&[]complex128{1}), declined: true},
		{name: "nil pointer to array", makeValue: func() reflect.Value { return reflect.ValueOf((*[2]int)(nil)) }, declined: true},
		{name: "a map", makeValue: func() reflect.Value { return reflect.ValueOf(map[int]int{0: 1}) }, declined: true},
		{name: "no value", makeValue: func() reflect.Value { return reflect.Value{} }, declined: true},
	}
}

func plainSliceElements(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().Kind() == reflect.Array {
		return v.Elem()
	}
	return v
}

func TestPlainSliceElementRoutesReadAsReflectDoes(t *testing.T) {
	for _, collection := range plainSliceCollections() {
		for _, route := range plainSliceRoutes() {
			value := collection.makeValue()
			length := int64(0)
			var elementKind reflect.Kind
			if !collection.declined {
				elements := plainSliceElements(value)
				length, elementKind = int64(elements.Len()), elements.Type().Elem().Kind()
			}
			for index := int64(-2); index <= length+1; index++ {
				got, ok := route.get(value, index)
				want := !collection.declined && index >= 0 && index < length && route.kinds(elementKind)
				require.Equal(t, want, ok, "%s, %s route, index %d", collection.name, route.name, index)
				if ok {
					require.Equal(t, route.reflectGet(plainSliceElements(value).Index(int(index))), got,
						"%s, %s route, index %d", collection.name, route.name, index)
				}
			}
		}
	}
}

func TestPlainSliceElementRoutesWriteAsReflectDoes(t *testing.T) {
	for _, collection := range plainSliceCollections() {
		for _, route := range plainSliceRoutes() {
			routed, reflected := collection.makeValue(), collection.makeValue()
			if collection.declined {
				require.False(t, route.set(routed, 0, 1), "%s, %s route", collection.name, route.name)
				continue
			}
			elements := plainSliceElements(routed)
			length, elementKind := elements.Len(), elements.Type().Elem().Kind()
			for index := -1; index <= length; index++ {
				ok := route.set(routed, int64(index), index+3)
				want := route.writes && collection.writable && index >= 0 && index < length && route.kinds(elementKind)
				require.Equal(t, want, ok, "%s, %s route, index %d", collection.name, route.name, index)
				if ok {
					route.reflectSet(plainSliceElements(reflected).Index(index), index+3)
				}
			}
			require.Equal(t, fmt.Sprint(plainSliceElements(reflected)), fmt.Sprint(plainSliceElements(routed)),
				"%s, %s route: the routed writes are the setters' writes", collection.name, route.name)
		}
	}
}

func TestPlainSliceElementRoutesDoNotAllocate(t *testing.T) {
	bytes := reflect.ValueOf(&[]uint8{1, 2, 3}).Elem()
	ints := reflect.ValueOf(&[3]int32{1, 2, 3}).Elem()
	allocations := testing.AllocsPerRun(100, func() {
		_, _ = getElementUint64WithoutReflect(bytes, 1)
		_ = setElementUint64WithoutReflect(nil, bytes, 2, 7)
		_, _ = getElementInt64WithoutReflect(ints, 1)
		_ = setElementInt64WithoutReflect(nil, ints, 2, -7)
	})
	require.Zero(t, allocations)
}

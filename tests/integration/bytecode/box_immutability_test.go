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
	"pipit.sh/pipit/sdk/stdlib"
)

const boxImmutabilitySource = `package main

type N int
type U uint64
type F float64
type C complex128
type S string

type cell struct{ V N }

var (
	keptN     N
	keptAny   any
	keptAnys  []any
	keptMap   map[string]N
	keptPtr   *N
	keptCell  cell
	keptChan  chan N
	keptS     S
	keptF     F
	keptC     C
	keptU     U
)

func AliasAfterAddr() int {
	t := N(1000)
	u := t
	p := &t
	*p = 5
	return int(u)*10000 + int(t)
}

func GlobalThenWriteThroughPointer() int {
	t := N(1000)
	keptN = t
	p := &t
	*p += 1
	return int(keptN)*10000 + int(t)
}

func InterfaceGlobalThenIncrement() int {
	t := N(1000)
	keptAny = t
	t++
	return int(keptAny.(N))*10000 + int(t)
}

func InterfaceGlobalThenWriteThroughPointer() int {
	t := N(1000)
	keptAny = t
	p := &t
	*p += 7
	return int(keptAny.(N))*10000 + int(t)
}

func SliceOfAnyThenWriteThroughPointer() int {
	t := N(1000)
	keptAnys = append([]any(nil), t)
	p := &t
	*p = 3
	return int(keptAnys[0].(N))*10000 + int(t)
}

func MapThenWriteThroughPointer() int {
	t := N(1000)
	keptMap = map[string]N{}
	keptMap["k"] = t
	p := &t
	*p = 4
	return int(keptMap["k"])*10000 + int(t)
}

func StructFieldThenWriteThroughPointer() int {
	t := N(1000)
	keptCell = cell{V: t}
	p := &t
	*p = 6
	return int(keptCell.V)*10000 + int(t)
}

func ChannelThenWriteThroughPointer() int {
	t := N(1000)
	keptChan = make(chan N, 1)
	keptChan <- t
	p := &t
	*p = 8
	got := <-keptChan
	return int(got)*10000 + int(t)
}

func PointerKeptThenReturned() N {
	t := N(1000)
	keptPtr = &t
	return t
}

func BumpKeptPointer() N {
	*keptPtr += 10
	return *keptPtr
}

func LoopVariablesStayDistinct() int {
	keptAnys = nil
	for i := N(1000); i < 1003; i++ {
		keptAnys = append(keptAnys, i)
	}
	sum := 0
	for _, v := range keptAnys {
		sum = sum*10 + int(v.(N))-999
	}
	return sum
}

func ClosureIncrementAfterHandOff() int {
	t := N(1000)
	keptAny = t
	bump := func() { t++ }
	bump()
	bump()
	return int(keptAny.(N))*10000 + int(t)
}

func GoroutineArgumentThenWrite() int {
	t := N(1000)
	done := make(chan N)
	p := &t
	go func(v N) {
		done <- v
	}(t)
	*p = 9
	got := <-done
	return int(got)*10000 + int(t)
}

func StringBoxThenWrite() string {
	s := S("a string long enough to box")
	keptS = s
	p := &s
	*p = "x"
	return string(keptS) + "|" + string(s)
}

func FloatBoxThenWrite() float64 {
	f := F(1234.5)
	keptAny = f
	p := &f
	*p = 2
	return float64(keptAny.(F))*10000 + float64(f)
}

func ComplexBoxThenWrite() complex128 {
	c := C(complex(1000, 3))
	keptC = c
	p := &c
	*p = 1
	return complex128(keptC)*10000 + complex128(c)
}

func UintBoxThenWrite() uint64 {
	u := U(1 << 40)
	keptU = u
	p := &u
	*p = 1
	return uint64(keptU) + uint64(u)
}
type bumper interface{ Bump() int }

func (n N) Bump() int { p := &n; *p += 1; return int(n) }
func (u U) Bump() int { p := &u; *p += 1; return int(u) }
func (f F) Bump() int { p := &f; *p += 1; return int(f) }
func (c C) Bump() int { p := &c; *p += 1; return int(real(c)) }
func (s S) Bump() int { p := &s; *p += "!"; return len(s) }

func InterfaceMethodWritesItsOwnCopy() int {
	var b bumper = N(1000)
	x := b.Bump()
	y := b.Bump()
	return int(b.(N))*10000 + x + y
}

func InterfaceCopiesStayDistinct() int {
	var a any = N(1000)
	d := a
	a.(bumper).Bump()
	return int(a.(N))*10000 + int(d.(N))
}

func HandedOffInterfaceThenMethod() int {
	var b bumper = N(1000)
	keptAny = b
	b.Bump()
	return int(keptAny.(N))*10000 + int(b.(N))
}

func EveryBoxKindThroughAMethod() int {
	values := []bumper{N(1000), U(1 << 40), F(1000.5), C(complex(1000, 1)), S("a string long enough to box")}
	kept := make([]any, len(values))
	copy(kept, []any{values[0], values[1], values[2], values[3], values[4]})
	sum := 0
	for _, v := range values {
		v.Bump()
		sum = sum*2 + v.Bump()
	}
	same := 0
	for i, v := range values {
		if kept[i] == any(v) {
			same++
		}
	}
	return sum*10 + same
}

func GenericCallee[T any](t T, z T) T { p := &t; *p = z; return t }

func GenericCallerKeepsItsValue() int {
	var a any = N(1000)
	keptAny = a
	GenericCallee[any](a, N(5))
	return int(keptAny.(N))*10000 + int(a.(N))
}
`

type boxN int

type boxU uint64

type boxF float64

type boxC complex128

type boxS string

type boxCell struct{ V boxN }

var (
	nativeBoxKeptN    boxN
	nativeBoxKeptAny  any
	nativeBoxKeptAnys []any
	nativeBoxKeptMap  map[string]boxN
	nativeBoxKeptPtr  *boxN
	nativeBoxKeptCell boxCell
	nativeBoxKeptChan chan boxN
	nativeBoxKeptS    boxS
	nativeBoxKeptC    boxC
	nativeBoxKeptU    boxU
)

type boxBumper interface{ Bump() int }

func nativeBoxAliasAfterAddr() int {
	t := boxN(1000)
	copied := t
	pointer := &t
	*pointer = 5
	return int(copied)*10000 + int(t)
}

func nativeBoxGlobalThenWriteThroughPointer() int {
	t := boxN(1000)
	nativeBoxKeptN = t
	pointer := &t
	*pointer += 1
	return int(nativeBoxKeptN)*10000 + int(t)
}

func nativeBoxInterfaceGlobalThenIncrement() int {
	t := boxN(1000)
	nativeBoxKeptAny = t
	t++
	return int(nativeBoxKeptAny.(boxN))*10000 + int(t)
}

func nativeBoxInterfaceGlobalThenWriteThroughPointer() int {
	t := boxN(1000)
	nativeBoxKeptAny = t
	pointer := &t
	*pointer += 7
	return int(nativeBoxKeptAny.(boxN))*10000 + int(t)
}

func nativeBoxSliceOfAnyThenWriteThroughPointer() int {
	t := boxN(1000)
	nativeBoxKeptAnys = append([]any(nil), t)
	pointer := &t
	*pointer = 3
	return int(nativeBoxKeptAnys[0].(boxN))*10000 + int(t)
}

func nativeBoxMapThenWriteThroughPointer() int {
	t := boxN(1000)
	nativeBoxKeptMap = map[string]boxN{}
	nativeBoxKeptMap["k"] = t
	pointer := &t
	*pointer = 4
	return int(nativeBoxKeptMap["k"])*10000 + int(t)
}

func nativeBoxStructFieldThenWriteThroughPointer() int {
	t := boxN(1000)
	nativeBoxKeptCell = boxCell{V: t}
	pointer := &t
	*pointer = 6
	return int(nativeBoxKeptCell.V)*10000 + int(t)
}

func nativeBoxChannelThenWriteThroughPointer() int {
	t := boxN(1000)
	nativeBoxKeptChan = make(chan boxN, 1)
	nativeBoxKeptChan <- t
	pointer := &t
	*pointer = 8
	got := <-nativeBoxKeptChan
	return int(got)*10000 + int(t)
}

func nativeBoxPointerKeptThenReturned() boxN {
	t := boxN(1000)
	nativeBoxKeptPtr = &t
	return t
}

func nativeBoxBumpKeptPointer() boxN {
	*nativeBoxKeptPtr += 10
	return *nativeBoxKeptPtr
}

func nativeBoxLoopVariablesStayDistinct() int {
	nativeBoxKeptAnys = nil
	for i := boxN(1000); i < 1003; i++ {
		nativeBoxKeptAnys = append(nativeBoxKeptAnys, i)
	}
	sum := 0
	for _, v := range nativeBoxKeptAnys {
		sum = sum*10 + int(v.(boxN)) - 999
	}
	return sum
}

func nativeBoxClosureIncrementAfterHandOff() int {
	t := boxN(1000)
	nativeBoxKeptAny = t
	bump := func() { t++ }
	bump()
	bump()
	return int(nativeBoxKeptAny.(boxN))*10000 + int(t)
}

func nativeBoxGoroutineArgumentThenWrite() int {
	t := boxN(1000)
	done := make(chan boxN)
	pointer := &t
	go func(v boxN) {
		done <- v
	}(t)
	*pointer = 9
	got := <-done
	return int(got)*10000 + int(t)
}

func nativeBoxStringBoxThenWrite() string {
	boxed := boxS("a string long enough to box")
	nativeBoxKeptS = boxed
	pointer := &boxed
	*pointer = "x"
	return string(nativeBoxKeptS) + "|" + string(boxed)
}

func nativeBoxFloatBoxThenWrite() float64 {
	boxed := boxF(1234.5)
	nativeBoxKeptAny = boxed
	pointer := &boxed
	*pointer = 2
	return float64(nativeBoxKeptAny.(boxF))*10000 + float64(boxed)
}

func nativeBoxComplexBoxThenWrite() complex128 {
	boxed := boxC(complex(1000, 3))
	nativeBoxKeptC = boxed
	pointer := &boxed
	*pointer = 1
	return complex128(nativeBoxKeptC)*10000 + complex128(boxed)
}

func nativeBoxUintBoxThenWrite() uint64 {
	boxed := boxU(1 << 40)
	nativeBoxKeptU = boxed
	pointer := &boxed
	*pointer = 1
	return uint64(nativeBoxKeptU) + uint64(boxed)
}

func (n boxN) Bump() int { p := &n; *p += 1; return int(n) }

func (u boxU) Bump() int { p := &u; *p += 1; return int(u) }

func (f boxF) Bump() int { p := &f; *p += 1; return int(f) }

func (c boxC) Bump() int { p := &c; *p += 1; return int(real(c)) }

func (s boxS) Bump() int { p := &s; *p += "!"; return len(s) }

func nativeBoxInterfaceMethodWritesItsOwnCopy() int {
	var b boxBumper = boxN(1000)
	x := b.Bump()
	y := b.Bump()
	return int(b.(boxN))*10000 + x + y
}

func nativeBoxInterfaceCopiesStayDistinct() int {
	var a any = boxN(1000)
	d := a
	a.(boxBumper).Bump()
	return int(a.(boxN))*10000 + int(d.(boxN))
}

func nativeBoxHandedOffInterfaceThenMethod() int {
	var b boxBumper = boxN(1000)
	nativeBoxKeptAny = b
	b.Bump()
	return int(nativeBoxKeptAny.(boxN))*10000 + int(b.(boxN))
}

func nativeBoxEveryBoxKindThroughAMethod() int {
	values := []boxBumper{boxN(1000), boxU(1 << 40), boxF(1000.5), boxC(complex(1000, 1)), boxS("a string long enough to box")}
	kept := make([]any, len(values))
	copy(kept, []any{values[0], values[1], values[2], values[3], values[4]})
	sum := 0
	for _, v := range values {
		v.Bump()
		sum = sum*2 + v.Bump()
	}
	same := 0
	for i, v := range values {
		if kept[i] == any(v) {
			same++
		}
	}
	return sum*10 + same
}

func nativeBoxGenericCallee[T any](t T, z T) T { p := &t; *p = z; return t }

func nativeBoxGenericCallerKeepsItsValue() int {
	var a any = boxN(1000)
	nativeBoxKeptAny = a
	nativeBoxGenericCallee[any](a, boxN(5))
	return int(nativeBoxKeptAny.(boxN))*10000 + int(a.(boxN))
}

func boxProbeValue(value any) any {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int64:
		return reflected.Int()
	case reflect.Uint, reflect.Uint64:
		return reflected.Uint()
	case reflect.Float64:
		return reflected.Float()
	case reflect.Complex128:
		return reflected.Complex()
	case reflect.String:
		return reflected.String()
	default:
		return value
	}
}

func TestBoxImmutabilityMatchesNativeGo(t *testing.T) {
	service := app.NewService()
	service.UseSymbolProviders(stdlib.Providers()...)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": boxImmutabilitySource})
	require.NoError(t, err, "compiling the box probes")
	probes := []struct {
		name string
		want any
	}{
		{"AliasAfterAddr", nativeBoxAliasAfterAddr()},
		{"GlobalThenWriteThroughPointer", nativeBoxGlobalThenWriteThroughPointer()},
		{"InterfaceGlobalThenIncrement", nativeBoxInterfaceGlobalThenIncrement()},
		{"InterfaceGlobalThenWriteThroughPointer", nativeBoxInterfaceGlobalThenWriteThroughPointer()},
		{"SliceOfAnyThenWriteThroughPointer", nativeBoxSliceOfAnyThenWriteThroughPointer()},
		{"MapThenWriteThroughPointer", nativeBoxMapThenWriteThroughPointer()},
		{"StructFieldThenWriteThroughPointer", nativeBoxStructFieldThenWriteThroughPointer()},
		{"ChannelThenWriteThroughPointer", nativeBoxChannelThenWriteThroughPointer()},
		{"LoopVariablesStayDistinct", nativeBoxLoopVariablesStayDistinct()},
		{"ClosureIncrementAfterHandOff", nativeBoxClosureIncrementAfterHandOff()},
		{"GoroutineArgumentThenWrite", nativeBoxGoroutineArgumentThenWrite()},
		{"StringBoxThenWrite", nativeBoxStringBoxThenWrite()},
		{"FloatBoxThenWrite", nativeBoxFloatBoxThenWrite()},
		{"ComplexBoxThenWrite", nativeBoxComplexBoxThenWrite()},
		{"UintBoxThenWrite", nativeBoxUintBoxThenWrite()},
		{"InterfaceMethodWritesItsOwnCopy", nativeBoxInterfaceMethodWritesItsOwnCopy()},
		{"InterfaceCopiesStayDistinct", nativeBoxInterfaceCopiesStayDistinct()},
		{"HandedOffInterfaceThenMethod", nativeBoxHandedOffInterfaceThenMethod()},
		{"EveryBoxKindThroughAMethod", nativeBoxEveryBoxKindThroughAMethod()},
		{"GenericCallerKeepsItsValue", nativeBoxGenericCallerKeepsItsValue()},
	}
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			call := copyRuleCall(t, service, compiled)
			require.Equal(t, boxProbeValue(probe.want), boxProbeValue(call(probe.name)))
		})
	}

	t.Run("ReturnedValueSurvivesALaterWriteThroughAKeptPointer", func(t *testing.T) {
		call := copyRuleCall(t, service, compiled)
		returned := boxProbeValue(call("PointerKeptThenReturned"))
		bumped := boxProbeValue(call("BumpKeptPointer"))
		nativeReturned := nativeBoxPointerKeptThenReturned()
		nativeBumped := nativeBoxBumpKeptPointer()
		require.Equal(t, boxProbeValue(nativeReturned), returned, "the host's copy must not see the write")
		require.Equal(t, boxProbeValue(nativeBumped), bumped)
	})
}

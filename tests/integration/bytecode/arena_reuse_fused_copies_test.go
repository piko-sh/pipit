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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

const fusedCopySource = `package main

import "strconv"

type U struct {
	S string
	N int
}

type T struct {
	A any
	P *U
	I []int
	M map[string]string
	F []string
}

var G = &T{}

var GS = make([]any, 2)

var GU = make([]U, 2)

var GI = make([][]int, 2)

func GrantedToGlobal(n int) int {
	s := &T{}
	s.A = "a" + strconv.Itoa(n) + "-fused-copy-interface-field"
	s.P = &U{S: "p" + strconv.Itoa(n) + "-fused-copy-pointer-field", N: n}
	s.I = []int{n, n + 1, n + 2, n + 3}
	s.M = map[string]string{"k": "m" + strconv.Itoa(n) + "-fused-copy-map-field"}
	G.A = s.A
	G.P = s.P
	G.I = s.I
	G.M = s.M
	return n
}

func LocalToGlobal(n int) int {
	var s T
	s.A = "b" + strconv.Itoa(n) + "-fused-copy-interface-field"
	s.I = []int{n * 2, n * 3}
	G.A = s.A
	G.I = s.I
	return n
}

func ElementToHost(dst *T, n int) int {
	ts := make([]T, 2)
	ts[1].A = "c" + strconv.Itoa(n) + "-fused-copy-into-host-struct"
	ts[1].I = []int{n, -n}
	dst.A = ts[1].A
	dst.I = ts[1].I
	return n
}

func CopyAny(n int) int {
	local := []any{"d" + strconv.Itoa(n) + "-copy-builtin-any", n}
	return copy(GS, local)
}

func CopyStructs(n int) int {
	local := []U{{S: "e" + strconv.Itoa(n) + "-copy-builtin-struct", N: n}, {S: "f" + strconv.Itoa(n) + "-copy-builtin-struct"}}
	return copy(GU, local)
}

func CopyInts(n int) int {
	local := [][]int{{n, n + 1}, {n + 2}}
	return copy(GI, local)
}

func CopyIntoHost(dst []any, n int) int {
	local := []any{"g" + strconv.Itoa(n) + "-copy-builtin-into-host", strconv.Itoa(n) + "-second"}
	return copy(dst, local)
}

func use(t *T) int { return len(t.I) }

func LiteralGranted(n int) int {
	s := T{}
	use(&s)
	s.A = "h" + strconv.Itoa(n) + "-granted-literal-interface"
	s.I = []int{n, n * 5}
	s.P = &U{S: "q" + strconv.Itoa(n) + "-granted-literal-pointer", N: n}
	G.A = s.A
	G.I = s.I
	G.P = s.P
	return use(&s)
}

func LiteralPlain(n int) int {
	s := T{}
	s.A = "i" + strconv.Itoa(n) + "-plain-literal-interface"
	s.I = []int{n, n * 7}
	G.A = s.A
	G.I = s.I
	return len(s.I)
}

func PointerLiteral(n int) int {
	s := &T{}
	use(s)
	s.A = "j" + strconv.Itoa(n) + "-pointer-literal-interface"
	s.I = []int{n, n * 9}
	G.A = s.A
	G.I = s.I
	return use(s)
}

type V struct {
	Q []U
	P *U
	M map[string]string
	X []int
}

var GV = &V{}

func VGranted(n int) int {
	v := V{}
	v.Q = []U{{S: "r" + strconv.Itoa(n) + "-granted-slice-of-structs", N: n}}
	v.P = &U{S: "s" + strconv.Itoa(n) + "-granted-pointer", N: n}
	v.M = map[string]string{"k": "t" + strconv.Itoa(n) + "-granted-map"}
	v.X = []int{n, n + 1, n + 2, n + 3, n + 4, n + 5}
	GV.Q = v.Q
	GV.P = v.P
	GV.M = v.M
	GV.X = v.X
	return len(v.Q) + len(v.X)
}

func ReadV() string {
	text := ""
	for _, u := range GV.Q {
		text += u.S + "/" + strconv.Itoa(u.N)
	}
	if GV.P != nil {
		text += "|" + GV.P.S
	}
	if GV.M != nil {
		text += "|" + GV.M["k"]
	}
	for _, x := range GV.X {
		text += "," + strconv.Itoa(x)
	}
	return text
}

func Read() string {
	text := ""
	if G.A != nil {
		text += G.A.(string)
	}
	if G.P != nil {
		text += "|" + G.P.S
	}
	text += "|" + strconv.Itoa(len(G.I))
	for _, v := range G.I {
		text += "," + strconv.Itoa(v)
	}
	if G.M != nil {
		text += "|" + G.M["k"]
	}
	return text
}

func ReadCopies() string {
	text := ""
	for _, v := range GS {
		if s, ok := v.(string); ok {
			text += s
		}
		if i, ok := v.(int); ok {
			text += "/" + strconv.Itoa(i)
		}
	}
	for _, u := range GU {
		text += "|" + u.S + strconv.Itoa(u.N)
	}
	for _, xs := range GI {
		for _, x := range xs {
			text += "," + strconv.Itoa(x)
		}
	}
	return text
}
`

type fusedCopyHostT struct {
	A any
	P *struct {
		S string
		N int
	}
	I []int
	M map[string]string
	F []string
}

func fusedCopyRead(seed int, local bool) string {
	if local {
		return fmt.Sprintf("b%d-fused-copy-interface-field|p41-fused-copy-pointer-field|2,%d,%d|m41-fused-copy-map-field",
			seed, seed*2, seed*3)
	}
	return fmt.Sprintf("a%d-fused-copy-interface-field|p%d-fused-copy-pointer-field|4,%d,%d,%d,%d|m%d-fused-copy-map-field",
		seed, seed, seed, seed+1, seed+2, seed+3, seed)
}

func TestFusedFieldCopyAndCopyBuiltinSurviveReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled, counts := compileCountingMnemonics(t, service, fusedCopySource, "COPY_STRUCT_FIELD_GENERAL", "COPY_GENERAL")
	observations := &hazardObservations{}
	observations.note("listing: COPY_STRUCT_FIELD_GENERAL=%d COPY_GENERAL=%d", counts["COPY_STRUCT_FIELD_GENERAL"], counts["COPY_GENERAL"])
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")

	hazardCall(t, service, compiled, "GrantedToGlobal", 41)
	compareRendering(observations, "Read() straight after GrantedToGlobal(41)", fusedCopyRead(41, false), hazardCall(t, service, compiled, "Read"))
	clobberArenas(t, service, 8)
	compareRendering(observations, "Read() after GrantedToGlobal(41) and 8 clobber runs", fusedCopyRead(41, false), hazardCall(t, service, compiled, "Read"))

	hazardCall(t, service, compiled, "LocalToGlobal", 42)
	clobberArenas(t, service, 8)
	compareRendering(observations, "Read() after LocalToGlobal(42) and 8 clobber runs", fusedCopyRead(42, true), hazardCall(t, service, compiled, "Read"))

	hazardCall(t, service, compiled, "CopyAny", 43)
	hazardCall(t, service, compiled, "CopyStructs", 43)
	hazardCall(t, service, compiled, "CopyInts", 43)
	wantCopies := "d43-copy-builtin-any/43|e43-copy-builtin-struct43|f43-copy-builtin-struct0,43,44,45"
	compareRendering(observations, "ReadCopies() straight after the copies", wantCopies, hazardCall(t, service, compiled, "ReadCopies"))
	clobberArenas(t, service, 8)
	compareRendering(observations, "ReadCopies() after 8 clobber runs", wantCopies, hazardCall(t, service, compiled, "ReadCopies"))

	hazardCall(t, service, compiled, "VGranted", 46)
	wantV := "r46-granted-slice-of-structs/46|s46-granted-pointer|t46-granted-map,46,47,48,49,50,51"
	compareRendering(observations, "ReadV() straight after VGranted(46)", wantV, hazardCall(t, service, compiled, "ReadV"))
	clobberArenas(t, service, 8)
	compareRendering(observations, "ReadV() after VGranted(46) and 8 clobber runs", wantV, hazardCall(t, service, compiled, "ReadV"))

	hostSlice := make([]any, 2)
	hazardCall(t, service, compiled, "CopyIntoHost", hostSlice, 44)
	wantHost := fmt.Sprint([]any{"g44-copy-builtin-into-host", "44-second"})
	compareRendering(observations, "host slice straight after CopyIntoHost(44)", wantHost, fmt.Sprint(hostSlice))
	clobberArenas(t, service, 8)
	compareRendering(observations, "host slice after 8 clobber runs", wantHost, fmt.Sprint(hostSlice))

	window.settle(observations)
	runtime.KeepAlive(hostSlice)
	assertNoHazard(t, observations, missingMnemonic(counts, "COPY_STRUCT_FIELD_GENERAL"))
}

func TestFusedFieldCopyIntoAHostStruct(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled, counts := compileCountingMnemonics(t, service, fusedCopySource, "COPY_STRUCT_FIELD_GENERAL")
	observations := &hazardObservations{}
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")
	host := &fusedCopyHostT{}
	result, err := service.CallFunction(t.Context(), compiled, "ElementToHost", host, 45)
	if err != nil {
		observations.note("ElementToHost refused a host *T: %v", err)
		assertNoHazard(t, observations, "the host struct type is not the guest's T")
		return
	}
	observations.note("ElementToHost returned %v (%s)", result, reflect.TypeOf(result))
	render := func() string { return fmt.Sprintf("%v|%v|%v|%v|%v", host.A, host.P, host.I, host.M, host.F) }
	want := "c45-fused-copy-into-host-struct|<nil>|[45 -45]|map[]|[]"
	compareRendering(observations, "host struct straight after ElementToHost(45)", want, render())
	clobberArenas(t, service, 8)
	compareRendering(observations, "host struct after ElementToHost(45) and 8 clobber runs", want, render())
	window.settle(observations)
	runtime.KeepAlive(host)
	assertNoHazard(t, observations, missingMnemonic(counts, "COPY_STRUCT_FIELD_GENERAL"))
}

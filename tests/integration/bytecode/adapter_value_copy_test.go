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

//go:build integration && !(js && wasm)

package bytecode_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

const adapterValueCopySource = `package main

import "adapthost"

type Lab struct {
	Name string
	N    int
}

func (l Lab) String() string { return l.Name }

func (l Lab) Error() string { return "error:" + l.Name }

func (l Lab) MarshalJSON() ([]byte, error) { return []byte("\"" + l.Name + "\""), nil }

type Pair [2]string

func (p Pair) String() string { return p[0] + "+" + p[1] }

type Ref struct{ Name string }

func (r *Ref) String() string { return r.Name }

func Stringer() int {
	l := Lab{Name: "first", N: 1}
	adapthost.KeepStringer(l)
	l.Name = "changed"
	return l.N
}

func ErrorValue() int {
	l := Lab{Name: "first", N: 2}
	adapthost.KeepError(l)
	l.Name = "changed"
	return l.N
}

func Marshaler() int {
	l := Lab{Name: "first", N: 3}
	adapthost.KeepMarshaler(l)
	l.Name = "changed"
	return l.N
}

func Array() int {
	p := Pair{"a", "b"}
	adapthost.KeepStringer(p)
	p[0] = "changed"
	return len(p)
}

func Captured() int {
	l := Lab{Name: "first", N: 4}
	set := func(s string) { l.Name = s }
	adapthost.KeepStringer(l)
	set("changed")
	return l.N
}

func Pointer() int {
	r := &Ref{Name: "first"}
	adapthost.KeepStringer(r)
	r.Name = "changed"
	return 0
}
`

type adapterHost struct {
	stringers  []fmt.Stringer
	errs       []error
	marshalers []json.Marshaler
}

func TestInterfaceAdaptersCopyTheValueTheGuestConverted(t *testing.T) {
	host := &adapterHost{}
	exports := maps.Clone(stdlib.Exports())
	exports["adapthost"] = map[string]reflect.Value{
		"KeepStringer":  reflect.ValueOf(func(s fmt.Stringer) { host.stringers = append(host.stringers, s) }),
		"KeepError":     reflect.ValueOf(func(err error) { host.errs = append(host.errs, err) }),
		"KeepMarshaler": reflect.ValueOf(func(m json.Marshaler) { host.marshalers = append(host.marshalers, m) }),
	}
	service := app.NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": adapterValueCopySource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))

	call := func(name string) {
		t.Helper()
		_, err := service.CallFunction(t.Context(), compiled, name)
		require.NoError(t, err, "calling %s", name)
	}

	call("Stringer")
	require.Len(t, host.stringers, 1)
	require.Equal(t, "first", host.stringers[0].String(), "a kept Stringer holds the value converted, as in Go")

	call("ErrorValue")
	require.Len(t, host.errs, 1)
	require.Equal(t, "error:first", host.errs[0].Error(), "a kept error holds the value converted")

	call("Marshaler")
	require.Len(t, host.marshalers, 1)
	encoded, err := host.marshalers[0].MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, `"first"`, string(encoded), "a kept json.Marshaler holds the value converted")

	call("Array")
	require.Len(t, host.stringers, 2)
	require.Equal(t, "a+b", host.stringers[1].String(), "a kept array Stringer holds the value converted")

	call("Captured")
	require.Len(t, host.stringers, 3)
	require.Equal(t, "first", host.stringers[2].String(), "a captured variable's later write does not reach the kept value")

	call("Pointer")
	require.Len(t, host.stringers, 4)
	require.Equal(t, "changed", host.stringers[3].String(), "a pointer receiver keeps its identity, as in Go")

	for range 8 {
		call("Stringer")
	}
	require.Equal(t, "first", host.stringers[0].String())
	require.Equal(t, "error:first", host.errs[0].Error())
	require.Equal(t, "a+b", host.stringers[1].String())
}

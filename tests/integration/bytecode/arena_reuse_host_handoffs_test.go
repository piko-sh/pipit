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
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type HostKeptT struct {
	S string
	N int
}

type HostKeptHolder struct{ last string }

var hostKeptValue any

const hostHandOffSource = `package main

import (
	"errors"
	"strconv"
	"keephost"
)

var gS string
var gSP string
var gSC string
var gE error
var gB []byte
var gSS []string
var gP *keephost.T

func mk(n int) string { return "kept-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func KeepString(n int) int     { keephost.String(mk(n)); return n }
func KeepAny(n int) int        { keephost.Any(mk(n)); return n }
func KeepIface(n int) int      { var v any = mk(n); keephost.Any(v); return n }
func KeepError(n int) int      { keephost.Any(errors.New(mk(n))); return n }
func KeepMethod(n int) int     { h := keephost.NewHolder(); h.Set(mk(n)); return n }
func KeepFuncValue(n int) int  { f := keephost.String; f(mk(n)); return n }
func KeepDeferred(n int) int   { defer keephost.String(mk(n)); return n }
func KeepTypedStrings(n int) int {
	xs := make([]string, 2)
	xs[0] = mk(n)
	xs[1] = mk(n + 1)
	keephost.Strings(xs)
	return n
}
func KeepAppendedBytes(n int) int {
	b := make([]byte, 0, 32)
	b = append(b, mk(n)...)
	keephost.Bytes(b)
	return n
}
func KeepStructValue(n int) int { var t keephost.T; t.S = mk(n); t.N = n; keephost.Struct(t); return n }
func KeepStructAny(n int) int   { var t keephost.T; t.S = mk(n); t.N = n; keephost.Any(t); return n }
func KeepPointer(n int) int     { t := &keephost.T{}; t.S = mk(n); keephost.Pointer(t); return n }
func KeepArray(n int) int       { a := [2]string{mk(n), mk(n + 1)}; keephost.Any(a); return n }
func KeepInt(n int) int         { keephost.Any(n * 100003); return n }

func SetString(n int) int        { gS = mk(n); return n }
func SetConcat(n int) int        { s := strconv.Itoa(n); gS = "kept-" + s + "-" + strconv.Itoa(n*7); return len(gS) }
func SetStringPointer(n int) int { p := &gSP; *p = mk(n); return n }
func SetStringCallback(n int) int { keephost.Call(func() { gSC = mk(n) }); return n }
func SetError(n int) int         { gE = errors.New(mk(n)); return n }
func SetBytes(n int) int         { gB = []byte(mk(n)); return n }
func SetStrings(n int) int       { gSS = []string{mk(n), mk(n + 1)}; return n }
func SetPointer(n int) int       { gP = &keephost.T{S: mk(n), N: n}; return n }
func ReadString() string         { return gS }
func ReadStringPointer() string  { return gSP }
func ReadStringCallback() string { return gSC }

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7))
	}
	return t
}
func ReadError() string          { return gE.Error() }
func ReadBytes() string          { return string(gB) }
func ReadStrings() string        { return gSS[0] + "," + gSS[1] }
func ReadPointer() string        { return gP.S }
`

func (h *HostKeptHolder) Set(s string) { h.last = s; hostKeptValue = s }

func reuseHostHandOffArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 24)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
	}
}

func hostHandOffMk(n int) string { return fmt.Sprintf("kept-%d-%d", n, n*7) }

func hostHandOffService(t *testing.T) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	exports := maps.Clone(stdlib.Exports())
	exports["keephost"] = map[string]reflect.Value{
		"String":    reflect.ValueOf(func(s string) { hostKeptValue = s }),
		"Any":       reflect.ValueOf(func(v any) { hostKeptValue = v }),
		"Strings":   reflect.ValueOf(func(v []string) { hostKeptValue = v }),
		"Bytes":     reflect.ValueOf(func(v []byte) { hostKeptValue = v }),
		"Struct":    reflect.ValueOf(func(v HostKeptT) { hostKeptValue = v }),
		"Pointer":   reflect.ValueOf(func(v *HostKeptT) { hostKeptValue = v }),
		"Call":      reflect.ValueOf(func(f func()) { f() }),
		"NewHolder": reflect.ValueOf(func() *HostKeptHolder { return &HostKeptHolder{} }),
		"Holder":    reflect.ValueOf((*HostKeptHolder)(nil)),
		"T":         reflect.ValueOf((*HostKeptT)(nil)),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled := hazardCompile(t, service, hostHandOffSource)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func renderHostKept(value any) string {
	switch x := value.(type) {
	case []string:
		return strings.Join(x, ",")
	case []byte:
		return string(x)
	case HostKeptT:
		return x.S
	case *HostKeptT:
		return x.S
	case [2]string:
		return x[0] + "," + x[1]
	case error:
		return x.Error()
	default:
		return fmt.Sprint(value)
	}
}

func TestHostKeptArgumentsSurviveReuse(t *testing.T) {
	cases := []struct{ id, entry, want string }{
		{"string", "KeepString", hostHandOffMk(5)},
		{"any", "KeepAny", hostHandOffMk(5)},
		{"iface", "KeepIface", hostHandOffMk(5)},
		{"error", "KeepError", hostHandOffMk(5)},
		{"method", "KeepMethod", hostHandOffMk(5)},
		{"func-value", "KeepFuncValue", hostHandOffMk(5)},
		{"typed-strings", "KeepTypedStrings", hostHandOffMk(5) + "," + hostHandOffMk(6)},
		{"appended-bytes", "KeepAppendedBytes", hostHandOffMk(5)},
		{"deferred", "KeepDeferred", hostHandOffMk(5)},
		{"struct-value", "KeepStructValue", hostHandOffMk(5)},
		{"struct-any", "KeepStructAny", hostHandOffMk(5)},
		{"pointer", "KeepPointer", hostHandOffMk(5)},
		{"array", "KeepArray", hostHandOffMk(5) + "," + hostHandOffMk(6)},
		{"int", "KeepInt", "500015"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			service, compiled := hostHandOffService(t)
			hazardCall(t, service, compiled, tc.entry, 5)
			kept := hostKeptValue
			observations := &hazardObservations{}
			observations.compareString("host-kept value straight after the run", tc.want, renderHostKept(kept))
			reuseHostHandOffArenas(t, service, compiled)
			observations.compareString("host-kept value after reuse", tc.want, renderHostKept(kept))
			window.settle(observations)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestGlobalsSetByCallFunctionSurviveReuse(t *testing.T) {
	cases := []struct{ id, set, read, want string }{
		{"string", "SetString", "ReadString", hostHandOffMk(5)},
		{"concat", "SetConcat", "ReadString", hostHandOffMk(5)},
		{"string-pointer", "SetStringPointer", "ReadStringPointer", hostHandOffMk(5)},
		{"string-callback", "SetStringCallback", "ReadStringCallback", hostHandOffMk(5)},
		{"error", "SetError", "ReadError", hostHandOffMk(5)},
		{"bytes", "SetBytes", "ReadBytes", hostHandOffMk(5)},
		{"strings", "SetStrings", "ReadStrings", hostHandOffMk(5) + "," + hostHandOffMk(6)},
		{"pointer", "SetPointer", "ReadPointer", hostHandOffMk(5)},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			service, compiled := hostHandOffService(t)
			hazardCall(t, service, compiled, tc.set, 5)
			observations := &hazardObservations{}
			observations.compareString("global straight after the run", tc.want, fmt.Sprint(hazardCall(t, service, compiled, tc.read)))
			reuseHostHandOffArenas(t, service, compiled)
			observations.compareString("global read after reuse", tc.want, fmt.Sprint(hazardCall(t, service, compiled, tc.read)))
			window.settle(observations)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestStringsSentOnHostChannelsSurviveReuse(t *testing.T) {
	route := newHandOffRoute(t, `func Fill(n int) int {
	ch := mhost.StringChan()
	if len(ch) == 0 {
		ch <- mk(n)
	}
	ac := mhost.AnyChan()
	if len(ac) == 0 {
		ac <- []string{mk(n + 1)}
	}
	return n
}
`)
	route.call("Fill", 4242)
	host := route.host
	route.keeps(func() map[string]string {
		text := <-host.stringChan
		host.stringChan <- text
		value := <-host.anyChan
		host.anyChan <- value
		return map[string]string{"chan string": text, "chan any": routeString(value)}
	})
	route.finish("channel-send")
}

func TestValuesPassedToAHostMethodSurviveReuse(t *testing.T) {
	route := newHandOffRoute(t, `func Fill(n int) int {
	k := mhost.Keeper()
	k.Keep(mk(n))
	k.Keep([]string{mk(n + 1)})
	return n
}
`)
	route.call("Fill", 4242)
	keeper := route.host.keeper
	route.keeps(func() map[string]string {
		out := map[string]string{}
		for index, value := range keeper.kept[:min(2, len(keeper.kept))] {
			out[fmt.Sprintf("kept[%d]", index)] = routeString(value)
		}
		return out
	})
	route.finish("native-argument")
}

func TestAppendsIntoWideAndDeepHostArgumentsSurviveReuse(t *testing.T) {
	route := newHandOffRoute(t, `func Wide(dst []string, n int) int {
	local := make([]string, 4)
	for i := range local {
		local[i] = mk(n + i)
	}
	_ = append(dst[:len(dst)-4], local...)
	return len(dst)
}

func Deep(dst *******[]string, n int) int { return Wide(*******dst, n) }
`)
	wide := make([]string, 70000)
	route.call("Wide", wide, 100)
	deep := make([]string, 4)
	p1 := &deep
	p2 := &p1
	p3 := &p2
	p4 := &p3
	p5 := &p4
	p6 := &p5
	p7 := &p6
	route.call("Deep", p7, 200)
	route.keeps(func() map[string]string {
		return map[string]string{"wide tail": wide[len(wide)-1], "deep": deep[0]}
	})
	route.finish("container-store", wide, p7)
}

func TestStoreIntoAHostMapBeyondTheWalkBudgetSurvivesReuse(t *testing.T) {
	route := newHandOffRoute(t, `func WideMap(dst []string, m map[string]any, n int) int {
	m["k"] = []string{mk(n), mk(n + 1)}
	return len(dst)
}
`)
	wide := make([]string, 65535)
	extraMap := map[string]any{"a": 1, "b": 2, "c": 3}
	route.call("WideMap", wide, extraMap, 300)
	route.keeps(func() map[string]string {
		return map[string]string{"map[k]": routeString(extraMap["k"])}
	})
	route.finish("container-store", wide, extraMap)
}

func TestStoresIntoHostStructFieldsBeyondTheWalkBudgetSurviveReuse(t *testing.T) {
	route := newHandOffRoute(t, `func WideHolder(dst []string, h *struct {
	S   string
	Xs  []string
	Any any
}, n int) int {
	h.Xs = []string{mk(n), mk(n + 1)}
	h.Any = []string{mk(n + 2)}
	return len(dst)
}
`)
	wide := make([]string, 65535)
	holder := &struct {
		S   string
		Xs  []string
		Any any
	}{}
	route.call("WideHolder", wide, holder, 400)
	route.keeps(func() map[string]string {
		return map[string]string{"holder.Xs": strings.Join(holder.Xs, ","), "holder.Any": routeString(holder.Any)}
	})
	route.finish("container-store", wide, holder)
}

func TestStoreIntoANilStringMapPanicsAndIsRecovered(t *testing.T) {
	route := newHandOffRoute(t, `func Fill(n int) (out int) {
	defer func() {
		if recover() != nil {
			out = n
		}
	}()
	var m map[string]string
	m[mk(n)] = mk(n + 1)
	return 0
}
`)
	require.Equal(t, 4242, route.call("Fill", 4242), "the guest recovers the nil-map panic")
	route.reuse()
	route.finish("container-store")
}

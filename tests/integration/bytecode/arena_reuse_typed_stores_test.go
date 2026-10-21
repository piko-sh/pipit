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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

const typedStoresSource = `package main

import (
	"strconv"
	"strings"
	"storehost"
)

type pt struct{ a, b, c int }

type node struct {
	name string
	n    int
}

var globalPoint pt
var globalString string
var globalNode *node

func label(i int) string { return "store-" + strconv.Itoa(i) + "-" + strconv.Itoa(i*31%97) }

// Typed stores with no intermediate copy.
func DerefPointerFree(n int) int {
	p := new(pt)
	q := &pt{}
	for i := 0; i < n; i++ {
		*p = pt{a: i, b: i + 1, c: i + 2}
		*q = *p
	}
	return p.a + q.c
}

func DerefCopySemantics() int {
	p := new(pt)
	*p = pt{1, 2, 3}
	q := new(pt)
	*q = *p
	p.a = 9
	return q.a*100 + p.a
}

func GlobalFromLocal(n int) int {
	local := pt{a: n, b: n * 2, c: n * 3}
	p := &globalPoint
	*p = local
	local.a = -1
	return globalPoint.a + globalPoint.c
}

func ArrayDeref(n int) int {
	p := new([4]int)
	q := new([4]int)
	*p = [4]int{n, n + 1, n + 2, n + 3}
	*q = *p
	p[0] = -7
	return q[0]*10 + q[3]
}

func OwnCellNode(n int) string {
	var v node
	p := &v
	*p = node{name: label(n), n: n}
	return p.name + "/" + strconv.Itoa(v.n)
}

func KeepNode(n int) int {
	p := new(node)
	*p = node{name: label(n + 1), n: n}
	storehost.Keep(p)
	return n
}

func GlobalNode(n int) int {
	p := new(node)
	q := new(node)
	*q = node{name: label(n + 2), n: n}
	*p = *q
	globalNode = p
	return n
}

func ReadGlobalNode() string { return globalNode.name }

func Elements(n int) int {
	xs := make([]pt, 8)
	for i := 0; i < n; i++ {
		xs[i%8] = pt{a: i, b: i + 1, c: i + 2}
	}
	return xs[0].a + xs[7].c
}

// String intrinsics built in the byte slab.
func Upper(s string) string   { return strings.ToUpper(s) }
func Lower(s string) string   { return strings.ToLower(s) }
func Repeat(s string, n int) string { return strings.Repeat(s, n) }
func Replace(s, old, nw string) string { return strings.ReplaceAll(s, old, nw) }
func Join(n int, sep string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = label(i)
	}
	return strings.Join(parts, sep)
}
func LocalLengths(n int) int {
	t := 0
	for i := 0; i < n; i++ {
		t += len(strings.ToUpper(label(i))) + len(strings.Repeat("ab", i%5)) + len(strings.ReplaceAll(label(i), "-", "+"))
	}
	return t
}
func GlobalUpper(s string) int { globalString = strings.ToUpper(s) + strings.Join([]string{s, s}, "|"); return len(globalString) }
func ReadGlobalString() string { return globalString }
func GlobalConcat(s string) int { globalString = s + "|" + strconv.Itoa(len(s)); return len(globalString) }
func KeepConcat(n int) int {
	storehost.Keep(label(n) + ";" + strconv.Itoa(n))
	return n
}
func KeepJoined(n int) int {
	storehost.Keep(strings.Join([]string{label(n), strings.ToLower("ABC"), strings.Repeat("z", n%7)}, ";"))
	return n
}
func StoreInMap(m map[string]string, n int) int {
	m["upper"] = strings.ToUpper(label(n))
	m["repl"] = strings.ReplaceAll(label(n), "-", "::")
	return n
}
func KeepSlice(n int) int {
	out := []string{}
	for i := 0; i < n; i++ {
		out = append(out, strings.ToUpper(label(i)))
	}
	storehost.Keep(out)
	return n
}

// Read-only []byte(s) conversions with no copy.
func BytesSum(s string) int {
	b := []byte(s)
	n := 0
	for i, c := range b {
		n += i * int(c)
	}
	if len(b) == 0 {
		return n
	}
	return n + len(b) + int(b[0])
}
func BytesSeeTheOldString(s string) int {
	b := []byte(s)
	s = "zzzzzzzz"
	n := 0
	for _, c := range b {
		n += int(c)
	}
	return n*1000 + len(s)
}
func RangeConversionSeesTheOldString(s string) int {
	n := 0
	for _, c := range []byte(s) {
		s = "q"
		n += int(c)
	}
	return n
}
func IndexConversion(s string, i int) int { return int([]byte(s)[i]) + len([]byte(s)) }
func Written(s string) string {
	b := []byte(s)
	b[0] = 'X'
	return string(b) + "|" + s
}
func CountCommas(s string) int {
	b := []byte(s)
	n := 0
	for i := 0; i < len(b); i++ {
		if b[i] == ',' {
			n++
		}
	}
	return n
}
`

var typedStoresKept any

type typedStoresCase struct {
	name string
	give func(t *testing.T, call func(string, ...any) any) (read func() string, want string)
}

func typedStoresLabel(i int) string { return "store-" + strconv.Itoa(i) + "-" + strconv.Itoa(i*31%97) }

func typedStoresService(t *testing.T) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	exports := maps.Clone(stdlib.Exports())
	exports["storehost"] = map[string]reflect.Value{"Keep": reflect.ValueOf(func(v any) { typedStoresKept = v })}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": typedStoresSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func typedStoresReturned(name, entry string, want any, arguments ...any) typedStoresCase {
	return typedStoresCase{name: name, give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
		got := call(entry, arguments...)
		return func() string { return fmt.Sprint(got) }, fmt.Sprint(want)
	}}
}

func nativeBytesSum(text string) int {
	b := []byte(text)
	sum := 0
	for i, c := range b {
		sum += i * int(c)
	}
	if len(b) == 0 {
		return sum
	}
	return sum + len(b) + int(b[0])
}

func typedStoresCases() []typedStoresCase {
	nativeJoin := func(count int, sep string) string {
		parts := make([]string, count)
		for i := range parts {
			parts[i] = typedStoresLabel(i)
		}
		return strings.Join(parts, sep)
	}
	localLengths := 0
	for i := range 40 {
		localLengths += len(strings.ToUpper(typedStoresLabel(i))) + len(strings.Repeat("ab", i%5)) + len(strings.ReplaceAll(typedStoresLabel(i), "-", "+"))
	}
	upperSlice := make([]string, 30)
	for i := range upperSlice {
		upperSlice[i] = strings.ToUpper(typedStoresLabel(i))
	}
	return []typedStoresCase{
		typedStoresReturned("typed store deref pointer-free", "DerefPointerFree", 999+1001, 1000),
		typedStoresReturned("typed store deref copy semantics", "DerefCopySemantics", 109),
		typedStoresReturned("typed store global from a local", "GlobalFromLocal", 7+21, 7),
		typedStoresReturned("typed store array deref", "ArrayDeref", 50+8, 5),
		typedStoresReturned("typed store own-cell node", "OwnCellNode", typedStoresLabel(12)+"/12", 12),
		typedStoresReturned("typed store elements", "Elements", 992+1001, 1000),
		{name: "typed store node kept by the host", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("KeepNode", 21)
			kept := typedStoresKept
			return func() string { return reflect.ValueOf(kept).Elem().Field(0).String() }, typedStoresLabel(22)
		}},
		{name: "typed store node in a global", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("GlobalNode", 5)
			return func() string { return fmt.Sprint(call("ReadGlobalNode")) }, typedStoresLabel(7)
		}},
		typedStoresReturned("intrinsic upper", "Upper", "HELLO, WORLD", "hello, World"),
		typedStoresReturned("intrinsic upper unicode", "Upper", strings.ToUpper("héllo ǆ"), "héllo ǆ"),
		typedStoresReturned("intrinsic lower", "Lower", "mixed case", "MiXeD CaSe"),
		typedStoresReturned("intrinsic repeat", "Repeat", strings.Repeat("ab-", 9), "ab-", 9),
		typedStoresReturned("intrinsic repeat empty", "Repeat", "", "ab", 0),
		typedStoresReturned("intrinsic replace", "Replace", "a::b::c", "a-b-c", "-", "::"),
		typedStoresReturned("intrinsic replace empty old", "Replace", strings.ReplaceAll("héj", "", "."), "héj", "", "."),
		typedStoresReturned("intrinsic join", "Join", nativeJoin(20, ", "), 20, ", "),
		typedStoresReturned("intrinsic join one", "Join", typedStoresLabel(0), 1, ", "),
		typedStoresReturned("intrinsic local lengths", "LocalLengths", localLengths, 40),
		{name: "intrinsic global", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("GlobalUpper", "glob")
			return func() string { return fmt.Sprint(call("ReadGlobalString")) }, "GLOBglob|glob"
		}},
		{name: "global concat", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("GlobalConcat", "glob")
			return func() string { return fmt.Sprint(call("ReadGlobalString")) }, "glob|4"
		}},
		{name: "concat kept by the host", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("KeepConcat", 9)
			kept := typedStoresKept
			return func() string { return fmt.Sprint(kept) }, typedStoresLabel(9) + ";9"
		}},
		{name: "intrinsic kept by the host", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("KeepJoined", 9)
			kept := typedStoresKept
			return func() string { return fmt.Sprint(kept) }, typedStoresLabel(9) + ";abc;zz"
		}},
		{name: "intrinsic stored in a host map", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			m := map[string]string{}
			call("StoreInMap", m, 4)
			return func() string { return m["upper"] + " " + m["repl"] }, strings.ToUpper(typedStoresLabel(4)) + " " + strings.ReplaceAll(typedStoresLabel(4), "-", "::")
		}},
		{name: "intrinsic slice kept by the host", give: func(t *testing.T, call func(string, ...any) any) (func() string, string) {
			call("KeepSlice", 30)
			kept := typedStoresKept
			return func() string { return joinStrings(kept) }, strings.Join(upperSlice, ",")
		}},
		typedStoresReturned("byte view bytes sum", "BytesSum", nativeBytesSum("hello, wörld"), "hello, wörld"),
		typedStoresReturned("byte view bytes sum empty", "BytesSum", 0, ""),
		typedStoresReturned("byte view old string", "BytesSeeTheOldString", 97*3*1000+8, "aaa"),
		typedStoresReturned("byte view range conversion", "RangeConversionSeesTheOldString", 97+98+99, "abc"),
		typedStoresReturned("byte view index conversion", "IndexConversion", int('l')+5, "hello", 2),
		typedStoresReturned("byte view written keeps its copy", "Written", "Xbc|abc", "abc"),
		typedStoresReturned("byte view counted loop", "CountCommas", 3, "a,b,,c"),
	}
}

func TestTypedStoresIntrinsicsAndByteViewsSurviveReuse(t *testing.T) {
	for _, tc := range typedStoresCases() {
		t.Run(tc.name, func(t *testing.T) {
			service, compiled := typedStoresService(t)
			call := func(name string, arguments ...any) any {
				result, err := service.CallFunction(t.Context(), compiled, name, arguments...)
				require.NoError(t, err, "calling %s", name)
				return result
			}
			window := openAuditWindow(t, 0)
			read, want := tc.give(t, call)
			require.Equal(t, want, read(), "before reuse")
			clobberArenas(t, service, 24)
			for round := range 4 {
				call("LocalLengths", 10+round)
				call("Join", 30+round, "+")
			}
			observations := &hazardObservations{}
			observations.compareString("after reuse", want, read())
			window.settle(observations)
			require.Empty(t, observations.fired, "%s", strings.Join(observations.details, "; "))
		})
	}
}

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
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type BytesRec struct{ S string }

type bytesHost struct {
	mu      sync.Mutex
	bytes   [][]byte
	strings [][]string
	recs    []*BytesRec
}

const bytesSource = `package main

import (
	"byteshost"
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

var sinkB []byte

var sinkS []string

func mk(n int) string { return "bytes-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*3) }

func Construct(n int) string {
	b := []byte(mk(n))
	m := make([]byte, 0, 4)
	m = append(m, 'a', 'b')
	m = append(m, mk(n+1)...)
	c := []byte(mk(n + 2))
	s1 := string(b)
	s2 := string(b[1:4])
	s3 := s1 + "/" + string(m)
	s4 := strings.ToUpper(string(b))
	s5 := strings.Repeat(string(b[:2]), 3)
	s6 := strings.Join([]string{string(b), string(m)}, ",")
	s7 := unsafe.String(&c[0], len(c))
	s8 := fmt.Sprintf("%s|%x", b, m)
	s9 := strings.ReplaceAll(string(m), "a", "Z")
	s10 := string(rune(b[0])) + strconv.Itoa(len(m))
	s11 := strings.ToLower(string(m))
	for i := range b {
		b[i] = 'X'
	}
	for i := range m {
		m[i] = 'Y'
	}
	return strings.Join([]string{s1, s2, s3, s4, s5, s6, s7, s8, s9, s10, s11}, ";")
}

func Confined(n int) int {
	b := []byte(mk(n))
	b[0] = 'Q'
	parts := strings.Split(string(b)+",x,yy", ",")
	t := 0
	for _, p := range parts {
		t += len(p)
	}
	return t*1000 + int(b[0]) + len(parts)
}

func Escape(n int) int {
	b := []byte(mk(n))
	byteshost.KeepBytes(b)
	parts := strings.Split(mk(n)+","+mk(n+1), ",")
	byteshost.KeepStrings(parts)
	m := make([]byte, 3)
	m[0], m[1], m[2] = 'q', byte('0'+n%10), 'z'
	byteshost.KeepBytes(m)
	sinkB = []byte(mk(n + 2))
	sinkS = strings.Split(mk(n+3)+":"+mk(n+4), ":")
	return len(b) + len(parts)
}

func Globals() string { return string(sinkB) + "|" + strings.Join(sinkS, ",") }

func Returned(n int) []byte { return []byte(mk(n)) }

func ReturnedSplit(n int) []string { return strings.Split(mk(n)+"|"+mk(n+1), "|") }

type W struct{ name string }

type R struct{ S string }

func (w W) String() string {
	buf := make([]string, 2)
	buf[0] = w.name
	buf[1] = w.name + "!"
	byteshost.KeepStrings(buf)
	r := &R{}
	r.S = w.name
	byteshost.KeepRec(&byteshost.Rec{S: r.S})
	return "w"
}

func Stringify(n int) string { return fmt.Sprint(W{name: mk(n)}) + fmt.Sprint(W{name: mk(n) + "+" + mk(n+1)}) }

func CallbackLocal(n int) int {
	total := 0
	keys := []string{mk(n) + "/" + mk(n+1)}
	byteshost.EachWith(keys[0], 3, func(i int, s string) {
		buf := make([]string, 2)
		buf[0] = keys[0]
		buf[1] = keys[0] + "!"
		byteshost.KeepStrings(buf)
		total += len(buf[0])
	})
	return total
}

type V struct{ n int }

func (v *V) String() string {
	v.n += 100
	return "p" + strconv.Itoa(v.n)
}

func Print(n int) int {
	v := V{n: n}
	s := mk(n)
	x := n*1000 + 7
	fmt.Println(&v, v, s, x, 3.5, uint(x))
	fmt.Println(s+"/"+mk(n+1), int64(x)*3)
	return v.n
}

type Bump struct{ p *[3]int }

func (b Bump) String() string {
	b.p[0] += 100
	return "b" + strconv.Itoa(b.p[0])
}

func PrintArray(n int) int {
	a := [3]int{n, n + 1, n + 2}
	fmt.Println(Bump{&a}, a, n)
	return a[0]
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		b := make([]byte, 16)
		for j := range b {
			b[j] = 'Z'
		}
		t += len(string(b)) + len("ZZ-"+strconv.Itoa(n+i))
		parts := strings.Split("ZZ,ZZ,"+strconv.Itoa(i), ",")
		t += len(parts)
	}
	return t
}
`

const bytesRounds = 12

type nativeV struct{ n int }

type nativeBump struct{ p *[3]int }

func (h *bytesHost) keepBytes(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bytes = append(h.bytes, b)
}

func (h *bytesHost) keepStrings(s []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.strings = append(h.strings, s)
}

func (h *bytesHost) keepRec(r *BytesRec) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, r)
}

func bytesMk(n int) string { return "bytes-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*3) }

func nativeConstruct(seed int) string {
	b := []byte(bytesMk(seed))
	built := make([]byte, 0, 4)
	built = append(built, 'a', 'b')
	built = append(built, bytesMk(seed+1)...)
	converted := []byte(bytesMk(seed + 2))
	s1 := string(b)
	s2 := string(b[1:4])
	s3 := s1 + "/" + string(built)
	s4 := strings.ToUpper(string(b))
	s5 := strings.Repeat(string(b[:2]), 3)
	s6 := strings.Join([]string{string(b), string(built)}, ",")
	s7 := string(converted)
	s8 := fmt.Sprintf("%s|%x", b, built)
	s9 := strings.ReplaceAll(string(built), "a", "Z")
	s10 := string(rune(b[0])) + strconv.Itoa(len(built))
	s11 := strings.ToLower(string(built))
	for i := range b {
		b[i] = 'X'
	}
	for i := range built {
		built[i] = 'Y'
	}
	return strings.Join([]string{s1, s2, s3, s4, s5, s6, s7, s8, s9, s10, s11}, ";")
}

func nativeConfined(seed int) int {
	b := []byte(bytesMk(seed))
	b[0] = 'Q'
	parts := strings.Split(string(b)+",x,yy", ",")
	t := 0
	for _, p := range parts {
		t += len(p)
	}
	return t*1000 + int(b[0]) + len(parts)
}

func bytesService(t *testing.T, host *bytesHost) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	exports := maps.Clone(stdlib.Exports())
	exports["byteshost"] = map[string]reflect.Value{
		"Rec":         reflect.ValueOf((*BytesRec)(nil)),
		"KeepBytes":   reflect.ValueOf(host.keepBytes),
		"KeepStrings": reflect.ValueOf(host.keepStrings),
		"KeepRec":     reflect.ValueOf(host.keepRec),
		"EachWith": reflect.ValueOf(func(s string, n int, fn func(int, string)) {
			for i := range n {
				fn(i, s)
			}
		}),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled := hazardCompile(t, service, bytesSource)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func reuseByteArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
	}
}

func TestNoStringIsBuiltOverMutableBytes(t *testing.T) {
	host := &bytesHost{}
	service, compiled := bytesService(t, host)
	window := openHazardWindows(t)
	grows := engine.LicenceDiagnostics().MutableByteSlabs
	observations := &hazardObservations{}
	results := make([]string, bytesRounds)
	for n := range bytesRounds {
		results[n] = requireAs[string](t, hazardCall(t, service, compiled, "Construct", n))
	}
	reuseByteArenas(t, service, compiled)
	for n := range bytesRounds {
		observations.compareString(fmt.Sprintf("Construct(%d)", n), nativeConstruct(n), results[n])
	}

	if engine.LicenceDiagnostics().MutableByteSlabs == grows {
		observations.note("no mutable byte slab installed during the case (pooled arenas kept theirs)")
	}

	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestByteBackingsAndSplitResultsSurviveReuse(t *testing.T) {
	host := &bytesHost{}
	service, compiled := bytesService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	returned := make([][]byte, 0, bytesRounds)
	returnedSplit := make([][]string, 0, bytesRounds)
	for n := range bytesRounds {
		hazardCall(t, service, compiled, "Escape", n)
		returned = append(returned, hazardCall(t, service, compiled, "Returned", n).([]byte))
		returnedSplit = append(returnedSplit, hazardCall(t, service, compiled, "ReturnedSplit", n).([]string))
	}
	globals := requireAs[string](t, hazardCall(t, service, compiled, "Globals"))
	reuseByteArenas(t, service, compiled)
	last := bytesRounds - 1
	observations.compareString("globals", bytesMk(last+2)+"|"+bytesMk(last+3)+","+bytesMk(last+4), globals)
	for round := range bytesRounds {
		observations.compareString(fmt.Sprintf("kept []byte(s) n=%d", round), bytesMk(round), string(host.bytes[2*round]))
		observations.compareString(fmt.Sprintf("kept make n=%d", round), "q"+strconv.Itoa(round%10)+"z", string(host.bytes[2*round+1]))
		observations.compareString(fmt.Sprintf("kept Split n=%d", round), bytesMk(round)+","+bytesMk(round+1), strings.Join(host.strings[round], ","))
		observations.compareString(fmt.Sprintf("returned []byte n=%d", round), bytesMk(round), string(returned[round]))
		observations.compareString(fmt.Sprintf("returned Split n=%d", round), bytesMk(round)+","+bytesMk(round+1), strings.Join(returnedSplit[round], ","))
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestConfinedConversionsAndSplitsMatchNativeGo(t *testing.T) {
	host := &bytesHost{}
	service, compiled := bytesService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	got := make([]int, bytesRounds)
	for n := range bytesRounds {
		got[n] = int(hazardCall(t, service, compiled, "Confined", n).(int))
	}
	reuseByteArenas(t, service, compiled)
	for n := range bytesRounds {
		observations.compareString(fmt.Sprintf("Confined(%d)", n), strconv.Itoa(nativeConfined(n)), strconv.Itoa(got[n]))
		again := int(hazardCall(t, service, compiled, "Confined", n).(int))
		observations.compareString(fmt.Sprintf("Confined(%d) after reuse", n), strconv.Itoa(nativeConfined(n)), strconv.Itoa(again))
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestForeignStringsKeptInCallbackStorageSurviveReuse(t *testing.T) {
	host := &bytesHost{}
	service, compiled := bytesService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	keeps := engine.LicenceDiagnostics().ForeignStringsKept
	for n := range bytesRounds {
		observations.compareString(fmt.Sprintf("Stringify(%d)", n), "ww", hazardCall(t, service, compiled, "Stringify", n).(string))
	}
	receiverKeeps := engine.LicenceDiagnostics().ForeignStringsKept - keeps
	stringified := len(host.strings)
	for n := range bytesRounds {
		got := hazardCall(t, service, compiled, "CallbackLocal", n)
		observations.compareString(fmt.Sprintf("CallbackLocal(%d)", n), strconv.Itoa(3*len(bytesMk(n)+"/"+bytesMk(n+1))), fmt.Sprint(got))
	}
	callbackKeeps := engine.LicenceDiagnostics().ForeignStringsKept - keeps - receiverKeeps
	observations.note("foreign strings kept uncloned: %d through bound-method receivers (materialised before the call), %d through a slice the callback captured", receiverKeeps, callbackKeeps)
	reuseByteArenas(t, service, compiled)
	require.Len(t, host.recs, 2*bytesRounds)
	for round := range bytesRounds {
		names := []string{bytesMk(round), bytesMk(round) + "+" + bytesMk(round+1)}
		for k, name := range names {
			observations.compareString(fmt.Sprintf("kept callback slice n=%d/%d", round, k), name+","+name+"!", strings.Join(host.strings[2*round+k], ","))
			observations.compareString(fmt.Sprintf("kept callback record n=%d/%d", round, k), name, host.recs[2*round+k].S)
		}
		key := bytesMk(round) + "/" + bytesMk(round+1)
		for i := range 3 {
			observations.compareString(fmt.Sprintf("kept CallbackLocal slice n=%d/%d", round, i), key+","+key+"!", strings.Join(host.strings[stringified+3*round+i], ","))
		}
	}
	window.settle(observations)
	inconclusive := ""
	if callbackKeeps == 0 {
		inconclusive = "no callback kept a foreign string uncloned"
	}
	assertNoHazard(t, observations, inconclusive)
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	previous := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		done <- buffer.String()
	}()
	func() {
		defer func() { os.Stdout = previous }()
		run()
	}()
	require.NoError(t, writer.Close())
	return <-done
}

func (v *nativeV) String() string {
	v.n += 100
	return "p" + strconv.Itoa(v.n)
}

func nativePrint(writer io.Writer, seed int) int {
	printed := nativeV{n: seed}
	text := bytesMk(seed)
	number := seed*1000 + 7
	fmt.Fprintln(writer, &printed, printed, text, number, 3.5, uint(number))
	fmt.Fprintln(writer, text+"/"+bytesMk(seed+1), int64(number)*3)
	return printed.n
}

func (b nativeBump) String() string {
	b.p[0] += 100
	return "b" + strconv.Itoa(b.p[0])
}

func nativePrintArray(w io.Writer, n int) int {
	a := [3]int{n, n + 1, n + 2}
	fmt.Fprintln(w, nativeBump{&a}, a, n)
	return a[0]
}

func TestBoxesPassedToBorrowingNativesAreCopies(t *testing.T) {
	host := &bytesHost{}
	service, compiled := bytesService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	for round := range bytesRounds {
		var want bytes.Buffer
		wantN := nativePrint(&want, round)
		var gotN any
		got := captureStdout(t, func() { gotN = hazardCall(t, service, compiled, "Print", round) })
		observations.compareString(fmt.Sprintf("Print(%d) output", round), want.String(), got)
		observations.compareString(fmt.Sprintf("Print(%d) result", round), strconv.Itoa(wantN), fmt.Sprint(gotN))

		want.Reset()
		wantN = nativePrintArray(&want, round)
		got = captureStdout(t, func() { gotN = hazardCall(t, service, compiled, "PrintArray", round) })
		observations.compareString(fmt.Sprintf("PrintArray(%d) output", round), want.String(), got)
		observations.compareString(fmt.Sprintf("PrintArray(%d) result", round), strconv.Itoa(wantN), fmt.Sprint(gotN))
		if round%4 == 3 {
			reuseByteArenas(t, service, compiled)
		}
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

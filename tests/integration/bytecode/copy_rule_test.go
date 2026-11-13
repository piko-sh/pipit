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
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/sdk/stdlib"
)

const copyRuleSource = `package main

type pair struct {
	A int
	B int
}

type holder struct {
	M map[string]int
	N int
}

var (
	keptInts   []int
	keptPairs  []pair
	keptBox    *pair
	keptAny    any
	keptHolder holder
	keptChan   chan int
)

func Look(cfg map[string]any) int { return len(cfg) }

func AppendThroughGlobal() int {
	s := make([]int, 1, 8)
	keptInts = s
	keptInts = append(keptInts, 5)
	return s[:2][1]
}

func AppendPairsThroughGlobal() int {
	s := make([]pair, 1, 8)
	keptPairs = s
	keptPairs = append(keptPairs, pair{A: 5})
	return s[:2][1].A
}

func CapAfterGlobalStore() int {
	s := make([]pair, 2, 64)
	keptPairs = s
	return cap(keptPairs)
}

func GivePairs() []pair {
	s := make([]pair, 2, 16)
	keptPairs = s
	return s
}

func ReadKeptPair() int { return keptPairs[0].A }

func GiveInts() []int {
	s := make([]int, 2, 16)
	keptInts = s
	return s
}

func ReadKeptInt() int { return keptInts[0] }

func GivePointer() *pair {
	p := &pair{A: 1}
	keptBox = p
	return p
}

func ReadKeptPointer() int { return keptBox.A }

func SameKeptPointer(p *pair) bool { return p == keptBox }

func GiveAnyMap() any {
	m := map[string]int{"a": 1}
	keptAny = m
	return m
}

func ReadAnyMap() int { return keptAny.(map[string]int)["b"] }

func GiveHolder() holder {
	h := holder{M: map[string]int{"a": 1}, N: 1}
	keptHolder = h
	return h
}

func ReadHolder() int { return keptHolder.M["b"] }

func GiveChan() chan int {
	c := make(chan int, 4)
	keptChan = c
	return c
}

func SendKept(v int) int {
	keptChan <- v
	return len(keptChan)
}

func GoroutinePairs() int {
	s := make([]pair, 2, 4)
	done := make(chan bool)
	go func(x []pair) {
		x[0].A = 100
		x = append(x, pair{A: 7})
		done <- true
	}(s)
	<-done
	return s[0].A + s[:3][2].A
}

func ClosureCapture() int {
	s := make([]pair, 2, 4)
	t := s
	f := func() {
		s[0].A = 9
		s = append(s, pair{A: 3})
	}
	f()
	return t[0].A + t[:3][2].A
}

func StructSliceField() int {
	s := make([]pair, 1, 4)
	h := struct{ S []pair }{S: s}
	keptAny = h
	h.S[0].A = 11
	return keptAny.(struct{ S []pair }).S[0].A
}

func AppendToCapturedInClosure() int {
	var out []pair
	func() {
		t := append(out, pair{A: 1})
		out = t
		t[0].A = 9
	}()
	return out[0].A
}

func AppendToCapturedInGoroutine() int {
	var out []string
	done := make(chan bool)
	go func() {
		t := append(out, "a")
		out = t
		t[0] = "abcd"
		done <- true
	}()
	<-done
	return len(out[0])
}

func CapturedKeepsCapacity() int {
	var out []pair
	func() {
		s := make([]pair, 1, 40)
		out = s
	}()
	return cap(out)
}
`

type copyRulePair struct {
	A int
	B int
}

var (
	nativeKeptInts  []int
	nativeKeptPairs []copyRulePair
	nativeKeptAny   any
)

type copyRuleHostHolder struct {
	M map[string]int
}

type copyRuleOutcome struct {
	name string
	got  any
	want any
}

type copyRuleCaller func(name string, arguments ...any) any

func nativeAppendThroughGlobal() int {
	s := make([]int, 1, 8)
	nativeKeptInts = s
	nativeKeptInts = append(nativeKeptInts, 5)
	return s[:2][1]
}

func nativeAppendPairsThroughGlobal() int {
	s := make([]copyRulePair, 1, 8)
	nativeKeptPairs = s
	nativeKeptPairs = append(nativeKeptPairs, copyRulePair{A: 5})
	return s[:2][1].A
}

func nativeCapAfterGlobalStore() int {
	s := make([]copyRulePair, 2, 64)
	nativeKeptPairs = s
	return cap(nativeKeptPairs)
}

func nativeGoroutinePairs() int {
	pairs := make([]copyRulePair, 2, 4)
	done := make(chan bool)
	go func(x []copyRulePair) {
		x[0].A = 100
		x = append(x, copyRulePair{A: 7})
		_ = x
		done <- true
	}(pairs)
	<-done
	return pairs[0].A + pairs[:3][2].A
}

func nativeClosureCapture() int {
	pairs := make([]copyRulePair, 2, 4)
	t := pairs
	appendToShared := func() {
		pairs[0].A = 9
		pairs = append(pairs, copyRulePair{A: 3})
	}
	appendToShared()
	return t[0].A + t[:3][2].A
}

func nativeStructSliceField() int {
	pairs := make([]copyRulePair, 1, 4)
	holder := struct{ S []copyRulePair }{S: pairs}
	nativeKeptAny = holder
	holder.S[0].A = 11
	return nativeKeptAny.(struct{ S []copyRulePair }).S[0].A
}

func nativeAppendToCapturedInClosure() int {
	var out []copyRulePair
	func() {
		t := append(out, copyRulePair{A: 1})
		out = t
		t[0].A = 9
	}()
	return out[0].A
}

func nativeAppendToCapturedInGoroutine() int {
	var out []string
	done := make(chan bool)
	go func() {
		t := append(out, "a")
		out = t
		t[0] = "abcd"
		done <- true
	}()
	<-done
	return len(out[0])
}

func nativeCapturedKeepsCapacity() int {
	var out []copyRulePair
	func() {
		s := make([]copyRulePair, 1, 40)
		out = s
	}()
	return cap(out)
}

func runCopyRuleProbes(call copyRuleCaller) []copyRuleOutcome {
	var outcomes []copyRuleOutcome
	add := func(name string, got, want any) {
		outcomes = append(outcomes, copyRuleOutcome{name: name, got: got, want: want})
	}

	add("append through a global shares the backing", call("AppendThroughGlobal"), nativeAppendThroughGlobal())
	add("append of structs through a global shares the backing", call("AppendPairsThroughGlobal"), nativeAppendPairsThroughGlobal())
	add("capacity kept by a global store", call("CapAfterGlobalStore"), nativeCapAfterGlobalStore())

	pairs := reflect.ValueOf(call("GivePairs"))
	add("returned struct slice keeps capacity", pairs.Cap(), 16)
	pairs.Index(0).FieldByName("A").SetInt(42)
	add("host write through a returned struct slice reaches the guest global", call("ReadKeptPair"), 42)

	ints, isInts := call("GiveInts").([]int)
	add("GiveInts returns an int slice", isInts, true)
	add("returned int slice keeps capacity", cap(ints), 16)
	if len(ints) > 0 {
		ints[0] = 43
	}
	add("host write through a returned int slice reaches the guest global", call("ReadKeptInt"), 43)

	pointer := reflect.ValueOf(call("GivePointer"))
	pointer.Elem().FieldByName("A").SetInt(44)
	add("host write through a returned pointer reaches the guest global", call("ReadKeptPointer"), 44)
	add("a returned pointer equals the guest's global", call("SameKeptPointer", pointer.Interface()), true)

	anyMap, isMap := call("GiveAnyMap").(map[string]int)
	add("GiveAnyMap returns a map", isMap, true)
	if anyMap != nil {
		anyMap["b"] = 45
	}
	add("host write through a map returned in an interface reaches the guest global", call("ReadAnyMap"), 45)

	held := reflect.ValueOf(call("GiveHolder"))
	held.FieldByName("M").SetMapIndex(reflect.ValueOf("b"), reflect.ValueOf(46))
	add("host write through a returned struct's map field reaches the guest global", call("ReadHolder"), 46)

	channel, isChannel := call("GiveChan").(chan int)
	add("GiveChan returns a channel", isChannel, true)
	sentLen := call("SendKept", 47)
	received := -1
	if channel != nil {
		select {
		case received = <-channel:
		default:
		}
	}
	add("a returned channel is the guest's channel", []any{sentLen, received}, []any{1, 47})

	inner := map[string]int{"x": 1}
	hostHolder := copyRuleHostHolder{M: map[string]int{"x": 1}}
	hostChan := make(chan int, 1)
	hostSlice := make([]int, 1, 8)
	config := map[string]any{"map": inner, "holder": hostHolder, "chan": hostChan, "slice": hostSlice}
	call("Look", config)
	sameMap := reflect.ValueOf(config["map"]).UnsafePointer() == reflect.ValueOf(inner).UnsafePointer()
	heldBack, isHolder := config["holder"].(copyRuleHostHolder)
	add("the holder is still a copyRuleHostHolder", isHolder, true)
	sameHolderMap := reflect.ValueOf(heldBack.M).UnsafePointer() == reflect.ValueOf(hostHolder.M).UnsafePointer()
	sameChan := config["chan"] == any(hostChan)
	slice, isSlice := config["slice"].([]int)
	add("the slice is still an int slice", isSlice, true)
	sameSlice := cap(slice) == 8 && len(slice) == 1 && reflect.ValueOf(slice).Pointer() == reflect.ValueOf(hostSlice).Pointer()
	add("host map, struct map field, channel and slice inside interfaces keep identity",
		[]bool{sameMap, sameHolderMap, sameChan, sameSlice}, []bool{true, true, true, true})

	add("goroutine slice argument shares the backing and capacity", call("GoroutinePairs"), nativeGoroutinePairs())
	add("closure capture shares the backing", call("ClosureCapture"), nativeClosureCapture())
	add("slice field of a struct boxed in a global shares the backing", call("StructSliceField"), nativeStructSliceField())
	add("a slice written to a captured variable keeps its backing", call("AppendToCapturedInClosure"), nativeAppendToCapturedInClosure())
	add("a slice a goroutine writes to a captured variable keeps its backing", call("AppendToCapturedInGoroutine"), nativeAppendToCapturedInGoroutine())
	add("a slice written to a captured variable keeps its capacity", call("CapturedKeepsCapacity"), nativeCapturedKeepsCapacity())
	return outcomes
}

func compileCopyRuleProbes(t *testing.T, options ...app.Option) copyRuleCaller {
	t.Helper()
	return compileCopyRuleProbesSource(t, copyRuleSource, options...)
}

func compileCopyRuleProbesSource(t *testing.T, source string, options ...app.Option) copyRuleCaller {
	t.Helper()
	service := app.NewService(options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err, "compiling the copy-rule probes")
	return copyRuleCall(t, service, compiled)
}

func copyRuleCall(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) copyRuleCaller {
	t.Helper()
	return func(name string, arguments ...any) any {
		t.Helper()
		result, err := service.CallFunction(t.Context(), compiled, name, arguments...)
		require.NoError(t, err, "calling %s", name)
		return result
	}
}

func TestCopyRuleMatchesNativeGo(t *testing.T) {
	call := compileCopyRuleProbes(t)
	var mismatches []string
	for _, outcome := range runCopyRuleProbes(call) {
		if !reflect.DeepEqual(outcome.got, outcome.want) {
			mismatches = append(mismatches, fmt.Sprintf("%s: pipit %v, native Go %v", outcome.name, outcome.got, outcome.want))
		}
	}
	require.Empty(t, mismatches, "Pipit's copies match native Go's")
}

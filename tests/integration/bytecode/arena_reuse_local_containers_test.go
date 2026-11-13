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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const localContainerFill = `
func fill(n int) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < 96; i++ {
		key := "key-" + strconv.Itoa(i%7)
		m[key] = append(m[key], "value-"+strconv.Itoa(n)+"-"+strconv.Itoa(i)+"-kept-in-a-map-the-function-made")
	}
	return m
}
`

func requireLocalContainerGrant(t *testing.T, compiled *program.CompiledFileSet, function string) {
	t.Helper()
	for _, candidate := range compiled.Root().Functions {
		if candidate.Name == function {
			require.NotEmptyf(t, candidate.LocalContainerStorePCs,
				"test harness: %s has no granted map store, so the trace never stored into a map without a hand-off", function)
			return
		}
	}
	require.Failf(t, "test harness", "the program has no function %s", function)
}

func runLocalContainerReplay(t *testing.T, granted string, testCase replayCase) {
	t.Helper()
	service := newHazardService(t)
	service.RegisterPackage("retainhost", (&replayRecorder{}).symbols())
	requireLocalContainerGrant(t, hazardCompile(t, service, testCase.source), granted)
	runReplay(t, testCase)
}

func TestMapFilledLocallyThenReturnedToTheHostSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "fill", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)
` + localContainerFill + `
func Run(n int) int {
	m := fill(n)
	retainhost.Retain(m)
	return len(m)
}
`})
}

func TestMapFilledLocallyThenHandedToTheHostInTheSameFunctionSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	m := map[string][]string{}
	for i := 0; i < 64; i++ {
		key := "key-" + strconv.Itoa(i%5)
		m[key] = append(m[key], "before-the-hand-off-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
	}
	retainhost.Retain(m)
	for i := 0; i < 64; i++ {
		key := "key-" + strconv.Itoa(i%5)
		m[key] = append(m[key], "after-the-hand-off-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
	}
	retainhost.Settle()
	return len(m)
}
`})
}

func TestElementReadFromALocallyFilledMapSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "fill", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)
` + localContainerFill + `
func Run(n int) int {
	m := fill(n)
	names := m["key-3"]
	retainhost.Retain(names)
	return len(names)
}
`})
}

func TestMapFilledLocallyThenStoredInAGlobalSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "fill", replayCase{entry: "Run", rounds: 4, guestCheck: "Bad", source: `package main

import (
	"retainhost"
	"strconv"
)

var kept map[string][]string
var keptFor int
` + localContainerFill + `
func Run(n int) int {
	kept = fill(n)
	keptFor = n
	retainhost.Retain(len(kept))
	return len(kept)
}

func Bad() int {
	bad := 0
	seen := 0
	for i := 0; i < 96; i++ {
		key := "key-" + strconv.Itoa(i%7)
		want := "value-" + strconv.Itoa(keptFor) + "-" + strconv.Itoa(i) + "-kept-in-a-map-the-function-made"
		if kept[key][i/7] != want {
			bad++
		}
		seen++
	}
	if seen != 96 || len(kept) != 7 {
		bad++
	}
	return bad
}
`})
}

func TestMapFilledLocallyThenCapturedByAClosureSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "fill", replayCase{entry: "Run", rounds: 4, guestCheck: "Bad", source: `package main

import (
	"retainhost"
	"strconv"
)

var read func(key string, index int) string
var readFor int
` + localContainerFill + `
func Run(n int) int {
	m := fill(n)
	read = func(key string, index int) string { return m[key][index] }
	readFor = n
	retainhost.Retain(len(m))
	return len(m)
}

func Bad() int {
	bad := 0
	for i := 0; i < 96; i++ {
		want := "value-" + strconv.Itoa(readFor) + "-" + strconv.Itoa(i) + "-kept-in-a-map-the-function-made"
		if read("key-"+strconv.Itoa(i%7), i/7) != want {
			bad++
		}
	}
	return bad
}
`})
}

func TestMapFilledLocallyThenSentToAGoroutineSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "fill", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)
` + localContainerFill + `
func Run(n int) int {
	m := fill(n)
	sent := make(chan map[string][]string, 1)
	done := make(chan int)
	go func() {
		got := <-sent
		retainhost.Retain(got)
		done <- len(got)
	}()
	sent <- m
	return <-done
}
`})
}

func TestMapFilledLocallyThenStoredInAnotherMapSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	outer := map[string]map[string][]string{}
	for group := 0; group < 4; group++ {
		inner := map[string][]string{}
		for i := 0; i < 24; i++ {
			key := "key-" + strconv.Itoa(i%3)
			inner[key] = append(inner[key], "inner-"+strconv.Itoa(n)+"-"+strconv.Itoa(group)+"-"+strconv.Itoa(i))
		}
		outer["group-"+strconv.Itoa(group)] = inner
	}
	retainhost.Retain(outer)
	return len(outer)
}
`})
}

func TestMapOfInterfacesFilledLocallySurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

type entry struct {
	Name  string
	Count int
}

func Run(n int) int {
	m := map[string]any{}
	for i := 0; i < 48; i++ {
		if i%2 == 0 {
			m["names-"+strconv.Itoa(i)] = []string{"first-" + strconv.Itoa(n), "second-" + strconv.Itoa(i)}
		} else {
			m["entry-"+strconv.Itoa(i)] = &entry{Name: "entry-" + strconv.Itoa(n) + "-" + strconv.Itoa(i), Count: i}
		}
	}
	retainhost.Retain(m)
	return len(m)
}
`})
}

func TestMapOfInterfacesFilledWithBoxedValuesSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	m := map[string]any{}
	for i := 0; i < 48; i++ {
		switch i % 3 {
		case 0:
			m["text-"+strconv.Itoa(i)] = "text-" + strconv.Itoa(n) + "-" + strconv.Itoa(i) + "-boxed-into-the-map"
		case 1:
			m["names-"+strconv.Itoa(i)] = []string{"first-" + strconv.Itoa(n), "second-" + strconv.Itoa(i)}
		default:
			m["count-"+strconv.Itoa(i)] = n*1000 + i
		}
	}
	retainhost.Retain(m)
	return len(m)
}
`})
}

func TestMapOfInterfacesKeyedByIntFilledWithBoxedValuesSurvivesReuse(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	m := map[int]any{}
	for i := 0; i < 48; i++ {
		if i%2 == 0 {
			m[i] = "text-" + strconv.Itoa(n) + "-" + strconv.Itoa(i) + "-boxed-into-the-map"
		} else {
			m[i] = []string{"first-" + strconv.Itoa(n), "second-" + strconv.Itoa(i)}
		}
	}
	retainhost.Retain(m)
	return len(m)
}
`})
}

func TestMapFilledThroughCommaOkLookupsSurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	m := map[string][]string{}
	seen := 0
	for i := 0; i < 48; i++ {
		key := "key-" + strconv.Itoa(i%8)
		if existing, ok := m[key]; ok {
			m[key] = append(existing, "more-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
			seen++
		} else {
			m[key] = []string{"first-" + strconv.Itoa(n) + "-" + strconv.Itoa(i)}
		}
	}
	retainhost.Retain(m)
	return len(m) + seen
}
`})
}

func TestMapFilledThroughTheBoxingStoreIsNotGrantedAndSurvivesReuse(t *testing.T) {
	testCase := replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	m := map[float64]any{}
	for i := 0; i < 48; i++ {
		if i%2 == 0 {
			m[float64(i)] = "text-" + strconv.Itoa(n) + "-" + strconv.Itoa(i) + "-boxed-into-the-map"
		} else {
			m[float64(i)/2] = []string{"first-" + strconv.Itoa(n), "second-" + strconv.Itoa(i)}
		}
	}
	retainhost.Retain(m)
	return len(m)
}
`}
	service := newHazardService(t)
	service.RegisterPackage("retainhost", (&replayRecorder{}).symbols())
	for _, function := range hazardCompile(t, service, testCase.source).Root().Functions {
		require.Emptyf(t, function.LocalContainerStorePCs,
			"%s stores a boxed key and value through the general map store, which hands the map off", function.Name)
	}
	runReplay(t, testCase)
}

func TestMapKeyedByIntFilledLocallySurvivesReuse(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

func Run(n int) int {
	m := map[int][]string{}
	for i := 0; i < 80; i++ {
		m[i%9] = append(m[i%9], "keyed-by-int-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
	}
	retainhost.Retain(m)
	return len(m)
}
`})
}

func TestMapFilledLocallyAcrossMinorCollectionsReadsBackRight(t *testing.T) {
	runLocalContainerReplay(t, "Run", replayCase{entry: "Run", rounds: 3, guestCheck: "Bad", budget: replayBudget, needsCompaction: true, source: `package main

import (
	"retainhost"
	"strconv"
)

var bad int

func Run(n int) int {
	m := map[string][]string{}
	for i := 0; i < 6000; i++ {
		key := "key-" + strconv.Itoa(i%37)
		m[key] = append(m[key], "filled-while-the-arena-collects-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
	}
	bad = 0
	for i := 0; i < 6000; i++ {
		want := "filled-while-the-arena-collects-" + strconv.Itoa(n) + "-" + strconv.Itoa(i)
		if m["key-"+strconv.Itoa(i%37)][i/37] != want {
			bad++
		}
	}
	retainhost.Retain(len(m))
	return len(m)
}

func Bad() int { return bad }
`})
}

const localContainerGroup = `
func group(n int) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < 96; i++ {
		key := "key-" + strconv.Itoa(i%7)
		value := "value-" + strconv.Itoa(n) + "-" + strconv.Itoa(i) + "-appended-where-the-map-holds-its-slice"
		m[key] = append(m[key], value)
	}
	return m
}
`

func requireMapEntryAppends(t *testing.T, compiled *program.CompiledFileSet, function string, granted, ungranted int) {
	t.Helper()
	for _, candidate := range compiled.Root().Functions {
		if candidate.Name != function {
			continue
		}
		var withGrant, without int
		for programCounter, word := range candidate.Body {
			switch {
			case word.Op != isa.OpAppendStringToMapString:
			case candidate.LocalContainerStorePCs.Has(programCounter):
				withGrant++
			default:
				without++
			}
		}
		require.Equalf(t, granted, withGrant, "test harness: %s must grow %d map entries in place under a grant", function, granted)
		require.Equalf(t, ungranted, without, "test harness: %s must grow %d map entries in place without a grant", function, ungranted)
		return
	}
	require.Failf(t, "test harness", "the program has no function %s", function)
}

func runMapEntryAppendReplay(t *testing.T, function string, granted, ungranted int, testCase replayCase) {
	t.Helper()
	service := newHazardService(t)
	service.RegisterPackage("retainhost", (&replayRecorder{}).symbols())
	requireMapEntryAppends(t, hazardCompile(t, service, testCase.source), function, granted, ungranted)
	runReplay(t, testCase)
}

func TestMapEntriesGrownInPlaceThenReturnedToTheHostSurviveReuse(t *testing.T) {
	runMapEntryAppendReplay(t, "group", 1, 0, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)
` + localContainerGroup + `
func Run(n int) int {
	m := group(n)
	retainhost.Retain(m)
	return len(m)
}
`})
}

func TestMapEntriesGrownInPlaceBeforeAndAfterAHandOffSurviveReuse(t *testing.T) {
	runMapEntryAppendReplay(t, "Run", 1, 1, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

var published map[string][]string

func Run(n int) int {
	m := map[string][]string{}
	for i := 0; i < 48; i++ {
		key := "key-" + strconv.Itoa(i%5)
		value := "before-the-hand-off-" + strconv.Itoa(n) + "-" + strconv.Itoa(i)
		m[key] = append(m[key], value)
	}
	published = m
	for i := 0; i < 48; i++ {
		key := "key-" + strconv.Itoa(i%9)
		value := "after-the-hand-off-" + strconv.Itoa(n) + "-" + strconv.Itoa(i)
		m[key] = append(m[key], value)
	}
	retainhost.Retain(published)
	return len(m)
}
`})
}

func TestMapEntriesGrownInAMapTheFunctionWasGivenSurviveReuse(t *testing.T) {
	runMapEntryAppendReplay(t, "add", 0, 1, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"strconv"
)

var kept = map[string][]string{}

func add(m map[string][]string, key, value string) {
	m[key] = append(m[key], value)
}

func Run(n int) int {
	for i := 0; i < 64; i++ {
		add(kept, "key-"+strconv.Itoa(i%6), "given-map-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
	}
	retainhost.Retain(kept["key-"+strconv.Itoa(n%6)])
	return len(kept)
}
`})
}

func TestMapEntriesGrownInPlaceAcrossMinorCollectionsReadBackRight(t *testing.T) {
	runMapEntryAppendReplay(t, "Run", 1, 0, replayCase{entry: "Run", rounds: 3, guestCheck: "Bad", budget: replayBudget, needsCompaction: true, source: `package main

import (
	"retainhost"
	"strconv"
)

var bad int

func Run(n int) int {
	m := map[string][]string{}
	for i := 0; i < 6000; i++ {
		key := "key-" + strconv.Itoa(i%37)
		value := "grown-in-place-while-the-arena-collects-" + strconv.Itoa(n) + "-" + strconv.Itoa(i)
		m[key] = append(m[key], value)
	}
	bad = 0
	for i := 0; i < 6000; i++ {
		want := "grown-in-place-while-the-arena-collects-" + strconv.Itoa(n) + "-" + strconv.Itoa(i)
		if m["key-"+strconv.Itoa(i%37)][i/37] != want {
			bad++
		}
	}
	retainhost.Retain(len(m))
	return len(m)
}

func Bad() int { return bad }
`})
}

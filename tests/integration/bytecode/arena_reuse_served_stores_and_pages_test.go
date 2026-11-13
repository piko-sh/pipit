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
	"slices"
	"sort"
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

type ServedItem struct {
	ID    int
	Name  string
	Tags  []string
	Nums  []int
	Score float64
}

type servedHost struct {
	mu      sync.Mutex
	strings [][]string
	ints    [][]int
	anys    []any
}

const servedSource = `package main

import (
	"servedhost"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func mk(n int) string { return "served-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func Served(n int) string {
	idx := make([]int, 64)
	names := make([]string, 64)
	for i := range names {
		idx[i] = (i*37 + n) % 64
		names[i] = mk(n + i)
	}
	seen := make([]string, 64)
	sort.Slice(idx, func(i, j int) bool {
		seen[i] = names[idx[j]]
		seen[63-j] = names[idx[i]]
		return idx[i] < idx[j]
	})
	kept := make([]string, 64)
	joined := ""
	for i := range seen {
		kept[i] = seen[i]
		if i > 0 {
			joined += ","
		}
		joined += seen[i]
	}
	servedhost.KeepStrings(kept)
	return joined
}

func Nested(n int) string {
	idx := make([]int, 64)
	seen := make([]string, 64)
	for i := range idx {
		idx[i] = (i*37 + n) % 64
	}
	sort.Slice(idx, func(i, j int) bool {
		owns := make([]string, 64)
		owns[0] = mk(n*100 + idx[i])
		inner := []int{3, 1, 2}
		sort.Slice(inner, func(a, b int) bool {
			seen[idx[j]] = owns[0]
			return inner[a] < inner[b]
		})
		return idx[i] < idx[j]
	})
	joined := ""
	for i := range seen {
		if i > 0 {
			joined += ","
		}
		joined += seen[i]
	}
	return joined
}

func Concurrent(n int) string {
	names := make([]string, 64)
	for i := range names {
		names[i] = mk(n + i)
	}
	seen := make([]string, 64)
	var mu sync.Mutex
	got := make([]string, 8)
	servedhost.Parallel(8, func(i int) {
		s := names[i] + "/" + strconv.Itoa(i)
		mu.Lock()
		got[i] = s
		mu.Unlock()
	})
	idx := make([]int, 64)
	for i := range idx {
		idx[i] = 63 - i
	}
	sort.Slice(idx, func(i, j int) bool {
		seen[idx[i]] = names[idx[j]]
		return idx[i] < idx[j]
	})
	return strings.Join(seen, ",") + "|" + strings.Join(got, ",")
}

type Item struct {
	ID    int
	Name  string
	Tags  []string
	Nums  []int
	Score float64
}

func Build(n int) Item {
	pad := []string{mk(n)}
	for i := 0; i < n%5; i++ {
		pad = append(pad, mk(i))
	}
	nums := []int{n, n + 1, n + 2}
	for i := 0; i < n%3; i++ {
		nums = append(nums, i)
	}
	return Item{ID: n, Name: mk(n), Tags: []string{mk(n + 1), mk(n + 2), pad[len(pad)-1]}, Nums: nums, Score: float64(n) / 3}
}

func Grown(n int) int {
	var tags []string
	var nums []int
	for i := 0; i < 20+n%7; i++ {
		tags = append(tags, mk(n*10+i))
		nums = append(nums, n*1000+i)
	}
	servedhost.KeepStrings(tags)
	servedhost.KeepInts(nums)
	servedhost.KeepAny(n * 3)
	servedhost.KeepAny(mk(n + 9))
	servedhost.KeepAny(float64(n) + 0.5)
	return len(tags) + len(nums)
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		ss := []string{"ZZ-" + strconv.Itoa(n+i), "ZZZZ"}
		is := []int{-1, -2, -3, -4, -5, -6, -7, -8}
		for j := 0; j < 24; j++ {
			ss = append(ss, "YY")
			is = append(is, -9)
		}
		var v any = n + i
		t += len(ss) + len(is) + v.(int)
	}
	return t
}
`

const servedRounds = 12

func (h *servedHost) keepStrings(s []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.strings = append(h.strings, s)
}

func (h *servedHost) keepInts(s []int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ints = append(h.ints, s)
}

func (h *servedHost) keepAny(v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.anys = append(h.anys, v)
}

func runParallel(workers int, body func(int)) {
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { body(i) })
	}
	wg.Wait()
}

func servedMk(n int) string { return "served-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func nativeServed(seed int) string {
	indices := make([]int, 64)
	names := make([]string, 64)
	for i := range names {
		indices[i] = (i*37 + seed) % 64
		names[i] = servedMk(seed + i)
	}
	seen := make([]string, 64)
	sort.Slice(indices, func(i, j int) bool {
		seen[i] = names[indices[j]]
		seen[63-j] = names[indices[i]]
		return indices[i] < indices[j]
	})
	return strings.Join(seen, ",")
}

func nativeNested(seed int) string {
	indices := make([]int, 64)
	seen := make([]string, 64)
	for i := range indices {
		indices[i] = (i*37 + seed) % 64
	}
	sort.Slice(indices, func(i, j int) bool {
		own := servedMk(seed*100 + indices[i])
		inner := []int{3, 1, 2}
		sort.Slice(inner, func(a, b int) bool {
			seen[indices[j]] = own
			return inner[a] < inner[b]
		})
		return indices[i] < indices[j]
	})
	return strings.Join(seen, ",")
}

func nativeConcurrent(seed int) string {
	names := make([]string, 64)
	for i := range names {
		names[i] = servedMk(seed + i)
	}
	seen := make([]string, 64)
	got := make([]string, 8)
	for i := range 8 {
		got[i] = names[i] + "/" + strconv.Itoa(i)
	}
	indices := make([]int, 64)
	for i := range indices {
		indices[i] = 63 - i
	}
	sort.Slice(indices, func(i, j int) bool {
		seen[indices[i]] = names[indices[j]]
		return indices[i] < indices[j]
	})
	return strings.Join(seen, ",") + "|" + strings.Join(got, ",")
}

func nativeBuild(seed int) ServedItem {
	pad := []string{servedMk(seed)}
	for i := 0; i < seed%5; i++ {
		pad = append(pad, servedMk(i))
	}
	nums := []int{seed, seed + 1, seed + 2}
	for i := 0; i < seed%3; i++ {
		nums = append(nums, i)
	}
	return ServedItem{ID: seed, Name: servedMk(seed), Tags: []string{servedMk(seed + 1), servedMk(seed + 2), pad[len(pad)-1]}, Nums: nums, Score: float64(seed) / 3}
}

func servedService(t *testing.T, host *servedHost) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	exports := maps.Clone(stdlib.Exports())
	exports["servedhost"] = map[string]reflect.Value{
		"KeepStrings": reflect.ValueOf(host.keepStrings),
		"KeepInts":    reflect.ValueOf(host.keepInts),
		"KeepAny":     reflect.ValueOf(host.keepAny),
		"Parallel":    reflect.ValueOf(runParallel),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled := hazardCompile(t, service, servedSource)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func reuseServedArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
	}
}

func TestServedStoresSurviveReuse(t *testing.T) {
	host := &servedHost{}
	service, compiled := servedService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().ServedStores
	results := make([]string, servedRounds)
	for n := range servedRounds {
		results[n] = requireAs[string](t, hazardCall(t, service, compiled, "Served", n))
	}
	stats := engine.LicenceDiagnostics().ServedStores
	observations.note("served stores: %d (refused unshared: %d)", stats.Stores-before.Stores, stats.Unshared-before.Unshared)
	reuseServedArenas(t, service, compiled)
	require.Len(t, host.strings, servedRounds)
	for n := range servedRounds {
		want := nativeServed(n)
		observations.compareString(fmt.Sprintf("Served(%d)", n), want, results[n])
		observations.compareString(fmt.Sprintf("kept slice of Served(%d)", n), want, strings.Join(host.strings[n], ","))
	}
	window.settle(observations)
	inconclusive := ""
	if stats.Stores == before.Stores {
		inconclusive = "no served store was made"
	}
	require.Equal(t, before.Unshared, engine.LicenceDiagnostics().ServedStores.Unshared,
		"a real callback VM refused a served store for walking unshared")
	assertNoHazard(t, observations, inconclusive)
}

func TestNestedCallbackStringsAreNotServedStores(t *testing.T) {
	host := &servedHost{}
	service, compiled := servedService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	results := make([]string, servedRounds)
	for n := range servedRounds {
		results[n] = requireAs[string](t, hazardCall(t, service, compiled, "Nested", n))
	}
	reuseServedArenas(t, service, compiled)
	for n := range servedRounds {
		observations.compareString(fmt.Sprintf("Nested(%d)", n), nativeNested(n), results[n])
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestServedStoresApplyOnlyOnTheCallersGoroutine(t *testing.T) {
	host := &servedHost{}
	service, compiled := servedService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	results := make([]string, servedRounds)
	for n := range servedRounds {
		results[n] = requireAs[string](t, hazardCall(t, service, compiled, "Concurrent", n))
	}
	reuseServedArenas(t, service, compiled)
	for n := range servedRounds {
		observations.compareString(fmt.Sprintf("Concurrent(%d)", n), nativeConcurrent(n), results[n])
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestPageRevocationPartialReleasesSurviveReuse(t *testing.T) {
	host := &servedHost{}
	service, compiled := servedService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().PageRevocation
	items := make([]ServedItem, servedRounds)
	extents := map[string][]string{}
	for round := range servedRounds {
		got := hazardCall(t, service, compiled, "Build", round)
		raw, err := jsonLikeItem(got)
		require.NoError(t, err)
		items[round] = raw
		hazardCall(t, service, compiled, "Grown", round)
	}
	for k, s := range host.strings {
		extents[fmt.Sprintf("tags %d", k)] = slices.Clone(s[:cap(s)])
	}
	intExtents := make([][]int, len(host.ints))
	for k, s := range host.ints {
		intExtents[k] = slices.Clone(s[:cap(s)])
	}
	itemExtents := make([][]string, len(items))
	numExtents := make([][]int, len(items))
	for n, item := range items {
		itemExtents[n] = slices.Clone(item.Tags[:cap(item.Tags)])
		numExtents[n] = slices.Clone(item.Nums[:cap(item.Nums)])
	}
	stats := engine.LicenceDiagnostics().PageRevocation
	observations.note("page revocation: pages revoked %d, runs kept %d, too short %d, pages retired %d, capacity fallbacks %d",
		stats.PagesRevoked-before.PagesRevoked, stats.RunsKept-before.RunsKept, stats.RunsTooShort-before.RunsTooShort,
		stats.PagesRetired-before.PagesRetired, stats.CapacityFallbacks-before.CapacityFallbacks)
	reuseServedArenas(t, service, compiled)
	for n, item := range items {
		want := nativeBuild(n)
		observations.compareString(fmt.Sprintf("Build(%d)", n), fmt.Sprint(want), fmt.Sprint(item))
		observations.compareString(fmt.Sprintf("Build(%d).Tags up to cap", n), fmt.Sprint(itemExtents[n]), fmt.Sprint(item.Tags[:cap(item.Tags)]))
		observations.compareString(fmt.Sprintf("Build(%d).Nums up to cap", n), fmt.Sprint(numExtents[n]), fmt.Sprint(item.Nums[:cap(item.Nums)]))
	}
	require.Len(t, host.strings, servedRounds)
	require.Len(t, host.ints, servedRounds)
	require.Len(t, host.anys, 3*servedRounds)
	for round := range servedRounds {
		var tags []string
		var nums []int
		for i := range 20 + round%7 {
			tags = append(tags, servedMk(round*10+i))
			nums = append(nums, round*1000+i)
		}
		observations.compareString(fmt.Sprintf("Grown(%d) tags", round), strings.Join(tags, ","), strings.Join(host.strings[round], ","))
		observations.compareString(fmt.Sprintf("Grown(%d) nums", round), fmt.Sprint(nums), fmt.Sprint(host.ints[round]))
		observations.compareString(fmt.Sprintf("Grown(%d) tags up to cap", round), fmt.Sprint(extents[fmt.Sprintf("tags %d", round)]), fmt.Sprint(host.strings[round][:cap(host.strings[round])]))
		observations.compareString(fmt.Sprintf("Grown(%d) nums up to cap", round), fmt.Sprint(intExtents[round]), fmt.Sprint(host.ints[round][:cap(host.ints[round])]))
		observations.compareString(fmt.Sprintf("Grown(%d) boxes", round), fmt.Sprint([]any{round * 3, servedMk(round + 9), float64(round) + 0.5}), fmt.Sprint(host.anys[3*round:3*round+3]))
	}
	window.settle(observations)
	inconclusive := ""
	if stats.RunsKept == before.RunsKept {
		inconclusive = "page revocation kept no run"
	}
	assertNoHazard(t, observations, inconclusive)
}

func jsonLikeItem(built any) (ServedItem, error) {
	value := reflect.ValueOf(built)
	if value.Kind() != reflect.Struct {
		return ServedItem{}, fmt.Errorf("Build returned %T", built)
	}
	return ServedItem{
		ID:    int(value.FieldByName("ID").Int()),
		Name:  value.FieldByName("Name").String(),
		Tags:  value.FieldByName("Tags").Interface().([]string),
		Nums:  value.FieldByName("Nums").Interface().([]int),
		Score: value.FieldByName("Score").Float(),
	}, nil
}

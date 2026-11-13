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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type watchedNode struct {
	next  *watchedNode
	label string
}

func TestGlobalReuseWatchAndImage(t *testing.T) {
	store := NewGlobalStore()
	names := []string{"b", "a", "c"}
	table := map[string][]int{"k": {1, 2}}
	slots := []program.GlobalVariableInfo{
		{Index: store.AllocGeneral(reflect.ValueOf(names)), Kind: isa.RegisterGeneral},
		{Index: store.AllocGeneral(reflect.ValueOf(table)), Kind: isa.RegisterGeneral},
		{Index: store.AllocInt(7), Kind: isa.RegisterInt64},
		{Index: store.AllocString("s"), Kind: isa.RegisterString},
		{Index: store.AllocGeneral(reflect.ValueOf(struct{ A, B int }{1, 2})), Kind: isa.RegisterGeneral},
	}
	watch := WatchGlobalsForReuse(store, slots)
	defer watch.Stop()
	dirty, _ := watch.Dirty()
	require.False(t, dirty)
	for _, reaches := range []reflect.Value{reflect.ValueOf(names[1:]), reflect.ValueOf(table), reflect.ValueOf(table["k"]),
		reflect.ValueOf(struct{ X []string }{names}), reflect.ValueOf(&names)} {
		check := watchCheck{watch: watch}
		check.visit(reaches, globalWatchDepth)
		require.True(t, check.found, "%v reaches a global", reaches)
	}
	for _, unrelated := range []reflect.Value{reflect.ValueOf([]string{"b"}), reflect.ValueOf("s"), reflect.ValueOf(map[string]int{})} {
		check := watchCheck{watch: watch}
		check.visit(unrelated, globalWatchDepth)
		require.False(t, check.found, "%v reaches no global", unrelated)
	}
	image := CaptureGlobalSlotImage(store, slots)
	matches, _ := image.Matches(store)
	require.True(t, matches)
	store.SetInt(slots[2].Index, 8)
	matches, why := image.Matches(store)
	require.False(t, matches, why)
	store.SetInt(slots[2].Index, 7)
	store.SetGeneral(slots[4].Index, reflect.ValueOf(struct{ A, B int }{1, 3}))
	matches, _ = image.Matches(store)
	require.False(t, matches)
	store.SetGeneral(slots[4].Index, reflect.ValueOf(struct{ A, B int }{1, 2}))
	matches, _ = image.Matches(store)
	require.True(t, matches, "an equal by-value struct matches")
	store.SetGeneral(slots[0].Index, reflect.ValueOf(append([]string(nil), names...)))
	matches, _ = image.Matches(store)
	require.False(t, matches, "a different backing does not match")
	replacement := WatchGlobalsForReuse(store, slots)
	defer replacement.Stop()
	dirty, why = watch.Dirty()
	require.True(t, dirty)
	require.Contains(t, why, "replaced")
}

func TestStopGlobalReuseWatchRemovesTheStoresWatch(t *testing.T) {
	before := globalReuseWatchCount.Load()
	store := NewGlobalStore()
	slots := []program.GlobalVariableInfo{{Index: store.AllocGeneral(reflect.ValueOf([]int{1})), Kind: isa.RegisterGeneral}}
	watch := WatchGlobalsForReuse(store, slots)
	require.True(t, watch.Installed())
	require.Equal(t, before+1, globalReuseWatchCount.Load())

	StopGlobalReuseWatch(store)
	require.False(t, watch.Installed())
	require.Equal(t, before, globalReuseWatchCount.Load(), "the count is back to its prior value")
	StopGlobalReuseWatch(store)
	watch.Stop()
	require.Equal(t, before, globalReuseWatchCount.Load(), "stopping again changes nothing")
	StopGlobalReuseWatch(nil)
	require.False(t, (*GlobalReuseWatch)(nil).Installed())
}

func TestWatchCollectorFollowsEveryReference(t *testing.T) {
	leaf := &watchedNode{label: "leaf"}
	var boxed any = leaf
	function := func() {}
	channel := make(chan int)
	cases := []struct {
		value reflect.Value
		name  string
		spans int
	}{
		{name: "a slice of pointers", value: reflect.ValueOf([]*watchedNode{leaf}), spans: 2},
		{name: "an array of pointers", value: reflect.ValueOf([1]*watchedNode{leaf}), spans: 1},
		{name: "a map of pointers", value: reflect.ValueOf(map[string]*watchedNode{"k": leaf}), spans: 2},
		{name: "a map of scalars", value: reflect.ValueOf(map[string]int{"k": 1}), spans: 1},
		{name: "a struct holding a pointer", value: reflect.ValueOf(watchedNode{next: leaf}), spans: 1},
		{name: "an interface", value: reflect.ValueOf(&boxed).Elem(), spans: 1},
		{name: "a function and a channel", value: reflect.ValueOf(struct {
			function func()
			channel  chan int
		}{function, channel}), spans: 2},
		{name: "a nil pointer", value: reflect.ValueOf((*watchedNode)(nil)), spans: 0},
		{name: "an empty slice", value: reflect.ValueOf([]*watchedNode{}), spans: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			collector := watchCollector{seen: map[uintptr]bool{}}
			collector.collect(testCase.value, globalWatchDepth)
			require.False(t, collector.incomplete)
			require.Len(t, collector.ranges, testCase.spans)
		})
	}
}

func TestWatchCollectorMarksAnImageBeyondItsBounds(t *testing.T) {
	deep := &watchedNode{}
	for range globalWatchDepth + 1 {
		deep = &watchedNode{next: deep}
	}
	wide := make([]*watchedNode, globalWatchElementLimit+1)
	many := make(map[int]*watchedNode, globalWatchElementLimit+1)
	for index := range globalWatchElementLimit + 1 {
		many[index] = &watchedNode{}
	}
	for name, value := range map[string]reflect.Value{"too deep": reflect.ValueOf(deep), "too many elements": reflect.ValueOf(wide), "too many entries": reflect.ValueOf(many)} {
		t.Run(name, func(t *testing.T) {
			collector := watchCollector{seen: map[uintptr]bool{}}
			collector.collect(value, globalWatchDepth)
			require.True(t, collector.incomplete)
		})
	}
	collector := watchCollector{seen: map[uintptr]bool{}}
	for index := range globalWatchRangeLimit {
		require.True(t, collector.add(uintptr(index+1)*8, 8))
	}
	require.False(t, collector.add(1<<40, 8), "past the range limit")
	require.True(t, collector.incomplete)
	require.False(t, collector.add(0, 8), "nothing at address zero")
}

func TestWatchGlobalsForReuseIsDirtyForAnUnwatchableImage(t *testing.T) {
	store := NewGlobalStore()
	slots := []program.GlobalVariableInfo{{Index: store.AllocGeneral(reflect.ValueOf(1)), Kind: isa.RegisterGeneral, IsIndirect: true}}
	watch := WatchGlobalsForReuse(store, slots)
	defer watch.Stop()
	dirty, why := watch.Dirty()
	require.True(t, dirty)
	require.Equal(t, "indirect global", why)

	deep := &watchedNode{}
	for range globalWatchDepth + 1 {
		deep = &watchedNode{next: deep}
	}
	other := NewGlobalStore()
	deepWatch := WatchGlobalsForReuse(other, []program.GlobalVariableInfo{{Index: other.AllocGeneral(reflect.ValueOf(deep)), Kind: isa.RegisterGeneral}})
	defer deepWatch.Stop()
	dirty, why = deepWatch.Dirty()
	require.True(t, dirty)
	require.Contains(t, why, "exceeds the watch's bounds")

	var missing *GlobalReuseWatch
	dirty, why = missing.Dirty()
	require.True(t, dirty)
	require.Equal(t, "no watch", why)
	missing.markDirty("ignored")
	missing.Stop()
}

func TestWatchCheckFindsWatchedStorageAnywhereInAnArgument(t *testing.T) {
	store := NewGlobalStore()
	watchedSlice := []*watchedNode{{label: "watched"}}
	watchedMap := map[string]*watchedNode{"k": {label: "watched"}}
	slots := []program.GlobalVariableInfo{
		{Index: store.AllocGeneral(reflect.ValueOf(watchedSlice)), Kind: isa.RegisterGeneral},
		{Index: store.AllocGeneral(reflect.ValueOf(watchedMap)), Kind: isa.RegisterGeneral},
	}
	watch := WatchGlobalsForReuse(store, slots)
	defer watch.Stop()
	var boxed any = watchedSlice[0]
	reaching := map[string]reflect.Value{
		"the slice":                       reflect.ValueOf(watchedSlice),
		"an element":                      reflect.ValueOf(watchedSlice[0]),
		"the map":                         reflect.ValueOf(watchedMap),
		"an array holding an element":     reflect.ValueOf([1]*watchedNode{watchedSlice[0]}),
		"an interface holding an element": reflect.ValueOf(&boxed).Elem(),
		"a map holding an element":        reflect.ValueOf(map[int]*watchedNode{1: watchedSlice[0]}),
		"a heap node pointing at one":     reflect.ValueOf(&watchedNode{next: watchedSlice[0]}),
	}
	for name, value := range reaching {
		check := watchCheck{watch: watch}
		check.visit(value, globalWatchDepth)
		require.True(t, check.found, name)
	}
	unrelated := map[string]reflect.Value{
		"a heap node":    reflect.ValueOf(&watchedNode{}),
		"a function":     reflect.ValueOf(func() {}),
		"a heap map":     reflect.ValueOf(map[int]*watchedNode{1: {}}),
		"scalar entries": reflect.ValueOf(map[int]int{1: 2}),
		"a heap array":   reflect.ValueOf([2]*watchedNode{}),
	}
	for name, value := range unrelated {
		check := watchCheck{watch: watch}
		check.visit(value, globalWatchDepth)
		require.False(t, check.found, name)
	}
	exhausted := watchCheck{watch: watch}
	exhausted.visit(reflect.ValueOf(&watchedNode{}), 0)
	require.True(t, exhausted.found, "a walk that runs out answers that the argument may reach a global")
}

func TestNativeCallHandedAGlobalsStorageDirtiesTheWatch(t *testing.T) {
	vm := newTestVM(t)
	watched := []*watchedNode{{label: "watched"}}
	slots := []program.GlobalVariableInfo{{Index: vm.Globals.AllocGeneral(reflect.ValueOf(watched)), Kind: isa.RegisterGeneral}}
	keep := func(nodes []*watchedNode, label string, count int) {}
	registers := &Registers{
		General: []reflect.Value{reflect.ValueOf(keep), reflect.ValueOf([]*watchedNode{{}}), reflect.ValueOf(watched)},
		Strings: []string{"label"},
		Ints:    []int64{3},
	}
	site := &program.CallSite{NativeRegister: 0, Arguments: []program.VarLocation{
		{Register: 1, Kind: isa.RegisterGeneral},
		{Register: 0, Kind: isa.RegisterString},
		{Register: 0, Kind: isa.RegisterInt64},
	}}

	noteNativeCallIfGlobalsWatched(vm, registers, site)
	watch := WatchGlobalsForReuse(vm.Globals, slots)
	defer watch.Stop()
	noteNativeCallIfGlobalsWatched(vm, registers, site)
	dirty, _ := watch.Dirty()
	require.False(t, dirty, "an argument that reaches no global")

	method := &program.CallSite{NativeRegister: 0, IsMethod: true, MethodReceiverRegister: 2}
	noteNativeCallIfGlobalsWatched(vm, registers, method)
	dirty, why := watch.Dirty()
	require.True(t, dirty, "a receiver that is a global's storage")
	require.Contains(t, why, "was handed a global's storage")
	noteNativeCallForGlobalReuse(nil, registers, site)
}

func TestReadsOnlyNativeLeavesTheWatchClean(t *testing.T) {
	vm := newTestVM(t)
	watched := []byte("watched")
	slots := []program.GlobalVariableInfo{{Index: vm.Globals.AllocGeneral(reflect.ValueOf(watched)), Kind: isa.RegisterGeneral}}
	measure := func(data []byte) int { return len(data) }
	declarations := NewBorrowDeclarations()
	require.NoError(t, declarations.Declare(measure, BorrowReadsOnly))
	declarations.Freeze()
	vm.Arena = &RegisterArena{licensed: true, borrowDeclarations: declarations}
	watch := WatchGlobalsForReuse(vm.Globals, slots)
	defer watch.Stop()
	registers := &Registers{General: []reflect.Value{reflect.ValueOf(measure), reflect.ValueOf(watched)}}
	noteNativeCallForGlobalReuse(vm, registers, &program.CallSite{Arguments: []program.VarLocation{{Register: 1, Kind: isa.RegisterGeneral}}})
	dirty, _ := watch.Dirty()
	require.False(t, dirty, "a reads-only native cannot change what it is handed")
}

func TestGlobalSlotImageMatchesEveryBank(t *testing.T) {
	store := NewGlobalStore()
	slots := []program.GlobalVariableInfo{
		{Index: store.AllocFloat(1.5), Kind: isa.RegisterFloat64},
		{Index: store.AllocBool(true), Kind: isa.RegisterBool},
		{Index: store.AllocUint(9), Kind: isa.RegisterUint64},
		{Index: store.AllocComplex(2 + 3i), Kind: isa.RegisterComplex128},
		{Index: store.AllocString("text"), Kind: isa.RegisterString},
	}
	image := CaptureGlobalSlotImage(store, slots)
	matches, why := image.Matches(store)
	require.True(t, matches, why)
	store.SetBool(slots[1].Index, false)
	matches, why = image.Matches(store)
	require.False(t, matches)
	require.Contains(t, why, "changed")
	store.SetBool(slots[1].Index, true)
	store.AllocInt(1)
	matches, why = image.Matches(store)
	require.False(t, matches)
	require.Equal(t, "global banks changed", why)

	var missing *GlobalSlotImage
	matches, why = missing.Matches(store)
	require.False(t, matches)
	require.Equal(t, "no image", why)
	require.False(t, readGlobalSlotImage(store, program.GlobalVariableInfo{Index: 99, Kind: isa.RegisterInt64}).present, "a slot outside its bank")
}

func TestSameGlobalValueComparesByIdentityOrValue(t *testing.T) {
	backing := []int{1, 2, 3}
	pointer := new(int)
	function := func() {}
	type uncomparable struct{ items []int }
	var nilInterface, boxed any = nil, 3
	cases := []struct {
		current, image reflect.Value
		name           string
		same           bool
	}{
		{name: "both absent", same: true},
		{name: "one absent", current: reflect.ValueOf(1), same: false},
		{name: "different types", current: reflect.ValueOf(1), image: reflect.ValueOf(int8(1)), same: false},
		{name: "the same slice", current: reflect.ValueOf(backing), image: reflect.ValueOf(backing), same: true},
		{name: "a resliced slice", current: reflect.ValueOf(backing[:2]), image: reflect.ValueOf(backing), same: false},
		{name: "an equal copy of a slice", current: reflect.ValueOf([]int{1, 2, 3}), image: reflect.ValueOf(backing), same: false},
		{name: "the same pointer", current: reflect.ValueOf(pointer), image: reflect.ValueOf(pointer), same: true},
		{name: "another pointer", current: reflect.ValueOf(new(int)), image: reflect.ValueOf(pointer), same: false},
		{name: "the same function", current: reflect.ValueOf(function), image: reflect.ValueOf(function), same: true},
		{name: "nil interfaces", current: reflect.ValueOf(&nilInterface).Elem(), image: reflect.ValueOf(&nilInterface).Elem(), same: true},
		{name: "a nil and a set interface", current: reflect.ValueOf(&nilInterface).Elem(), image: reflect.ValueOf(&boxed).Elem(), same: false},
		{name: "equal interfaces", current: reflect.ValueOf(&boxed).Elem(), image: reflect.ValueOf(&boxed).Elem(), same: true},
		{name: "an uncomparable struct", current: reflect.ValueOf(uncomparable{}), image: reflect.ValueOf(uncomparable{}), same: false},
		{name: "equal arrays", current: reflect.ValueOf([2]int{1, 2}), image: reflect.ValueOf([2]int{1, 2}), same: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.same, sameGlobalValue(testCase.current, testCase.image, 2))
		})
	}
}

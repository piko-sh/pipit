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

//go:build !safe && !(js && wasm)

package app

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

type foreignStringItem struct {
	Name  string
	Index int
}

func TestCallbackStoresAParentArenaStringSafely(t *testing.T) {
	t.Parallel()

	cache := map[string]int{}
	values := map[int]string{}
	storedParent := false
	var vm *engine.VM
	var harness *callbackHarness
	harness, vm = newCancellationVM(t, t.Context(), map[string]reflect.Value{
		"Apply": reflect.ValueOf(func(s string, fn func(string)) {
			fn(s)
			for key, index := range cache {
				storedParent = storedParent || vm.Arena.OwnsString(key) || vm.Arena.OwnsString(values[index])
			}
		}),
		"Cache":  reflect.ValueOf(func() map[string]int { return cache }),
		"Values": reflect.ValueOf(func() map[int]string { return values }),
	}, engine.VMLimits{})
	_, err := vm.Execute(harness.compile(t, `import "host"
func fill() {
	keys := host.Cache()
	vals := host.Values()
	for i := range 64 {
		key := "parent-key-" + string(rune('a'+i%26)) + string(rune('A'+i/26))
		host.Apply(key, func(_ string) {
			keys[key] = i
			vals[i] = key
		})
	}
}
fill()`))
	require.NoError(t, err)
	require.Len(t, cache, 64)
	require.Len(t, values, 64)
	require.False(t, storedParent, "a stored key or value aliases the parent's arena")

	for range 4 {
		_, err := vm.Execute(harness.compile(t, `total := ""
for i := range 2000 {
	total = "churn-" + string(rune('a'+i%26)) + total[:min(len(total), 40)]
}`))
		require.NoError(t, err)
	}

	for i := range 64 {
		want := "parent-key-" + string(rune('a'+i%26)) + string(rune('A'+i/26))
		got, ok := cache[want]
		require.True(t, ok, "key %q survives the parent's arena being reused", want)
		require.Equal(t, i, got)
		require.Equal(t, want, values[i], "value %d survives the parent's arena being reused", i)
	}
}

func TestCallbackStoresForeignStringsThroughEveryStoreRoute(t *testing.T) {
	t.Parallel()
	routes := map[string]struct {
		source string
	}{
		"struct literal into a map": {source: `m := host.Items()
	host.Apply(key, func(s string) { m[i] = host.Item{Name: s, Index: i} })`},
		"slice literal into a map": {source: `m := host.Lists()
	host.Apply(key, func(s string) { m[i] = []string{s, s} })`},
		"interface into a map": {source: `m := host.Anys()
	host.Apply(key, func(s string) { m[i] = any(s) })`},
		"channel send": {source: `ch := host.Chan()
	host.Apply(key, func(s string) { ch <- s })`},
		"append through a pointer": {source: `p := host.SlicePtr()
	host.Apply(key, func(s string) { *p = append(*p, s) })`},
		"field through a pointer": {source: `item := host.ItemPtr()
	host.Apply(key, func(s string) { item.Name = s })`},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			items := map[int]foreignStringItem{}
			lists := map[int][]string{}
			anys := map[int]any{}
			channel := make(chan string, 8)
			var slice []string
			item := &foreignStringItem{}
			var vm *engine.VM
			var harness *callbackHarness
			harness, vm = newCancellationVM(t, t.Context(), map[string]reflect.Value{
				"Item":     reflect.ValueOf((*foreignStringItem)(nil)),
				"Items":    reflect.ValueOf(func() map[int]foreignStringItem { return items }),
				"Lists":    reflect.ValueOf(func() map[int][]string { return lists }),
				"Anys":     reflect.ValueOf(func() map[int]any { return anys }),
				"Chan":     reflect.ValueOf(func() chan string { return channel }),
				"SlicePtr": reflect.ValueOf(func() *[]string { return &slice }),
				"ItemPtr":  reflect.ValueOf(func() *foreignStringItem { return item }),
				"Apply": reflect.ValueOf(func(s string, fn func(string)) {
					fn(s)
				}),
			}, engine.VMLimits{})
			_, err := vm.Execute(harness.compile(t, `import "host"
func fill() {
	for i := range 4 {
	key := "item-" + string(rune('a'+i))
	`+strings.ReplaceAll(route.source, "func(s string) {", "func(_ string) { s := key;")+`
	}
}
fill()`))
			require.NoError(t, err)

			for range 4 {
				_, err := vm.Execute(harness.compile(t, `total := ""
for i := range 2000 {
	total = "churn-" + string(rune('a'+i%26)) + total[:min(len(total), 40)]
}`))
				require.NoError(t, err)
			}
			close(channel)

			var stored []string
			for _, value := range items {
				stored = append(stored, value.Name)
			}
			for _, list := range lists {
				stored = append(stored, list...)
			}
			for _, value := range anys {
				stored = append(stored, value.(string))
			}
			for value := range channel {
				stored = append(stored, value)
			}
			stored = append(stored, slice...)
			if item.Name != "" {
				stored = append(stored, item.Name)
			}
			require.NotEmpty(t, stored)
			keys := []string{"item-a", "item-b", "item-c", "item-d"}
			for _, value := range stored {
				require.True(t, slices.Contains(keys, value), "stored string %q survives the parent's arena being reused", value)
			}
		})
	}
}

func TestBoundMethodStoresAParentArenaStringSafely(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	storedParent := false
	var vm *engine.VM
	var harness *callbackHarness
	harness, vm = newCancellationVM(t, t.Context(), map[string]reflect.Value{
		"Names": reflect.ValueOf(func() map[string]bool { return names }),
		"Apply": reflect.ValueOf(func(s string, fn func(string)) {
			fn(s)
			for name := range names {
				storedParent = storedParent || vm.Arena.OwnsString(name)
			}
		}),
	}, engine.VMLimits{})
	_, err := vm.Execute(harness.compile(t, `import "host"
type keeper struct {
	names map[string]bool
	tag   string
}

func (k keeper) Keep(_ string) { k.names[k.tag] = true }

func fill() {
	for i := range 4 {
		tag := "item-" + string(rune('a'+i))
		k := keeper{names: host.Names(), tag: tag}
		host.Apply(tag, k.Keep)
	}
}
fill()`))
	require.NoError(t, err)
	require.Len(t, names, 4)
	require.False(t, storedParent, "a stored name aliases the parent's arena")
}

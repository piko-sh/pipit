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

package engine

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type largeElem136 [17]int64

type inlineElem128 [16]int64

type largeElemPost struct {
	Title       string
	Description string
	Slug        string
	Date        string
	Story       string
	Label       string
	Summary     string
	Updated     string
	Author      string
	Image       string
	Series      string
	ArticleDate string
	URL         string
	DateLabel   string
	Year        string
	Byline      string
	Tags        []string
	Related     []string
	Order       int
	Minutes     int
	Draft       bool
	Retro       bool
}

func largeElem136For(seed int64) largeElem136 {
	var value largeElem136
	for i := range value {
		value[i] = seed*100 + int64(i) + 1
	}
	return value
}

func largeElemPostFor(seed int) largeElemPost {
	label := fmt.Sprintf("post-%d", seed)
	return largeElemPost{
		Title: label + " title", Description: label + " description", Slug: label,
		Date: "2026-10-20", Story: label + " story", Label: label + " label",
		Summary: label + " summary", Updated: "2026-10-21", Author: "author",
		Image: label + ".png", Series: "series", ArticleDate: "2026-10-19",
		URL: "/thoughts/" + label, DateLabel: "October 2026", Year: "2026",
		Byline: label + " byline", Tags: []string{"a", label}, Related: []string{label + "-r"},
		Order: seed, Minutes: seed % 13, Draft: seed%2 == 0, Retro: seed%3 == 0,
	}
}

func setStringGeneral(t *testing.T, vm *VM, registers *Registers, m reflect.Value, key string, value reflect.Value) {
	t.Helper()
	frame := &CallFrame{Function: &program.CompiledFunction{}}
	registers.General[0] = m
	registers.Strings[1] = key
	registers.General[2] = value
	require.Equal(t, opContinue, handleMapSetStringGeneral(vm, frame, registers, isa.NewInstruction(isa.OpMapSetStringGeneral, 0, 1, 2)))
}

func nativeLookup[V any](m map[string]V, key string) (value V, ok bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("native read of %q faulted: %v", key, recovered)
		}
	}()
	value, ok = m[key]
	return value, ok, nil
}

func TestMapSetStringGeneralLargeElemReadsBackNatively(t *testing.T) {
	t.Parallel()

	t.Run("an array one word over the limit", func(t *testing.T) {
		t.Parallel()
		vm, registers := newHandlerTestVM(t)
		m := map[string]largeElem136{}
		want := largeElem136For(1)
		setStringGeneral(t, vm, registers, reflect.ValueOf(m), "k", reflect.ValueOf(want))

		got, ok, err := nativeLookup(m, "k")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, got, "a native read must see the value the interpreter stored")
	})

	t.Run("a page-render struct", func(t *testing.T) {
		t.Parallel()
		vm, registers := newHandlerTestVM(t)
		m := map[string]largeElemPost{}
		want := largeElemPostFor(7)
		setStringGeneral(t, vm, registers, reflect.ValueOf(m), want.Slug, reflect.ValueOf(want))

		got, ok, err := nativeLookup(m, want.Slug)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, got)
	})

	t.Run("an array exactly at the limit stays inline", func(t *testing.T) {
		t.Parallel()
		vm, registers := newHandlerTestVM(t)
		m := map[string]inlineElem128{}
		var want inlineElem128
		for i := range want {
			want[i] = int64(i) + 1
		}
		setStringGeneral(t, vm, registers, reflect.ValueOf(m), "k", reflect.ValueOf(want))

		got, ok, err := nativeLookup(m, "k")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, got)
	})
}

func TestMapSetStringGeneralLargeElemSurvivesGrowthAndDeletes(t *testing.T) {
	t.Parallel()
	vm, registers := newHandlerTestVM(t)

	m := map[string]largeElem136{}
	first := largeElem136For(0)
	setStringGeneral(t, vm, registers, reflect.ValueOf(m), "key-0", reflect.ValueOf(first))
	got, _, err := nativeLookup(m, "key-0")
	require.NoError(t, err, "the first large element reads back natively")
	require.Equal(t, first, got, "the first large element reads back natively")

	const keys = 1000
	for i := 1; i < keys; i++ {
		setStringGeneral(t, vm, registers, reflect.ValueOf(m), fmt.Sprintf("key-%d", i), reflect.ValueOf(largeElem136For(int64(i))))
	}
	require.Len(t, m, keys)
	for i := 0; i < keys; i += 3 {
		delete(m, fmt.Sprintf("key-%d", i))
	}
	for i := 0; i < keys; i += 6 {
		setStringGeneral(t, vm, registers, reflect.ValueOf(m), fmt.Sprintf("key-%d", i), reflect.ValueOf(largeElem136For(int64(i+keys))))
	}
	for i := range keys {
		key := fmt.Sprintf("key-%d", i)
		got, ok, err := nativeLookup(m, key)
		require.NoError(t, err)
		switch {
		case i%6 == 0:
			require.True(t, ok, key)
			require.Equal(t, largeElem136For(int64(i+keys)), got, key)
		case i%3 == 0:
			require.False(t, ok, key)
		default:
			require.True(t, ok, key)
			require.Equal(t, largeElem136For(int64(i)), got, key)
		}
	}
}

func TestMapReadLargeElemFastPathsMatchNativeValues(t *testing.T) {
	t.Parallel()

	const entries = 16
	stringKeyed := make(map[string]largeElem136, entries)
	intKeyed := make(map[int64]largeElem136, entries)
	for i := range entries {
		stringKeyed[fmt.Sprintf("k%d", i)] = largeElem136For(int64(i))
		intKeyed[int64(i)] = largeElem136For(int64(i))
	}

	type readCase struct {
		name    string
		handler opcodeHandler
		m       reflect.Value
		hitKey  any
		missKey any
		hit     largeElem136
		fused   bool
	}
	tests := []readCase{
		{name: "the string-keyed get", handler: handleMapGetStringGeneral, m: reflect.ValueOf(stringKeyed), hitKey: "k5", missKey: "nope", hit: largeElem136For(5)},
		{name: "the string-keyed comma-ok", handler: handleMapIndexOkStringGeneral, m: reflect.ValueOf(stringKeyed), hitKey: "k9", missKey: "nope", hit: largeElem136For(9), fused: true},
		{name: "the string-keyed fused jump", handler: handleMapIndexOkJumpIfFalseStringGeneral, m: reflect.ValueOf(stringKeyed), hitKey: "k3", missKey: "nope", hit: largeElem136For(3), fused: true},
		{name: "the int-keyed comma-ok", handler: handleMapIndexOkIntGeneral, m: reflect.ValueOf(intKeyed), hitKey: int64(11), missKey: int64(99), hit: largeElem136For(11), fused: true},
		{name: "the int-keyed fused jump", handler: handleMapIndexOkJumpIfFalseIntGeneral, m: reflect.ValueOf(intKeyed), hitKey: int64(2), missKey: int64(99), hit: largeElem136For(2), fused: true},
	}

	setKey := func(registers *Registers, key any) {
		switch key := key.(type) {
		case int64:
			registers.Ints[fusedMapKeyRegister] = key
		case string:
			registers.Strings[fusedMapKeyRegister] = key
		}
	}

	for _, tt := range tests {
		t.Run(tt.name+" hits", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedMapFrame(t)
			registers.General[fusedMapMapRegister] = tt.m
			setKey(registers, tt.hitKey)
			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister)))
			got, ok := reflect.TypeAssert[largeElem136](registers.General[fusedMapValueRegister])
			require.True(t, ok, "the result must carry the map's element type")
			require.Equal(t, tt.hit, got, "the fast path must return the stored element, not the slot's pointer word")
			if tt.fused {
				require.Equal(t, int64(1), registers.Ints[fusedMapOkRegister])
			}
		})
		t.Run(tt.name+" misses", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedMapFrame(t)
			registers.General[fusedMapMapRegister] = tt.m
			setKey(registers, tt.missKey)
			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister)))
			got, ok := reflect.TypeAssert[largeElem136](registers.General[fusedMapValueRegister])
			require.True(t, ok)
			require.Equal(t, largeElem136{}, got)
			if tt.fused {
				require.Equal(t, int64(0), registers.Ints[fusedMapOkRegister])
			}
		})
	}
}

func TestMapElemStoredInlineBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		elem   reflect.Type
		inline bool
	}{
		{name: "a word", elem: reflect.TypeFor[int64](), inline: true},
		{name: "120 bytes", elem: reflect.TypeFor[[15]int64](), inline: true},
		{name: "exactly 128 bytes", elem: reflect.TypeFor[inlineElem128](), inline: true},
		{name: "129 bytes", elem: reflect.TypeFor[[129]byte](), inline: false},
		{name: "136 bytes", elem: reflect.TypeFor[largeElem136](), inline: false},
		{name: "256 bytes", elem: reflect.TypeFor[[32]int64](), inline: false},
		{name: "4096 bytes", elem: reflect.TypeFor[[512]int64](), inline: false},
		{name: "a page-render struct", elem: reflect.TypeFor[largeElemPost](), inline: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.inline, mapElemStoredInline(tt.elem), "%s is %d bytes", tt.elem, tt.elem.Size())
		})
	}
}

func TestMapSetStringGeneralKeepsTheFastPathForInlineElements(t *testing.T) {
	t.Parallel()
	vm, registers := newHandlerTestVM(t)

	inline := map[string]inlineElem128{}
	setStringGeneral(t, vm, registers, reflect.ValueOf(inline), "k", reflect.ValueOf(inlineElem128{1}))
	var stored string
	for key := range inline {
		stored = key
	}
	setStringGeneral(t, vm, registers, reflect.ValueOf(inline), ArenaConcatString(vm.Arena, "", "k"), reflect.ValueOf(inlineElem128{2}))
	for key := range inline {
		require.Equal(t, stringDataAddress(stored), stringDataAddress(key), "an inline hit is written through the slot and keeps the map's own key")
	}
	require.Equal(t, inlineElem128{2}, inline["k"])

	indirect := map[string]largeElem136{}
	setStringGeneral(t, vm, registers, reflect.ValueOf(indirect), ArenaConcatString(vm.Arena, "k", "ey"), reflect.ValueOf(largeElem136For(3)))
	for key := range indirect {
		require.False(t, vm.Arena.OwnsString(key), "the reflect fallback stores a materialised key")
	}
	require.Equal(t, largeElem136For(3), indirect["key"])
}

func TestWriteReflectValueToSlotZeroesTheWholeElement(t *testing.T) {
	t.Parallel()

	t.Run("64 bytes", func(t *testing.T) {
		t.Parallel()
		var slot [8]int64
		for i := range slot {
			slot[i] = -1
		}
		writeReflectValueToSlot(reflectValueABIType(reflect.TypeFor[[8]int64]()), unsafe.Pointer(&slot), reflect.Value{})
		require.Equal(t, [8]int64{}, slot)
	})
	t.Run("128 bytes", func(t *testing.T) {
		t.Parallel()
		var slot inlineElem128
		for i := range slot {
			slot[i] = -1
		}
		writeReflectValueToSlot(reflectValueABIType(reflect.TypeFor[inlineElem128]()), unsafe.Pointer(&slot), reflect.Value{})
		require.Equal(t, inlineElem128{}, slot)
	})
}

func TestMapReadInlineElemFastPathsStillMatchNativeValues(t *testing.T) {
	t.Parallel()

	stringKeyed := make(map[string]inlineElem128, 16)
	for i := range 16 {
		var value inlineElem128
		value[0], value[15] = int64(i)+1, int64(i)+100
		stringKeyed[fmt.Sprintf("k%d", i)] = value
	}
	tests := []struct {
		name    string
		handler opcodeHandler
	}{
		{name: "map get", handler: handleMapGetStringGeneral},
		{name: "comma-ok read", handler: handleMapIndexOkStringGeneral},
		{name: "fused comma-ok jump", handler: handleMapIndexOkJumpIfFalseStringGeneral},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newFusedMapFrame(t)
			registers.General[fusedMapMapRegister] = reflect.ValueOf(stringKeyed)
			registers.Strings[fusedMapKeyRegister] = "k4"
			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(fusedMapValueRegister, fusedMapMapRegister, fusedMapKeyRegister)))
			require.Equal(t, stringKeyed["k4"], registers.General[fusedMapValueRegister].Interface())
		})
	}
}

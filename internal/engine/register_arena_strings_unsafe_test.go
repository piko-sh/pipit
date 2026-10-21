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
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

var arenaStringInputs = []string{"", "a", "hello world", "HELLO World", "already lower", "ALREADY UPPER",
	"a-b-c-d-e", "ÀÉÎ mixed ünïcode", "ǅ title", "x\xffy", strings.Repeat("ab", 3000)}

func sameString(a, b string) bool {
	return len(a) == len(b) && (len(a) == 0 || unsafe.StringData(a) == unsafe.StringData(b))
}

func TestArenaStringIntrinsicsAgreeWithPackageStrings(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	for _, input := range arenaStringInputs {
		require.Equal(t, strings.ToUpper(input), arenaToUpper(arena, input), "ToUpper %q", input)
		require.Equal(t, strings.ToLower(input), arenaToLower(arena, input), "ToLower %q", input)
		for _, count := range []int{0, 1, 2, 7} {
			require.Equal(t, strings.Repeat(input, count), arenaRepeat(arena, input, count), "Repeat %q %d", input, count)
		}
		for _, pair := range [][2]string{{"-", "+"}, {"", "|"}, {"o", ""}, {"l", "LL"}, {"zz", "y"}, {"a", "a"}} {
			require.Equal(t, strings.ReplaceAll(input, pair[0], pair[1]), arenaReplaceAll(arena, input, pair[0], pair[1]),
				"ReplaceAll %q %q %q", input, pair[0], pair[1])
		}
	}
	for _, parts := range [][]string{nil, {}, {"one"}, {"a", "bb", "ccc"}, {"", ""}, {strings.Repeat("x", 5000), "y"}} {
		for _, sep := range []string{"", ",", " - "} {
			require.Equal(t, strings.Join(parts, sep), arenaJoin(arena, reflect.ValueOf(parts), sep), "Join %q %q", parts, sep)
		}
	}
	type named string
	require.Equal(t, "p+q", arenaJoin(arena, reflect.ValueOf([]named{"p", "q"}), "+"))
	require.Equal(t, "p+q", arenaJoin(arena, reflect.ValueOf([2]string{"p", "q"}), "+"))
}

func TestArenaStringIntrinsicsReturnArenaStringsOrTheirInput(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	for name, input := range map[string]string{
		"upper":   arenaToUpper(arena, "hello"),
		"lower":   arenaToLower(arena, "HELLO"),
		"repeat":  arenaRepeat(arena, "ab", 8),
		"replace": arenaReplaceAll(arena, "a-b-c", "-", "+"),
		"join":    arenaJoin(arena, reflect.ValueOf([]string{"a", "b"}), ","),
	} {
		require.True(t, arena.OwnsString(input), "%s: built in the byte slab", name)
	}
	for _, s := range []string{"HELLO", "ÀB"} {
		if strings.ToUpper(s) == s {
			require.True(t, sameString(s, arenaToUpper(arena, s)), "ToUpper returns an unchanged input itself")
		}
	}
	require.True(t, sameString("lower", arenaToLower(arena, "lower")))
	require.True(t, sameString("abc", arenaReplaceAll(arena, "abc", "-", "+")))
	require.True(t, sameString("abc", arenaRepeat(arena, "abc", 1)))
	one := "only"
	require.True(t, sameString(one, arenaJoin(arena, reflect.ValueOf([]string{one}), ",")))

	big := strings.Repeat("a", arenaIntrinsicMaxBytes+1)
	require.False(t, arena.OwnsString(arenaToUpper(arena, big)), "a large result stays on the heap")
	require.False(t, arena.OwnsString(arenaRepeat(arena, "ab", arenaIntrinsicMaxBytes)), "a large repetition stays on the heap")
	require.False(t, arena.OwnsString(arenaToUpper(arena, "ünï")), "a non-ASCII mapping stays on the heap")
	require.Equal(t, "HI", arenaToUpper(nil, "hi"), "nil arena")
}

func TestArenaStringIntrinsicsAllocateNothingOnTheHeap(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	parts := reflect.ValueOf([]string{"a", "bb", "ccc", "dddd"})
	allocs := testing.AllocsPerRun(50, func() {
		_ = arenaJoin(arena, parts, ",")
		_ = arenaToUpper(arena, "hello world")
		_ = arenaReplaceAll(arena, "a-b-c-d-e", "-", "+")
		_ = arenaRepeat(arena, "ab", 8)
	})
	require.Zero(t, allocs)
}

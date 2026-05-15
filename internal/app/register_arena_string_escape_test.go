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

//go:build !safe

package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	arenaChurnSource = `s := ""
for i := 0; i < 4096; i++ { s = s + "0123456789abcdef" }
len(s)`
)

func submitForArenaEscape(t *testing.T, session *Session, code string) any {
	t.Helper()
	result, err := session.Submit(context.Background(), code)
	require.NoError(t, err, code)
	return result
}

func TestArenaStringsInSessionMapSurviveChurn(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submitForArenaEscape(t, session, `m := map[string]string{}`)
	submitForArenaEscape(t, session, `for i := 0; i < 3; i++ { m["k"+string(rune('0'+i))] = "val-" + string(rune('0'+i)) + "-suffix" }`)
	require.Equal(t, "val-1-suffix", submitForArenaEscape(t, session, `m["k1"]`))
	submitForArenaEscape(t, session, arenaChurnSource)
	require.EqualValues(t, 3, submitForArenaEscape(t, session, `len(m)`))
	require.Equal(t, "val-1-suffix", submitForArenaEscape(t, session, `m["k1"]`), "arena-built map value read back after churn")
	require.Equal(t, "k0k1k2", submitForArenaEscape(t, session, `func() string { out := ""; for _, k := range []string{"k0","k1","k2"} { if _, ok := m[k]; ok { out += k } }; return out }()`), "arena-built map keys still resolvable after churn")
}

func TestArenaStringInPointerFieldSurvivesChurn(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submitForArenaEscape(t, session, `type Box struct{ S string }`)
	submitForArenaEscape(t, session, `p := &Box{}`)
	submitForArenaEscape(t, session, `p.S = "val-" + string(rune('7')) + "-suffix"`)
	require.Equal(t, "val-7-suffix", submitForArenaEscape(t, session, `p.S`))
	submitForArenaEscape(t, session, arenaChurnSource)
	require.Equal(t, "val-7-suffix", submitForArenaEscape(t, session, `p.S`), "arena-built string behind a pointer after churn")
}

func TestPointerFieldStringSurvivesArenaReset(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submitForArenaEscape(t, session, `type Box struct{ S string }`)
	submitForArenaEscape(t, session, `p := &Box{}`)
	submitForArenaEscape(t, session, `n := 7`)
	submitForArenaEscape(t, session, `p.S = "val-" + string(rune('0'+n)) + "-suffix"`)
	require.Equal(t, "val-7-suffix", submitForArenaEscape(t, session, `p.S`))
	submitForArenaEscape(t, session, arenaChurnSource)
	require.Equal(t, "val-7-suffix", submitForArenaEscape(t, session, `p.S`), "runtime-built string behind a heap pointer after churn")
}

func TestAnyBoxedStringGlobalSurvivesArenaReset(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submitForArenaEscape(t, session, `n := 5`)
	submitForArenaEscape(t, session, `var x any = "box-" + string(rune('0'+n)) + "-suffix"`)
	submitForArenaEscape(t, session, `m := map[string]any{}`)
	submitForArenaEscape(t, session, `m["k"] = "box-" + string(rune('0'+n)) + "-map"`)
	submitForArenaEscape(t, session, arenaChurnSource)
	require.Equal(t, "box-5-suffix", submitForArenaEscape(t, session, `x.(string)`), "boxed string global after churn")
	require.Equal(t, "box-5-map", submitForArenaEscape(t, session, `m["k"].(string)`), "boxed string map value after churn")
}

func TestNativeArgMapDoesNotAliasArena(t *testing.T) {
	t.Parallel()
	var captured map[string]string
	service := newTestServiceWithFunctions(t, "capture", map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(m map[string]string) { captured = m }),
	})
	source := `package main
import "capture"

func run() int {
	m := map[string]string{}
	for i := 0; i < 3; i++ {
		m["k"+string(rune('0'+i))] = "val-" + string(rune('0'+i)) + "-suffix"
	}
	capture.Keep(m)
	return len(m)
}
func main() {}
`
	ctx := context.Background()
	_, err := service.EvalFile(ctx, source, "run")
	require.NoError(t, err)
	_, err = service.Eval(ctx, arenaChurnSource)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"k0": "val-0-suffix", "k1": "val-1-suffix", "k2": "val-2-suffix"}, captured)
}

func TestMaterialiseArenaValueDetachesStringBox(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()
	boxed := engine.BoxStringToGeneral(arena, strings.Repeat("h", 8))
	require.True(t, arena.OwnsStringBox(engine.ReflectValuePtr(boxed)))
	out := engine.MaterialiseArenaValue(arena, boxed)
	require.False(t, arena.OwnsStringBox(engine.ReflectValuePtr(out)))
	arena.Reset()
	require.Equal(t, "hhhhhhhh", out.String())
}

func TestMaterialiseStringForSliceStoreSkipsArenaBacking(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()
	arenaString := engine.ArenaConcatString(arena, "abcd", "efgh")
	require.True(t, arena.OwnsString(arenaString))
	arenaBacking := arena.AllocStringBacking(2)
	heapBacking := make([]string, 2)

	require.Equal(t, unsafe.StringData(arenaString), unsafe.StringData(engine.MaterialiseStringForTypedSliceStore(arena, arenaBacking, arenaString)), "arena backing keeps the arena string")
	heapStored := engine.MaterialiseStringForTypedSliceStore(arena, heapBacking, arenaString)
	require.False(t, arena.OwnsString(heapStored), "heap backing receives a clone")
	require.Equal(t, "abcdefgh", heapStored)

	require.Equal(t, unsafe.StringData(arenaString), unsafe.StringData(engine.MaterialiseStringForGoAppend(arena, arenaBacking[:1], arenaString)), "spare arena capacity keeps the arena string")
	require.False(t, arena.OwnsString(engine.MaterialiseStringForGoAppend(arena, arenaBacking, arenaString)), "Go append growth clones")
	require.Equal(t, unsafe.StringData(arenaString), unsafe.StringData(engine.MaterialiseStringForArenaAppend(arena, arenaBacking, arenaString)), "arena append growth keeps the arena string")
}

func TestMaterialiseStringForFieldStoreSkipsArenaStruct(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()
	arenaString := engine.ArenaConcatString(arena, "abcd", "efgh")
	arenaStruct := arena.AllocBytes(16, 8)
	heapStruct := new([1]string)

	require.Equal(t, unsafe.StringData(arenaString), unsafe.StringData(engine.MaterialiseStringForFieldStore(arena, arenaStruct, arenaString)))
	heapStored := engine.MaterialiseStringForFieldStore(arena, unsafe.Pointer(heapStruct), arenaString)
	require.False(t, arena.OwnsString(heapStored))
	require.Equal(t, "abcdefgh", heapStored)
}

func TestMaterialiseArenaMapEntriesDetachesInPlace(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()
	arenaString := engine.ArenaConcatString(arena, "abcd", "efgh")
	values := map[string]string{"k": arenaString}
	original := reflect.ValueOf(values)
	out := engine.MaterialiseArenaValue(arena, original)
	require.Equal(t, original.UnsafePointer(), out.UnsafePointer(), "map identity preserved")
	require.False(t, arena.OwnsString(values["k"]))
	require.Equal(t, "abcdefgh", values["k"])

	keys := map[string]int{arenaString: 1}
	engine.MaterialiseArenaValue(arena, reflect.ValueOf(keys))
	require.Len(t, keys, 1)
	for key := range keys {
		require.False(t, arena.OwnsString(key))
		require.Equal(t, "abcdefgh", key)
	}
}

func TestMaterialiseArenaSliceUnconditionalDecidesByBacking(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()
	arenaString := engine.ArenaConcatString(arena, "abcd", "efgh")

	heapBacking := []string{"heap", "clean"}
	heapValue := reflect.ValueOf(heapBacking)
	require.Equal(t, heapValue, engine.MaterialiseArenaSliceUnconditional(arena, heapValue), "heap header and backing pass through")

	packed := engine.PackTypedSliceStringToGeneral(arena, heapBacking)
	require.True(t, arena.OwnsSliceHeaderPointer(engine.ReflectValuePtr(packed)))
	rewrapped := engine.MaterialiseArenaSliceUnconditional(arena, packed)
	require.False(t, arena.OwnsSliceHeaderPointer(engine.ReflectValuePtr(rewrapped)), "arena header moves to the heap")
	require.Equal(t, heapValue.UnsafePointer(), rewrapped.UnsafePointer(), "heap backing is shared, not copied")

	arenaBacking := arena.AllocStringBacking(4)[:2]
	arenaBacking[0] = arenaString
	arenaBacking[1] = "heap"
	copied := engine.MaterialiseArenaSliceUnconditional(arena, engine.PackTypedSliceStringToGeneral(arena, arenaBacking))
	require.False(t, arena.OwnsSliceBacking(copied.UnsafePointer()), "arena backing is copied to the heap")
	require.Equal(t, 2, copied.Len())
	require.Equal(t, 4, copied.Cap(), "spare capacity survives the copy")
	require.False(t, arena.OwnsString(copied.Index(0).String()), "arena element is cloned during the copy")
	require.Equal(t, []string{"abcdefgh", "heap"}, copied.Interface())
}

const (
	fillStringSliceSource = `func fill(s []string) { for i := range s { s[i] = "val-" + string(rune('0'+i)) + "-suffix" } }`
)

func TestSliceSetStringDirectStoreSurvivesArenaChurn(t *testing.T) {
	t.Parallel()
	compiledFunction, err := NewService().Compile(context.Background(), fillStringSliceSource+"\nfill(make([]string, 1))")
	require.NoError(t, err)
	require.NotEmpty(t, program.ExportFunctions(compiledFunction))
	requireContainsTier1SubOp(t, program.ExportFunctions(compiledFunction)[0], isa.SubOpSliceSetStringDirect)

	session := NewService().NewSession()
	submitForArenaEscape(t, session, fillStringSliceSource)
	submitForArenaEscape(t, session, `words := make([]string, 3)`)
	submitForArenaEscape(t, session, `fill(words)`)
	require.Equal(t, "val-1-suffix", submitForArenaEscape(t, session, `words[1]`))
	submitForArenaEscape(t, session, arenaChurnSource)
	require.Equal(t, "val-0-suffixval-1-suffixval-2-suffix", submitForArenaEscape(t, session, `words[0] + words[1] + words[2]`), "strings stored through the tier-1 []string element store after churn")
}

const (
	arenaScalarBoxChurnSource = `var sink any
for i := 0; i < 65536; i++ { sink = i + 7000000 }
sink.(int)`
)

func TestAnyBoxedIntGlobalSurvivesArenaReset(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submitForArenaEscape(t, session, `n := 5`)
	submitForArenaEscape(t, session, `var x any = n * 1000003`)
	submitForArenaEscape(t, session, `func identity(v any) any { return v }`)
	submitForArenaEscape(t, session, `var y any = identity(n * 2000003)`)
	submitForArenaEscape(t, session, `var f any = float64(n) * 1.5`)
	submitForArenaEscape(t, session, `m := map[string]any{}`)
	submitForArenaEscape(t, session, `m["k"] = n * 3000003`)
	submitForArenaEscape(t, session, `m["f"] = float64(n) * 2.5`)
	submitForArenaEscape(t, session, `boxed := []any{n * 4000003}`)
	submitForArenaEscape(t, session, arenaScalarBoxChurnSource)
	submitForArenaEscape(t, session, arenaChurnSource)
	require.EqualValues(t, 5000015, submitForArenaEscape(t, session, `x.(int)`), "boxed int global after churn")
	require.EqualValues(t, 10000015, submitForArenaEscape(t, session, `y.(int)`), "boxed int through interface call after churn")
	require.EqualValues(t, 7.5, submitForArenaEscape(t, session, `f.(float64)`), "boxed float global after churn")
	require.EqualValues(t, 15000015, submitForArenaEscape(t, session, `m["k"].(int)`), "boxed int map value after churn")
	require.EqualValues(t, 12.5, submitForArenaEscape(t, session, `m["f"].(float64)`), "boxed float map value after churn")
	require.EqualValues(t, 20000015, submitForArenaEscape(t, session, `boxed[0].(int)`), "boxed int slice element after churn")
}

func TestAnyBoxedIntReturnedFromEvalSurvivesArenaReset(t *testing.T) {
	t.Parallel()
	service := NewService()
	ctx := t.Context()
	result, err := service.Eval(ctx, `func identity(v any) any { return v }
n := 5
identity(n * 1000003)`)
	require.NoError(t, err)
	_, err = service.Eval(ctx, arenaScalarBoxChurnSource)
	require.NoError(t, err)
	require.EqualValues(t, 5000015, result)
}

func TestTypeAssertedStructStringSurvivesArenaChurn(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submitForArenaEscape(t, session, `type named struct{ name string; n int }`)
	submitForArenaEscape(t, session, `var kept named`)
	submitForArenaEscape(t, session, `func keep(v any) { if s, ok := v.(named); ok { kept = s } }`)
	submitForArenaEscape(t, session, `keep(named{name: "val-" + string(rune('7')) + "-suffix", n: 7})`)
	require.Equal(t, "val-7-suffix", submitForArenaEscape(t, session, `kept.name`))
	submitForArenaEscape(t, session, arenaChurnSource)
	require.Equal(t, "val-7-suffix", submitForArenaEscape(t, session, `kept.name`))
}

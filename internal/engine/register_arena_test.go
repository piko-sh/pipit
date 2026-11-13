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
	"runtime"
	"strconv"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"piko.sh/stablepool"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

var (
	arenaItoaFormatIntParityCases = []int64{
		0, 1, -1, 7, -7, 10, -10, 99, -99, 100, -100,
		12345, -12345, 1 << 31, -(1 << 31),
		(1 << 62), -(1 << 62),
		9223372036854775807,
		-9223372036854775808,
	}
)

func TestRegisterArenaGrowSlabs(t *testing.T) {
	t.Parallel()
	a := &RegisterArena{
		intSlab:     make([]int64, 2),
		floatSlab:   make([]float64, 2),
		stringSlab:  make([]string, 2),
		generalSlab: make([]reflect.Value, 2),
	}

	regs := a.AllocRegisters([isa.NumRegisterKinds]uint32{2, 1, 1, 1})
	require.Len(t, regs.Ints, 2)
	require.Len(t, regs.Floats, 1)

	regs2 := a.AllocRegisters([isa.NumRegisterKinds]uint32{2, 2, 2, 2})
	require.Len(t, regs2.Ints, 2)
	require.Len(t, regs2.Floats, 2)
}

func TestRegisterArenaSaveRestore(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	save := a.save()

	regs := a.AllocRegisters([isa.NumRegisterKinds]uint32{4, 2, 2, 2})
	regs.Ints[0] = 42
	regs.Strings[0] = "hello"

	a.Restore(save)
	require.Equal(t, save.IntIndex, a.IntIndex)
	require.Equal(t, save.FloatIndex, a.FloatIndex)
}

func TestRegisterArenaGrowIndividualSlabs(t *testing.T) {
	t.Parallel()
	a := &RegisterArena{
		intSlab:     make([]int64, 1),
		floatSlab:   make([]float64, 100),
		stringSlab:  make([]string, 100),
		generalSlab: make([]reflect.Value, 100),
	}
	regs := a.AllocRegisters([isa.NumRegisterKinds]uint32{5, 1, 1, 1})
	require.Len(t, regs.Ints, 5)
	require.GreaterOrEqual(t, len(a.intSlab), 5)
}

func TestRegisterArenaPoolRoundTrip(t *testing.T) {
	t.Parallel()
	a := GetRegisterArena()
	require.NotNil(t, a)
	_ = a.AllocRegisters([isa.NumRegisterKinds]uint32{10, 5, 3, 2})
	PutRegisterArena(a)
}

func TestPutRegisterArenaNil(t *testing.T) {
	t.Parallel()
	PutRegisterArena(nil)
}

func TestRegisterArenaUpvalueCellAllocation(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()

	sp := a.save()
	cells := a.allocUpvalueCells(3)
	refs := a.allocUpvalueRefs(3)

	require.Len(t, cells, 3)
	require.Len(t, refs, 3)

	require.Equal(t, isa.RegisterKind(0), cells[0].Kind)
	require.Equal(t, int64(0), cells[0].IntValue)
	require.Empty(t, cells[0].StringValue)
	require.Nil(t, refs[0].Value)

	cells[0].Kind = isa.RegisterInt64
	cells[0].IntValue = 42
	refs[0].Value = &cells[0]

	a.Restore(sp)
	require.Equal(t, sp.UpvalueCellIndex, a.UpvalueCellIndex)
	require.Equal(t, sp.UpvalueReferenceIndex, a.UpvalueReferenceIndex)

	cells2 := a.allocUpvalueCells(3)
	refs2 := a.allocUpvalueRefs(3)
	require.Equal(t, isa.RegisterKind(0), cells2[0].Kind)
	require.Equal(t, int64(0), cells2[0].IntValue)
	require.Nil(t, refs2[0].Value)
}

func TestRegisterArenaUpvalueCellGrow(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()

	cells := a.allocUpvalueCells(initialUpvalueCellSlabs + 10)
	require.Len(t, cells, initialUpvalueCellSlabs+10)
	require.GreaterOrEqual(t, len(a.upvalueCellSlab), initialUpvalueCellSlabs+10)

	refs := a.allocUpvalueRefs(initialUpvalueRefSlabs + 10)
	require.Len(t, refs, initialUpvalueRefSlabs+10)
	require.GreaterOrEqual(t, len(a.upvalueReferenceSlab), initialUpvalueRefSlabs+10)
}

func TestArenaSaveRestoreLIFO(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	sp0 := arena.save()
	regs0 := arena.AllocRegisters([isa.NumRegisterKinds]uint32{4, 2, 2, 2})
	regs0.Ints[0] = 100
	regs0.Strings[0] = "level0"

	sp1 := arena.save()
	regs1 := arena.AllocRegisters([isa.NumRegisterKinds]uint32{4, 2, 2, 2})
	regs1.Ints[0] = 200
	regs1.Strings[0] = "level1"

	sp2 := arena.save()
	regs2 := arena.AllocRegisters([isa.NumRegisterKinds]uint32{4, 2, 2, 2})
	regs2.Ints[0] = 300

	require.Equal(t, int64(100), regs0.Ints[0])
	require.Equal(t, int64(200), regs1.Ints[0])
	require.Equal(t, int64(300), regs2.Ints[0])

	arena.Restore(sp2)
	require.Equal(t, sp2.IntIndex, arena.IntIndex)

	require.Equal(t, int64(100), regs0.Ints[0])
	require.Equal(t, "level0", regs0.Strings[0])
	require.Equal(t, int64(200), regs1.Ints[0])

	arena.Restore(sp1)
	require.Equal(t, sp1.IntIndex, arena.IntIndex)
	require.Equal(t, int64(100), regs0.Ints[0])

	arena.Restore(sp0)
	require.Equal(t, sp0.IntIndex, arena.IntIndex)
}

func TestArenaGrowthUnderDeepCalls(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	const depth = 100

	saves := make([]ArenaSavePoint, depth)
	allRegs := make([]Registers, depth)

	for i := range depth {
		saves[i] = arena.save()
		allRegs[i] = arena.AllocRegisters([isa.NumRegisterKinds]uint32{4, 2, 1, 1})
		allRegs[i].Ints[0] = int64(i)
	}

	for i := range depth {
		require.Equal(t, int64(i), allRegs[i].Ints[0],
			"level %d int register incorrect", i)
	}

	for i := depth - 1; i >= 0; i-- {
		arena.Restore(saves[i])
	}
}

func TestArenaResetClearsState(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	regs := arena.AllocRegisters([isa.NumRegisterKinds]uint32{10, 5, 5, 5})
	regs.Ints[0] = 999
	regs.Strings[0] = "before reset"

	arena.Reset()

	require.Equal(t, 0, arena.IntIndex)
	require.Equal(t, 0, arena.FloatIndex)
	require.Equal(t, 0, arena.StringIndex)
	require.Equal(t, 0, arena.GeneralIndex)

	regs2 := arena.AllocRegisters([isa.NumRegisterKinds]uint32{2, 1, 1, 1})
	require.Len(t, regs2.Ints, 2)
	require.Equal(t, int64(0), regs2.Ints[0], "reset should yield zeroed registers")
}

func TestArenaPoolReuse(t *testing.T) {
	t.Parallel()

	a := GetRegisterArena()
	require.NotNil(t, a)

	regs := a.AllocRegisters([isa.NumRegisterKinds]uint32{4, 2, 2, 2, 1, 1, 1})
	require.Len(t, regs.Ints, 4)

	PutRegisterArena(a)

	b := GetRegisterArena()
	require.NotNil(t, b)
	PutRegisterArena(b)
}

func TestShrinkOvergrownSlabs(t *testing.T) {
	t.Parallel()

	a := NewRegisterArena()

	a.intSlab = make([]int64, initialIntSlabs*maxArenaMultiplier+1)
	a.floatSlab = make([]float64, initialFloatSlabs*maxArenaMultiplier+1)
	a.stringSlab = make([]string, initialStringSlabs*maxArenaMultiplier+1)
	a.generalSlab = make([]reflect.Value, initialGeneralSlabs*maxArenaMultiplier+1)
	a.boolSlab = make([]bool, initialBoolSlabs*maxArenaMultiplier+1)
	a.uintSlab = make([]uint64, initialUintSlabs*maxArenaMultiplier+1)
	a.complexSlab = make([]complex128, initialComplexSlabs*maxArenaMultiplier+1)
	a.frameSlab = make([]CallFrame, initialFrameSlabs*maxArenaMultiplier+1)
	a.callInfoBasesSlab = make([]uintptr, initialFrameSlabs*maxArenaMultiplier+1)
	a.dispatchSavesSlab = make([]asmDispatchSave, initialFrameSlabs*maxArenaMultiplier+1)
	a.upvalueCellSlab = make([]program.UpvalueCell, initialUpvalueCellSlabs*maxArenaMultiplier+1)
	a.upvalueReferenceSlab = make([]upvalue, initialUpvalueRefSlabs*maxArenaMultiplier+1)
	a.byteSlab = make([]byte, InitialByteSlabSize*maxArenaMultiplier+1)

	for range backingSlabIdleResetsBeforeShrink {
		a.Reset()
	}

	require.Len(t, a.intSlab, initialIntSlabs)
	require.Len(t, a.floatSlab, initialFloatSlabs)
	require.Len(t, a.stringSlab, initialStringSlabs)
	require.Len(t, a.generalSlab, initialGeneralSlabs)
	require.Len(t, a.boolSlab, initialBoolSlabs)
	require.Len(t, a.uintSlab, initialUintSlabs)
	require.Len(t, a.complexSlab, initialComplexSlabs)
	require.Len(t, a.frameSlab, initialFrameSlabs)
	require.Len(t, a.callInfoBasesSlab, initialFrameSlabs)
	require.Len(t, a.dispatchSavesSlab, initialFrameSlabs)
	require.Len(t, a.upvalueCellSlab, initialUpvalueCellSlabs)
	require.Len(t, a.upvalueReferenceSlab, initialUpvalueRefSlabs)
	require.Len(t, a.byteSlab, InitialByteSlabSize)
}

func TestShrinkOvergrownSlabsWithinThreshold(t *testing.T) {
	t.Parallel()

	a := NewRegisterArena()

	originalIntLen := len(a.intSlab)
	originalFloatLen := len(a.floatSlab)

	a.Reset()

	require.Len(t, a.intSlab, originalIntLen)
	require.Len(t, a.floatSlab, originalFloatLen)
}

func TestArenaConcatenateStringRuneTailNoGrowth(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	s := ArenaConcatenateString(arena, "", "ab")
	result := arenaConcatenateStringRune(arena, s, 'c')
	require.Equal(t, "abc", result)
}

func TestArenaConcatenateStringRuneTailTriggersGrowth(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	padding := make([]byte, len(arena.byteSlab)-3)
	for i := range padding {
		padding[i] = 'x'
	}
	_ = arenaBytesToString(arena, padding)

	s := ArenaConcatenateString(arena, "", "ab")
	require.Equal(t, "ab", s)

	result := arenaConcatenateStringRune(arena, s, '€')
	require.Equal(t, "ab€", result)
}

func TestArenaConcatenateStringRuneNonTail(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	first := ArenaConcatenateString(arena, "", "hello")
	_ = ArenaConcatenateString(arena, "", "world")

	result := arenaConcatenateStringRune(arena, first, '!')
	require.Equal(t, "hello!", result)
}

func TestArenaConcatenateStringRuneEmptyBase(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	result := arenaConcatenateStringRune(arena, "", 'x')
	require.Equal(t, "x", result)
}

func TestArenaConcatenateStringRuneInvalidRune(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	result := arenaConcatenateStringRune(arena, "hello", rune(-1))
	require.Equal(t, "hello\uFFFD", result)
}

func TestArenaConcatenateStringRuneMultiByteBoundary(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	padding := make([]byte, len(arena.byteSlab)-3)
	for i := range padding {
		padding[i] = 'x'
	}
	_ = arenaBytesToString(arena, padding)

	s := ArenaConcatenateString(arena, "", "a")

	result := arenaConcatenateStringRune(arena, s, '🎉')
	require.Equal(t, "a🎉", result)
}

func TestArenaConcatenateStringBothEmpty(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	result := ArenaConcatenateString(arena, "", "")
	require.Empty(t, result)
}

func TestArenaConcatenateStringTailNoGrowth(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	s := ArenaConcatenateString(arena, "", "he")
	result := ArenaConcatenateString(arena, s, "llo")
	require.Equal(t, "hello", result)
}

func TestArenaConcatenateStringTailTriggersGrowth(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	padding := make([]byte, len(arena.byteSlab)-4)
	for i := range padding {
		padding[i] = 'x'
	}
	_ = arenaBytesToString(arena, padding)

	s := ArenaConcatenateString(arena, "", "ab")
	require.Equal(t, "ab", s)

	result := ArenaConcatenateString(arena, s, "world")
	require.Equal(t, "abworld", result)
}

func TestArenaRuneToStringAcrossEncodedWidths(t *testing.T) {
	t.Parallel()

	t.Run("1-byte ASCII rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaRuneToString(arena, 'A')
		require.Equal(t, "A", result)
	})

	t.Run("2-byte rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaRuneToString(arena, 'é')
		require.Equal(t, "é", result)
	})

	t.Run("3-byte CJK rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaRuneToString(arena, '日')
		require.Equal(t, "日", result)
	})

	t.Run("4-byte emoji rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaRuneToString(arena, '🎉')
		require.Equal(t, "🎉", result)
	})

	t.Run("max valid rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaRuneToString(arena, '\U0010FFFF')
		require.Equal(t, "\U0010FFFF", result)
		require.Len(t, result, 4)
	})

	t.Run("surrogate half produces replacement", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()

		result := arenaRuneToString(arena, rune(0xD800))
		require.Equal(t, "\uFFFD", result)
	})

	t.Run("NUL rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaRuneToString(arena, 0)
		require.Equal(t, "\x00", result)
		require.Len(t, result, 1)
	})
}

func TestArenaConcatenateStringRuneAcrossUTF8Widths(t *testing.T) {
	t.Parallel()

	t.Run("concat 4-byte emoji to ASCII", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		s := ArenaConcatenateString(arena, "", "ok")
		result := arenaConcatenateStringRune(arena, s, '🎊')
		require.Equal(t, "ok🎊", result)
	})

	t.Run("concat surrogate half produces replacement", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		s := ArenaConcatenateString(arena, "", "abc")
		result := arenaConcatenateStringRune(arena, s, rune(0xD800))
		require.Equal(t, "abc\uFFFD", result)
	})

	t.Run("concat max valid rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		s := ArenaConcatenateString(arena, "", "x")
		result := arenaConcatenateStringRune(arena, s, '\U0010FFFF')
		require.Equal(t, "x\U0010FFFF", result)
	})

	t.Run("concat NUL rune", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		s := ArenaConcatenateString(arena, "", "ab")
		result := arenaConcatenateStringRune(arena, s, 0)
		require.Equal(t, "ab\x00", result)
	})
}

func TestArenaConcatenateStringAcrossUTF8Widths(t *testing.T) {
	t.Parallel()

	t.Run("concat two multi-byte strings", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := ArenaConcatenateString(arena, "日本", "語")
		require.Equal(t, "日本語", result)
	})

	t.Run("concat emoji strings", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := ArenaConcatenateString(arena, "🎉", "🎊")
		require.Equal(t, "🎉🎊", result)
	})

	t.Run("concat with NUL bytes", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := ArenaConcatenateString(arena, "a\x00", "b\x00c")
		require.Equal(t, "a\x00b\x00c", result)
	})

	t.Run("concat invalid UTF-8 preserves bytes", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := ArenaConcatenateString(arena, "\x80\x81", "\x82\x83")
		require.Equal(t, "\x80\x81\x82\x83", result)
		require.Len(t, result, 4)
	})
}

func TestArenaBytesToStringAtLengthBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("invalid UTF-8 preserved", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaBytesToString(arena, []byte{0x80, 0x81, 0x82})
		require.Equal(t, "\x80\x81\x82", result)
		require.Len(t, result, 3)
	})

	t.Run("NUL bytes preserved", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaBytesToString(arena, []byte{0, 0, 0})
		require.Equal(t, "\x00\x00\x00", result)
		require.Len(t, result, 3)
	})

	t.Run("empty slice returns empty string", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		result := arenaBytesToString(arena, []byte{})
		require.Empty(t, result)
	})
}

func TestGrowByteSlabDoublesSize(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	originalLen := len(arena.byteSlab)
	arena.GrowByteSlab(1)

	require.Len(t, arena.byteSlab, originalLen*2)
	require.Equal(t, 0, arena.byteIndex)
	require.Len(t, arena.oldByteSlabs, 1)
	require.Len(t, arena.oldByteSlabs[0], originalLen)
}

func TestGrowByteSlabMinExtraLargerThanDouble(t *testing.T) {
	t.Parallel()
	arena := &RegisterArena{
		byteSlab: make([]byte, 10),
	}

	arena.GrowByteSlab(100)

	require.Len(t, arena.byteSlab, 100)
	require.Equal(t, 0, arena.byteIndex)
}

func TestGrowByteSlabPreservesOldStrings(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	s := arenaBytesToString(arena, []byte("preserved"))

	arena.GrowByteSlab(1)

	require.Equal(t, "preserved", s)
}

func TestGrowByteSlabMultipleGrowths(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	arena.GrowByteSlab(1)
	arena.GrowByteSlab(1)
	arena.GrowByteSlab(1)

	require.Len(t, arena.oldByteSlabs, 3)
}

func TestArenaItoaStringParityWithStrconv(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	for _, x := range arenaItoaFormatIntParityCases {
		got := arenaItoaString(arena, x)
		want := strconv.FormatInt(x, 10)
		require.Equal(t, want, got, "arenaItoaString(%d) parity mismatch", x)
	}
}

func TestArenaFormatIntStringParityWithStrconv(t *testing.T) {
	t.Parallel()

	bases := []int{2, 8, 10, 16, 36}
	arena := NewRegisterArena()
	for _, x := range arenaItoaFormatIntParityCases {
		for _, base := range bases {
			got := arenaFormatIntString(arena, x, base)
			want := strconv.FormatInt(x, base)
			require.Equal(t, want, got, "arenaFormatIntString(%d, base=%d) parity mismatch", x, base)
		}
	}
}

func TestArenaItoaStringRewindsByteIndex(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: arena byteIndex bump pointer is not exposed")
	}
	t.Parallel()

	arena := NewRegisterArena()
	startIndex := arena.byteIndex

	_ = arenaItoaString(arena, 42)
	require.Equal(t, startIndex+2, arena.byteIndex, "byteIndex should advance by len(\"42\")")

	_ = arenaItoaString(arena, 7)
	require.Equal(t, startIndex+3, arena.byteIndex, "byteIndex should advance by len(\"7\")")

	_ = arenaItoaString(arena, -100)
	require.Equal(t, startIndex+7, arena.byteIndex, "byteIndex should advance by len(\"-100\")")
}

func TestArenaItoaStringNoAllocSteadyState(t *testing.T) {

	arena := NewRegisterArena()

	for _, x := range arenaItoaFormatIntParityCases {
		_ = arenaItoaString(arena, x)
	}

	allocs := testing.AllocsPerRun(100, func() {
		_ = arenaItoaString(arena, 12345)
	})
	if allocs != 0 {
		t.Logf("note: arenaItoaString reported %v allocs/op (expected 0 in unsafe build); "+
			"if running with -tags=safe, this is expected.", allocs)
	}
}

func TestArenaAllocIntBackingShape(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	s := arena.AllocIntBacking(5)
	require.Len(t, s, 5, "AllocIntBacking len")
	require.Equal(t, 5, cap(s), "AllocIntBacking cap (three-index form)")

	t2 := arena.AllocIntBacking(3)
	require.Equal(t, 3, cap(t2))

	for i := range s {
		s[i] = int64(i + 100)
	}
	for i := range t2 {
		t2[i] = int64(i + 200)
	}
	for i, v := range s {
		require.Equal(t, int64(i+100), v)
	}
	for i, v := range t2 {
		require.Equal(t, int64(i+200), v)
	}
}

func TestArenaAllocIntBackingZero(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	for _, n := range []int{0, -1, -100} {
		requireEmptyNonNilBacking(t, arena.AllocIntBacking(n), n)
		requireEmptyNonNilBacking(t, arena.AllocFloatBacking(n), n)
		requireEmptyNonNilBacking(t, arena.AllocStringBacking(n), n)
		requireEmptyNonNilBacking(t, arena.AllocBoolBacking(n), n)
		requireEmptyNonNilBacking(t, arena.AllocUintBacking(n), n)
	}
}

func requireEmptyNonNilBacking[T any](t *testing.T, s []T, n int) {
	t.Helper()
	require.NotNil(t, s, "n=%d should return a non-nil empty backing", n)
	require.Empty(t, s, "n=%d should return a zero-length backing", n)
	require.Zero(t, cap(s), "n=%d should return a zero-capacity backing", n)
}

func TestArenaAllocIntBackingGrow(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	initialCap := len(arena.intBackingSlab)

	drained := arena.AllocIntBacking(initialCap)
	for i := range drained {
		drained[i] = int64(i)
	}

	require.Empty(t, arena.oldIntBackings, "no old slabs before grow")
	grown := arena.AllocIntBacking(10)
	require.Len(t, grown, 10)
	require.Len(t, arena.oldIntBackings, 1, "exactly one old slab retained")
	require.Len(t, arena.oldIntBackings[0], initialCap,
		"retained slab is the previous one at initial capacity")

	for i, v := range drained {
		require.Equal(t, int64(i), v, "post-grow read of pre-grow slice")
	}

	require.GreaterOrEqual(t, len(arena.intBackingSlab), 2*initialCap,
		"new slab is at least 2x previous")
}

func TestArenaAppendInt64InCapNoAlloc(t *testing.T) {

	arena := NewRegisterArena()
	s := arena.AllocIntBacking(100)[:0:100]

	allocs := testing.AllocsPerRun(50, func() {
		s = arenaAppendInt64(arena, s[:0], 42)
	})
	require.Zero(t, allocs, "append in-cap must not allocate")
}

func TestArenaAppendInt64GrowFromHeapSlice(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: arenaAppendInt64 does not route through intBackingSlab")
	}
	t.Parallel()

	arena := NewRegisterArena()

	s := []int64{1, 2, 3}
	require.Equal(t, len(s), cap(s))

	out := arenaAppendInt64(arena, s, 4)
	require.Equal(t, []int64{1, 2, 3, 4}, out)
	require.GreaterOrEqual(t, cap(out), 4, "grown cap is at least len(out)")
	require.False(t, arena.OwnsSliceBacking(unsafe.Pointer(&out[0])),
		"a heap-born slice keeps growing on the heap")

	arenaBorn := arenaAppendInt64(arena, nil, 1)
	require.True(t, arena.OwnsSliceBacking(unsafe.Pointer(&arenaBorn[0])), "an empty slice starts in the arena")
	grown := arenaAppendInt64(arena, arenaBorn[:cap(arenaBorn)], 2)
	require.True(t, arena.OwnsSliceBacking(unsafe.Pointer(&grown[0])), "an arena-born slice keeps growing in the arena")
	require.Equal(t, int64(2), grown[len(grown)-1])
}

func TestArenaAppendStringPreservesUnderlyingType(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	s := arenaAppendString(arena, []string{"a", "b"}, "c")
	require.Equal(t, []string{"a", "b", "c"}, s)
}

func TestArenaResetClearsBackingIndices(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	_ = arena.AllocIntBacking(50)
	_ = arena.AllocFloatBacking(50)
	_ = arena.AllocStringBacking(50)
	_ = arena.AllocBoolBacking(50)
	_ = arena.AllocUintBacking(50)

	require.NotZero(t, arena.intBackingIndex)
	require.NotZero(t, arena.floatBackingIndex)
	require.NotZero(t, arena.stringBackingIndex)
	require.NotZero(t, arena.boolBackingIndex)
	require.NotZero(t, arena.uintBackingIndex)

	arena.Reset()

	require.Zero(t, arena.intBackingIndex)
	require.Zero(t, arena.floatBackingIndex)
	require.Zero(t, arena.stringBackingIndex)
	require.Zero(t, arena.boolBackingIndex)
	require.Zero(t, arena.uintBackingIndex)
	require.Empty(t, arena.oldIntBackings)
	require.Empty(t, arena.oldFloatBackings)
	require.Empty(t, arena.oldStringBackings)
	require.Empty(t, arena.oldBoolBackings)
	require.Empty(t, arena.oldUintBackings)
}

func TestArenaEnsureCapacityRespectsByteHint(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	startSize := len(arena.byteSlab)

	arena.ensureCapacity(program.TypedSlabCounts{Bytes: startSize * 4})

	require.GreaterOrEqual(t, len(arena.byteSlab), startSize*4,
		"EnsureCapacity should grow byteSlab to at least the hinted size")
}

func TestArenaEnsureCapacityRespectsBackingHints(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	hint := 4096
	arena.ensureCapacity(program.TypedSlabCounts{
		IntBacking:    hint,
		FloatBacking:  hint,
		StringBacking: hint,
		BoolBacking:   hint,
		UintBacking:   hint,
	})

	require.GreaterOrEqual(t, len(arena.intBackingSlab), hint, "intBackingSlab")
	require.GreaterOrEqual(t, len(arena.floatBackingSlab), hint, "floatBackingSlab")
	require.GreaterOrEqual(t, len(arena.stringBackingSlab), hint, "stringBackingSlab")
	require.GreaterOrEqual(t, len(arena.boolBackingSlab), hint, "boolBackingSlab")
	require.GreaterOrEqual(t, len(arena.uintBackingSlab), hint, "uintBackingSlab")
}

func TestArenaEnsureCapacityDoesNotShrink(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	arena.ensureCapacity(program.TypedSlabCounts{Bytes: InitialByteSlabSize * 8})
	bigSize := len(arena.byteSlab)
	require.Greater(t, bigSize, InitialByteSlabSize)

	arena.ensureCapacity(program.TypedSlabCounts{Bytes: 64})
	require.Len(t, arena.byteSlab, bigSize,
		"EnsureCapacity must not shrink an already-grown slab")
}

func TestAccumulateArenaBytecodeHints(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		{Op: isa.OpConcatenateString},
		{Op: isa.OpConcatenateString},
		{Op: isa.OpConcatenateStringRune},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpConvertBytesToString)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpStrconvItoa)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpStrconvFormatInt)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpConvertRuneToString)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpMakeSliceInt64)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpMakeSliceFloat64)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpMakeSliceString)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpMakeSliceBool)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpMakeSliceUint64)},

		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpMathSin)},
		{Op: isa.OpDrillTier1, A: uint8(isa.SubOpCapacityGeneral)},
	}

	var hints arenaBytecodeHints
	accumulateArenaBytecodeHints(&hints, body, nil)

	expectedBytes := 2*arenaConcatenateStringAvgBytes +
		arenaConcatenateStringRuneAvgBytes +
		arenaBytesToStringAvgBytes +
		arenaItoaMaxBytes +
		arenaFormatIntMaxBytes +
		arenaRuneToStringMaxBytes
	require.Equal(t, expectedBytes, hints.Bytes, "byte hint sum")

	require.Equal(t, 1, hints.MakeSliceInt64)
	require.Equal(t, 1, hints.MakeSliceFloat64)
	require.Equal(t, 1, hints.MakeSliceString)
	require.Equal(t, 1, hints.MakeSliceBool)
	require.Equal(t, 1, hints.MakeSliceUint64)
}

func TestArenaAllocByteBackingShape(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()

	s := arena.AllocByteBacking(5)
	require.Len(t, s, 5)
	require.Equal(t, 5, cap(s))

	t2 := arena.AllocByteBacking(3)
	require.Equal(t, 3, cap(t2))

	for i := range s {
		s[i] = byte(i + 100)
	}
	for i := range t2 {
		t2[i] = byte(i + 200)
	}
	for i, v := range s {
		require.Equal(t, byte(i+100), v)
	}
	for i, v := range t2 {
		require.Equal(t, byte(i+200), v)
	}
}

func TestArenaAllocByteBackingZero(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	for _, n := range []int{0, -1, -100} {
		requireEmptyNonNilBacking(t, arena.AllocByteBacking(n), n)
	}
}

func TestArenaAppendByteGrowFromHeapSlice(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: arenaAppendByte does not route through byteBackingSlab")
	}
	t.Parallel()

	arena := NewRegisterArena()
	s := []byte{1, 2, 3}
	require.Equal(t, len(s), cap(s))

	out := arenaAppendByte(arena, s, 4)
	require.Equal(t, []byte{1, 2, 3, 4}, out)
	require.GreaterOrEqual(t, cap(out), 4)
	require.False(t, arena.OwnsSliceBacking(unsafe.Pointer(&out[0])), "a heap-born slice keeps growing on the heap")

	arenaBorn := arenaAppendByte(arena, nil, 1)
	require.Equal(t, &arena.byteSlab[0], &arenaBorn[0], "an empty slice starts in the arena")
	grown := arenaAppendByte(arena, arenaBorn[:cap(arenaBorn)], 2)
	require.True(t, arena.OwnsSliceBacking(unsafe.Pointer(&grown[0])), "an arena-born slice keeps growing in the arena")
}

func TestOwnsBytePointer(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: arena byte slabs are not exposed")
	}
	t.Parallel()
	arena := NewRegisterArena()

	p1 := arena.AllocBytes(16, 8)
	require.True(t, arena.ownsBytePointer(p1),
		"freshly allocated arena byte pointer must be owned")

	initialBacking := arena.genericBytesSlab
	for range 64 {

		arena.AllocBytes(4096, 8)
	}
	require.NotEqual(t,
		uintptr(unsafe.Pointer(&initialBacking[0])),
		uintptr(unsafe.Pointer(&arena.genericBytesSlab[0])),
		"slab must have been re-allocated by growth")
	require.GreaterOrEqual(t, len(arena.oldGenericByteSlabs), 1,
		"retired slabs must be tracked in oldGenericByteSlabs")

	require.True(t, arena.ownsBytePointer(p1),
		"retired-slab pointer must still report owned after growth")

	p2 := arena.AllocBytes(8, 8)
	require.True(t, arena.ownsBytePointer(p2),
		"new-slab pointer must be owned")

	heapValue := new(int64)
	require.False(t, arena.ownsBytePointer(unsafe.Pointer(heapValue)),
		"heap pointer must not be reported as arena-owned")

	require.False(t, arena.ownsBytePointer(nil),
		"nil pointer must return false")

	require.False(t, arena.ownsBytePointer(zeroSizeAllocPtr),
		"zero-size sentinel must not be reported as arena-owned")
}

func TestOwnsSliceHeaderPointer(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: arena slice-header slabs are not exposed")
	}
	t.Parallel()
	arena := NewRegisterArena()

	hdr1 := arena.allocSliceHeader()
	require.True(t, arena.OwnsSliceHeaderPointer(unsafe.Pointer(hdr1)),
		"freshly allocated arena slice header must be owned")

	initialBacking := arena.sliceHeaderSlab
	for range initialSliceHeaderCapacity * 2 {
		_ = arena.allocSliceHeader()
	}
	require.NotEqual(t,
		uintptr(unsafe.Pointer(&initialBacking[0])),
		uintptr(unsafe.Pointer(&arena.sliceHeaderSlab[0])),
		"slice-header slab must have been re-allocated by growth")
	require.GreaterOrEqual(t, len(arena.oldSliceHeaderSlabs), 1,
		"retired slice-header slabs must be tracked")

	require.True(t, arena.OwnsSliceHeaderPointer(unsafe.Pointer(hdr1)),
		"retired slice-header pointer must still report owned")

	hdr2 := arena.allocSliceHeader()
	require.True(t, arena.OwnsSliceHeaderPointer(unsafe.Pointer(hdr2)),
		"new-slab slice header must be owned")

	heapHeader := &arenaSliceHeader{}
	require.False(t, arena.OwnsSliceHeaderPointer(unsafe.Pointer(heapHeader)),
		"heap slice-header must not be reported as arena-owned")

	require.False(t, arena.OwnsSliceHeaderPointer(nil),
		"nil pointer must return false")
}

func TestMaterialiseArenaValue(t *testing.T) {
	t.Parallel()
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: arena slab-ownership semantics are not exposed")
	}

	type smallStruct struct {
		A int64
		B int64
	}

	t.Run("nil_arena_passes_through", func(t *testing.T) {
		t.Parallel()
		v := reflect.ValueOf(smallStruct{A: 1, B: 2})
		out := MaterialiseArenaValue(nil, v)
		require.Equal(t, v.Interface(), out.Interface())
	})

	t.Run("invalid_value_passes_through", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		out := MaterialiseArenaValue(arena, reflect.Value{})
		require.False(t, out.IsValid())
	})

	t.Run("heap_struct_passes_through", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		original := reflect.New(reflect.TypeFor[smallStruct]()).Elem()
		original.Set(reflect.ValueOf(smallStruct{A: 7, B: 8}))
		out := MaterialiseArenaValue(arena, original)

		require.Equal(t,
			ReflectValuePtr(original),
			ReflectValuePtr(out),
			"heap struct must not be re-copied")
	})

	t.Run("arena_struct_is_copied_to_heap", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		t1 := reflect.TypeFor[smallStruct]()

		ptr := arena.AllocBytes(t1.Size(), uintptr(t1.Align()))
		require.True(t, arena.ownsBytePointer(ptr))
		arenaValue := unsafeNewAt(reflectValueABIType(t1), ptr, reflect.Struct)
		arenaValue.Set(reflect.ValueOf(smallStruct{A: 11, B: 22}))

		out := MaterialiseArenaValue(arena, arenaValue)
		require.NotEqual(t,
			ReflectValuePtr(arenaValue),
			ReflectValuePtr(out),
			"arena struct's storage must be re-anchored to heap")
		require.False(t, arena.ownsBytePointer(ReflectValuePtr(out)),
			"materialised value's storage must NOT be in arena")
		require.Equal(t, smallStruct{A: 11, B: 22}, out.Interface().(smallStruct))

		arenaValue.Set(reflect.ValueOf(smallStruct{A: 99, B: 99}))
		require.Equal(t, smallStruct{A: 11, B: 22}, out.Interface().(smallStruct),
			"materialised copy must be independent of arena slot")
	})

	t.Run("arena_struct_survives_slab_growth", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		t1 := reflect.TypeFor[smallStruct]()
		ptr := arena.AllocBytes(t1.Size(), uintptr(t1.Align()))
		arenaValue := unsafeNewAt(reflectValueABIType(t1), ptr, reflect.Struct)
		arenaValue.Set(reflect.ValueOf(smallStruct{A: 33, B: 44}))

		for range 64 {
			arena.AllocBytes(4096, 8)
		}
		require.GreaterOrEqual(t, len(arena.oldGenericByteSlabs), 1)

		require.True(t, arena.ownsBytePointer(ReflectValuePtr(arenaValue)))
		out := MaterialiseArenaValue(arena, arenaValue)
		require.False(t, arena.ownsBytePointer(ReflectValuePtr(out)))
		require.Equal(t, smallStruct{A: 33, B: 44}, out.Interface().(smallStruct))
	})

	t.Run("scalar_kinds_pass_through", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		for _, v := range []reflect.Value{
			reflect.ValueOf(int64(42)),
			reflect.ValueOf(uint64(42)),
			reflect.ValueOf(float64(3.14)),
			reflect.ValueOf(true),
			reflect.ValueOf("hello"),
		} {
			out := MaterialiseArenaValue(arena, v)
			require.Equal(t, v.Interface(), out.Interface())
		}
	})

	t.Run("pointer_map_chan_func_pass_through", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		x := 7
		ptr := reflect.ValueOf(&x)
		require.Equal(t, &x, MaterialiseArenaValue(arena, ptr).Interface())

		m := map[string]int{"a": 1}
		mv := reflect.ValueOf(m)

		require.True(t, reflect.DeepEqual(m, MaterialiseArenaValue(arena, mv).Interface()))

		ch := make(chan int, 1)
		cv := reflect.ValueOf(ch)
		require.Equal(t,
			reflect.ValueOf(ch).Pointer(),
			MaterialiseArenaValue(arena, cv).Pointer())

		fv := reflect.ValueOf(func() int { return 1 })
		require.Equal(t, fv.Pointer(), MaterialiseArenaValue(arena, fv).Pointer())
	})

	t.Run("heap_slice_passes_through", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		heap := reflect.ValueOf([]int{1, 2, 3})
		out := MaterialiseArenaValue(arena, heap)

		require.Equal(t, []int{1, 2, 3}, out.Interface().([]int))
	})
}

func BenchmarkArenaSaveInto(b *testing.B) {
	arena := NewRegisterArena()
	arena.IntIndex = 17
	arena.FloatIndex = 23
	arena.StringIndex = 11
	arena.GeneralIndex = 41
	arena.BoolIndex = 7
	arena.UintIndex = 13
	arena.ComplexIndex = 5
	arena.SlicesIntIndex = 3
	arena.SlicesFloatIndex = 4
	arena.SlicesStringIndex = 6
	arena.SlicesBoolIndex = 8
	arena.SlicesUintIndex = 9
	arena.SlicesByteIndex = 10
	arena.UpvalueCellIndex = 1
	arena.UpvalueReferenceIndex = 2
	var savePoint ArenaSavePoint
	b.ReportAllocs()
	for b.Loop() {
		arena.SaveInto(&savePoint)
	}
}

func BenchmarkArenaSaveRestoreRoundTrip(b *testing.B) {
	arena := NewRegisterArena()
	var savePoint ArenaSavePoint
	arena.SaveInto(&savePoint)
	b.ReportAllocs()
	for b.Loop() {
		arena.SaveInto(&savePoint)
		arena.Restore(savePoint)
	}
}

func TestMustPoolStopsOnARejectedConfiguration(t *testing.T) {
	require.PanicsWithValue(t,
		"engine: the engine.sharedCellMap pool's configuration is rejected: "+mustPoolError(t).Error(),
		func() { mustPool(stablepool.New[sharedCellMap](func(*sharedCellMap) {}, nil, 0)) },
		"a pool whose fixed configuration is rejected stops initialisation with the reason rather than leaving a nil pool")
	require.NotNil(t, mustPool(stablepool.New[sharedCellMap](func(*sharedCellMap) {}, nil, 1)))
}

func mustPoolError(t *testing.T) error {
	t.Helper()
	_, err := stablepool.New[sharedCellMap](func(*sharedCellMap) {}, nil, 0)
	require.Error(t, err)
	return err
}

func stablepoolRetentionBound(capacity, ceiling int) int {
	shards := 1
	for shards < runtime.GOMAXPROCS(0) {
		shards <<= 1
	}
	perShard := (max(capacity, ceiling) + shards - 1) / shards
	return shards*perShard + shards
}

func burst[T any](count int, take func() *T, give func(*T)) []*T {
	objects := make([]*T, count)
	var group sync.WaitGroup
	for index := range objects {
		group.Go(func() { objects[index] = take() })
	}
	group.Wait()
	for _, object := range objects {
		group.Go(func() { give(object) })
	}
	group.Wait()
	return objects
}

func TestRegisterArenaPoolKeepsNoMoreThanItsBoundAfterABurst(t *testing.T) {
	bound := stablepoolRetentionBound(registerArenaPool.Cap(), registerArenaPool.MaxCap())
	burst(2*bound, GetRegisterArena, PutRegisterArena)
	kept := registerArenaPool.Drain()
	t.Cleanup(func() {
		for _, arena := range kept {
			registerArenaPool.Put(arena)
		}
	})
	require.LessOrEqual(t, len(kept), bound, "after a burst of %d arenas the pool keeps at most its bound, not the burst's peak", 2*bound)
}

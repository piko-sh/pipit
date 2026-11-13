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

package passes

import (
	"context"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type fusionRefusal struct {
	compiledFunction *program.CompiledFunction
	targets          map[int]bool
	name             string
}

func requireFusionRefused(t *testing.T, fuse slotFusion, refusals []fusionRefusal) {
	t.Helper()
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			t.Parallel()
			want := slices.Clone(refusal.compiledFunction.Body)
			require.False(t, fuse(refusal.compiledFunction, refusal.compiledFunction.Body, 0, refusal.targets))
			require.Equal(t, want, refusal.compiledFunction.Body, "a refused fusion leaves the body as it found it")
		})
	}
}

func TestLateFusionStopsWhenItsContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	const negated, operand = 3, 4
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpNotInt64, negated, operand), mk(isa.OpJumpIfFalse, negated, 1, 0),
		returnVoid(), returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body)}
	compiledFunction.NumRegisters[isa.RegisterInt64] = 8

	err := (fuseLatePairsPass{}).Run(ctx, &PassContext{Analysis: newFunctionAnalysis(compiledFunction)}, compiledFunction)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, body, compiledFunction.Body, "a pass that is cancelled before its first slot fuses nothing")

	require.NoError(t, (fuseLatePairsPass{}).Run(t.Context(), &PassContext{Analysis: newFunctionAnalysis(compiledFunction)}, compiledFunction))
	require.NotEqual(t, body, compiledFunction.Body, "the same body is fused when the pass is left to run")
}

func TestFuseConstantLatchFoldsTheIncrementIntoItsTest(t *testing.T) {
	t.Parallel()
	const counter, bound = 4, 2
	latch := func(offset int16) []isa.Instruction {
		low, high := jumpOffset(offset)
		return []isa.Instruction{
			mk(isa.OpAddInt64, 6, 6, counter),
			isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, counter),
			isa.NewTier1Instruction(isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, counter, bound),
			mk(isa.OpExtension, low, high, 0),
			returnVoid(),
		}
	}

	body := latch(-4)
	require.True(t, fuseConstantLatch(body, 1, map[int]bool{0: true, 1: true}), "a jump may land on the increment, as continue does")
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant, counter, bound), body[1])
	require.True(t, isCanonicalNoOperation(body[3]), "the third word is left for compaction")
	target, ok := program.JumpTargetAt(body, 1)
	require.True(t, ok)
	require.Equal(t, 0, target, "the latch still lands on the loop body")

	body = latch(-4)
	original := slices.Clone(body)
	require.False(t, fuseConstantLatch(body, 1, map[int]bool{2: true}), "a jump that lands on the test skips the increment")
	require.Equal(t, original, body)

	body = latch(-4)
	body[2] = isa.NewTier1Instruction(isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, counter+1, bound)
	require.False(t, fuseConstantLatch(body, 1, nil), "the test must be of the register just incremented")
}

func TestFuseMultiplyAddFoldsAProductIntoTheAdditionThatUsesIt(t *testing.T) {
	t.Parallel()
	const product, factor, base, stride, sum = 9, 4, 14, 6, 7
	pair := func(addition isa.Instruction, tail ...isa.Instruction) *program.CompiledFunction {
		body := append([]isa.Instruction{mk(isa.OpMultiplyInt64Constant, product, factor, stride), addition}, tail...)
		return &program.CompiledFunction{Body: append(body, returnVoid())}
	}
	fused := mk(isa.OpMultiplyInt64ConstantThenAdd, sum, factor, stride)

	compiledFunction := pair(mk(isa.OpAddInt64, sum, base, product))
	require.True(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, []isa.Instruction{fused, mk(isa.OpExtension, base, 0, 0), returnVoid()}, compiledFunction.Body)

	compiledFunction = pair(mk(isa.OpAddInt64, sum, product, base))
	require.True(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, nil), "addition commutes, so the product may be either operand")
	require.Equal(t, mk(isa.OpExtension, base, 0, 0), compiledFunction.Body[1])

	compiledFunction = pair(mk(isa.OpAddInt64, product, base, product), mk(isa.OpAddInt64, 3, 3, product))
	require.True(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, nil), "an addition that overwrites the product leaves nothing to keep")
	require.Equal(t, mk(isa.OpMultiplyInt64ConstantThenAdd, product, factor, stride), compiledFunction.Body[0])

	compiledFunction = pair(mk(isa.OpAddInt64, sum, base, product), mk(isa.OpAddInt64, 3, 3, product))
	original := slices.Clone(compiledFunction.Body)
	require.False(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, nil), "the product is read again after the addition")
	require.Equal(t, original, compiledFunction.Body)

	compiledFunction = pair(mk(isa.OpAddInt64, sum, product, product))
	require.False(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, nil), "the product added to itself has no second register to name")

	compiledFunction = pair(mk(isa.OpAddInt64, sum, base, product))
	require.False(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, map[int]bool{1: true}), "a jump that lands on the addition skips the multiplication")

	compiledFunction = &program.CompiledFunction{Body: []isa.Instruction{
		mk(isa.OpMultiplyInt64Constant, product, factor, stride),
		mk(isa.OpNoOperation, 0, 0, 0),
		mk(isa.OpAddInt64, sum, base, product),
		returnVoid(),
	}}
	require.True(t, fuseMultiplyAdd(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, []isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0), fused, mk(isa.OpExtension, base, 0, 0), returnVoid()}, compiledFunction.Body,
		"the extension word must follow its instruction, so the pair takes the last two words")
}

func TestCloseUnhoistedByteViewPutsTheElementOperationBack(t *testing.T) {
	t.Parallel()
	const view, collection, index, value = 2, 5, 7, 3
	unbox := isa.NewTier1Instruction(isa.SubOpUnboxSliceByte, view, collection)
	read := []isa.Instruction{
		unbox,
		isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, value, view),
		mk(isa.OpExtension, index, 0, 0),
		returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(read)}
	require.True(t, closeUnhoistedByteView(compiledFunction, compiledFunction.Body, 0, nil))
	require.True(t, isCanonicalNoOperation(compiledFunction.Body[0]))
	require.Equal(t, mk(isa.OpGetElementUint64, value, collection, index), compiledFunction.Body[1])
	require.True(t, isCanonicalNoOperation(compiledFunction.Body[2]))

	store := []isa.Instruction{
		unbox,
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, view, index),
		mk(isa.OpExtension, value, 0, 0),
		returnVoid(),
	}
	compiledFunction = &program.CompiledFunction{Body: slices.Clone(store)}
	require.True(t, closeUnhoistedByteView(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, mk(isa.OpSetElementUint64, collection, index, value), compiledFunction.Body[1])

	displaced := slices.Clone(store)
	displaced[2] = mk(isa.OpExtension, value, 3, 0)
	compiledFunction = &program.CompiledFunction{Body: displaced}
	require.False(t, closeUnhoistedByteView(compiledFunction, compiledFunction.Body, 0, nil), "the element operation cannot carry a displacement")

	reused := append(slices.Clone(read[:3]),
		isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, value+1, view), mk(isa.OpExtension, index, 0, 0), returnVoid())
	compiledFunction = &program.CompiledFunction{Body: reused}
	original := slices.Clone(reused)
	require.False(t, closeUnhoistedByteView(compiledFunction, compiledFunction.Body, 0, nil), "a view read again later must stay open")
	require.Equal(t, original, compiledFunction.Body)

	apart := []isa.Instruction{unbox, mk(isa.OpAddInt64, 1, 1, 1), read[1], read[2], returnVoid()}
	compiledFunction = &program.CompiledFunction{Body: apart}
	require.False(t, closeUnhoistedByteView(compiledFunction, compiledFunction.Body, 0, nil), "only an unboxing still beside its access is one the hoist left behind")

	compiledFunction = &program.CompiledFunction{Body: slices.Clone(read)}
	require.False(t, closeUnhoistedByteView(compiledFunction, compiledFunction.Body, 0, map[int]bool{1: true}), "a jump onto the access would skip the unboxing")
}

func byteStoreAt(displacement, value uint8) []isa.Instruction {
	return []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice, displacedIndex),
		mk(isa.OpExtension, value, displacement, 0),
	}
}

func byteStores(stores ...[]isa.Instruction) []isa.Instruction {
	return append(slices.Concat(stores...), returnVoid())
}

func TestFuseByteStoreRunJoinsStoresToConsecutivePositions(t *testing.T) {
	t.Parallel()
	run := isa.NewTier1Instruction(isa.SubOpSetSliceByteElementRun, displacedSlice, displacedIndex)
	word := func(value uint8) isa.Instruction { return mk(isa.OpExtension, value, 0, 0) }
	function := func(body []isa.Instruction) *program.CompiledFunction { return &program.CompiledFunction{Body: body} }

	body := byteStores(byteStoreAt(3, 10), byteStoreAt(4, 11), byteStoreAt(5, 12), byteStoreAt(6, 13))
	require.True(t, fuseByteStoreRun(function(body), body, 0, nil))
	require.Equal(t, []isa.Instruction{
		run, mk(isa.OpExtension, 10, 3, 4), word(11), word(12), word(13), nop(), nop(), nop(), returnVoid(),
	}, body)

	body = byteStores(byteStoreAt(0, 10), byteStoreAt(1, 11))
	require.True(t, fuseByteStoreRun(function(body), body, 0, nil))
	require.Equal(t, []isa.Instruction{run, mk(isa.OpExtension, 10, 0, 2), word(11), nop(), returnVoid()}, body)

	body = byteStores(byteStoreAt(0, 10), byteStoreAt(1, 11), byteStoreAt(2, 12))
	require.True(t, fuseByteStoreRun(function(body), body, 0, nil))
	require.Equal(t, []isa.Instruction{run, mk(isa.OpExtension, 10, 0, 3), word(11), word(12), nop(), nop(), returnVoid()}, body)

	body = byteStores(byteStoreAt(0, 10), byteStoreAt(1, 11), byteStoreAt(2, 12), byteStoreAt(3, 13), byteStoreAt(4, 14))
	require.True(t, fuseByteStoreRun(function(body), body, 0, nil))
	require.Equal(t, mk(isa.OpExtension, 10, 0, 4), body[1], "a run holds four stores at most")
	require.Equal(t, byteStoreAt(4, 14), body[8:10], "the fifth store stays a store of its own")

	gap := []isa.Instruction{nop()}
	body = byteStores(byteStoreAt(0, 10), gap, byteStoreAt(1, 11), gap, gap, byteStoreAt(2, 12), gap)
	require.True(t, fuseByteStoreRun(function(body), body, 0, map[int]bool{0: true}), "no-operations between the stores are stepped over")
	require.Equal(t, []isa.Instruction{
		run, mk(isa.OpExtension, 10, 0, 3), word(11), word(12), nop(), nop(), nop(), nop(), nop(), nop(), returnVoid(),
	}, body)

	body = byteStores(byteStoreAt(254, 10), byteStoreAt(255, 11))
	require.True(t, fuseByteStoreRun(function(body), body, 0, nil))
	require.Equal(t, mk(isa.OpExtension, 10, 254, 2), body[1])
}

func TestFuseByteStoreRunGivesEachStoresLineAWord(t *testing.T) {
	t.Parallel()
	line := func(number int32) program.SourcePosition {
		return program.SourcePosition{Line: number, Column: 1, FileID: 0, Inlined: false}
	}
	body := byteStores(byteStoreAt(0, 10), []isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0)}, byteStoreAt(1, 11), byteStoreAt(2, 12), byteStoreAt(3, 13))
	positions := []program.SourcePosition{line(1), line(1), line(2), line(2), line(2), line(3), line(3), line(4), line(4), line(5)}
	compiledFunction := &program.CompiledFunction{Body: body, DebugSourceMap: &program.SourceMap{Files: nil, Positions: positions, Epilogue: line(5)}}

	require.True(t, fuseByteStoreRun(compiledFunction, body, 0, nil))
	require.Equal(t, []program.SourcePosition{line(1), line(1), line(2), line(3), line(4)}, compiledFunction.DebugSourceMap.Positions[:5],
		"the run and its first word keep the first store's line and each later word takes its own store's")

	dead := make([]bool, len(body))
	for pc, word := range body {
		dead[pc] = isCanonicalNoOperation(word)
	}
	keepSoleSourceLines(compiledFunction.DebugSourceMap, dead)
	for pc, word := range body {
		require.Equal(t, isCanonicalNoOperation(word), dead[pc], "compaction keeps no freed word, pc %d", pc)
	}
}

func TestFuseByteStoreRunRefusals(t *testing.T) {
	t.Parallel()
	otherSlice := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice+1, displacedIndex),
		mk(isa.OpExtension, 11, 1, 0),
	}
	otherIndex := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice, displacedIndex+1),
		mk(isa.OpExtension, 11, 1, 0),
	}
	tests := []struct {
		name    string
		body    []isa.Instruction
		targets map[int]bool
	}{
		{name: "one store alone", body: byteStores(byteStoreAt(0, 10))},
		{name: "the same position twice", body: byteStores(byteStoreAt(0, 10), byteStoreAt(0, 11))},
		{name: "positions that fall", body: byteStores(byteStoreAt(1, 10), byteStoreAt(0, 11))},
		{name: "a position skipped", body: byteStores(byteStoreAt(0, 10), byteStoreAt(2, 11))},
		{name: "another slice", body: byteStores(byteStoreAt(0, 10), otherSlice)},
		{name: "another index register", body: byteStores(byteStoreAt(0, 10), otherIndex)},
		{name: "a jump that lands on the second store", body: byteStores(byteStoreAt(0, 10), byteStoreAt(1, 11)), targets: map[int]bool{2: true}},
		{
			name: "an instruction between the stores",
			body: byteStores(byteStoreAt(0, 10), []isa.Instruction{mk(isa.OpAddInt64, 1, 2, 3)}, byteStoreAt(1, 11)),
		},
		{
			name:    "a jump that lands on a no-operation between the stores",
			body:    byteStores(byteStoreAt(0, 10), []isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0)}, byteStoreAt(1, 11)),
			targets: map[int]bool{2: true},
		},
		{name: "not a store", body: []isa.Instruction{mk(isa.OpAddInt64, 1, 2, 3), returnVoid()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := slices.Clone(tt.body)
			require.False(t, fuseByteStoreRun(&program.CompiledFunction{Body: tt.body}, tt.body, 0, tt.targets))
			require.Equal(t, original, tt.body)
		})
	}

	body := byteStores(byteStoreAt(0, 10), byteStoreAt(1, 11), byteStoreAt(2, 12))
	require.True(t, fuseByteStoreRun(&program.CompiledFunction{Body: body}, body, 0, map[int]bool{4: true}),
		"the stores before a jump target still join")
	require.Equal(t, mk(isa.OpExtension, 10, 0, 2), body[1])
	require.Equal(t, byteStoreAt(2, 12), body[4:6])
}

func TestFuseByteReadConversionReadsTheByteIntoTheIntRegister(t *testing.T) {
	t.Parallel()
	const view, index, unsigned, signed, collection = 2, 5, 6, 8, 3
	convert := isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, signed, unsigned)
	throughView := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, unsigned, view), mk(isa.OpExtension, index, 0, 0)}
	element := []isa.Instruction{mk(isa.OpGetElementUint64, unsigned, collection, index)}
	function := func(parts ...[]isa.Instruction) *program.CompiledFunction {
		return &program.CompiledFunction{Body: append(slices.Concat(parts...), returnVoid())}
	}

	compiledFunction := function(throughView, []isa.Instruction{convert})
	require.True(t, fuseByteReadConversion(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, signed, view), mk(isa.OpExtension, index, 0, 0), nop(), returnVoid(),
	}, compiledFunction.Body)

	compiledFunction = function(element, []isa.Instruction{nop(), convert})
	require.True(t, fuseByteReadConversion(compiledFunction, compiledFunction.Body, 0, nil), "a view closed again leaves a no-operation before the conversion")
	require.Equal(t, []isa.Instruction{mk(isa.OpGetElementInt64, signed, collection, index), nop(), nop(), returnVoid()}, compiledFunction.Body)

	refusals := []fusionRefusal{
		{name: "the byte is read again", compiledFunction: function(throughView, []isa.Instruction{convert, isa.NewTier1Instruction(isa.SubOpMoveUint64, 1, unsigned)})},
		{name: "another register is converted", compiledFunction: function(throughView, []isa.Instruction{isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, signed, unsigned+1)})},
		{name: "a jump lands on the conversion", compiledFunction: function(throughView, []isa.Instruction{convert}), targets: map[int]bool{2: true}},
		{name: "a jump lands between the two", compiledFunction: function(element, []isa.Instruction{nop(), convert}), targets: map[int]bool{1: true}},
		{name: "an instruction between the two", compiledFunction: function(element, []isa.Instruction{mk(isa.OpAddInt64, 1, 2, 3), convert})},
		{name: "no conversion", compiledFunction: function(throughView)},
		{name: "not a read", compiledFunction: function([]isa.Instruction{convert})},
	}
	requireFusionRefused(t, fuseByteReadConversion, refusals)
}

func TestFuseFieldReadRunJoinsASnapshotMoveToItsNarrowReads(t *testing.T) {
	t.Parallel()
	const destination, source, snapshotMode = 2, 5, 4
	const narrowA, narrowB, narrowC, narrowD, wide, embedded = 0, 1, 2, 3, 4, 5
	layouts := []program.StructFieldLayout{
		narrowA: {Kind: uint8(reflect.Uint8)}, narrowB: {Kind: uint8(reflect.Uint16)}, narrowC: {Kind: uint8(reflect.Uint32)},
		narrowD: {Kind: uint8(reflect.Uint8)}, wide: {Kind: uint8(reflect.Uint64)},
		embedded: {Kind: uint8(reflect.Uint64), Flags: program.StructFieldLayoutFlagEmbedded},
	}
	move := mk(isa.OpMoveGeneral, destination, source, snapshotMode)
	read := func(register, layout uint8) isa.Instruction {
		return mk(isa.OpGetStructFieldUint64, register, destination, layout)
	}
	run := isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, destination, source)
	function := func(words ...isa.Instruction) *program.CompiledFunction {
		return &program.CompiledFunction{Body: append(words, returnVoid()), StructLayoutTable: layouts}
	}
	first := func(register, layout, reads uint8) isa.Instruction {
		return mk(isa.OpExtension, register, layout, reads|snapshotMode<<isa.FieldRunModeShift)
	}

	compiledFunction := function(move, read(1, narrowA), read(3, narrowB), read(4, narrowC))
	require.True(t, fuseFieldReadRun(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, []isa.Instruction{
		run, first(1, narrowA, 3), mk(isa.OpExtension, 3, narrowB, 0), mk(isa.OpExtension, 4, narrowC, 0), returnVoid(),
	}, compiledFunction.Body)

	compiledFunction = function(move, nop(), read(1, narrowA), nop(), read(3, embedded), read(4, wide))
	require.True(t, fuseFieldReadRun(compiledFunction, compiledFunction.Body, 0, map[int]bool{0: true}), "no-operations are stepped over and a wide read ends the run")
	require.Equal(t, []isa.Instruction{
		run, first(1, narrowA, 2), mk(isa.OpExtension, 3, embedded, 0), nop(), nop(), read(4, wide), returnVoid(),
	}, compiledFunction.Body)

	compiledFunction = function(move, read(1, narrowA), read(3, narrowB), read(4, narrowC), read(6, narrowD), read(7, narrowA))
	require.True(t, fuseFieldReadRun(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, first(1, narrowA, 4), compiledFunction.Body[1], "a run holds four reads at most")
	require.Equal(t, read(7, narrowA), compiledFunction.Body[5], "the fifth read stays an instruction of its own")

	refusals := []fusionRefusal{
		{name: "one read", compiledFunction: function(move, read(1, narrowA))},
		{name: "wide fields", compiledFunction: function(move, read(1, wide), read(3, wide))},
		{name: "reads of another register", compiledFunction: function(move, mk(isa.OpGetStructFieldUint64, 1, source, narrowA), mk(isa.OpGetStructFieldUint64, 3, source, narrowB))},
		{name: "a jump that lands on the second read", compiledFunction: function(move, read(1, narrowA), read(3, narrowB)), targets: map[int]bool{2: true}},
		{name: "an instruction between the move and the reads", compiledFunction: function(move, mk(isa.OpAddInt64, 1, 2, 3), read(1, narrowA), read(3, narrowB))},
		{name: "a layout the table does not hold", compiledFunction: function(move, read(1, 200), read(3, 201))},
		{name: "a mode that does not fit beside the count", compiledFunction: function(mk(isa.OpMoveGeneral, destination, source, 16), read(1, narrowA), read(3, narrowB))},
		{name: "not a move", compiledFunction: function(read(1, narrowA), read(3, narrowB))},
	}
	requireFusionRefused(t, fuseFieldReadRun, refusals)
}

func TestFuseFieldReadRunGivesEachReadsLineAWord(t *testing.T) {
	t.Parallel()
	line := func(number int32) program.SourcePosition {
		return program.SourcePosition{Line: number, Column: 1, FileID: 0, Inlined: false}
	}
	body := []isa.Instruction{
		mk(isa.OpMoveGeneral, 2, 5, 4), mk(isa.OpNoOperation, 0, 0, 0),
		mk(isa.OpGetStructFieldUint64, 1, 2, 0), mk(isa.OpGetStructFieldUint64, 3, 2, 0), returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{
		Body:              body,
		StructLayoutTable: []program.StructFieldLayout{{Kind: uint8(reflect.Uint8)}},
		DebugSourceMap:    &program.SourceMap{Files: nil, Positions: []program.SourcePosition{line(1), line(1), line(2), line(3), line(4)}, Epilogue: line(4)},
	}
	require.True(t, fuseFieldReadRun(compiledFunction, body, 0, nil))
	require.Equal(t, []program.SourcePosition{line(1), line(2), line(3)}, compiledFunction.DebugSourceMap.Positions[:3])
}

func TestDropBoolRoundTripBeforeJumpTestsTheOriginalInt(t *testing.T) {
	t.Parallel()
	const source, flag, converted = 4, 1, 5
	toBool := isa.NewTier1Instruction(isa.SubOpConvertInt64ToBool, flag, source)
	toInt := isa.NewTier1Instruction(isa.SubOpConvertBoolToInt64, converted, flag)
	function := func(jumpRegister uint8, tail ...isa.Instruction) *program.CompiledFunction {
		body := make([]isa.Instruction, 0, 5+len(tail))
		body = append(body, toBool, toInt, mk(isa.OpJumpIfFalse, jumpRegister, 0, 0), returnVoid(), returnVoid())
		require.True(t, program.SetJumpTarget(body, 2, 4))
		return &program.CompiledFunction{Body: append(body, tail...)}
	}

	compiledFunction := function(converted)
	require.True(t, dropBoolRoundTripBeforeJump(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, nop(), compiledFunction.Body[0])
	require.Equal(t, nop(), compiledFunction.Body[1])
	require.Equal(t, uint8(source), compiledFunction.Body[2].A, "the jump tests the int the round trip started from")

	refusals := []fusionRefusal{
		{name: "the jump tests another register", compiledFunction: function(converted + 1)},
		{name: "a jump lands on the second conversion", compiledFunction: function(converted), targets: map[int]bool{1: true}},
		{name: "a jump lands on the jump", compiledFunction: function(converted), targets: map[int]bool{2: true}},
	}
	requireFusionRefused(t, dropBoolRoundTripBeforeJump, refusals)

	readAgain := &program.CompiledFunction{Body: []isa.Instruction{
		toBool, toInt, mk(isa.OpJumpIfFalse, converted, 0, 0), isa.NewTier1Instruction(isa.SubOpMoveInt64, 2, converted), returnVoid(),
	}}
	require.True(t, program.SetJumpTarget(readAgain.Body, 2, 4))
	require.False(t, dropBoolRoundTripBeforeJump(readAgain, readAgain.Body, 0, nil), "the converted int is read after the jump")
}

func TestDropBoolRoundTripBeforeJumpKeepsTheSenseOfTheJump(t *testing.T) {
	t.Parallel()
	const source, flag, converted = 4, 1, 5
	for _, jump := range []isa.Opcode{isa.OpJumpIfFalse, isa.OpJumpIfTrue} {
		t.Run(jump.String(), func(t *testing.T) {
			t.Parallel()
			body := []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpConvertInt64ToBool, flag, source),
				isa.NewTier1Instruction(isa.SubOpConvertBoolToInt64, converted, flag),
				mk(jump, converted, 0, 0), returnVoid(), returnVoid(),
			}
			require.True(t, program.SetJumpTarget(body, 2, 4))
			compiledFunction := &program.CompiledFunction{Body: body}

			require.True(t, dropBoolRoundTripBeforeJump(compiledFunction, body, 0, nil))

			require.Equal(t, []isa.Instruction{nop(), nop()}, body[:2])
			require.Equal(t, jump, body[2].Op, "an int is zero exactly when its round trip through a bool is")
			require.Equal(t, uint8(source), body[2].A)
		})
	}
}

func TestLateFusionTestsANegatedBoolWithOneJump(t *testing.T) {
	t.Parallel()
	const flag, asInt, negated, negatedAsInt = 2, 4, 3, 5
	tests := []struct {
		name string
		jump isa.Opcode
		want isa.SubOpcodeTier2
	}{
		{name: "a jump taken when the negation is false", jump: isa.OpJumpIfFalse, want: isa.SubOpTier2JumpIfTrueBool},
		{name: "a jump taken when the negation is true", jump: isa.OpJumpIfTrue, want: isa.SubOpTier2JumpIfFalseBool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpConvertBoolToInt64, asInt, flag),
				isa.NewTier1Instruction(isa.SubOpNotInt64, asInt, asInt),
				isa.NewTier1Instruction(isa.SubOpConvertInt64ToBool, negated, asInt),
				isa.NewTier1Instruction(isa.SubOpConvertBoolToInt64, negatedAsInt, negated),
				mk(tt.jump, negatedAsInt, 0, 0),
				mk(isa.OpAddInt64, 0, 0, 0),
				returnVoid(),
			}
			require.True(t, program.SetJumpTarget(body, 4, 6))
			compiledFunction := &program.CompiledFunction{Body: body}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 8
			compiledFunction.NumRegisters[isa.RegisterBool] = 4

			require.NoError(t, (fuseLatePairsPass{}).Run(t.Context(), &PassContext{Analysis: newFunctionAnalysis(compiledFunction)}, compiledFunction))

			require.Equal(t, []isa.Instruction{nop(), nop(), nop()}, body[:3], "the conversions and the negation are gone")
			require.Equal(t, isa.NewTier2Instruction(tt.want, flag), body[3], "the jump tests the flag itself, for the other value")
			target, ok := program.JumpTargetAt(body, 3)
			require.True(t, ok)
			require.Equal(t, 6, target, "the jump still lands where it did")
			require.Equal(t, mk(isa.OpAddInt64, 0, 0, 0), body[5])
		})
	}
}

func TestJumpOnBoolRegisterTestsTheFlagItself(t *testing.T) {
	t.Parallel()
	const flag, converted = 1, 5
	toInt := isa.NewTier1Instruction(isa.SubOpConvertBoolToInt64, converted, flag)
	function := func(t *testing.T, jump isa.Opcode, jumpRegister uint8, between []isa.Instruction, tail ...isa.Instruction) *program.CompiledFunction {
		t.Helper()
		body := slices.Concat([]isa.Instruction{toInt}, between, []isa.Instruction{mk(jump, jumpRegister, 0, 0), returnVoid(), returnVoid()})
		at := len(between) + 1
		require.True(t, program.SetJumpTarget(body, at, at+2))
		return &program.CompiledFunction{Body: append(body, tail...)}
	}
	for name, tt := range map[string]struct {
		jump  isa.Opcode
		fused isa.SubOpcodeTier2
	}{
		"a jump when the flag is false": {isa.OpJumpIfFalse, isa.SubOpTier2JumpIfFalseBool},
		"a jump when the flag is true":  {isa.OpJumpIfTrue, isa.SubOpTier2JumpIfTrueBool},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			adjacent := function(t, tt.jump, converted, nil)
			before, ok := program.JumpTargetAt(adjacent.Body, 1)
			require.True(t, ok)
			require.True(t, jumpOnBoolRegister(adjacent, adjacent.Body, 0, nil))
			require.Equal(t, isa.NewTier2Instruction(tt.fused, flag), adjacent.Body[0])
			require.Equal(t, isa.OpExtension, adjacent.Body[1].Op)
			after, ok := program.JumpTargetAt(adjacent.Body, 0)
			require.True(t, ok)
			require.Equal(t, before, after, "the jump lands where it did")

			apart := function(t, tt.jump, converted, []isa.Instruction{nop()})
			require.True(t, jumpOnBoolRegister(apart, apart.Body, 0, nil))
			require.Equal(t, nop(), apart.Body[0], "the conversion is gone")
			require.Equal(t, isa.NewTier2Instruction(tt.fused, flag), apart.Body[1], "the jump sits in the word before its offset")
			require.Equal(t, isa.OpExtension, apart.Body[2].Op)
		})
	}

	readAgain := function(t, isa.OpJumpIfFalse, converted, nil, mk(isa.OpAddInt64, 6, converted, converted), returnVoid())
	require.True(t, program.SetJumpTarget(readAgain.Body, 1, 4))
	refusals := []fusionRefusal{
		{name: "the jump tests another register", compiledFunction: function(t, isa.OpJumpIfFalse, converted+1, nil)},
		{name: "a jump lands on the jump", compiledFunction: function(t, isa.OpJumpIfFalse, converted, nil), targets: map[int]bool{1: true}},
		{name: "the converted int is read after the jump", compiledFunction: readAgain},
		{name: "the conversion feeds something else", compiledFunction: &program.CompiledFunction{
			Body: []isa.Instruction{toInt, mk(isa.OpAddInt64, 6, converted, converted), returnVoid()}}},
	}
	requireFusionRefused(t, jumpOnBoolRegister, refusals)
}

func TestFuseElementFieldRunReadsTheFieldsFromTheElement(t *testing.T) {
	t.Parallel()
	const collection, index, element, receiver = 3, 7, 1, 0
	const mode = 4
	read := mk(isa.OpGetElement, element, collection, index)
	words := []isa.Instruction{
		mk(isa.OpExtension, 1, 0, 3|mode<<isa.FieldRunModeShift), mk(isa.OpExtension, 3, 2, 0), mk(isa.OpExtension, 4, 3, 0),
	}
	function := func(runReceiver, runSource uint8, between []isa.Instruction, tail ...isa.Instruction) *program.CompiledFunction {
		run := isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, runReceiver, runSource)
		body := slices.Concat([]isa.Instruction{read}, between, []isa.Instruction{run}, words, tail, []isa.Instruction{returnVoid()})
		return &program.CompiledFunction{Body: body, StructLayoutTable: make([]program.StructFieldLayout, 4)}
	}

	for name, between := range map[string][]isa.Instruction{"adjacent": nil, "past a no-operation": {nop()}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := function(receiver, element, between)
			at := 1 + len(between)
			require.True(t, fuseElementFieldRun(compiledFunction, compiledFunction.Body, 0, nil))
			body := compiledFunction.Body
			require.Equal(t, nop(), body[0], "the element read is gone")
			require.Equal(t, isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, collection, index), body[at])
			require.Equal(t, words[0], body[at+1], "the first word keeps the count and the move's mode")
			require.Equal(t, mk(isa.OpExtension, 3, 2, receiver), body[at+2], "the second word names the run's register")
			require.Equal(t, words[2], body[at+3])
		})
	}

	positioned := function(receiver, element, nil)
	positioned.DebugSourceMap = &program.SourceMap{Positions: make([]program.SourcePosition, len(positioned.Body))}
	positioned.DebugSourceMap.Positions[0].Line = 41
	positioned.DebugSourceMap.Positions[1].Line = 42
	require.True(t, fuseElementFieldRun(positioned, positioned.Body, 0, nil))
	require.Equal(t, 41, int(positioned.DebugSourceMap.Positions[1].Line), "a failing index is reported where the element was read")

	refusals := []fusionRefusal{
		{name: "the run reads another register", compiledFunction: function(receiver, element+1, nil)},
		{name: "a jump lands on the run", compiledFunction: function(receiver, element, nil), targets: map[int]bool{1: true}},
		{name: "the element is read again", compiledFunction: function(receiver, element, nil, mk(isa.OpMoveGeneral, 5, element, 1))},
		{name: "the run's register is read again", compiledFunction: function(receiver, element, nil, mk(isa.OpMoveGeneral, 5, receiver, 1))},
		{name: "something else follows the element read", compiledFunction: &program.CompiledFunction{
			Body: []isa.Instruction{read, mk(isa.OpMoveGeneral, 5, element, 1), returnVoid()}}},
		{name: "a field is not held at its offset", compiledFunction: withLayoutFlag(function(receiver, element, nil), 2)},
		{name: "a layout is outside the table", compiledFunction: &program.CompiledFunction{
			Body: function(receiver, element, nil).Body, StructLayoutTable: make([]program.StructFieldLayout, 3)}},
	}
	requireFusionRefused(t, fuseElementFieldRun, refusals)
}

func withLayoutFlag(compiledFunction *program.CompiledFunction, layout int) *program.CompiledFunction {
	compiledFunction.StructLayoutTable[layout].Flags = 1
	return compiledFunction
}

func TestFuseElementFieldRunJoinsSeparateReadsOfTheElement(t *testing.T) {
	t.Parallel()
	const collection, index, element = 3, 7, 1
	read := mk(isa.OpGetElement, element, collection, index)
	field := func(destination, layout uint8) isa.Instruction {
		return mk(isa.OpGetStructFieldUint64, destination, element, layout)
	}
	function := func(body ...isa.Instruction) *program.CompiledFunction {
		return &program.CompiledFunction{Body: append(body, returnVoid()), StructLayoutTable: make([]program.StructFieldLayout, 6)}
	}
	head := isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, collection, index)
	first := func(count uint8, destination, layout uint8) isa.Instruction {
		return mk(isa.OpExtension, destination, layout, count|engine.MoveGeneralModeAlias<<isa.FieldRunModeShift)
	}

	three := function(read, field(2, 0), nop(), field(3, 1), field(4, 2))
	require.True(t, fuseElementFieldRun(three, three.Body, 0, nil))
	require.Equal(t, []isa.Instruction{
		head, first(3, 2, 0), mk(isa.OpExtension, 3, 1, element), mk(isa.OpExtension, 4, 2, 0), nop(), returnVoid(),
	}, three.Body, "the instruction takes the element read's place, its words follow it, and the second names the element's register")

	two := function(read, field(2, 5), field(3, 4))
	require.True(t, fuseElementFieldRun(two, two.Body, 0, nil))
	require.Equal(t, []isa.Instruction{head, first(2, 2, 5), mk(isa.OpExtension, 3, 4, element), returnVoid()}, two.Body)

	refusals := []fusionRefusal{
		{name: "one read", compiledFunction: function(read, field(2, 0))},
		{name: "five reads, which leave the element needed after the fourth",
			compiledFunction: function(read, field(2, 0), field(3, 1), field(4, 2), field(5, 3), field(6, 4))},
		{name: "the element is read again", compiledFunction: function(read, field(2, 0), field(3, 1), mk(isa.OpMoveGeneral, 5, element, 1))},
		{name: "a jump lands on a read", compiledFunction: function(read, field(2, 0), field(3, 1)), targets: map[int]bool{2: true}},
		{name: "a field is not held at its offset", compiledFunction: withLayoutFlag(function(read, field(2, 0), field(3, 1)), 1)},
		{name: "the reads are of another register", compiledFunction: function(read,
			mk(isa.OpGetStructFieldUint64, 2, element+1, 0), mk(isa.OpGetStructFieldUint64, 3, element+1, 1))},
	}
	requireFusionRefused(t, fuseElementFieldRun, refusals)
}

func TestDropMaskAfterTruncationRemovesAnAllOnesMask(t *testing.T) {
	t.Parallel()
	const register = 2
	function := func(width uint8, mask uint64, operand uint8) *program.CompiledFunction {
		return &program.CompiledFunction{
			UintConstants: []uint64{mask},
			Body: []isa.Instruction{
				mk(isa.OpTruncateToWidth, register, width, uint8(isa.RegisterUint64)),
				isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, operand, operand),
				mk(isa.OpExtension, 0, 0, 0),
				returnVoid(),
			},
		}
	}

	compiledFunction := function(32, 0xFFFFFFFF, register)
	require.True(t, dropMaskAfterTruncation(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, []isa.Instruction{mk(isa.OpTruncateToWidth, register, 32, uint8(isa.RegisterUint64)), nop(), nop(), returnVoid()}, compiledFunction.Body)

	require.False(t, dropMaskAfterTruncation(function(32, 0xFFFF, register), function(32, 0xFFFF, register).Body, 0, nil), "a narrower mask clears more bits")
	narrower := function(16, 0xFFFFFFFF, register)
	require.False(t, dropMaskAfterTruncation(narrower, narrower.Body, 0, nil), "a mask wider than the truncation keeps a different width")
	other := function(32, 0xFFFFFFFF, register+1)
	require.False(t, dropMaskAfterTruncation(other, other.Body, 0, nil), "the mask applies to another register")
	targeted := function(32, 0xFFFFFFFF, register)
	require.False(t, dropMaskAfterTruncation(targeted, targeted.Body, 0, map[int]bool{1: true}), "a jump lands on the mask")
}

func TestDropMaskAfterTruncationKnowsTheOperationsThatTruncateTheirOwnResult(t *testing.T) {
	t.Parallel()
	const register = 2
	mask := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, register, register), mk(isa.OpExtension, 0, 0, 0)}
	constant := mk(isa.OpExtension, 1, 0, 0)
	for name, producer := range map[string][]isa.Instruction{
		"a 32-bit sum":                 {mk(isa.OpAddUint32, register, 3, 4)},
		"a 32-bit difference":          {mk(isa.OpSubtractUint32, register, 3, 4)},
		"a 32-bit product":             {mk(isa.OpMultiplyUint32, register, 3, 4)},
		"a 32-bit sum with a constant": {isa.NewTier1Instruction(isa.SubOpAddUint32Constant, register, 3), constant},
		"a 32-bit difference from a constant": {
			isa.NewTier1Instruction(isa.SubOpSubtractUint32Constant, register, 3), constant,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := slices.Concat(producer, mask, []isa.Instruction{returnVoid()})
			compiledFunction := &program.CompiledFunction{UintConstants: []uint64{0xFFFFFFFF, 7}, Body: slices.Clone(body)}
			require.True(t, dropMaskAfterTruncation(compiledFunction, compiledFunction.Body, 0, nil))
			require.Equal(t, slices.Concat(producer, []isa.Instruction{nop(), nop(), returnVoid()}), compiledFunction.Body)

			narrower := &program.CompiledFunction{UintConstants: []uint64{0xFFFF, 7}, Body: slices.Clone(body)}
			require.False(t, dropMaskAfterTruncation(narrower, narrower.Body, 0, nil), "a 16-bit mask clears bits the operation keeps")
		})
	}
	t.Run("a full 64-bit product", func(t *testing.T) {
		t.Parallel()
		body := slices.Concat([]isa.Instruction{mk(isa.OpMultiplyUint64, register, 3, 4)}, mask, []isa.Instruction{returnVoid()})
		compiledFunction := &program.CompiledFunction{UintConstants: []uint64{0xFFFFFFFF}, Body: slices.Clone(body)}
		require.False(t, dropMaskAfterTruncation(compiledFunction, compiledFunction.Body, 0, nil))
		require.Equal(t, body, compiledFunction.Body)
	})
}

func TestDropMaskAfterTruncationReadsTheWholeConstantIndex(t *testing.T) {
	t.Parallel()
	const register = 2
	constants := make([]uint64, 258)
	constants[1], constants[257] = 0xFFFFFFFF, 0xFF
	body := []isa.Instruction{
		mk(isa.OpTruncateToWidth, register, 32, uint8(isa.RegisterUint64)),
		isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, register, register),
		mk(isa.OpExtension, 1, 1, 0),
		returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{UintConstants: constants, Body: slices.Clone(body)}
	require.False(t, dropMaskAfterTruncation(compiledFunction, compiledFunction.Body, 0, nil),
		"the mask is constant 257, an 8-bit one; constant 1 only shares its low index byte")
	require.Equal(t, body, compiledFunction.Body)
}

func TestFlipJumpOverNotTestsTheOperandWithTheOppositeJump(t *testing.T) {
	t.Parallel()
	const operand, negated = 4, 5
	function := func(t *testing.T, jump isa.Opcode, destination, jumpRegister uint8, tail ...isa.Instruction) *program.CompiledFunction {
		t.Helper()
		body := make([]isa.Instruction, 0, 4+len(tail))
		body = append(body, isa.NewTier1Instruction(isa.SubOpNotInt64, destination, operand), mk(jump, jumpRegister, 0, 0), returnVoid(), returnVoid())
		require.True(t, program.SetJumpTarget(body, 1, 3))
		return &program.CompiledFunction{Body: append(body, tail...)}
	}

	flips := []struct {
		name        string
		jump, want  isa.Opcode
		destination uint8
	}{
		{name: "a jump when the negation is false", jump: isa.OpJumpIfFalse, want: isa.OpJumpIfTrue, destination: negated},
		{name: "a jump when the negation is true", jump: isa.OpJumpIfTrue, want: isa.OpJumpIfFalse, destination: negated},
		{name: "a negation in place", jump: isa.OpJumpIfFalse, want: isa.OpJumpIfTrue, destination: operand},
	}
	for _, flip := range flips {
		t.Run(flip.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := function(t, flip.jump, flip.destination, flip.destination)

			require.True(t, flipJumpOverNot(compiledFunction, compiledFunction.Body, 0, nil))

			require.Equal(t, nop(), compiledFunction.Body[0])
			require.Equal(t, flip.want, compiledFunction.Body[1].Op)
			require.Equal(t, uint8(operand), compiledFunction.Body[1].A, "the jump tests what was negated")
			requireJumpTarget(t, compiledFunction.Body, 1, 3)
		})
	}

	readOnFallThrough := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveInt64, 2, negated), returnVoid()}
	refusals := []fusionRefusal{
		{name: "the jump tests another register", compiledFunction: function(t, isa.OpJumpIfFalse, negated, negated+1)},
		{name: "a jump lands on the jump", compiledFunction: function(t, isa.OpJumpIfFalse, negated, negated), targets: map[int]bool{1: true}},
		{name: "the negation is read where the jump lands", compiledFunction: function(t, isa.OpJumpIfFalse, negated, negated, readOnFallThrough...)},
	}
	require.True(t, program.SetJumpTarget(refusals[2].compiledFunction.Body, 1, 4))
	requireFusionRefused(t, flipJumpOverNot, refusals)

	readAfter := &program.CompiledFunction{Body: []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpNotInt64, negated, operand), mk(isa.OpJumpIfFalse, negated, 0, 0),
		isa.NewTier1Instruction(isa.SubOpMoveInt64, 2, negated), returnVoid(), returnVoid(),
	}}
	require.True(t, program.SetJumpTarget(readAfter.Body, 1, 4))
	require.False(t, flipJumpOverNot(readAfter, readAfter.Body, 0, nil), "the negation is read after the jump falls through")
}

func TestDropAllOnesMaskLeavesTheValueOrAMove(t *testing.T) {
	t.Parallel()
	function := func(destination, source uint8, index int, constants ...uint64) *program.CompiledFunction {
		return &program.CompiledFunction{
			UintConstants: constants,
			Body: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, destination, source),
				mk(isa.OpExtension, uint8(index&0xFF), uint8(index>>8), 0),
				returnVoid(),
			},
		}
	}
	wide := make([]uint64, 301)
	wide[300] = math.MaxUint64

	inPlace := function(2, 2, 1, 7, math.MaxUint64)
	require.True(t, dropAllOnesMask(inPlace, inPlace.Body, 0, nil))
	require.Equal(t, []isa.Instruction{nop(), nop(), returnVoid()}, inPlace.Body, "a mask that keeps every bit of its own register does nothing")

	copied := function(3, 2, 300, wide...)
	require.True(t, dropAllOnesMask(copied, copied.Body, 0, nil))
	require.Equal(t, []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveUint64, 3, 2), nop(), returnVoid()}, copied.Body,
		"a mask that keeps every bit into another register is a move")

	refusals := []fusionRefusal{
		{name: "a mask that clears a bit", compiledFunction: function(2, 2, 0, math.MaxUint64-1)},
		{name: "a constant index outside the pool", compiledFunction: function(2, 2, 4, math.MaxUint64)},
		{name: "a jump onto the extension word", compiledFunction: function(2, 2, 0, math.MaxUint64), targets: map[int]bool{1: true}},
	}
	requireFusionRefused(t, dropAllOnesMask, refusals)
}

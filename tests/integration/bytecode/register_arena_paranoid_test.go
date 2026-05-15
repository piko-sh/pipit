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

//go:build integration && pipit_arena_paranoid

package bytecode_test

import (
	"context"
	"testing"
	"unsafe"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestArenaParanoid_RestorePoisonsReleasedPrimitiveRanges(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()

	var save engine.ArenaSavePoint
	arena.SaveInto(&save)

	var counts [isa.NumRegisterKinds]uint32
	counts[isa.RegisterInt] = 4
	counts[isa.RegisterFloat] = 2
	counts[isa.RegisterUint] = 2
	counts[isa.RegisterBool] = 2
	registers := arena.AllocRegisters(counts)
	for i := range registers.Ints {
		registers.Ints[i] = int64(i + 1)
	}
	registers.Floats[0] = 3.14
	registers.Uints[0] = 7
	registers.Bools[0] = false

	intBacking := registers.Ints
	arena.Restore(save)

	for i, got := range intBacking {
		require.Equalf(t, engine.ArenaPoisonInt, got,
			"released int slot %d must be poisoned, got %d", i, got)
	}
}

func TestArenaParanoid_CallHeavyWorkloadStaysCorrect(t *testing.T) {
	t.Parallel()
	source := `package main

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func accumulate(limit int) int {
	total := 0
	for i := 0; i < limit; i++ {
		total += fib(i % 12)
	}
	return total
}

func run() int {
	adder := func(base int) func(int) int {
		return func(x int) int { return base + x }
	}
	plusFive := adder(5)
	return accumulate(64) + plusFive(fib(10))
}`

	referenceFib := func(n int) int {
		a, b := 0, 1
		for range n {
			a, b = b, a+b
		}
		return a
	}
	expected := 0
	for i := 0; i < 64; i++ {
		expected += referenceFib(i % 12)
	}
	expected += 5 + referenceFib(10)

	service := app.NewService()
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)

	require.Equal(t, expected, result, "poisoned-arena run must match the reference result")
}

func TestArenaParanoid_ResetPoisonsReleasedGenericBytes(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()

	const size = 32
	pointer := arena.AllocBytes(size, 8)
	region := unsafe.Slice((*byte)(pointer), size)
	for i := range region {
		region[i] = byte(i + 1)
	}

	arena.Reset()

	for i, got := range region {
		require.Equalf(t, engine.ArenaPoisonByte, got,
			"released generic-byte slot %d must be poisoned after Reset, got %#x", i, got)
	}
}

func TestArenaParanoid_ResetPoisonsRetiredGenericByteSlabs(t *testing.T) {
	t.Parallel()
	arena := engine.NewRegisterArena()

	const size = 64
	first := arena.AllocBytes(size, 8)
	firstRegion := unsafe.Slice((*byte)(first), size)
	for i := range firstRegion {
		firstRegion[i] = byte(i + 1)
	}
	for range 64 {
		arena.AllocBytes(4096, 8)
	}

	arena.Reset()

	poisoned := 0
	for _, got := range firstRegion {
		if got == engine.ArenaPoisonByte {
			poisoned++
		}
	}
	require.Equalf(t, size, poisoned,
		"every byte of the retired slab must be poisoned after Reset, got %d of %d", poisoned, size)
}

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

//go:build integration

package language_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func TestClosureASMInlinePipeline(t *testing.T) {
	t.Parallel()
	source := `package main

func makeMultiply(factor uint32) func(uint32) uint32 {
	return func(value uint32) uint32 {
		return (value * factor) & 0xFFFFFFFF
	}
}

func makeDivisorFilter(divisor uint32) func(uint32) bool {
	return func(value uint32) bool {
		return value%divisor != 0
	}
}

func makeAdd(offset uint32) func(uint32) uint32 {
	return func(value uint32) uint32 {
		return (value + offset) & 0xFFFFFFFF
	}
}

func makeFold(shift uint32) func(uint32, uint32) uint32 {
	return func(acc uint32, value uint32) uint32 {
		rotated := ((value << shift) | (value >> (32 - shift))) & 0xFFFFFFFF
		return (acc ^ rotated) & 0xFFFFFFFF
	}
}

func applyMap(input []uint32, fn func(uint32) uint32) []uint32 {
	output := make([]uint32, len(input))
	for index := 0; index < len(input); index++ {
		output[index] = fn(input[index])
	}
	return output
}

func applyFilter(input []uint32, predicate func(uint32) bool) []uint32 {
	output := make([]uint32, 0, len(input))
	for index := 0; index < len(input); index++ {
		current := input[index]
		if predicate(current) {
			output = append(output, current)
		}
	}
	return output
}

func applyReduce(input []uint32, initial uint32, fn func(uint32, uint32) uint32) uint32 {
	accumulator := initial
	for index := 0; index < len(input); index++ {
		accumulator = fn(accumulator, input[index])
	}
	return accumulator
}

func EntrypointRun() uint64 {
	state := uint32(0xCAFEBABE)
	input := make([]uint32, 2000)
	for index := 0; index < len(input); index++ {
		state = (state*1664525 + 1013904223) & 0xFFFFFFFF
		input[index] = state
	}
	stage1 := applyMap(input, makeMultiply(3))
	stage2 := applyFilter(stage1, makeDivisorFilter(7))
	stage3 := applyMap(stage2, makeAdd(1234))
	stage4 := applyFilter(stage3, makeDivisorFilter(3))
	folded := applyReduce(stage4, 0, makeFold(5))
	return uint64(folded)*1000000 + uint64(len(stage4))
}`
	expected := closurePipelineNativeReplica()

	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(t, err)
	got, ok := result.(uint64)
	require.Truef(t, ok, "result type = %T, want uint64", result)
	require.Equal(t, expected, got)
}

func closurePipelineNativeReplica() uint64 {
	state := uint32(0xCAFEBABE)
	input := make([]uint32, 2000)
	for index := range input {
		state = state*1664525 + 1013904223
		input[index] = state
	}
	stage2 := make([]uint32, 0, len(input))
	for _, value := range input {
		mapped := value * 3
		if mapped%7 != 0 {
			stage2 = append(stage2, mapped)
		}
	}
	stage4 := make([]uint32, 0, len(stage2))
	for _, value := range stage2 {
		mapped := value + 1234
		if mapped%3 != 0 {
			stage4 = append(stage4, mapped)
		}
	}
	folded := uint32(0)
	for _, value := range stage4 {
		rotated := (value << 5) | (value >> 27)
		folded ^= rotated
	}
	return uint64(folded)*1000000 + uint64(len(stage4))
}

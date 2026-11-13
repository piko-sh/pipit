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

package bytecode_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func buildOptimiseBenchFunction() *program.CompiledFunction {
	builder := newBytecodeBuilder()
	builder.intRegisters(8).returnInt()
	for i := range 16 {
		builder.Emit(isa.OpAddInt64, uint8(i%4), uint8((i+1)%4), uint8((i+2)%4))
	}
	for i := range 8 {
		builder.Emit(isa.OpMultiplyInt64, uint8(i%4), uint8((i+1)%4), uint8((i+2)%4))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return builder.build()
}

func BenchmarkOptimisePipeline(b *testing.B) {
	ctx := b.Context()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		function := buildOptimiseBenchFunction()
		b.StartTimer()
		require.NoError(b, passes.Optimise(ctx, passes.DefaultOptions(), function))
	}
}

func buildInlinerBenchFunction() *program.CompiledFunction {
	callee := makeDoubleCallee()
	return &program.CompiledFunction{
		Name: "caller",
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2Return),
				1,
			),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments:    []program.VarLocation{{Register: 5, Kind: isa.RegisterInt64}},
				Returns:      []program.VarLocation{{Register: 6, Kind: isa.RegisterInt64}},
			},
		},
		ResultKinds:  []isa.RegisterKind{isa.RegisterInt64},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 7},
		Functions:    []*program.CompiledFunction{callee},
	}
}

func BenchmarkInliner(b *testing.B) {
	ctx := b.Context()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		function := buildInlinerBenchFunction()
		b.StartTimer()
		require.NoError(b, inline.RunBytecodeInliner(ctx, function, passes.DefaultOptions()))
	}
}

func BenchmarkEscapeAnalysis(b *testing.B) {
	ctx := b.Context()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		function := buildOptimiseBenchFunction()
		b.StartTimer()
		require.NoError(b, escape.RunEscapeAnalysisPass(ctx, function))
	}
}

func BenchmarkGCMarkCompact(b *testing.B) {
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		vm := &engine.VM{
			Globals:   &engine.GlobalStore{},
			Arena:     engine.NewRegisterArena(),
			CallStack: []engine.CallFrame{},
		}
		arena := vm.Arena
		liveBuffer := arena.AllocStringBytes(128)
		copy(liveBuffer, strings.Repeat("x", 128))
		vm.Globals.Strings = []string{string(liveBuffer)}
		for range 4 {
			arena.GrowByteSlab(engine.InitialByteSlabSize)
		}
		liveInts := arena.AllocIntBacking(32)
		for i := range liveInts {
			liveInts[i] = int64(i)
		}
		for range 2 {
			arena.GrowIntBackingSlab(engine.InitialIntBackingSize)
		}
		vm.CallStack = []engine.CallFrame{{
			Registers: engine.Registers{
				SlicesInt: [][]int64{liveInts},
			},
		}}
		vm.FramePointer = 0
		b.StartTimer()
		arena.MinorGC(vm)
	}
}

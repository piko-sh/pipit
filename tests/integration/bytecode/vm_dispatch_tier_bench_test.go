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

//go:build integration && !safe && !(js && wasm) && (amd64 || arm64)

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/isa"
)

const (
	dispatchBenchInstructionCount = 1000
)

func BenchmarkDispatchOpNopMainTier(b *testing.B) {
	builder := newBytecodeBuilder()
	builder.intRegisters(1).returnInt()
	for range dispatchBenchInstructionCount {
		builder.Emit(isa.OpNop, 0, 0, 0)
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	compiled := builder.build()
	service := app.NewService()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := service.Execute(ctx, compiled)
		if err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

func BenchmarkDispatchOpNopTier3Form(b *testing.B) {
	builder := newBytecodeBuilder()
	builder.intRegisters(1).returnInt()
	for range dispatchBenchInstructionCount {
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3Nop))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	compiled := builder.build()
	service := app.NewService()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := service.Execute(ctx, compiled)
		if err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

func BenchmarkDispatchTier0SliceGetIntDirect(b *testing.B) {
	builder := newBytecodeBuilder()
	builder.intRegisters(4).sliceIntRegisters(1).returnInt()
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 8)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpMakeSliceInt), 0, 1)
	builder.Emit(isa.OpExt, 1, 0, 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 2, 3)
	for range dispatchBenchInstructionCount {
		builder.Emit(isa.OpSliceGetIntDirect, 3, 0, 2)
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	compiled := builder.build()
	service := app.NewService()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := service.Execute(ctx, compiled)
		if err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

func BenchmarkDispatchTier0AddInt(b *testing.B) {
	builder := newBytecodeBuilder()
	builder.intRegisters(3).returnInt()
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 1)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 2, 1)
	for range dispatchBenchInstructionCount {
		builder.Emit(isa.OpAddInt, 1, 1, 2)
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	compiled := builder.build()
	service := app.NewService()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := service.Execute(ctx, compiled)
		if err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

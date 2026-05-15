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
	"fmt"
	"sync"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// functionPipelines memoises FunctionPipeline per Options value. The passes it holds are
// stateless, so one slice serves every function compiled with the same options and the
// per-function build (one allocation) goes away.
var functionPipelines sync.Map

// Optimise applies the per-function pipeline to compiledFunction and then recurses into
// every nested function.
//
// Takes opts (Options) which selects the optional passes.
// Takes compiledFunction (*program.CompiledFunction) whose body, and whose nested
// functions' bodies, are rewritten in place.
//
// Returns error when cancellation fires or a pass reports failure.
func Optimise(ctx context.Context, opts Options, compiledFunction *program.CompiledFunction) error {
	if err := OptimiseFunction(ctx, opts, compiledFunction); err != nil {
		return err
	}
	for _, child := range compiledFunction.Functions {
		if err := Optimise(ctx, opts, child); err != nil {
			return err
		}
	}
	return nil
}

// OptimiseFunction runs the function pipeline on compiledFunction alone, leaving
// compiledFunction.Functions untouched.
//
// Takes opts (Options) which selects the optional passes.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires or a pass reports failure.
func OptimiseFunction(ctx context.Context, opts Options, compiledFunction *program.CompiledFunction) error {
	return optimiseWithPipeline(ctx, functionPipelineFor(opts), opts, compiledFunction)
}

// functionPipelineFor() returns the memoised FunctionPipeline for opts.
//
// Takes opts (Options) which selects the optional passes.
//
// Returns []pass which is the cached pipeline slice.
func functionPipelineFor(opts Options) []pass {
	if cached, ok := functionPipelines.Load(opts); ok {
		if pipeline, ok := cached.([]pass); ok {
			return pipeline
		}
	}
	pipeline := functionPipeline(opts)
	functionPipelines.Store(opts, pipeline)
	return pipeline
}

// optimiseWithPipeline runs one prebuilt pipeline over compiledFunction and recurses into
// compiledFunction.Functions so the pipeline is assembled once per Optimise call rather
// than once per function.
//
// Takes pipeline ([]Pass) which is the prebuilt FunctionPipeline.
// Takes opts (Options) which every pass can consult.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires, the function's jump range was exceeded during
// emission, or a pass reports failure.
func optimiseWithPipeline(ctx context.Context, pipeline []pass, opts Options, compiledFunction *program.CompiledFunction) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("Optimise cancelled: %w", err)
	}
	if compiledFunction.JumpRangeExceeded {
		return fmt.Errorf("%w: in function %q", program.ErrCompileJumpRange, compiledFunction.Name)
	}
	return RunPipeline(ctx, pipeline, opts, compiledFunction)
}

// runPeepholeFusions walks the body once and applies every peephole fusion / load-const
// optimisation the dispatcher recognises. Each fuse* helper returns true when it consumed
// the slot and the loop must advance to the next instruction.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is compiledFunction.body captured locally so the loop
// avoids repeated slice header loads.
//
// Returns error when cancellation fires.
func runPeepholeFusions(ctx context.Context, compiledFunction *program.CompiledFunction, body []isa.Instruction) error {
	n := len(body)
	jumpTargets := compiledFunction.BuildJumpTargets(body)
	for i := range n {
		if i&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("runPeepholeFusions cancelled: %w", err)
			}
		}
		if FuseCopyStructFieldGeneralT0(compiledFunction, body, i, n, jumpTargets) {
			continue
		}
		if fuseLongPatterns(compiledFunction, body, i, n, jumpTargets) ||
			FuseThreeInstrPatterns(compiledFunction, body, i, n, jumpTargets) ||
			FuseCompareRegJump(compiledFunction, body, i, n, jumpTargets) ||
			FuseArithConst(compiledFunction, body, i, n, jumpTargets) ||
			FuseAddIntJump(compiledFunction, body, i, n, jumpTargets) ||
			FuseConcatRune(compiledFunction, body, i, n, jumpTargets) ||
			FuseAppendMove(compiledFunction, body, i, n, jumpTargets) ||
			fuseMoveElimination(compiledFunction, body, i, n, jumpTargets) ||
			FuseStringIndexToInt(compiledFunction, body, i, n, jumpTargets) {
			continue
		}
		OptimiseLoadIntConst(compiledFunction, body, i)
		OptimiseLoadUintConst(compiledFunction, body, i)
	}
	return nil
}

// releaseConstantIndices clears the per-pool dedup maps and the Emit-time debug hook once
// Optimise() has finished walking the body. The runtime never reads these, so dropping
// them releases memory proportional to the program size.
//
// Takes compiledFunction (*program.CompiledFunction) whose dedup maps and debug hook are
// cleared.
func releaseConstantIndices(compiledFunction *program.CompiledFunction) {
	compiledFunction.IntConstIndex = nil
	compiledFunction.FloatConstIndex = nil
	compiledFunction.StringConstIndex = nil
	compiledFunction.UintConstIndex = nil
	compiledFunction.ComplexConstIndex = nil
	compiledFunction.TypeRefIndex = nil
	compiledFunction.DebugEmitHook = nil
}

// elideUnusedSyncClosureUpvalues replaces isa.SubOpTier2SyncClosureUpvalues with
// isa.OpNop when the caller never emits isa.OpMakeClosure.
//
// The sync is only meaningful when the caller has a sharedCells map, which requires
// emitting isa.OpMakeClosure. The tier-3 IIFE writeback is unaffected and always stays.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream.
func elideUnusedSyncClosureUpvalues(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	if program.MayCreateSharedCells(compiledFunction) {
		return
	}
	for i := range body {
		if isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2SyncClosureUpvalues) {
			body[i] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
		}
	}
}

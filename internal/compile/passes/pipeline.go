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

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// functionPipelineCapacity is the length of the full FunctionPipeline, used to size the
	// slice up front.
	functionPipelineCapacity = 9

	// postPurityPipelineCapacity is the length of the full PostPurityPipeline, used to size
	// the slice up front.
	postPurityPipelineCapacity = 20
)

var (
	// compareJumpSweep holds three-instruction patterns followed by compare-and-jump
	// fusions.
	compareJumpSweep = [][]slotFusion{{
		func(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
			return FuseThreeInstrPatterns(compiledFunction, body, i, len(body), jumpTargets)
		},
		fuseRegisterCompareJump,
	}}

	// compareJumpSweepAfterCoalescing holds compare-and-jump fusions followed by
	// increment-and-branch fusions.
	compareJumpSweepAfterCoalescing = [][]slotFusion{
		{fuseRegisterCompareJump},
		{func(_ *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
			return fuseIncrementThenCompareJump(body, i, len(body), jumpTargets)
		}},
	}
)

// licmPass hoists loop-invariant struct-field reads, and when constantLoads is set also
// loop-invariant constant loads, to the loop pre-header.
type licmPass struct {
	// constantLoads enables the constant-load family. Only the post-purity run sets it: in
	// the first run the peephole fusers have not yet consumed the constant loads that feed
	// compare-and-branch and arithmetic superinstructions, and hoisting one of those would
	// leave the fusion without its operand.
	constantLoads bool

	// elementStoresKeepFields carries ProgramFacts.ElementStoresKeepFields to the
	// struct-field family.
	elementStoresKeepFields bool
}

// ProgramFacts holds facts about the whole program for the post-purity passes.
type ProgramFacts struct {
	// ElementStoresKeepFields is true when element stores preserve scalar fields and slice
	// headers.
	//
	// The compiler sets it from the program's own code, so it assumes that no native
	// function returns a slice laid over the memory of a struct the program also reads
	// through its fields.
	ElementStoresKeepFields bool
}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (licmPass) Name() string { return "licm" }

// Run applies HoistLoopInvariantStructFieldReads() with the shared analysis, then the
// constant-load hoist when the pass enables it.
//
// A hoist releases the pointer-alias facts because it moves PCs, so the analysis is
// recomputed when the function had them. The in-place append and composite reuse passes
// would otherwise skip the function.
//
// Takes state (*PassContext) which carries the options and the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (pass licmPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	hadAliasInfo := compiledFunction.AliasInfo != nil
	if err := pass.hoist(ctx, state, compiledFunction); err != nil {
		return err
	}
	if hadAliasInfo && compiledFunction.AliasInfo == nil {
		return RunPointerAliasAnalysis(ctx, compiledFunction)
	}
	return nil
}

// hoist runs the enabled LICM families in order.
//
// Takes state (*PassContext) which carries the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (pass licmPass) hoist(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if pass.constantLoads {
		if err := hoistLoopInvariantGlobalLoads(ctx, compiledFunction, state.Analysis); err != nil {
			return err
		}
	}
	if err := hoistLoopInvariantStructFieldReads(ctx, compiledFunction, state.Analysis, pass.elementStoresKeepFields); err != nil {
		return err
	}
	if !pass.constantLoads {
		return nil
	}
	if err := hoistLoopInvariantConstantLoads(ctx, compiledFunction, state.Analysis); err != nil {
		return err
	}
	if err := hoistLoopInvariantArithmetic(ctx, compiledFunction, state.Analysis); err != nil {
		return err
	}
	hoistLoopInvariantSliceChains(compiledFunction, state.Analysis)
	return nil
}

// lateFieldReadsPass lifts loop-invariant struct-field reads a second time, once move
// coalescing has run.
type lateFieldReadsPass struct {
	// elementStoresKeepFields carries ProgramFacts.ElementStoresKeepFields to the
	// struct-field family.
	elementStoresKeepFields bool
}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (lateFieldReadsPass) Name() string { return "late-field-reads" }

// Run straightens the body and lifts its loop-invariant field reads.
//
// Straightening moves PCs and so releases the pointer-alias facts. They are computed
// again when the function had them.
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (pass lateFieldReadsPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if !bodyHasFieldReadAndLoop(compiledFunction.Body) {
		return nil
	}
	hadAliasInfo := compiledFunction.AliasInfo != nil
	changed, err := compactBody(ctx, compiledFunction)
	if changed {
		state.Analysis.invalidate()
	}
	if err != nil {
		return err
	}
	if hadAliasInfo && compiledFunction.AliasInfo == nil {
		if err := RunPointerAliasAnalysis(ctx, compiledFunction); err != nil {
			return err
		}
	}
	return licmPass{constantLoads: false, elementStoresKeepFields: pass.elementStoresKeepFields}.Run(ctx, state, compiledFunction)
}

// fuseConstOperandsPass folds constant loads into the arithmetic that consumes them once
// inlining has made them adjacent, before LICM would hoist the loads away.
type fuseConstOperandsPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (fuseConstOperandsPass) Name() string { return "fuse-const-operands" }

// Run applies FuseArithConst() at every slot.
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (fuseConstOperandsPass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	body := compiledFunction.Body
	jumpTargets := state.Analysis.JumpTargets()
	changed := false
	for i := range body {
		changed = FuseArithConst(compiledFunction, body, i, len(body), jumpTargets) || changed
	}
	if changed {
		state.Analysis.invalidate()
	}
	return nil
}

// fuseCompareJumpsPass fuses adjacent integer comparisons and branches.
type fuseCompareJumpsPass struct {
	// afterCoalescing limits the second run to register comparisons.
	afterCoalescing bool
}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (fuseCompareJumpsPass) Name() string { return "fuse-compare-jumps" }

// Run threads branches and fuses comparisons with their following branch.
//
// Takes ctx (context.Context) which cancels a sweep part way.
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when the context is cancelled.
func (pass fuseCompareJumpsPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if threadBranchesOnOneRegister(compiledFunction.Body) {
		state.Analysis.invalidate()
	}
	jumpTargets := state.Analysis.JumpTargets()
	fusions := compareJumpSweep
	if pass.afterCoalescing {
		fusions = compareJumpSweepAfterCoalescing
	}
	changed := false
	for _, sweep := range fusions {
		swept, err := sweepSlotFusions(ctx, compiledFunction, jumpTargets, sweep)
		changed = changed || swept
		if err != nil {
			if changed {
				state.Analysis.invalidate()
			}
			return fmt.Errorf("compare-and-jump fusion cancelled: %w", err)
		}
	}
	if changed {
		state.Analysis.invalidate()
	}
	return nil
}

// fuseElementConversionsPass folds unsigned reads into adjacent conversions to int.
type fuseElementConversionsPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (fuseElementConversionsPass) Name() string { return "fuse-element-conversions" }

// Run applies FuseGetElementUint64AsInt64() and FuseGetStringByteAsInt64() at every slot.
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (fuseElementConversionsPass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	body := compiledFunction.Body
	jumpTargets := state.Analysis.JumpTargets()
	changed := false
	for i := range body {
		changed = FuseGetElementUint64AsInt64(compiledFunction, body, i, len(body), jumpTargets) ||
			FuseGetStringByteAsInt64(compiledFunction, body, i, len(body), jumpTargets) || changed
	}
	if changed {
		state.Analysis.invalidate()
	}
	return nil
}

// peepholeFusionPass fuses common instruction sequences into superinstructions.
type peepholeFusionPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (peepholeFusionPass) Name() string { return "peephole-fusion" }

// Run applies runPeepholeFusions(), then invalidates the analysis. The fusions replace
// jumps with fused forms whose footprints and offsets differ, so the jump-target set the
// later passes consult must be rebuilt from the fused body.
//
// Takes state (*PassContext) which carries the options and the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (peepholeFusionPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if err := runPeepholeFusions(ctx, compiledFunction, compiledFunction.Body); err != nil {
		return err
	}
	state.Analysis.invalidate()
	return nil
}

// releaseConstantIndicesPass drops the Emit-time constant dedup maps.
type releaseConstantIndicesPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (releaseConstantIndicesPass) Name() string { return "release-constant-indices" }

// Run applies releaseConstantIndices().
//
// Takes compiledFunction (*program.CompiledFunction) whose dedup maps are cleared.
//
// Returns error (always nil).
func (releaseConstantIndicesPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	releaseConstantIndices(compiledFunction)
	return nil
}

// elideSynchroniseClosureCapturedVariablesPass nops
// isa.SubOpTier2SynchroniseClosureCapturedVariables in functions that never create
// closures.
type elideSynchroniseClosureCapturedVariablesPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (elideSynchroniseClosureCapturedVariablesPass) Name() string {
	return "elide-sync-closure-upvalues"
}

// Run applies elideUnusedSynchroniseClosureCapturedVariables().
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (elideSynchroniseClosureCapturedVariablesPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	elideUnusedSynchroniseClosureCapturedVariables(compiledFunction, compiledFunction.Body)
	return nil
}

// truncateElisionPass removes the redundant truncate / bit-and pairs.
type truncateElisionPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (truncateElisionPass) Name() string { return "truncate-elision" }

// Run applies both truncate elisions.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (truncateElisionPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	elideRedundantTruncateAfterBitAnd(compiledFunction, compiledFunction.Body)
	elideRedundantBitAndAfterTruncate(compiledFunction, compiledFunction.Body)
	return nil
}

// csePass eliminates redundant struct-field reads.
type csePass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (csePass) Name() string { return "cse" }

// Run applies elideRedundantStructFieldRead() with the shared jump-target set.
//
// Takes state (*PassContext) which carries the options and the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (csePass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	return elideRedundantStructFieldRead(ctx, compiledFunction, compiledFunction.Body, state.Analysis)
}

// syncSourceMapPass reconciles the debug source map with the rewritten body.
type syncSourceMapPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (syncSourceMapPass) Name() string { return "sync-source-map" }

// Run applies SyncSourceMapAfterOptimise().
//
// Takes compiledFunction (*program.CompiledFunction) whose source map is reconciled.
//
// Returns error (always nil).
func (syncSourceMapPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	SyncSourceMapAfterOptimise(compiledFunction, compiledFunction.Body)
	return nil
}

// allocCountsPass computes the per-bank register allocation counts.
type allocCountsPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (allocCountsPass) Name() string { return "alloc-counts" }

// Run applies EnsurePrecomputedAllocCounts().
//
// Takes compiledFunction (*program.CompiledFunction) whose alloc counts are computed.
//
// Returns error (always nil).
func (allocCountsPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	compiledFunction.EnsurePrecomputedAllocCounts()
	return nil
}

// tinyLeafPass classifies the function for the tiny-leaf call fast path.
type tinyLeafPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (tinyLeafPass) Name() string { return "tiny-leaf" }

// Run applies engine.ClassifyTinyLeaf().
//
// Takes compiledFunction (*program.CompiledFunction) which is classified for the
// tiny-leaf fast path.
//
// Returns error (always nil).
func (tinyLeafPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	engine.ClassifyTinyLeaf(compiledFunction)
	return nil
}

// gvnPass rewrites later equal-value computations as moves.
type gvnPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (gvnPass) Name() string { return "gvn" }

// Run applies runFunctionGvn() with the shared dominators and jump-target set.
//
// Takes state (*PassContext) which carries the options and the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (gvnPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	return runFunctionGvn(ctx, compiledFunction, state.Analysis)
}

// bcePass eliminates provably redundant bounds checks.
type bcePass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (bcePass) Name() string { return "bce" }

// Run applies elideRedundantBoundsChecks() with the shared jump-target set.
//
// Takes state (*PassContext) which carries the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (bcePass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	elideRedundantBoundsChecks(compiledFunction, compiledFunction.Body, state.Analysis)
	return nil
}

// rangeAliasPass resolves the range-value snapshot candidates.
type rangeAliasPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (rangeAliasPass) Name() string { return "range-alias" }

// Run applies AliasReadOnlyRangeValues().
//
// Takes compiledFunction (*program.CompiledFunction) whose range-value snapshots are
// resolved.
//
// Returns error (always nil).
func (rangeAliasPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	aliasReadOnlyRangeValues(compiledFunction)
	return nil
}

// inPlaceAppendPass demotes in-place appends whose source may be aliased.
type inPlaceAppendPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (inPlaceAppendPass) Name() string { return "inplace-append" }

// Run applies verifyInPlaceAppendSafety() with the shared jump-target set.
//
// Takes state (*PassContext) which carries the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose in-place appends are demoted
// when unsafe.
//
// Returns error (always nil).
func (inPlaceAppendPass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	verifyInPlaceAppendSafety(compiledFunction, state.Analysis)
	return nil
}

// compositeZeroReusePass promotes provably-safe composite-zero loads to the reuse form.
// Must run before releaseAliasInfoPass, which drops the alias info the promotion reads.
type compositeZeroReusePass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (compositeZeroReusePass) Name() string { return "composite-zero-reuse" }

// Run applies promoteCompositeZeroReuse() with the shared jump-target set.
//
// Takes state (*PassContext) which carries the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose eligible loads are promoted.
//
// Returns error (always nil).
func (compositeZeroReusePass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	promoteCompositeZeroReuse(compiledFunction, state.Analysis)
	return nil
}

// releaseAliasInfoPass drops the pointer-alias information once its consumers have run.
type releaseAliasInfoPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (releaseAliasInfoPass) Name() string { return "release-alias-info" }

// Run applies ReleaseAliasInfo().
//
// Takes compiledFunction (*program.CompiledFunction) whose alias information is released.
//
// Returns error (always nil).
func (releaseAliasInfoPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	releaseAliasInfo(compiledFunction)
	return nil
}

// functionPipeline returns the per-function passes Optimise runs, in order.
//
// Takes opts (Options) which selects the optional passes.
//
// Returns the passes in execution order.
func functionPipeline(opts Options) []Pass {
	pipeline := make([]Pass, 0, functionPipelineCapacity)
	if opts.LICM {
		pipeline = append(pipeline, licmPass{constantLoads: false, elementStoresKeepFields: false})
	}
	pipeline = append(pipeline,
		peepholeFusionPass{},
		releaseConstantIndicesPass{},
		elideSynchroniseClosureCapturedVariablesPass{},
		truncateElisionPass{},
	)
	if opts.CSE {
		pipeline = append(pipeline, csePass{})
	}
	return append(pipeline,
		syncSourceMapPass{},
		allocCountsPass{},
		tinyLeafPass{},
	)
}

// PostPurityPipeline returns the per-function passes the post-purity peephole stage runs,
// in order.
//
// Takes opts (Options) which selects the optional passes.
// Takes facts (ProgramFacts) which holds what the caller knows of the whole program.
// Takes rewriteMethodCalls (Pass) which promotes inlineable method calls. It lives in the
// inline package, which the passes package cannot import, so the caller supplies it. nil
// omits the step.
//
// Returns the passes in execution order.
func PostPurityPipeline(opts Options, facts ProgramFacts, rewriteMethodCalls Pass) []Pass {
	pipeline := make([]Pass, 0, postPurityPipelineCapacity)
	pipeline = append(pipeline, rangeAliasPass{})
	if opts.CSE {
		pipeline = append(pipeline, scalarizeSnapshotsPass{})
	}
	pipeline = append(pipeline, fuseConstOperandsPass{})
	if opts.CSE {
		pipeline = append(pipeline, fuseCompareJumpsPass{afterCoalescing: false})
	}
	if opts.LICM {
		pipeline = append(pipeline, licmPass{constantLoads: true, elementStoresKeepFields: facts.ElementStoresKeepFields})
	}
	if opts.GVN {
		pipeline = append(pipeline, gvnPass{})
	}
	if opts.CSE {
		pipeline = append(pipeline, csePass{})
	}
	if opts.BCE {
		pipeline = append(pipeline, bcePass{})
	}
	if opts.CSE {
		pipeline = append(pipeline, coalesceMovesPass{})
		if opts.LICM {
			pipeline = append(pipeline, lateFieldReadsPass{elementStoresKeepFields: facts.ElementStoresKeepFields})
		}
		pipeline = append(pipeline, fuseCompareJumpsPass{afterCoalescing: true}, fuseElementConversionsPass{}, elideRangedTruncatesPass{})
	}
	if rewriteMethodCalls != nil {
		pipeline = append(pipeline, rewriteMethodCalls)
	}
	pipeline = append(pipeline, inPlaceAppendPass{}, compositeZeroReusePass{}, releaseAliasInfoPass{}, fuseNarrow32Pass{})
	if opts.CSE {
		pipeline = append(pipeline, fuseLatePairsPass{})
	}
	return append(pipeline, compactBodyPass{})
}

// bodyHasFieldReadAndLoop reports whether the body has a hoistable field read and a
// backward jump.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns true when both are present.
func bodyHasFieldReadAndLoop(body []isa.Instruction) bool {
	reads, loops := false, false
	for pc := range body {
		reads = reads || isHoistableReadAt(body, pc)
		if target, jumps := program.JumpTargetAt(body, pc); jumps && target <= pc {
			loops = true
		}
		if reads && loops {
			return true
		}
	}
	return false
}

// fuseRegisterCompareJump is FuseCompareRegJump() over the whole body, as a slot fusion.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the body.
// Takes body ([]isa.Instruction) which is rewritten in place.
// Takes i (int) which is the candidate comparison.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the comparison was fused with its jump.
func fuseRegisterCompareJump(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	return FuseCompareRegJump(compiledFunction, body, i, len(body), jumpTargets)
}

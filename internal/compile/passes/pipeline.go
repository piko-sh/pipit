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

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	// functionPipelineCapacity is the length of the full FunctionPipeline, used to size the
	// slice up front.
	functionPipelineCapacity = 9

	// postPurityPipelineCapacity is the length of the full PostPurityPipeline, used to size
	// the slice up front.
	postPurityPipelineCapacity = 8
)

// licmPass hoists loop-invariant struct-field reads, and when constantLoads is set also
// loop-invariant constant loads, to the loop pre-header.
type licmPass struct {
	// constantLoads enables the constant-load family. Only the post-purity run sets it: in
	// the first run the peephole fusers have not yet consumed the constant loads that feed
	// compare-and-branch and arithmetic superinstructions, and hoisting one of those would
	// leave the fusion without its operand.
	constantLoads bool
}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (licmPass) Name() string { return "licm" }

// Run applies HoistLoopInvariantStructFieldReads() with the shared analysis, then the
// constant-load hoist when the pass enables it.
//
// Takes state (*PassContext) which carries the options and the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (pass licmPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if err := hoistLoopInvariantStructFieldReads(ctx, compiledFunction, state.Analysis); err != nil {
		return err
	}
	if !pass.constantLoads {
		return nil
	}
	return hoistLoopInvariantConstantLoads(ctx, compiledFunction, state.Analysis)
}

// peepholeFusionPass fuses common instruction sequences into superinstructions.
type peepholeFusionPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (peepholeFusionPass) Name() string { return "peephole-fusion" }

// Run applies runPeepholeFusions(), then invalidates the analysis. Some fused jump forms
// are not decoded by JumpTargetAt(), so the jump-target set the later passes consult must
// be rebuilt from the fused body.
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

// elideSyncClosureUpvaluesPass nops isa.SubOpTier2SyncClosureUpvalues in functions that
// never create closures.
type elideSyncClosureUpvaluesPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (elideSyncClosureUpvaluesPass) Name() string { return "elide-sync-closure-upvalues" }

// Run applies elideUnusedSyncClosureUpvalues().
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (elideSyncClosureUpvaluesPass) Run(_ context.Context, _ *PassContext, compiledFunction *program.CompiledFunction) error {
	elideUnusedSyncClosureUpvalues(compiledFunction, compiledFunction.Body)
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
// Passes whose optimisation is switched off in opts are left out rather than run as
// no-ops, so the returned list is also a statement of what the compilation does.
//
// Takes opts (Options) which selects the optional passes.
//
// Returns the passes in execution order.
func functionPipeline(opts Options) []pass {
	pipeline := make([]pass, 0, functionPipelineCapacity)
	if opts.LICM {
		pipeline = append(pipeline, licmPass{constantLoads: false})
	}
	pipeline = append(pipeline,
		peepholeFusionPass{},
		releaseConstantIndicesPass{},
		elideSyncClosureUpvaluesPass{},
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
// The stage re-runs LICM, GVN, CSE and BCE once heap purity and pointer aliasing are
// known, then resolves the range-value snapshots, promotes inlineable method calls,
// demotes unsafe in-place appends and releases the alias information.
//
// Takes opts (Options) which selects the optional passes.
// Takes rewriteMethodCalls (Pass) which promotes inlineable method calls. It lives in the
// inline package, which the passes package cannot import, so the caller supplies it. nil
// omits the step.
//
// Returns the passes in execution order.
func PostPurityPipeline(opts Options, rewriteMethodCalls pass) []pass {
	pipeline := make([]pass, 0, postPurityPipelineCapacity)
	if opts.LICM {
		pipeline = append(pipeline, licmPass{constantLoads: true})
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
	pipeline = append(pipeline, rangeAliasPass{})
	if rewriteMethodCalls != nil {
		pipeline = append(pipeline, rewriteMethodCalls)
	}
	return append(pipeline, inPlaceAppendPass{}, compositeZeroReusePass{}, releaseAliasInfoPass{})
}

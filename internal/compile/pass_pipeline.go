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

package compile

import (
	"context"
	"fmt"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
)

// programPass is one program-level step of the post-compilation pipeline: an analysis or
// rewrite that needs the whole function table rather than one body at a time.
type programPass struct {
	// Run applies the step to the program rooted at root.
	Run func(ctx context.Context, root *program.CompiledFunction) error

	// name is the short stable identifier for diagnostics.
	name string
}

// inlineableMethodCallsPass rewrites method calls that inlining has made direct. A named
// type rather than a closure so the interpreted/native-linked boundary crossing stays as
// a method call.
type inlineableMethodCallsPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (inlineableMethodCallsPass) Name() string { return "inlineable-method-calls" }

// Run rewrites the inlineable method calls of compiledFunction in place.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (inlineableMethodCallsPass) Run(
	_ context.Context,
	_ *passes.PassContext,
	compiledFunction *program.CompiledFunction,
) error {
	inline.RewriteInlineableMethodCalls(compiledFunction)
	return nil
}

// ProgramPipeline returns the compiler-owned program-level steps in execution order.
//
// Takes opts (passes.Options) which selects the optional passes.
//
// Returns the steps in execution order.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func ProgramPipeline(opts passes.Options) []programPass {
	return []programPass{
		{name: "escape-analysis", Run: func(ctx context.Context, root *program.CompiledFunction) error {
			return escape.RunEscapeAnalysisPassWithOptions(ctx, root, escape.PassOptions{ArenaPromotion: opts.ArenaPromotion})
		}},
		{name: "verify-source-maps-escape", Run: sourceMapVerifier("escape analysis")},
		{name: "inliner", Run: func(ctx context.Context, root *program.CompiledFunction) error {
			return inline.RunBytecodeInliner(ctx, root, opts)
		}},
		{name: "verify-source-maps-inlining", Run: sourceMapVerifier("inlining")},
		{name: "heap-purity", Run: escape.RunHeapPurityAnalysis},
		{name: "pointer-alias", Run: runPointerAliasAnalysisAll},
		{name: "post-purity-peephole", Run: func(ctx context.Context, root *program.CompiledFunction) error {
			return runPostPurityPeepholePass(ctx, opts, root)
		}},
		{name: "verify-source-maps-post-purity", Run: sourceMapVerifier("post-purity peephole")},
	}
}

// runPointerAliasAnalysisAll invokes RunPointerAliasAnalysis everywhere.
//
// Iteration order is irrelevant: each function's analysis is independent (callees are
// summarised as wild on call entry, so inter-procedural alias propagation is not
// modelled).
//
// Takes root (*CompiledFunction) which is the entry CompiledFunction whose reachable
// functions are analysed.
//
// Returns error when cancellation or a per-function analysis fails.
func runPointerAliasAnalysisAll(ctx context.Context, root *program.CompiledFunction) error {
	if root == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("RunPointerAliasAnalysisAll cancelled: %w", err)
	}
	for _, compiledFunction := range inline.CollectReachableFunctions(root) {
		if err := passes.RunPointerAliasAnalysis(ctx, compiledFunction); err != nil {
			return err
		}
	}
	return nil
}

// runPostPurityPeepholePass re-runs the peephole passes once heap purity is known.
//
// Acts on every reachable function once heapMutationClass is populated, through
// passes.PostPurityPipeline() with the inline method-call promotion supplied as the owned
// step. The re-run is idempotent because applied rewrites stay in place (the second-read
// instruction has been replaced with a MOVE) and hoisted instructions sit at the loop
// pre-header.
//
// Takes opts (passes.Options) which selects the optional passes.
// Takes root (*CompiledFunction) whose nested functions are re-optimised.
//
// Returns error when any per-function optimisation pass fails.
func runPostPurityPeepholePass(ctx context.Context, opts passes.Options, root *program.CompiledFunction) error {
	if root == nil {
		return nil
	}
	pipeline := passes.PostPurityPipeline(opts, inlineableMethodCallsPass{})
	for _, compiledFunction := range inline.CollectReachableFunctions(root) {
		if err := passes.RunPipeline(ctx, pipeline, opts, compiledFunction); err != nil {
			return err
		}
	}
	return nil
}

// sourceMapVerifier returns a ProgramPass body that checks every reachable function's
// source map still parallels its body, naming pass in any error.
//
// Takes pass (string) which names the step just completed, for the error message.
//
// Returns the step body.
func sourceMapVerifier(pass string) func(ctx context.Context, root *program.CompiledFunction) error {
	return func(_ context.Context, root *program.CompiledFunction) error {
		return inline.VerifySourceMapsParallel(root, pass)
	}
}

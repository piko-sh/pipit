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

	"pipit.sh/pipit/internal/engine/program"
)

// pass is one step of a per-function pipeline.
//
// A pass rewrites compiledFunction.Body in place under the contract described in the
// package documentation: the body only changes length through the splice helpers, dead
// slots become isa.OpNop, every jump target is valid on exit, and a second run over its
// own output is a no-op.
type pass interface {
	// Name returns the short stable identifier the pipeline reports in diagnostics.
	//
	// Returns string which is the pass identifier.
	Name() string

	// Run applies the pass to compiledFunction.
	//
	// Takes state (*PassContext) which carries the options and the shared analysis.
	// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
	//
	// Returns error when cancellation fires or the pass detects an inconsistency.
	Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error
}

// PassContext is what every pass in one pipeline run receives alongside the context.
type PassContext struct {
	// Analysis is the control-flow analysis shared by every pass of the run.
	Analysis *functionAnalysis

	// analysis backs Analysis so a pipeline run costs one allocation, not two.
	analysis functionAnalysis

	// options selects which optimisations the compilation runs.
	options Options
}

// FunctionPass adapts a plain function to the Pass interface so a caller outside this
// package can contribute a step to a pipeline.
type FunctionPass struct {
	// Fn is the pass body.
	Fn func(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error

	// PassName is what Name returns.
	PassName string
}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (pass FunctionPass) Name() string { return pass.PassName }

// Run applies Fn.
//
// Takes state (*PassContext) which carries the options and the shared analysis.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when the wrapped function reports failure.
func (pass FunctionPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	return pass.Fn(ctx, state, compiledFunction)
}

// RunPipeline builds the shared analysis for compiledFunction and runs pipeline over it
// in order.
//
// Takes pipeline ([]Pass) which lists the passes in execution order.
// Takes opts (Options) which every pass can consult.
// Takes compiledFunction (*program.CompiledFunction) whose body the passes rewrite.
//
// Returns error from the earliest pass that fails, unwrapped so callers see the pass's
// own message.
func RunPipeline(ctx context.Context, pipeline []pass, opts Options, compiledFunction *program.CompiledFunction) error {
	passCtx := new(PassContext{
		Analysis: nil,
		options:  opts,
		analysis: functionAnalysisFor(compiledFunction),
	})
	passCtx.Analysis = &passCtx.analysis
	for _, pass := range pipeline {
		if err := pass.Run(ctx, passCtx, compiledFunction); err != nil {
			return err
		}
	}
	return nil
}

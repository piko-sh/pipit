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

package inline

import (
	"fmt"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
)

// callSitePosition returns the source position of the isa.SubOpCall the splice replaces,
// which synthetic trampoline instructions inherit so the debugger attributes them to the
// call.
//
// Returns the call-site position, or the zero position when the caller has no source map.
func (ctx *inlineContext) callSitePosition() program.SourcePosition {
	sm := ctx.caller.DebugSourceMap
	if sm == nil || ctx.opCallPC < 0 || ctx.opCallPC >= len(sm.Positions) {
		return program.SourcePosition{}
	}
	return sm.Positions[ctx.opCallPC]
}

// appendSyntheticPosition appends the call-site position for a synthetic instruction the
// splice just appended to the caller body, keeping the source map parallel to the body.
func (ctx *inlineContext) appendSyntheticPosition() {
	sm := ctx.caller.DebugSourceMap
	if sm == nil {
		return
	}
	sm.Positions = append(sm.Positions, ctx.callSitePosition())
}

// appendCalleePosition appends the callee's source position for callee body index i,
// translating the callee's file identifier into the caller's file table. Callees without
// a source map contribute the call-site position instead.
//
// Takes i (int) which is the index into the callee body of the instruction just copied.
func (ctx *inlineContext) appendCalleePosition(i int) {
	sm := ctx.caller.DebugSourceMap
	if sm == nil {
		return
	}
	calleeMap := ctx.callee.DebugSourceMap
	if calleeMap == nil || i < 0 || i >= len(calleeMap.Positions) {
		sm.Positions = append(sm.Positions, ctx.callSitePosition())
		return
	}
	position := calleeMap.Positions[i]
	if position.Line != 0 && calleeMap.Files != sm.Files {
		position.FileID = sm.FileIDFor(calleeMap.FilePath(position.FileID))
	}
	if position.Line != 0 {
		position.Inlined = true
	}
	sm.Positions = append(sm.Positions, position)
}

// VerifySourceMapsParallel asserts that every function reachable from root whose debug
// source map is present has exactly one position per body instruction. Optimisation
// passes that append or insert instructions must extend the map alongside the body, so a
// mismatch names the pass that broke the invariant.
//
// Takes root (*CompiledFunction) which is the program's top-level function.
// Takes pass (string) which names the pass that just ran, for the error message.
//
// Returns an error naming the first function whose map is not parallel, or nil.
func VerifySourceMapsParallel(root *program.CompiledFunction, pass string) error {
	visited := make(map[*program.CompiledFunction]bool)
	return verifySourceMapParallelIn(root, pass, visited)
}

// sourceMapPositionsLength returns the length of compiledFunction's debug source-map
// positions, or zero when compiledFunction carries no source map.
//
// Takes compiledFunction (*CompiledFunction) whose source map is inspected.
//
// Returns the positions length.
func sourceMapPositionsLength(compiledFunction *program.CompiledFunction) int {
	if compiledFunction == nil || compiledFunction.DebugSourceMap == nil {
		return 0
	}
	return len(compiledFunction.DebugSourceMap.Positions)
}

// truncateSourceMapPositions rewinds compiledFunction's source-map positions to length,
// used when a splice rolls back the instructions it appended.
//
// Takes compiledFunction (*CompiledFunction) whose source map is rewound.
// Takes length (int) which is the positions length to restore.
func truncateSourceMapPositions(compiledFunction *program.CompiledFunction, length int) {
	if compiledFunction == nil || compiledFunction.DebugSourceMap == nil || length > len(compiledFunction.DebugSourceMap.Positions) {
		return
	}
	compiledFunction.DebugSourceMap.Positions = compiledFunction.DebugSourceMap.Positions[:length]
}

// recordInlineAnnotation marks the first instruction of an inlined callee body with
// PeepholeRewriteInline so the disassembler can name the call it replaced and the callee
// it came from. Self-recursive splices keep their dedicated unroll annotation.
//
// Takes ctx (*inlineContext) which carries caller, callee, site and opCallPC.
// Takes calleeBodyStart (int) which is the caller PC at which the appended body starts.
func recordInlineAnnotation(ctx *inlineContext, calleeBodyStart int) {
	if ctx == nil || ctx.caller == nil || ctx.callee == ctx.caller {
		return
	}
	if calleeBodyStart < 0 || calleeBodyStart >= len(ctx.caller.Body) {
		return
	}
	originFunction := -1
	if ctx.site != nil && !ctx.site.IsClosure {
		originFunction = int(ctx.site.FunctionIndex)
	}
	passes.RecordInlineRewrite(ctx.caller, calleeBodyStart, ctx.opCallPC, originFunction)
}

// verifySourceMapParallelIn is the recursive body of VerifySourceMapsParallel.
//
// Takes compiledFunction (*CompiledFunction) which is the function to check.
// Takes pass (string) which names the compiler pass for diagnostics.
// Takes visited (map[*CompiledFunction]bool) which guards against cycles.
//
// Returns the first mismatch error, or nil.
func verifySourceMapParallelIn(compiledFunction *program.CompiledFunction, pass string, visited map[*program.CompiledFunction]bool) error {
	if compiledFunction == nil || visited[compiledFunction] {
		return nil
	}
	visited[compiledFunction] = true
	if compiledFunction.DebugSourceMap != nil && len(compiledFunction.DebugSourceMap.Positions) != len(compiledFunction.Body) {
		return fmt.Errorf(
			"%w: after %s, function %q has %d source positions for %d instructions",
			ErrSourceMapNotParallel, pass, compiledFunction.Name,
			len(compiledFunction.DebugSourceMap.Positions), len(compiledFunction.Body),
		)
	}
	for _, child := range compiledFunction.Functions {
		if err := verifySourceMapParallelIn(child, pass, visited); err != nil {
			return err
		}
	}
	return nil
}

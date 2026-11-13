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
	"context"
	"fmt"
	"math"
	"slices"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// tailCallReach answers, for the whole program at once, whether a function can come back
// to one that tail-calls it.
type tailCallReach struct {
	// graph holds the calls that name their callee.
	graph *namedCallGraph

	// leadsAnywhere is true, per function, when a chain of those calls arrives at a call
	// whose target is not known when the program is compiled.
	leadsAnywhere []bool

	// component numbers the functions so that two share a number exactly when each can reach
	// the other.
	component []int
}

// cannotLeadBackTo reports whether no chain of calls starting in function can arrive at
// target, which holds a call that names function.
//
// Only calls whose callee is named when the program is compiled are followed. A call
// through a function value, a method value or an interface, a native call, which may be
// handed a function to call back, and a deferred call can each arrive anywhere, so a
// function that reaches one counts as able to lead back.
//
// Takes function (*program.CompiledFunction) which is where the chain starts.
// Takes target (*program.CompiledFunction) which is the function it must not reach.
//
// Returns bool which is true when target is out of reach.
func (reach *tailCallReach) cannotLeadBackTo(function, target *program.CompiledFunction) bool {
	from, known := reach.graph.index[function]
	to, targetKnown := reach.graph.index[target]
	if !known || !targetKnown || reach.leadsAnywhere[from] {
		return false
	}
	return reach.component[from] != reach.component[to]
}

// newTailCallReach reads the program's calls once.
//
// Takes ctx (context.Context) which cancels the reading.
// Takes functions ([]*program.CompiledFunction) which are the program's functions.
//
// Returns *tailCallReach which answers cannotLeadBackTo().
// Returns error when the context is cancelled.
func newTailCallReach(ctx context.Context, functions []*program.CompiledFunction) (*tailCallReach, error) {
	graph, err := newNamedCallGraph(ctx, functions, tailCallWord)
	if err != nil {
		return nil, err
	}
	return &tailCallReach{graph: graph, leadsAnywhere: graph.reachesMarked(), component: graph.components()}, nil
}

// demoteTailCallsThatSaveNoStack turns a tail call into a call followed by a return
// wherever replacing the frame buys nothing.
//
// Takes ctx (context.Context) which cancels the work between functions.
// Takes functions ([]*program.CompiledFunction) which are the program's functions.
//
// Returns error when the context is cancelled.
func demoteTailCallsThatSaveNoStack(ctx context.Context, functions []*program.CompiledFunction) error {
	var reach *tailCallReach
	for index, caller := range functions {
		if index&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("tail-call demotion cancelled: %w", err)
			}
		}
		if caller == nil || len(caller.NamedResultLocations) != 0 || !bodyTailCalls(caller.Body) {
			continue
		}
		if reach == nil {
			read, err := newTailCallReach(ctx, functions)
			if err != nil {
				return err
			}
			reach = read
		}
		demoteTailCallsOf(reach, caller)
	}
	return nil
}

// demoteTailCallsOf demotes every tail call of one function that saves no stack.
//
// Takes reach (*tailCallReach) which knows where each function's calls lead.
// Takes caller (*program.CompiledFunction) whose body is rewritten.
func demoteTailCallsOf(reach *tailCallReach, caller *program.CompiledFunction) {
	original := len(caller.Body)
	for pc := range original {
		if isa.InstrIsTier1SubOp(caller.Body[pc], isa.SubOpTailCall) && tailCallSavesNoStack(reach, caller, pc) {
			demoteTailCall(caller, pc)
		}
	}
	if len(caller.Body) != original {
		forgetTailCallRefusal(caller)
		caller.EnsurePrecomputedAllocCounts()
	}
}

// bodyTailCalls reports whether a body holds a tail call.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns bool which is true when it does.
func bodyTailCalls(body []isa.Instruction) bool {
	return slices.ContainsFunc(body, func(instruction isa.Instruction) bool {
		return isa.InstrIsTier1SubOp(instruction, isa.SubOpTailCall)
	})
}

// tailCallWord classifies a word for the question tailCallReach answers.
//
// Takes function (*program.CompiledFunction) which holds the word.
// Takes instruction (isa.Instruction) which is the word.
//
// Returns *program.CompiledFunction which is the callee of a call that names it.
// Returns callWord which is opaque for a deferred call, a built-in call or a call whose
// callee is not named.
func tailCallWord(function *program.CompiledFunction, instruction isa.Instruction) (*program.CompiledFunction, callWord) {
	if instruction.Op == isa.OpDefer || isa.InstrIsTier1SubOp(instruction, isa.SubOpCallBuiltin) {
		return nil, callWordOpaque
	}
	if !isCallSiteInstruction(instruction) {
		return nil, callWordPlain
	}
	site := calleeNamedAt(function, instruction)
	if site == nil || !namesItsCallee(instruction) || site.IsClosure || site.IsNative || site.IsMethod {
		return nil, callWordOpaque
	}
	return site.CachedCallee, callWordFollowed
}

// tailCallSavesNoStack reports whether the tail call at pc may run as a plain call.
//
// The caller's results must be plain result slots, which a function with named results
// does not guarantee (demoteTailCallsThatSaveNoStack leaves those alone), few enough for
// a return to count them, and the callee must be unable to reach the caller again (see
// tailCallReach.cannotLeadBackTo). A site with no known callee is kept as it is.
//
// Takes reach (*tailCallReach) which knows where each function's calls lead.
// Takes caller (*program.CompiledFunction) which holds the tail call.
// Takes pc (int) which is the tail call.
//
// Returns bool which is true when the call can be demoted.
func tailCallSavesNoStack(reach *tailCallReach, caller *program.CompiledFunction, pc int) bool {
	siteIndex := int(caller.Body[pc].WideIndex())
	if siteIndex >= len(caller.CallSites) {
		return false
	}
	site := &caller.CallSites[siteIndex]
	callee := site.CachedCallee
	if callee == nil || callee == caller || site.TailReuseFrameInPlace {
		return false
	}
	if len(caller.ResultKinds) > math.MaxUint8 || len(callee.ResultKinds) != len(caller.ResultKinds) ||
		!program.FitsJumpOffset(len(caller.Body)-pc-1) {
		return false
	}
	return reach.cannotLeadBackTo(callee, caller)
}

// demoteTailCall rewrites the tail call at pc as a jump to a call and a return appended
// to the body, which the final compaction moves back into place.
//
// A function that did nothing but tail-call may have had no register for a result at all,
// since the callee returned straight to its caller's caller, so the slots are reserved
// here.
//
// Takes caller (*program.CompiledFunction) whose body and call site are rewritten. It
// returns no more values than a return instruction can count (tailCallSavesNoStack).
// Takes pc (int) which is the tail call.
func demoteTailCall(caller *program.CompiledFunction, pc int) {
	tailCall := caller.Body[pc]
	site := &caller.CallSites[tailCall.WideIndex()]
	var ordinal [isa.NumRegisterKinds]uint8
	site.Returns = make([]program.VarLocation, len(caller.ResultKinds))
	for index, kind := range caller.ResultKinds {
		site.Returns[index] = program.VarLocation{Kind: kind, Register: ordinal[kind]}
		ordinal[kind]++
		caller.NumRegisters[kind] = max(caller.NumRegisters[kind], uint32(ordinal[kind]))
	}
	site.TailArgsAlias = false
	caller.PrecomputedAllocCountsValid = false

	low, high := SplitJumpOffsetBytes(len(caller.Body) - pc - 1)
	caller.Body[pc] = isa.NewInstruction(isa.OpDrillTier1, byte(isa.SubOpJump), low, high)
	finish := isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)
	if len(caller.ResultKinds) != 0 {
		finish = isa.NewTier2Instruction(isa.SubOpTier2Return, safeconv.IntToUint8(len(caller.ResultKinds)))
	}
	call := isa.NewTier1Instruction(program.DirectCallSubOpcode(site, site.CachedCallee), tailCall.B, tailCall.C)
	caller.Body = append(caller.Body, call, finish)
	repeatSourcePosition(caller, pc, len(caller.Body))
}

// repeatSourcePosition extends the source map to cover words appended to the body, giving
// each the position of the word they stand in for.
//
// Takes function (*program.CompiledFunction) whose source map is extended.
// Takes pc (int) which is the word whose position is repeated.
// Takes length (int) which is the body length the map must reach.
func repeatSourcePosition(function *program.CompiledFunction, pc, length int) {
	sourceMap := function.DebugSourceMap
	if sourceMap == nil || pc >= len(sourceMap.Positions) {
		return
	}
	for len(sourceMap.Positions) < length {
		sourceMap.Positions = append(sourceMap.Positions, sourceMap.Positions[pc])
	}
}

// forgetTailCallRefusal clears the tail-call refusal when the body has no tail calls.
//
// Takes function (*program.CompiledFunction) which may have lost its last tail call.
func forgetTailCallRefusal(function *program.CompiledFunction) {
	if bodyTailCalls(function.Body) {
		return
	}
	if function.EmittedInlineBlocker == program.InlineRefusalTailCall {
		function.EmittedInlineBlocker = program.InlineRefusalUnknown
	}
	function.CachedInlineRefusal = program.InlineRefusalUnknown
}

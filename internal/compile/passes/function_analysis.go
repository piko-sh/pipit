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
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// functionAnalysis is the control-flow analysis every pass in a pipeline shares. Built
// once per function by RunPipeline, with dominators and loops memoised on first request.
type functionAnalysis struct {
	// compiledFunction is the function the analysis describes.
	compiledFunction *program.CompiledFunction

	// jumpTargets caches BuildAllJumpTargets(compiledFunction.Body). Built on first use
	// because most passes gate on cheaper checks first and tiny functions never need it.
	jumpTargets map[int]bool

	// dominators caches Dominators().
	dominators *functionDominators

	// predecessors caches the per-PC predecessor lists Dominators() needs.
	predecessors [][]int

	// reachable caches the per-PC reachability mask Dominators() needs.
	reachable []bool

	// loops caches Loops().
	loops []loopRange

	// jumpTargetsBuilt is true when jumpTargets is current.
	jumpTargetsBuilt bool

	// cfgBuilt is true when predecessors and reachable are current.
	cfgBuilt bool

	// dominatorsComputed is true when dominators is current.
	dominatorsComputed bool

	// loopsComputed is true when loops is current.
	loopsComputed bool

	// hasTypeSwitch caches whether the body holds an isa.SubOpTypeSwitchJump.
	hasTypeSwitch bool

	// typeSwitchScanned is true when hasTypeSwitch is current.
	typeSwitchScanned bool
}

// newFunctionAnalysis builds the shared analysis for compiledFunction's current body.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function the passes
// will rewrite.
//
// Returns the analysis with its eager members populated and its memoised members empty.
func newFunctionAnalysis(compiledFunction *program.CompiledFunction) *functionAnalysis {
	analysis := functionAnalysisFor(compiledFunction)
	return &analysis
}

// JumpTargets returns the set of program counters that some jump in the body targets,
// built on first use and kept until Invalidate().
//
// Returns map[int]bool whose keys are every targeted PC.
func (analysis *functionAnalysis) JumpTargets() map[int]bool {
	if !analysis.jumpTargetsBuilt {
		analysis.jumpTargets = BuildAllJumpTargets(analysis.compiledFunction.Body)
		analysis.jumpTargetsBuilt = true
	}
	return analysis.jumpTargets
}

// Dominators returns the memoised full-function dominator table.
//
// Returns nil when the body is empty or the dominator dataflow did not converge; the
// callers treat nil as "no dominance information" and skip their rewrites.
func (analysis *functionAnalysis) Dominators() *functionDominators {
	if !analysis.dominatorsComputed {
		analysis.buildCFG()
		analysis.dominators = computeFunctionDominatorsFrom(analysis.compiledFunction.Body, analysis.predecessors, analysis.reachable)
		analysis.dominatorsComputed = true
	}
	return analysis.dominators
}

// Loops returns the memoised natural loops of the body, innermost (smallest span) first.
//
// Returns the loop list; callers must not modify it.
func (analysis *functionAnalysis) Loops() []loopRange {
	if !analysis.loopsComputed {
		analysis.loops = identifyLoops(analysis.compiledFunction.Body)
		analysis.loopsComputed = true
	}
	return analysis.loops
}

// HasTypeSwitch reports whether the body holds a multi-way type-switch dispatch, whose
// targets the single-target loop and region analyses cannot follow.
//
// Returns true when some instruction is an isa.SubOpTypeSwitchJump.
func (analysis *functionAnalysis) HasTypeSwitch() bool {
	if !analysis.typeSwitchScanned {
		analysis.hasTypeSwitch = bodyHasTypeSwitch(analysis.compiledFunction.Body)
		analysis.typeSwitchScanned = true
	}
	return analysis.hasTypeSwitch
}

// invalidate drops all cached analysis so later passes rebuild against the current body.
// A nil receiver is a no-op.
func (analysis *functionAnalysis) invalidate() {
	if analysis == nil {
		return
	}
	analysis.rebuild()
}

// buildCFG() computes the predecessor lists and reachability that Dominators needs, on
// first use.
func (analysis *functionAnalysis) buildCFG() {
	if analysis.cfgBuilt {
		return
	}
	body := analysis.compiledFunction.Body
	analysis.predecessors = buildAliasPredecessors(body)
	analysis.reachable = computeReachable(body, len(body))
	analysis.cfgBuilt = true
}

// rebuild recomputes every eager member from the current body and clears the memoised
// ones.
func (analysis *functionAnalysis) rebuild() {
	analysis.jumpTargets = nil
	analysis.jumpTargetsBuilt = false
	analysis.predecessors = nil
	analysis.reachable = nil
	analysis.cfgBuilt = false
	analysis.dominators = nil
	analysis.dominatorsComputed = false
	analysis.loops = nil
	analysis.loopsComputed = false
	analysis.hasTypeSwitch = false
	analysis.typeSwitchScanned = false
}

// functionAnalysisFor() returns the empty analysis value for compiledFunction. Every
// derived fact is computed on first use.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function the analysis
// will describe.
//
// Returns FunctionAnalysis with all caches empty.
func functionAnalysisFor(compiledFunction *program.CompiledFunction) functionAnalysis {
	return functionAnalysis{
		compiledFunction:   compiledFunction,
		jumpTargets:        nil,
		jumpTargetsBuilt:   false,
		predecessors:       nil,
		reachable:          nil,
		cfgBuilt:           false,
		dominators:         nil,
		dominatorsComputed: false,
		loops:              nil,
		loopsComputed:      false,
		hasTypeSwitch:      false,
		typeSwitchScanned:  false,
	}
}

// jumpTargetsFor returns the shared jump-target set when an analysis is available and
// decodes a private one from body otherwise.
//
// Takes analysis (*FunctionAnalysis) which may be nil when the caller has no analysis.
// Takes body ([]isa.Instruction) which is the instruction stream to decode when it is.
//
// Returns the jump-target set.
func jumpTargetsFor(analysis *functionAnalysis, body []isa.Instruction) map[int]bool {
	if analysis != nil {
		return analysis.JumpTargets()
	}
	return BuildAllJumpTargets(body)
}

// dominatorsFor returns the shared dominator table when an analysis is available and
// computes a private one from body otherwise.
//
// Takes analysis (*FunctionAnalysis) which may be nil when the caller has no analysis.
// Takes body ([]isa.Instruction) which is the instruction stream to analyse when it is.
//
// Returns the dominator table, or nil when the dataflow did not converge.
func dominatorsFor(analysis *functionAnalysis, body []isa.Instruction) *functionDominators {
	if analysis != nil {
		return analysis.Dominators()
	}
	return ComputeFunctionDominators(body)
}

// typeSwitchFor reports whether body holds a type-switch dispatch, using the shared
// analysis when one is available.
//
// Takes analysis (*FunctionAnalysis) which may be nil when the caller has no analysis.
// Takes body ([]isa.Instruction) which is the instruction stream to scan when it is.
//
// Returns true when some instruction is an isa.SubOpTypeSwitchJump.
func typeSwitchFor(analysis *functionAnalysis, body []isa.Instruction) bool {
	if analysis != nil {
		return analysis.HasTypeSwitch()
	}
	return bodyHasTypeSwitch(body)
}

// bodyHasTypeSwitch scans body for a type-switch dispatch.
//
// Takes body ([]isa.Instruction) which is the instruction stream to scan.
//
// Returns true when some instruction is an isa.SubOpTypeSwitchJump.
func bodyHasTypeSwitch(body []isa.Instruction) bool {
	for _, inst := range body {
		if isa.InstrIsTier1SubOp(inst, isa.SubOpTypeSwitchJump) {
			return true
		}
	}
	return false
}

// loopsFor returns the shared loop list when an analysis is available and discovers a
// private one from body otherwise.
//
// Takes analysis (*FunctionAnalysis) which may be nil when the caller has no analysis.
// Takes body ([]isa.Instruction) which is the instruction stream to scan when it is.
//
// Returns the natural loops, innermost first.
func loopsFor(analysis *functionAnalysis, body []isa.Instruction) []loopRange {
	if analysis != nil {
		return analysis.Loops()
	}
	return identifyLoops(body)
}

// naturalLoopMembers follows loop back-edges through their predecessors, including
// inlined blocks outside the header-to-latch range.
//
// Takes analysis (*functionAnalysis) which caches control-flow facts and is invalidated
// by a rewrite.
// Takes loop (loopRange) which identifies the loop header and latch.
//
// Returns []int containing sorted member PCs, or nil for side entries and unsupported
// multi-way dispatch.
func naturalLoopMembers(analysis *functionAnalysis, loop loopRange) []int {
	if analysis.HasTypeSwitch() {
		return nil
	}
	analysis.buildCFG()
	body := analysis.compiledFunction.Body
	seen := make([]bool, len(body))
	seen[loop.header] = true
	var pending []int
	for pc := range body {
		if target, jump := program.JumpTargetAt(body, pc); jump && target == loop.header && pc >= loop.header {
			pending = append(pending, pc)
		}
	}
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[pc] {
			continue
		}
		if pc == 0 {
			return nil
		}
		seen[pc] = true
		pending = append(pending, analysis.predecessors[pc]...)
	}
	var members []int
	for pc, member := range seen {
		if member {
			members = append(members, pc)
		}
	}
	return members
}

// loopFacts lazily computes the facts a loop hoist needs, so loops without candidates
// never pay for them.
type loopFacts struct {
	// analysis supplies the cached control-flow facts.
	analysis *functionAnalysis

	// dominatorTable caches the dominator relation once requested.
	dominatorTable *functionDominators

	// members caches the natural loop members once requested.
	members []int

	// loop is the loop under consideration.
	loop loopRange

	// membersDone is true when members is current.
	membersDone bool

	// dominatorsDone is true when dominatorTable is current.
	dominatorsDone bool

	// renamed is true when a search renamed a destination, which invalidates these facts and
	// calls for a fresh search.
	renamed bool
}

// newLoopFacts prepares the lazy facts for loop.
//
// Takes analysis (*functionAnalysis) which must not be nil.
// Takes loop (loopRange) which is the loop under consideration.
//
// Returns the facts with nothing computed yet.
func newLoopFacts(analysis *functionAnalysis, loop loopRange) *loopFacts {
	return &loopFacts{analysis: analysis, dominatorTable: nil, members: nil, loop: loop, membersDone: false, dominatorsDone: false, renamed: false}
}

// loopMembers returns the natural loop members.
//
// Returns the sorted member PCs, or nil when the loop has side entries or multi-way
// dispatch.
func (facts *loopFacts) loopMembers() []int {
	if !facts.membersDone {
		facts.members = naturalLoopMembers(facts.analysis, facts.loop)
		facts.membersDone = true
	}
	return facts.members
}

// dominators returns the function's dominator relation.
//
// Returns the relation, or nil when the dataflow did not converge.
func (facts *loopFacts) dominators() *functionDominators {
	if !facts.dominatorsDone {
		facts.dominatorTable = facts.analysis.Dominators()
		facts.dominatorsDone = true
	}
	return facts.dominatorTable
}

// dominatesLatch reports whether pc runs on every traversal that reaches the latch.
//
// Takes pc (int) which is the candidate instruction.
//
// Returns false when dominance is unknown.
func (facts *loopFacts) dominatesLatch(pc int) bool {
	dominators := facts.dominators()
	return dominators != nil && dominators.Dominates(pc, facts.loop.latch)
}

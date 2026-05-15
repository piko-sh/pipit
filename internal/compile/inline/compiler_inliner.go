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

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// defaultInlineBudget caps the callee hairyness score at non-loop sites.
	defaultInlineBudget = 40

	// loopInlineBudget caps the callee hairyness score at loop sites, doubled because the
	// amortised win scales with iteration count.
	loopInlineBudget = 80

	// SelfUnrollBudget caps the hairyness score of a callee considered for 1-level
	// self-recursive unrolling.
	SelfUnrollBudget = 20

	// maxCallerBodyAfterInline bounds a caller's body length post-inlining. Beyond this
	// threshold the verifier worklist costs grow superlinearly and jump offsets risk
	// overflow.
	maxCallerBodyAfterInline = 2048

	// maxInlinesPerCaller caps how many distinct call sites within one caller may be
	// inlined. Defence against a runaway caller pulling in hundreds of small callees and
	// bloating the function.
	maxInlinesPerCaller = 8

	// callGraphSeedCapacity is the initial capacity hint used when allocating visit-state
	// tables for the call-graph walk. Sized to fit the common case (a couple-of-dozen
	// reachable nested functions) without growth.
	callGraphSeedCapacity = 32

	// maxFunctionNestingDepth caps the recursion depth in CollectReachableFunctions() and
	// bottomUpOrder(). Functions nested beyond this limit are silently excluded from the
	// reachable set, which means they miss inlining but cannot stack-overflow the compiler.
	maxFunctionNestingDepth = 256

	// registerBankWatermark is the maximum number of register slots per bank the runtime can
	// address with a single uint8 operand. A splice that would push the caller past this is
	// refused as InlineRefusalCapWatermark.
	registerBankWatermark = 255
)

// inlineContext bundles per-splice mutable state for one (caller, callee, callSiteIndex)
// attempt.
type inlineContext struct {
	// caller is the function being mutated; the splice appends to its body.
	caller *program.CompiledFunction

	// callee is the function whose body is being spliced into caller.
	callee *program.CompiledFunction

	// site is the call site descriptor in caller.callSites being inlined.
	site *program.CallSite

	// paramPreCopies records the pre-copy MOVEs emitted before the inlined body so the
	// callee can write to its parameter slot without clobbering the caller's argument.
	paramPreCopies []paramPreCopy

	// opCallPC is the PC of the opCall instruction in caller.body being inlined.
	opCallPC int

	// remap is the per-bank callee-slot to caller-slot mapping, encoded as int16 with -1
	// meaning "unset".
	remap [isa.NumRegisterKinds][isa.GeneralRegisterBankSize]int16

	// siteIndex is the index of site within caller.callSites.
	siteIndex uint16
}

// resetRegisterRemap reinitialises ctx.remap to all -1 (unset).
func (ctx *inlineContext) resetRegisterRemap() {
	for k := range ctx.remap {
		for i := range ctx.remap[k] {
			ctx.remap[k][i] = -1
		}
	}
}

// lookupRegister returns the caller-side slot for a callee register.
//
// Takes bank (isa.RegisterKind) which names the bank holding the mapping.
// Takes calleeSlot (uint8) which is the callee-side register index.
//
// Returns the caller-side slot.
// Returns false when no mapping exists for the (bank, calleeSlot) pair.
func (ctx *inlineContext) lookupRegister(bank isa.RegisterKind, calleeSlot uint8) (uint8, bool) {
	v := ctx.remap[bank][calleeSlot]
	if v < 0 {
		return 0, false
	}
	return safeconv.Int16ToByte(v), true
}

// setRegister records the calleeSlot -> callerSlot mapping in bank.
//
// Takes bank (isa.RegisterKind) which names the bank holding the mapping.
// Takes calleeSlot (uint8) which is the callee-side register index.
// Takes callerSlot (uint8) which is the freshly allocated caller-side slot.
func (ctx *inlineContext) setRegister(bank isa.RegisterKind, calleeSlot, callerSlot uint8) {
	ctx.remap[bank][calleeSlot] = int16(callerSlot)
}

// paramPreCopy describes one MOVE emitted before the inlined callee body to seed a fresh
// caller-side parameter slot from the caller's argument source register.
type paramPreCopy struct {
	// sourceKind names the bank holding the caller-side argument register. For same-bank
	// pre-copies this equals kind.
	sourceKind isa.RegisterKind

	// kind names the register bank the move is emitted INTO (the fresh parameter slot's
	// bank). For same-bank pre-copies this equals sourceKind.
	kind isa.RegisterKind

	// destination is the fresh caller-side slot allocated for the callee's parameter.
	destination uint8

	// Source is the caller's argument source register feeding the pre-copy.
	Source uint8
}

// bottomUpWalker holds the depth-first state behind bottomUpOrder.
type bottomUpWalker struct {
	// allFuncs is the full set of reachable functions.
	allFuncs []*program.CompiledFunction

	// indexOf maps each function to its index in allFuncs.
	indexOf map[*program.CompiledFunction]int

	// visited tracks traversal state per function: 0 unvisited, 1 on the current path, 2
	// finished.
	visited []uint8

	// out accumulates functions in bottom-up order.
	out []*program.CompiledFunction
}

// visit appends allFuncs[i] after every callee it can reach that is not already on the
// current path, bounded by maxFunctionNestingDepth.
//
// Takes i (int) which indexes allFuncs.
// Takes depth (int) which is the current recursion depth.
func (w *bottomUpWalker) visit(i int, depth int) {
	if w.visited[i] != 0 || depth >= maxFunctionNestingDepth {
		return
	}
	w.visited[i] = 1
	compiledFunction := w.allFuncs[i]
	for siteIndex := range compiledFunction.CallSites {
		if j, ok := w.unvisitedCallee(&compiledFunction.CallSites[siteIndex]); ok {
			w.visit(j, depth+1)
		}
	}
	w.visited[i] = 2
	w.out = append(w.out, compiledFunction)
}

// unvisitedCallee resolves a call site to an index into allFuncs when the callee is
// known, reachable and not on the current path.
//
// Takes site (*program.CallSite) whose CachedCallee is consulted.
//
// Returns the callee index and true, or zero and false when the site should be skipped.
func (w *bottomUpWalker) unvisitedCallee(site *program.CallSite) (int, bool) {
	callee := site.CachedCallee
	if callee == nil {
		return 0, false
	}
	j, ok := w.indexOf[callee]
	if !ok || w.visited[j] == 1 {
		return 0, false
	}
	return j, true
}

// RunBytecodeInliner is the top-level inliner pass entry. It walks the call graph
// bottom-up, splicing eligible callees into their callers.
//
// Takes root (*CompiledFunction) which is the program's top-level compiled function whose
// nested closures are walked.
// Takes opts (passes.Options) whose UnrollSelfRecursive field gates one-level
// self-recursive unrolling.
//
// Returns nil when inlining is disabled or no callee is eligible; errors only surface on
// internal-consistency failures.
func RunBytecodeInliner(ctx context.Context, root *program.CompiledFunction, opts passes.Options) error {
	if root == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("runBytecodeInliner cancelled: %w", err)
	}
	allFuncs := CollectReachableFunctions(root)
	if !anyInlineableCallSite(allFuncs) {
		return nil
	}
	if root.Functions != nil {
		adjacency := program.BuildCallAdjacency(root.Functions)
		inSCC := program.FindCallGraphSCCs(adjacency)
		for i, compiledFunction := range root.Functions {
			if i < len(inSCC) {
				compiledFunction.InRecursionCycle = inSCC[i]
			}
		}
	}
	order := bottomUpOrder(allFuncs)
	for _, caller := range order {
		if inlineCallsIn(caller, opts) > 0 {
			if err := reoptimiseAfterInline(ctx, caller); err != nil {
				return err
			}
		}
	}
	return nil
}

// CollectReachableFunctions returns every function reachable from root.
//
// Takes root (*CompiledFunction) whose nested functions are traversed.
//
// Returns the reachable set in walk order.
func CollectReachableFunctions(root *program.CompiledFunction) []*program.CompiledFunction {
	visited := make(map[*program.CompiledFunction]struct{}, callGraphSeedCapacity)
	out := make([]*program.CompiledFunction, 0, callGraphSeedCapacity)
	var walk func(*program.CompiledFunction, int)
	walk = func(compiledFunction *program.CompiledFunction, depth int) {
		if compiledFunction == nil || depth >= maxFunctionNestingDepth {
			return
		}
		if _, ok := visited[compiledFunction]; ok {
			return
		}
		visited[compiledFunction] = struct{}{}
		out = append(out, compiledFunction)
		for _, child := range compiledFunction.Functions {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return out
}

// CanInlineSelfRecursive gates 1-level recursive unrolling for the special case where the
// call site's callee is the caller itself. The general inliner refuses recursive callees
// outright; this helper permits a single splice per site, with the inner recursive call
// inside the spliced body left as a regular isa.SubOpCall.
//
// Takes site (*CallSite) which is the call site under consideration.
// Takes callee (*CompiledFunction) which is the function being recursively spliced.
// Takes inLoop (bool) which is true when the call site sits inside a loop body.
//
// Returns InlineEligible when all gates pass.
func CanInlineSelfRecursive(site *program.CallSite, callee *program.CompiledFunction, inLoop bool) program.InlineRefusal {
	if site.RecursionUnrolled {
		return program.InlineRefusalAlreadyUnrolled
	}
	if inLoop {
		return program.InlineRefusalSelfInLoop
	}
	if reason := calleeInlineRefusal(callee); reason != program.InlineEligible {
		return reason
	}
	if calleeHairyness(callee) > SelfUnrollBudget {
		return program.InlineRefusalSelfHairy
	}
	return program.InlineEligible
}

// reoptimiseAfterInline re-runs peephole fusion after a splice.
//
// Re-runs peephole fusion on the caller only (not its nested functions) and recomputes
// precomputedAllocCounts so the new caller-side register slots are properly allocated at
// run time.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to
// re-optimise.
//
// Returns error when context cancellation fires.
func reoptimiseAfterInline(ctx context.Context, compiledFunction *program.CompiledFunction) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reoptimiseAfterInline cancelled: %w", err)
	}
	body := compiledFunction.Body
	n := len(body)
	jumpTargets := compiledFunction.BuildJumpTargets(body)
	for i := range n {
		if i&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("reoptimiseAfterInline cancelled: %w", err)
			}
		}
		if passes.FuseCopyStructFieldGeneralT0(compiledFunction, body, i, n, jumpTargets) {
			continue
		}
		if passes.FuseThreeInstrPatterns(compiledFunction, body, i, n, jumpTargets) ||
			passes.FuseArithConst(compiledFunction, body, i, n, jumpTargets) ||
			passes.FuseAddIntJump(compiledFunction, body, i, n, jumpTargets) ||
			passes.FuseConcatRune(compiledFunction, body, i, n, jumpTargets) ||
			passes.FuseAppendMove(compiledFunction, body, i, n, jumpTargets) ||
			passes.FuseStringIndexToInt(compiledFunction, body, i, n, jumpTargets) {
			continue
		}
		passes.OptimiseLoadIntConst(compiledFunction, body, i)
		passes.OptimiseLoadUintConst(compiledFunction, body, i)
	}
	passes.SyncSourceMapAfterOptimise(compiledFunction, body)
	compiledFunction.PrecomputedAllocCountsValid = false
	compiledFunction.EnsurePrecomputedAllocCounts()
	return nil
}

// anyInlineableCallSite reports whether any function holds an inlineable site.
//
// Takes allFuncs ([]*CompiledFunction) which holds every reachable function in the
// program.
//
// Returns true when at least one site's cachedCallee passes calleeInlineRefusal, false
// otherwise.
func anyInlineableCallSite(allFuncs []*program.CompiledFunction) bool {
	for _, compiledFunction := range allFuncs {
		if compiledFunction == nil {
			continue
		}
		for i := range compiledFunction.CallSites {
			site := &compiledFunction.CallSites[i]
			if site.IsNative || site.IsClosure || site.IsMethod {
				continue
			}
			callee := site.CachedCallee
			if callee == nil {
				continue
			}
			if calleeInlineRefusal(callee) == program.InlineEligible {
				return true
			}
		}
	}
	return false
}

// inlineCallsIn drives the splice for every eligible site in caller.
//
// Takes caller (*CompiledFunction) whose body is mutated by each successful splice.
// Takes opts (passes.Options) whose UnrollSelfRecursive field gates self-recursive sites.
//
// Returns the count of call sites successfully inlined.
func inlineCallsIn(caller *program.CompiledFunction, opts passes.Options) int {
	if caller == nil || len(caller.CallSites) == 0 {
		return 0
	}
	inLoopMask := computeInLoopMask(caller)
	sitePCs := buildSitePCTable(caller)
	spliced := 0
	for siteIndex := range caller.CallSites {
		if spliced >= maxInlinesPerCaller {
			break
		}
		if !trySpliceSite(caller, siteIndex, sitePCs, inLoopMask, opts) {
			continue
		}
		spliced++
		extendInLoopMaskForAppendedBody(caller, &inLoopMask)
		sitePCs = buildSitePCTable(caller)
	}
	return spliced
}

// trySpliceSite evaluates the inliner gates for one call site and, on success, performs
// the splice.
//
// Takes caller (*CompiledFunction) which is the function being optimised.
// Takes siteIndex (int) which is the index of the call site within caller.callSites.
// Takes sitePCs ([]int) which lists the program counters of each call site's
// isa.SubOpCall within the caller body.
// Takes inLoopMask ([]bool) which is the parallel mask marking sites that reside inside a
// loop.
// Takes opts (passes.Options) whose UnrollSelfRecursive field refuses self-recursive
// sites outright when false.
//
// Returns true when the splice landed and false otherwise.
func trySpliceSite(caller *program.CompiledFunction, siteIndex int, sitePCs []int, inLoopMask []bool, opts passes.Options) bool {
	site := &caller.CallSites[siteIndex]
	opCallPC := -1
	if siteIndex < len(sitePCs) {
		opCallPC = sitePCs[siteIndex]
	}
	if opCallPC < 0 {
		return false
	}
	if site.CachedCallee == caller && !opts.UnrollSelfRecursive {
		return false
	}
	inLoop := opCallPC < len(inLoopMask) && inLoopMask[opCallPC]
	if reason := canInline(caller, site, opCallPC, inLoop); reason != program.InlineEligible {
		return false
	}
	result := trySpliceCallAt(caller, safeconv.IntToUint16(siteIndex), opCallPC)
	if !result.Spliced {
		return false
	}
	if site.CachedCallee == caller {
		site.RecursionUnrolled = true
	}
	return true
}

// buildSitePCTable maps each call-site index to its isa.SubOpCall PC.
//
// Takes caller (*CompiledFunction) whose body is scanned.
//
// Returns a slice indexed by call-site index with the PC of that site's isa.SubOpCall, or
// -1 when the site has no matching isa.SubOpCall (already inlined, or another call
// variant).
func buildSitePCTable(caller *program.CompiledFunction) []int {
	n := len(caller.CallSites)
	if n == 0 {
		return nil
	}
	pcs := make([]int, n)
	for i := range pcs {
		pcs[i] = -1
	}
	for pc := range caller.Body {
		instr := caller.Body[pc]
		if !isa.InstrIsTier1SubOp(instr, isa.SubOpCall) {
			continue
		}
		index := int(uint16(instr.B) | uint16(instr.C)<<8)
		if index < n {
			pcs[index] = pc
		}
	}
	return pcs
}

// extendInLoopMaskForAppendedBody grows *mask to match caller body length.
//
// New PCs default to false (no back-edge target in or into the appended region) unless
// the appended bytes contain a backward jump, in which case the full mask is recomputed
// for the affected window.
//
// Takes caller (*CompiledFunction) whose body length sets the target size.
// Takes mask (*[]bool) which is grown (or reallocated) in place.
func extendInLoopMaskForAppendedBody(caller *program.CompiledFunction, mask *[]bool) {
	newLen := len(caller.Body)
	oldLen := len(*mask)
	if newLen <= oldLen {
		return
	}
	hasBackwardJump := false
	for pc := oldLen; pc < newLen; pc++ {
		if target, ok := program.JumpTargetAt(caller.Body, pc); ok && target <= pc {
			hasBackwardJump = true
			break
		}
	}
	if hasBackwardJump {
		*mask = computeInLoopMask(caller)
		return
	}
	if cap(*mask) >= newLen {
		*mask = (*mask)[:newLen]
		for i := oldLen; i < newLen; i++ {
			(*mask)[i] = false
		}
		return
	}
	grown := make([]bool, newLen)
	copy(grown, *mask)
	*mask = grown
}

// bottomUpOrder reorders allFuncs so call-graph leaves come first.
//
// Takes allFuncs ([]*CompiledFunction) which is the unsorted reachable set.
//
// Returns a fresh slice with allFuncs reordered bottom-up.
func bottomUpOrder(allFuncs []*program.CompiledFunction) []*program.CompiledFunction {
	walker := bottomUpWalker{
		allFuncs: allFuncs,
		indexOf:  make(map[*program.CompiledFunction]int, len(allFuncs)),
		visited:  make([]uint8, len(allFuncs)),
		out:      make([]*program.CompiledFunction, 0, len(allFuncs)),
	}
	for i, compiledFunction := range allFuncs {
		walker.indexOf[compiledFunction] = i
	}
	for i := range allFuncs {
		walker.visit(i, 0)
	}
	return walker.out
}

// canInline decides whether a (caller, callee, site) tuple is eligible.
//
// Takes caller (*CompiledFunction) whose body would absorb the splice.
// Takes site (*CallSite) which describes the candidate call.
// Takes _ (int) which is unused; kept on the signature for symmetry with callers that
// already supply the value.
// Takes inLoop (bool) which selects loopInlineBudget over defaultInlineBudget.
//
// Returns InlineEligible on success or one of the InlineRefusal* constants describing why
// the splice was refused.
func canInline(caller *program.CompiledFunction, site *program.CallSite, _ int, inLoop bool) program.InlineRefusal {
	if reason := canInlineSiteShape(site); reason != program.InlineEligible {
		return reason
	}
	callee := site.CachedCallee
	if callee == caller {
		return CanInlineSelfRecursive(site, callee, inLoop)
	}
	if callee.InRecursionCycle {
		return program.InlineRefusalRecursion
	}
	if reason := calleeInlineRefusal(callee); reason != program.InlineEligible {
		return reason
	}
	if reason := canInlineBodyBudget(caller, callee, inLoop); reason != program.InlineEligible {
		return reason
	}
	return canInlineRegisterBudget(caller, callee)
}

// canInlineSiteShape rejects call sites the inliner cannot handle at all.
//
// Takes site (*CallSite) which is the call site to classify.
//
// Returns InlineEligible when the shape is acceptable, or a specific refusal reason
// otherwise.
func canInlineSiteShape(site *program.CallSite) program.InlineRefusal {
	if site == nil {
		return program.InlineRefusalUnknown
	}
	if site.IsNative || site.IsClosure || site.IsMethod {
		return program.InlineRefusalSiteIndirect
	}
	if site.IsEllipsisSpread {
		return program.InlineRefusalVariadic
	}
	if site.CachedCallee == nil {
		return program.InlineRefusalNoBody
	}
	return program.InlineEligible
}

// canInlineBodyBudget enforces the hairyness-weighted size budget and the absolute caller
// body cap.
//
// Takes caller (*CompiledFunction) which is the function receiving the splice.
// Takes callee (*CompiledFunction) which is the function being spliced in.
// Takes inLoop (bool) which is true when the call site sits inside a loop body.
//
// Returns InlineEligible when both budgets pass, or a specific refusal reason otherwise.
func canInlineBodyBudget(caller, callee *program.CompiledFunction, inLoop bool) program.InlineRefusal {
	budget := defaultInlineBudget
	if inLoop {
		budget = loopInlineBudget
	}
	if calleeHairyness(callee) > budget {
		return program.InlineRefusalOversize
	}
	if len(caller.Body)+len(callee.Body) > maxCallerBodyAfterInline {
		return program.InlineRefusalCallerCap
	}
	return program.InlineEligible
}

// canInlineRegisterBudget rejects splices that would push the caller's per-bank register
// footprint past the watermark.
//
// Takes caller (*CompiledFunction) which is the function receiving the splice.
// Takes callee (*CompiledFunction) which is the function being spliced in.
//
// Returns InlineEligible when all banks stay under the watermark, or
// InlineRefusalCapWatermark otherwise.
func canInlineRegisterBudget(caller, callee *program.CompiledFunction) program.InlineRefusal {
	for kind := range isa.RegisterKind(isa.NumRegisterKinds) {
		paramCountK := countParamsInBank(callee, kind)
		localCountK := max(int(callee.NumRegisters[kind])-paramCountK, 0)
		if int(caller.NumRegisters[kind])+localCountK > registerBankWatermark {
			return program.InlineRefusalCapWatermark
		}
	}
	return program.InlineEligible
}

// calleeInlineRefusal returns the per-callee refusal reason, caching the result on the
// callee. If later phases mutate a callee body they MUST clear
// callee.CachedInlineRefusal.
//
// Takes callee (*CompiledFunction) whose body is probed.
//
// Returns the cached or freshly-computed InlineRefusal.
func calleeInlineRefusal(callee *program.CompiledFunction) program.InlineRefusal {
	if callee.CachedInlineRefusal != program.InlineRefusalUnknown {
		return callee.CachedInlineRefusal
	}
	reason := scanCalleeForRefusal(callee)
	callee.CachedInlineRefusal = reason
	return reason
}

// genericInlineRefusal reports why a generic callee cannot be spliced, or InlineEligible
// when genericness is not itself a reason to refuse.
//
// Takes callee (*CompiledFunction) whose generic state is inspected.
//
// Returns the InlineRefusal implied by the callee's generic state, or InlineEligible.
func genericInlineRefusal(callee *program.CompiledFunction) program.InlineRefusal {
	if callee.IsGenericFunction && callee.SpecialisationOrigin == nil {
		return program.InlineRefusalGenericPlaceholder
	}
	if callee.SpecialisationOrigin != nil && callee.HasReceiver {
		return program.InlineRefusalSpecialisedMethod
	}
	return program.InlineEligible
}

// scanCalleeForRefusal answers eligibility from the Emit-time flag.
//
// Takes callee (*CompiledFunction) whose body is probed.
//
// Returns the InlineRefusal describing why the callee was refused, or InlineEligible.
func scanCalleeForRefusal(callee *program.CompiledFunction) program.InlineRefusal {
	if len(callee.Body) == 0 {
		return program.InlineRefusalNoBody
	}
	if len(callee.UpvalueDescriptors) > 0 {
		return program.InlineRefusalUpvalues
	}
	if callee.HasRecover {
		return program.InlineRefusalRecover
	}
	if refusal := genericInlineRefusal(callee); refusal != program.InlineEligible {
		return refusal
	}
	if callee.EmittedInlineBlocker != program.InlineRefusalUnknown {
		if callee.EmittedInlineBlocker == program.InlineRefusalTailCall && calleeTailCallsAreAllCrossTarget(callee) {
			return program.InlineEligible
		}
		return callee.EmittedInlineBlocker
	}
	for i := range callee.Body {
		r := program.BlockerForInstruction(callee.Body[i])
		if r == program.InlineRefusalUnknown {
			continue
		}
		if r == program.InlineRefusalTailCall && calleeTailCallsAreAllCrossTarget(callee) {
			continue
		}
		return r
	}
	return program.InlineEligible
}

// calleeTailCallsAreAllCrossTarget reports whether every isa.SubOpTailCall in callee
// targets a different function.
//
// Takes callee (*CompiledFunction) which is the function whose tail calls are being
// classified.
//
// Returns true when every isa.SubOpTailCall is unambiguously cross-target, false
// otherwise.
func calleeTailCallsAreAllCrossTarget(callee *program.CompiledFunction) bool {
	for i := range callee.Body {
		instr := callee.Body[i]
		if !isa.InstrIsTier1SubOp(instr, isa.SubOpTailCall) {
			continue
		}
		siteIndex := instr.WideIndex()
		if int(siteIndex) >= len(callee.CallSites) {
			return false
		}
		site := &callee.CallSites[siteIndex]
		if site.CachedCallee == nil {
			return false
		}
		if site.CachedCallee == callee {
			return false
		}
	}
	return true
}

// calleeHairyness computes a weighted cost score for inlining a callee.
//
// Takes callee (*CompiledFunction) whose body is summed.
//
// Returns the total hairyness score, or 0 when callee is nil.
func calleeHairyness(callee *program.CompiledFunction) int {
	if callee == nil {
		return 0
	}
	score := 0
	for i := range callee.Body {
		score += hairyOpcodeCost(callee.Body[i].Op)
	}
	return score
}

// hairyOpcodeCost returns the per-opcode contribution to the score.
//
// Takes op (opcode) which is the opcode being weighed.
//
// Returns 0 for isa.OpNop, 2 for allocation-driven ops, or 1 for all others.
func hairyOpcodeCost(op isa.Opcode) int {
	switch op {
	case isa.OpMakeSlice, isa.OpMakeClosure,
		isa.OpAppend, isa.OpAppendSpread,
		isa.OpMapSet:
		return 2
	case isa.OpNop:
		return 0
	default:
	}
	return 1
}

// countParamsInBank counts callee parameters that live in the given bank.
//
// Takes callee (*CompiledFunction) whose parameterKinds are scanned.
// Takes bank (isa.RegisterKind) which is the bank being counted.
//
// Returns the number of parameters whose isa.RegisterKind matches bank.
func countParamsInBank(callee *program.CompiledFunction, bank isa.RegisterKind) int {
	n := 0
	for _, k := range callee.ParameterKinds {
		if k == bank {
			n++
		}
	}
	return n
}

// findOpCallPC locates the isa.SubOpCall instruction for a given call-site index.
//
// For the common isa.SubOpCall case the site index lives in instruction.b |
// (instruction.c << 8). Matching is restricted to isa.SubOpCall because other call
// variants encode the site index differently.
//
// Takes caller (*CompiledFunction) whose body is searched.
// Takes siteIndex (uint16) which is the call-site index to find.
//
// Returns the PC of the matching isa.SubOpCall, or -1 when none is found.
func findOpCallPC(caller *program.CompiledFunction, siteIndex uint16) int {
	for i := range caller.Body {
		instr := caller.Body[i]
		if !isa.InstrIsTier1SubOp(instr, isa.SubOpCall) {
			continue
		}
		index := uint16(instr.B) | uint16(instr.C)<<program.InstructionByteShift
		if index == siteIndex {
			return i
		}
	}
	return -1
}

// computeInLoopMask flags every PC that sits inside a loop body.
//
// A PC is "in loop" when some backward jump in the body has a source PC after it and a
// target PC at-or-before it. Computed in O(N) by walking the body, recording every
// backward jump as a (target_pc, source_pc) interval, and marking every PC in any
// interval. Conservative: nested loops mark the same PC multiple times (harmless).
// Forward-jump constructs (if/else, switch) do not mark anything as in-loop, which is
// correct.
//
// Takes caller (*CompiledFunction) whose body is scanned.
//
// Returns a slice indexed by PC, true where the PC is inside a loop.
func computeInLoopMask(caller *program.CompiledFunction) []bool {
	n := len(caller.Body)
	if n == 0 {
		return nil
	}
	mask := make([]bool, n)
	for i := range n {
		targetPC, isJump := program.JumpTargetAt(caller.Body, i)
		if !isJump || targetPC < 0 || targetPC > i {
			continue
		}
		for pc := targetPC; pc <= i && pc < n; pc++ {
			mask[pc] = true
		}
	}
	return mask
}

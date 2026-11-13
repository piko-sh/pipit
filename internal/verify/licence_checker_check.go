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

package verify

import (
	"context"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
)

// checkerTop is the abstract value of a register that may hold anything.
const checkerTop = -1

// checkerCancelCheckSteps is how many fixpoint steps the checker takes between checks of
// its context.
const checkerCancelCheckSteps = 4096

const (
	// checkerStepsPerCell is the fixpoint's step allowance per instruction and register.
	checkerStepsPerCell = 64

	// checkerStepsRegisterSlack is added to the register count in the step allowance, so a
	// function with few registers still has room.
	checkerStepsRegisterSlack = 8

	// checkerStepsFloor is the step allowance every function has regardless of its size.
	checkerStepsFloor = 1024
)

// inferredGrants holds grant inference results for the checker to reuse.
type inferredGrants struct {
	// conversion is the function's conversion under the grants the inference recorded, or
	// nil when the check must convert the function itself.
	conversion *CheckerProgram

	// live is the liveness of the inference's conversion, with nil sets when no certificate
	// was proposed.
	live checkerLiveness
}

// checkerState holds abstract register values and marked allocation sites whose objects
// may have been handed off.
//
// Each register holds a site or checkerTop.
type checkerState struct {
	// registers maps each register to its allocation site or checkerTop.
	registers []int

	// escaped lists allocation sites whose objects may have been handed off.
	escaped []int
}

// CheckerVerdict is the checker's answer for one function, naming the condition that
// failed when it rejects.
type CheckerVerdict int

const (
	// CheckerAccepted means every condition holds.
	CheckerAccepted CheckerVerdict = iota

	// CheckerRejectedGrant means a granted store's receiver is not a fresh, unmarked
	// allocation. The escape pass's grants are wrong; without them the program is checked
	// trivially.
	CheckerRejectedGrant

	// CheckerRejectedInstruction means an instruction is unclassified (an operation that may
	// hand off a reference whose hand-offs the ISA spec does not declare) or ill-formed.
	// This is a defect of the engine or the ISA spec, not of the program, and removing
	// grants does not cure it.
	CheckerRejectedInstruction

	// checkerRejectedCertificate means the proposed certificate does not hold at the entry
	// or is not closed under the transfer function (the fixpoint stopped at its step limit).
	checkerRejectedCertificate

	// checkerRejectedEmpty means the function has no code.
	checkerRejectedEmpty
)

// isStore reports whether instr stores a value into an object.
//
// Returns bool which is true for CheckerStore and checkerStoreValue.
func (instr CheckerInstr) isStore() bool {
	return instr.Kind == CheckerStore || instr.Kind == checkerStoreValue
}

// clone returns a deep copy.
//
// Returns checkerState which has independent copies of the register and escape lists.
func (a checkerState) clone() checkerState {
	return checkerState{registers: slices.Clone(a.registers), escaped: slices.Clone(a.escaped)}
}

// marked reports whether site s is marked.
//
// Takes s (int) which is the allocation site to test.
//
// Returns bool which is true when the allocation site is in the marked-site list.
func (a checkerState) marked(s int) bool { return slices.Contains(a.escaped, s) }

// le reports whether a is at most other in the abstract state order.
//
// Registers holding a site in other must hold the same site in a. Registers holding
// checkerTop in other may hold checkerTop or a site marked by other in a. Every site
// marked by a must also be marked by other.
//
// Takes other (checkerState) which is the abstract state used as the upper bound.
//
// Returns bool which is true when the receiver is at most other in the abstract state
// order.
func (a checkerState) le(other checkerState) bool {
	if len(a.registers) != len(other.registers) {
		return false
	}
	for r, x := range a.registers {
		if !other.covers(other.registers[r], x) {
			return false
		}
	}
	for _, s := range a.escaped {
		if !other.marked(s) {
			return false
		}
	}
	return true
}

// covers reports whether a register holding bound in the receiver bounds the same
// register holding lower in a lower state: equal sites, checkerTop over checkerTop, or
// checkerTop over a site the receiver marks.
//
// Takes bound (int) which is the register's value in the receiver.
// Takes lower (int) which is the register's value in the lower state.
//
// Returns bool which is true when bound bounds lower.
func (a checkerState) covers(bound, lower int) bool {
	switch {
	case lower == checkerTop:
		return bound == checkerTop
	case bound == checkerTop:
		return a.marked(lower)
	default:
		return lower == bound
	}
}

// get returns register r's value; a register out of range reads checkerTop.
//
// Takes r (int) which is the register index to read.
//
// Returns int which is the register allocation site, or checkerTop when the index is out
// of range.
func (a checkerState) get(r int) int {
	if r < 0 || r >= len(a.registers) {
		return checkerTop
	}
	return a.registers[r]
}

// set sets register r to v; a register out of range is left as it is.
//
// Takes r (int) which is the register index to update.
// Takes v (int) which is the allocation site or checkerTop to store.
func (a *checkerState) set(r, v int) {
	if r >= 0 && r < len(a.registers) {
		a.registers[r] = v
	}
}

// markFresh appends to sites the site register r holds, if any.
//
// Takes sites ([]int) which is the allocation-site list to extend.
// Takes r (int) which is the register whose allocation site is appended.
//
// Returns []int which contains the prior sites and the register allocation site, if
// known.
func (a *checkerState) markFresh(sites []int, r int) []int {
	if v := a.get(r); v != checkerTop {
		return append(sites, v)
	}
	return sites
}

// checkerTransfer computes the abstract register and escape state after an instruction.
//
// Takes programCounter (int) which is the instruction's allocation-site identifier.
// Takes instr (CheckerInstr) which is the instruction to interpret.
// Takes in (checkerState) which is the abstract state before the instruction.
//
// Returns checkerState which is the abstract state after the instruction.
func checkerTransfer(programCounter int, instr CheckerInstr, in checkerState) checkerState {
	state := in.clone()
	switch instr.Kind {
	case CheckerAlloc:
		state.set(instr.Destination, programCounter)
	case CheckerMove:
		state.set(instr.Destination, in.get(instr.Source))
	case CheckerStore:
		state.escaped = append(state.markFresh(nil, instr.Source), state.escaped...)
	case CheckerOp, CheckerRead:
		var marks []int
		for _, r := range instr.Sources {
			marks = in.markFresh(marks, r)
		}
		marks = append(marks, state.escaped...)
		state.escaped = marks
		for _, d := range instr.Destinations {
			state.set(d, checkerTop)
		}
	case checkerHavoc:
		var marks []int
		for _, r := range instr.Sources {
			marks = in.markFresh(marks, r)
		}
		marks = append(marks, in.escaped...)
		escaped := marks
		var fresh []int
		for _, v := range in.registers {
			if v != checkerTop {
				fresh = append(fresh, v)
			}
		}
		fresh = append(fresh, escaped...)
		state.escaped = fresh
		for r := range state.registers {
			state.registers[r] = checkerTop
		}
	case checkerNop, checkerUnclassified, checkerStoreValue:
	}
	return state
}

// checkerInstrOK checks that instr is classified and every register it names is in range.
//
// Takes registerCount (int) which is the number of general registers.
// Takes instr (CheckerInstr) which is the instruction to validate.
//
// Returns bool which is true when the instruction is classified and all register operands
// are in range.
func checkerInstrOK(registerCount int, instr CheckerInstr) bool {
	ok := func(r int) bool { return r >= 0 && r < registerCount }
	all := func(rs []int) bool {
		for _, r := range rs {
			if !ok(r) {
				return false
			}
		}
		return true
	}
	switch instr.Kind {
	case CheckerAlloc:
		return ok(instr.Destination)
	case CheckerMove:
		return ok(instr.Destination) && ok(instr.Source)
	case CheckerStore:
		return ok(instr.Receiver) && ok(instr.Source)
	case checkerStoreValue:
		return ok(instr.Receiver)
	case CheckerOp:
		return all(instr.Sources) && all(instr.Destinations)
	case CheckerRead:
		return ok(instr.Receiver) && all(instr.Sources) && all(instr.Destinations)
	case checkerHavoc:
		return all(instr.Sources)
	case checkerNop:
		return true
	case checkerUnclassified:
		return false
	}
	return false
}

// storeGranted reports whether instr's grant holds in state.
//
// The receiver must hold an allocation site that no path has marked, and the site must be
// of the sort the grant is for.
//
// Takes instr (CheckerInstr) which is the instruction whose store grant is checked.
// Takes state (checkerState) which is the abstract state before the instruction.
//
// Returns bool which is true when the store grant is justified, or the instruction needs
// no grant.
func (p *CheckerProgram) storeGranted(instr CheckerInstr, state checkerState) bool {
	if !instr.isStore() || !instr.Granted {
		return true
	}
	site := state.get(instr.Receiver)
	if site == checkerTop || state.marked(site) {
		return false
	}
	return p.containerSite(site) == instr.Container
}

// containerSite reports whether an allocation site is a map allocation.
//
// Takes site (int) which is the allocating instruction's pc.
//
// Returns bool which is true for the allocation of a map.
func (p *CheckerProgram) containerSite(site int) bool {
	return site >= 0 && site < len(p.Code) && p.Code[site].Kind == CheckerAlloc && p.Code[site].Container
}

// checkerJoin merges left and right where control flow meets.
//
// Takes left (checkerState) which is the first incoming abstract state.
// Takes right (checkerState) which is the second incoming abstract state.
//
// Returns checkerState which conservatively combines both incoming states.
func checkerJoin(left, right checkerState) checkerState {
	out := checkerState{registers: make([]int, min(len(left.registers), len(right.registers))), escaped: nil}
	var marks []int
	for register := range out.registers {
		leftSite, rightSite := left.registers[register], right.registers[register]
		switch {
		case leftSite != checkerTop && rightSite != checkerTop && leftSite == rightSite:
			out.registers[register] = leftSite
		default:
			out.registers[register] = checkerTop
			if leftSite != checkerTop {
				marks = append(marks, leftSite)
			}
			if rightSite != checkerTop {
				marks = append(marks, rightSite)
			}
		}
	}
	for _, s := range append(append(marks, left.escaped...), right.escaped...) {
		if !slices.Contains(out.escaped, s) {
			out.escaped = append(out.escaped, s)
		}
	}
	return out
}

// CheckLicence runs the licence checker over one function, after replacing its
// local-container grant tables.
//
// The tables are inferred when inferGrants is true and cleared otherwise.
//
// Takes function (*program.CompiledFunction) whose tables are replaced.
// Takes inferGrants (bool) which says whether map stores may be granted.
//
// Returns CheckerVerdict which is the verdict.
// Returns error which is the context's cause when it was cancelled first. The function
// then has no local-container grants, and the verdict is checkerRejectedCertificate.
func CheckLicence(ctx context.Context, function *program.CompiledFunction, inferGrants bool) (CheckerVerdict, error) {
	function.LocalContainerAllocPCs, function.LocalContainerStorePCs = nil, nil
	var inferred inferredGrants
	if inferGrants {
		var err error
		if inferred, err = inferLocalContainerGrants(ctx, function); err != nil {
			return checkerRejectedCertificate, err
		}
	}
	converted := inferred.conversion
	if converted == nil {
		converted = ConvertForLicenceChecker(function)
	}
	return converted.checkLicenceGrantsVerdict(ctx, inferred.live)
}

// inferLocalContainerGrants records map stores whose maps only the running VM can reach.
//
// Takes function (*program.CompiledFunction) whose tables are replaced.
//
// Returns inferredGrants which is what the check can reuse.
// Returns error which is the context's cause when it was cancelled first. The function
// then has no local-container grants.
func inferLocalContainerGrants(ctx context.Context, function *program.CompiledFunction) (inferredGrants, error) {
	function.LocalContainerAllocPCs, function.LocalContainerStorePCs = nil, nil
	none := inferredGrants{conversion: nil, live: checkerLiveness{}}
	if len(function.Body) == 0 {
		return none, nil
	}
	candidate := convertForLicenceChecker(function, checkerGrants{allocs: nil, stores: nil, every: true})
	hasStore, hasAlloc := candidate.hasContainerStore(), candidate.hasContainerAlloc()
	if !hasStore && !hasAlloc {
		return inferredGrants{conversion: candidate, live: checkerLiveness{}}, nil
	}
	if !hasStore || !hasAlloc {
		return none, nil
	}
	entry := checkerState{registers: slices.Repeat([]int{checkerTop}, candidate.Registers), escaped: nil}
	live, err := candidate.liveRegisters(ctx)
	if err != nil {
		return none, err
	}
	certificate, err := candidate.proposeCertificate(ctx, entry, live)
	if err != nil {
		return none, err
	}
	var allocs, stores program.PCSet
	for programCounter, state := range certificate {
		instr := candidate.Code[programCounter]
		if state == nil || !instr.isStore() || !instr.Container || !candidate.storeGranted(instr, *state) {
			continue
		}
		stores = stores.With(programCounter)
		allocs = allocs.With(state.get(instr.Receiver))
	}
	if stores != nil {
		function.LocalContainerAllocPCs, function.LocalContainerStorePCs = allocs, stores
	}
	return inferredGrants{conversion: nil, live: live}, nil
}

// hasContainerAlloc reports whether the program has a map allocation.
//
// Returns bool which is true when it has one.
func (p *CheckerProgram) hasContainerAlloc() bool {
	for index := range p.Code {
		if p.Code[index].Kind == CheckerAlloc && p.Code[index].Container {
			return true
		}
	}
	return false
}

// hasContainerStore reports whether the program has a granted container store.
//
// Returns bool which is true when it has one.
func (p *CheckerProgram) hasContainerStore() bool {
	for index := range p.Code {
		if p.Code[index].isStore() && p.Code[index].Container {
			return true
		}
	}
	return false
}

// String names the verdict.
//
// Returns string which is the name.
func (v CheckerVerdict) String() string {
	switch v {
	case CheckerAccepted:
		return "accepted"
	case CheckerRejectedGrant:
		return "rejected-grant"
	case CheckerRejectedInstruction:
		return "rejected-instruction"
	case checkerRejectedCertificate:
		return "rejected-certificate"
	case checkerRejectedEmpty:
		return "rejected-empty"
	}
	return "unknown"
}

// checkLicenceGrants runs the checker on one converted function.
//
// Returns bool which is true when the function is accepted.
// Returns error which is the context's cause when it was cancelled before the check
// finished.
func (p *CheckerProgram) checkLicenceGrants(ctx context.Context) (bool, error) {
	verdict, err := p.CheckLicenceGrantsVerdict(ctx)
	return verdict == CheckerAccepted && err == nil, err
}

// CheckLicenceGrantsVerdict is checkLicenceGrants naming the condition that failed. When
// several fail, an ill-formed or unclassified instruction is reported first, then a wrong
// grant, then the certificate.
//
// Returns CheckerVerdict which is the verdict.
// Returns error which is the context's cause when it was cancelled before the check
// finished; the verdict is then checkerRejectedCertificate.
func (p *CheckerProgram) CheckLicenceGrantsVerdict(ctx context.Context) (CheckerVerdict, error) {
	return p.checkLicenceGrantsVerdict(ctx, checkerLiveness{})
}

// checkLicenceGrantsVerdict is CheckLicenceGrantsVerdict reusing liveness sets computed
// for an equivalent conversion of the same function.
//
// Takes shared (checkerLiveness) which is reused when its sets are closed for p. Nil
// sets, or sets that are not closed, are replaced by p's own, computed once for the
// proposal and the check.
//
// Returns CheckerVerdict which is the verdict.
// Returns error which is the context's cause when it was cancelled first.
func (p *CheckerProgram) checkLicenceGrantsVerdict(ctx context.Context, shared checkerLiveness) (CheckerVerdict, error) {
	if len(p.Code) == 0 {
		return checkerRejectedEmpty, nil
	}
	live := shared
	if live.sets == nil || !live.closed(p) {
		computed, err := p.liveRegisters(ctx)
		if err != nil {
			return checkerRejectedCertificate, err
		}
		live = computed
	}
	entry := checkerState{registers: slices.Repeat([]int{checkerTop}, p.Registers), escaped: nil}
	certificate, err := p.proposeCertificate(ctx, entry, live)
	if err != nil {
		return checkerRejectedCertificate, err
	}
	return p.checkCertificate(entry, certificate, live), nil
}

// proposeCertificate computes a bounded fixpoint from the entry state.
//
// Takes entry (checkerState) which is the state on entry to the function.
// Takes live (checkerLiveness) which says which registers each instruction may read. The
// proposal trusts it. The check does not.
//
// Returns []*checkerState which holds each instruction's proposed state, nil where none
// was reached.
// Returns error which is the context's cause when it was cancelled first.
func (p *CheckerProgram) proposeCertificate(ctx context.Context, entry checkerState, live checkerLiveness) ([]*checkerState, error) {
	length := len(p.Code)
	certificate := make([]*checkerState, length)
	certificate[0] = &checkerState{registers: slices.Clone(entry.registers), escaped: nil}
	work := []int{0}
	queued := make([]bool, length)
	queued[0] = true
	limit := checkerStepsPerCell*length*(p.Registers+checkerStepsRegisterSlack) + checkerStepsFloor
	for steps := 0; len(work) > 0 && steps < limit; steps++ {
		if steps%checkerCancelCheckSteps == 0 {
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
		}
		programCounter := work[0]
		work = work[1:]
		queued[programCounter] = false
		out := checkerTransfer(programCounter, p.Code[programCounter], *certificate[programCounter])
		for _, successor := range p.Successors[programCounter] {
			if widenCertificate(certificate, successor, live.forget(successor, out)) && !queued[successor] {
				queued[successor] = true
				work = append(work, successor)
			}
		}
	}
	return certificate, nil
}

// widenCertificate joins a state flowing into a successor with the successor's proposed
// state.
//
// Takes certificate ([]*checkerState) which holds the proposed states.
// Takes successor (int) which is the successor's pc; one out of range is ignored.
// Takes out (checkerState) which is the state flowing in.
//
// Returns bool which is true when the successor's state changed and must be revisited.
func widenCertificate(certificate []*checkerState, successor int, out checkerState) bool {
	if successor < 0 || successor >= len(certificate) {
		return false
	}
	next := out
	if current := certificate[successor]; current != nil {
		next = checkerJoin(*current, out)
		if next.le(*current) && current.le(next) {
			return false
		}
	}
	certificate[successor] = new(next.clone())
	return true
}

// checkCertificate checks the checker's conditions on a proposed certificate.
//
// Takes entry (checkerState) which is the state on entry to the function.
// Takes certificate ([]*checkerState) which holds each instruction's proposed state.
// Takes live (checkerLiveness) which says which registers each instruction may read. The
// check rejects sets that are not closed for p, so the proposal's sets are not trusted.
//
// Returns CheckerVerdict which is the verdict.
func (p *CheckerProgram) checkCertificate(entry checkerState, certificate []*checkerState, live checkerLiveness) CheckerVerdict {
	if certificate[0] == nil || !entry.le(*certificate[0]) {
		return checkerRejectedCertificate
	}
	if !p.reachedInstructionsWellFormed(certificate) {
		return CheckerRejectedInstruction
	}
	if !live.closed(p) {
		return checkerRejectedCertificate
	}
	verdict := CheckerAccepted
	for programCounter, state := range certificate {
		if state == nil {
			continue
		}
		if len(state.registers) != p.Registers {
			return checkerRejectedCertificate
		}
		if !p.storeGranted(p.Code[programCounter], *state) {
			verdict = CheckerRejectedGrant
			continue
		}
		if covered := p.successorsCovered(certificate, live, programCounter, *state); !covered && verdict == CheckerAccepted {
			verdict = checkerRejectedCertificate
		}
	}
	return verdict
}

// reachedInstructionsWellFormed reports whether every instruction the certificate reaches
// is well formed and classified.
//
// Takes certificate ([]*checkerState) which holds the proposed states.
//
// Returns bool which is true when they all are.
func (p *CheckerProgram) reachedInstructionsWellFormed(certificate []*checkerState) bool {
	for programCounter, state := range certificate {
		if state != nil && !checkerInstrOK(p.Registers, p.Code[programCounter]) {
			return false
		}
	}
	return true
}

// successorsCovered reports whether the state leaving an instruction is at most every
// successor's proposed state.
//
// Takes certificate ([]*checkerState) which holds the proposed states.
// Takes live (checkerLiveness) which says which registers are live at each instruction.
// Takes programCounter (int) which is the instruction's pc.
// Takes state (checkerState) which is its proposed state.
//
// Returns bool which is true when every successor is in range, reached and covers it.
func (p *CheckerProgram) successorsCovered(certificate []*checkerState, live checkerLiveness, programCounter int, state checkerState) bool {
	out := checkerTransfer(programCounter, p.Code[programCounter], state)
	covered := true
	for _, successor := range p.Successors[programCounter] {
		if successor < 0 || successor >= len(certificate) || certificate[successor] == nil ||
			!live.forget(successor, out).le(*certificate[successor]) {
			covered = false
		}
	}
	return covered
}

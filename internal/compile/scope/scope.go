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

package scope

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"pipit.sh/pipit/internal/isa"
)

const (
	// SyntheticNamePrefix marks compiler-synthesised locals (for example the hidden named
	// results a defer-containing function declares for its unnamed results). Such names can
	// never collide with Go identifiers and are kept out of the debug variable table.
	SyntheticNamePrefix = "~"

	// maxRegisterIndex is the maximum index a register can have, constrained by the uint8
	// encoding in the instruction format.
	maxRegisterIndex = 255

	// spillThreshold is the register index at which new variable declarations are spilled
	// instead of receiving a real register.
	spillThreshold uint32 = 246

	// AddressableRegisters is how many registers of one bank an instruction can name.
	AddressableRegisters = maxRegisterIndex + 1

	// registerSetWordBits is how many registers one word of a RegisterSet holds.
	registerSetWordBits = 64

	// registerSetWords is how many words a RegisterSet holds per bank.
	registerSetWords = AddressableRegisters / registerSetWordBits

	// maxSpillSlotsPerBank caps the per-bank spill area at the uint16 limit. The runtime
	// spillSlot field is uint16 and the physical register file index is 256 + spillSlot, so
	// allocating beyond this would silently wrap the uint16 cast and alias two distinct
	// logical slots onto the same runtime register, corrupting state.
	maxSpillSlotsPerBank = 1 << 16
)

// forcedSpillLimit lowers the spill threshold of every spillable bank for testing
// (PIPIT_FORCE_SPILL_LIMIT=N), so the whole corpus runs with most locals in the spill
// area; zero leaves the threshold alone.
var forcedSpillLimit = parseForcedSpillLimit(os.Getenv("PIPIT_FORCE_SPILL_LIMIT"))

// LoweredLimit is a spill threshold for one bank below the default. The zero value keeps
// the default, so a limit of register 0, which spills every local, can still be set.
type LoweredLimit struct {
	// Register is the register index from which new locals spill.
	Register uint32

	// Active reports whether the limit applies.
	Active bool
}

// RegisterAllocator manages register allocation within a function, allocating
// sequentially per bank and freeing when Scopes exit. Counters are uint32 because a full
// bank touches index 256 and uint8 would wrap silently.
type RegisterAllocator struct {
	// OverflowErr is set when a register bank exceeds the 256-register limit imposed by the
	// uint8 instruction encoding. Checked by the Compiler after function compilation
	// completes.
	OverflowErr error

	// FunctionName identifies the function being compiled, used in diagnostic messages when
	// register overflow is detected.
	FunctionName string

	// recycledRegisters holds register indices returned by dead-variable analysis, consumed
	// LIFO by allocOrRecycle. Only entries at or above the current scope's base index may be
	// consumed, so inner scopes cannot steal outer-scope recycled registers.
	recycledRegisters [isa.NumRegisterKinds][]uint32

	// nextRegister tracks the next free register per bank.
	nextRegister [isa.NumRegisterKinds]uint32

	// peakRegister tracks the highest register ever allocated per bank. This determines the
	// register file size for the function.
	peakRegister [isa.NumRegisterKinds]uint32

	// nextSpillSlot tracks the next free spill slot per bank. Spill slots are indexed from
	// 0; the actual register file index is 256 + spillSlot.
	nextSpillSlot [isa.NumRegisterKinds]uint32

	// peakSpillSlot tracks the highest spill slot ever allocated per bank. Used together
	// with peakRegister to size the register file: max(peakRegister, 256 + peakSpillSlot).
	peakSpillSlot [isa.NumRegisterKinds]uint32

	// capturedFloor is, per bank, one past the highest register a closure has captured in
	// the function being compiled.
	capturedFloor [isa.NumRegisterKinds]uint32

	// refusedSpill records that a typed-slice bank needed a spill slot it cannot have, so
	// compiling the body again with a lower spill threshold cannot help.
	refusedSpill bool

	// spillLimits lowers the spill threshold of a spillable bank where active; set for a
	// body compiled again after it ran out of registers.
	spillLimits [isa.NumRegisterKinds]LoweredLimit

	// statementPeak is, per bank, the highest nextRegister reached since the innermost open
	// statement began.
	statementPeak [isa.NumRegisterKinds]uint32
}

// Alloc allocates a register in the given bank and returns its index.
//
// Takes kind (isa.RegisterKind) which specifies the register bank to allocate from.
//
// Returns the index of the newly allocated register.
func (r *RegisterAllocator) Alloc(kind isa.RegisterKind) uint8 {
	register := r.nextRegister[kind]
	r.nextRegister[kind]++
	r.notePeak(kind)
	if register > maxRegisterIndex && r.OverflowErr == nil {
		r.OverflowErr = fmt.Errorf(
			"%w in function %q: %s bank requires more than %d registers "+
				"(instruction encoding is limited to uint8 indices); "+
				"peak registers: int=%d float=%d string=%d general=%d bool=%d uint=%d complex=%d; "+
				"recycled pool lengths: int=%d float=%d string=%d general=%d bool=%d uint=%d complex=%d",
			fault.ErrRegisterOverflow, r.FunctionName, kind, maxRegisterIndex+1,
			r.peakRegister[isa.RegisterInt64], r.peakRegister[isa.RegisterFloat64], r.peakRegister[isa.RegisterString],
			r.peakRegister[isa.RegisterGeneral], r.peakRegister[isa.RegisterBool], r.peakRegister[isa.RegisterUint64],
			r.peakRegister[isa.RegisterComplex128],
			len(r.recycledRegisters[isa.RegisterInt64]), len(r.recycledRegisters[isa.RegisterFloat64]),
			len(r.recycledRegisters[isa.RegisterString]), len(r.recycledRegisters[isa.RegisterGeneral]),
			len(r.recycledRegisters[isa.RegisterBool]), len(r.recycledRegisters[isa.RegisterUint64]),
			len(r.recycledRegisters[isa.RegisterComplex128]),
		)
	}
	return uint8(register) //nolint:gosec // OverflowErr fails the compile
}

// AllocTemp allocates a temporary register. The caller must call FreeTemp when the
// temporary is no longer needed.
//
// Takes kind (isa.RegisterKind) which specifies the register bank to allocate from.
//
// Returns the index of the newly allocated temporary register.
func (r *RegisterAllocator) AllocTemp(kind isa.RegisterKind) uint8 {
	return r.Alloc(kind)
}

// FreeTemp releases a temporary register. Temporaries must be freed in reverse allocation
// order (LIFO).
//
// Takes kind (isa.RegisterKind) which specifies the register bank to free from.
func (r *RegisterAllocator) FreeTemp(kind isa.RegisterKind, _ uint8) {
	r.nextRegister[kind]--
}

// RecycleRegister returns a register to the recycled pool for reuse by future
// allocOrRecycle calls. Called when dead-variable analysis determines a declared variable
// will never be referenced again.
//
// Takes kind (isa.RegisterKind) which specifies the register bank.
// Takes register (uint8) which is the register index to recycle.
func (r *RegisterAllocator) RecycleRegister(kind isa.RegisterKind, register uint8) {
	r.recycledRegisters[kind] = append(r.recycledRegisters[kind], uint32(register))
}

// EnsureMin guarantees that the peak register count for the given bank is at least count.
//
// Used by return compilation to ensure the frame has registers for cross-bank
// return-position writes.
//
// Takes kind (isa.RegisterKind) which specifies the register bank to check.
// Takes count (uint32) which is the minimum peak count to enforce.
func (r *RegisterAllocator) EnsureMin(kind isa.RegisterKind, count uint32) {
	if count > r.peakRegister[kind] {
		r.peakRegister[kind] = count
	}
}

// ReserveLow guarantees that the next fresh allocation in kind's bank returns a register
// at or above count.
//
// Registers already handed out below count are unaffected.
//
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes count (uint32) which is the number of low registers to keep clear.
func (r *RegisterAllocator) ReserveLow(kind isa.RegisterKind, count uint32) {
	if count > r.nextRegister[kind] {
		r.nextRegister[kind] = count
	}
	if count > r.peakRegister[kind] {
		r.peakRegister[kind] = count
	}
	r.notePeak(kind)
}

// SetSpillLimits sets active spill thresholds for new locals. Typed-slice banks cannot
// spill and retain their defaults.
//
// Takes limits ([isa.NumRegisterKinds]LoweredLimit) which holds a limit per bank.
func (r *RegisterAllocator) SetSpillLimits(limits [isa.NumRegisterKinds]LoweredLimit) {
	r.spillLimits = limits
}

// SpillLimit returns the register index from which new locals of kind's bank are spilled.
//
// Takes kind (isa.RegisterKind) which is the bank.
//
// Returns uint32 which is the effective limit.
func (r *RegisterAllocator) SpillLimit(kind isa.RegisterKind) uint32 {
	limit := spillThreshold
	if isa.IsTypedSliceKind(kind) {
		return limit
	}
	if lowered := r.spillLimits[kind]; lowered.Active {
		limit = min(limit, lowered.Register)
	}
	if forcedSpillLimit != 0 {
		limit = min(limit, forcedSpillLimit)
	}
	return limit
}

// Snapshot returns the current allocation state for all banks.
//
// Used when entering a new scope so registers can be restored on exit.
//
// Returns a copy of the current register allocation counters.
func (r *RegisterAllocator) Snapshot() [isa.NumRegisterKinds]uint32 {
	return r.nextRegister
}

// Restore resets the allocation state to a previously saved Snapshot.
//
// Used when exiting a scope to free all registers allocated within it, except that
// registers below capturedFloor stay allocated.
//
// Takes saved ([NumRegisterKinds]uint32) which is the Snapshot to restore from.
//
// Panics if any saved counter exceeds the current nextRegister value for that bank.
func (r *RegisterAllocator) Restore(saved [isa.NumRegisterKinds]uint32) {
	for i := range isa.NumRegisterKinds {
		if saved[i] > r.nextRegister[i] {
			panic(fmt.Sprintf(
				"registerAllocator.restore: saved[%s]=%d exceeds nextRegister=%d in function %q",
				isa.RegisterKind(i), saved[i], r.nextRegister[i], r.FunctionName,
			))
		}
		r.nextRegister[i] = max(saved[i], r.capturedFloor[i])
	}
}

// ReserveCaptured keeps a captured variable's register out of reuse for the rest of the
// function.
//
// Takes kind (isa.RegisterKind) which is the variable's bank.
// Takes register (uint8) which is the variable's register.
func (r *RegisterAllocator) ReserveCaptured(kind isa.RegisterKind, register uint8) {
	r.capturedFloor[kind] = max(r.capturedFloor[kind], uint32(register)+1)
}

// RefusedSpill reports whether a typed-slice bank needed a spill slot, which it cannot
// have; lowering the spill threshold cannot fix such an overflow.
//
// Returns bool which is true once a spill was refused.
func (r *RegisterAllocator) RefusedSpill() bool {
	return r.refusedSpill
}

// keepBelow raises the next free register of kind's bank to end, so the registers below
// it stay allocated.
//
// Takes kind (isa.RegisterKind) which is the bank.
// Takes end (uint32) which is one past the highest register to keep.
func (r *RegisterAllocator) keepBelow(kind isa.RegisterKind, end uint32) {
	if end > r.nextRegister[kind] {
		r.nextRegister[kind] = end
		r.notePeak(kind)
	}
}

// notePeak records nextRegister of kind's bank in the function's and the open statement's
// peaks.
//
// Takes kind (isa.RegisterKind) which is the bank whose allocation grew.
func (r *RegisterAllocator) notePeak(kind isa.RegisterKind) {
	next := r.nextRegister[kind]
	r.peakRegister[kind] = max(r.peakRegister[kind], next)
	r.statementPeak[kind] = max(r.statementPeak[kind], next)
}

// allocOrRecycle returns a recycled register if one is available above baseFreeIndex,
// otherwise allocates a fresh register sequentially.
//
// Takes kind (isa.RegisterKind) which selects the register bank.
// Takes baseFreeIndex (int) which is the free-list length when the current scope was
// entered, preventing inner scopes from consuming outer-scope recycled entries.
//
// Returns the index of the allocated or recycled register.
func (r *RegisterAllocator) allocOrRecycle(kind isa.RegisterKind, baseFreeIndex int) uint8 {
	if length := len(r.recycledRegisters[kind]); length > baseFreeIndex {
		register := r.recycledRegisters[kind][length-1]
		r.recycledRegisters[kind] = r.recycledRegisters[kind][:length-1]

		if register >= r.nextRegister[kind] {
			r.nextRegister[kind] = register + 1
			r.notePeak(kind)
		}
		return uint8(register) //nolint:gosec // recycled values are uint8
	}
	return r.Alloc(kind)
}

// allocSpill allocates a spill slot in the given bank. The returned value is a 0-based
// index and the runtime register file position is 256 plus that value.
//
// Records fault.ErrSpillUnsupportedBank on OverflowErr for typed-slice banks.
//
// Takes kind (isa.RegisterKind) which selects the register bank.
//
// Returns uint16 which is the spill slot index.
//
// Panics if the per-bank spill area is exhausted (fault.ErrSpillAreaExhausted).
func (r *RegisterAllocator) allocSpill(kind isa.RegisterKind) uint16 {
	if isa.IsTypedSliceKind(kind) {
		r.refusedSpill = true
		if r.OverflowErr == nil {
			r.OverflowErr = fmt.Errorf("%w: %w: bank %s in function %q holds more than %d live locals",
				fault.ErrRegisterOverflow, fault.ErrSpillUnsupportedBank, kind, r.FunctionName, spillThreshold)
		}
		return 0
	}
	if r.nextSpillSlot[kind] >= maxSpillSlotsPerBank {
		panic(fmt.Errorf("%w: bank %s in function %q exhausted at %d slots",
			fault.ErrSpillAreaExhausted, kind, r.FunctionName, maxSpillSlotsPerBank))
	}
	slot := r.nextSpillSlot[kind]
	r.nextSpillSlot[kind]++
	if r.nextSpillSlot[kind] > r.peakSpillSlot[kind] {
		r.peakSpillSlot[kind] = r.nextSpillSlot[kind]
	}
	return uint16(slot)
}

// spillSnapshot returns the current spill allocation state for all banks.
//
// Used when entering a new scope so spill slots can be restored on exit.
//
// Returns [NumRegisterKinds]uint32 which is a copy of the current spill slot allocation
// counters.
func (r *RegisterAllocator) spillSnapshot() [isa.NumRegisterKinds]uint32 {
	return r.nextSpillSlot
}

// spillRestore resets the spill allocation state to a previously saved Snapshot.
//
// Used when exiting a scope to free spill slots allocated within it.
//
// Takes saved ([NumRegisterKinds]uint32) which is the Snapshot to restore from.
func (r *RegisterAllocator) spillRestore(saved [isa.NumRegisterKinds]uint32) {
	r.nextSpillSlot = saved
}

// needsSpill reports whether the next sequential allocation in the given bank would
// exceed the spill threshold.
//
// When true, the caller should use allocSpill instead of alloc for variable declarations.
//
// Takes kind (isa.RegisterKind) which specifies the register bank to check.
//
// Returns bool which is true when the next allocation would exceed the spill threshold.
func (r *RegisterAllocator) needsSpill(kind isa.RegisterKind) bool {
	return r.nextRegister[kind] >= r.SpillLimit(kind)
}

// RegisterSet is a set of registers per bank.
type RegisterSet [isa.NumRegisterKinds][registerSetWords]uint64

// Contains reports whether register of kind's bank is in the set.
//
// Takes kind (isa.RegisterKind) which is the bank.
// Takes register (uint32) which is the register; one past the bank's end is never in it.
//
// Returns bool which is true when the register is in the set.
func (set *RegisterSet) Contains(kind isa.RegisterKind, register uint32) bool {
	if register >= AddressableRegisters {
		return false
	}
	return set[kind][register/registerSetWordBits]&(1<<(register%registerSetWordBits)) != 0
}

// add puts register of kind's bank in the set.
//
// Takes kind (isa.RegisterKind) which is the bank.
// Takes register (uint8) which is the register.
func (set *RegisterSet) add(kind isa.RegisterKind, register uint8) {
	set[kind][register/registerSetWordBits] |= 1 << (register % registerSetWordBits)
}

// union adds every register of other to the set.
//
// Takes other (*RegisterSet) which is the set to add.
func (set *RegisterSet) union(other *RegisterSet) {
	for kind := range set {
		for word := range set[kind] {
			set[kind][word] |= other[kind][word]
		}
	}
}

// StatementMark is the allocation state saved when a statement begins.
type StatementMark struct {
	// start is nextRegister per bank when the statement began.
	start [isa.NumRegisterKinds]uint32

	// outerPeak is the enclosing statement's peak, restored when this one ends.
	outerPeak [isa.NumRegisterKinds]uint32

	// outerReads is the enclosing statement's read set, restored when this one ends.
	outerReads RegisterSet
}

// StatementUsage is how one statement used the registers.
type StatementUsage struct {
	// Start is nextRegister per bank when the statement began.
	Start [isa.NumRegisterKinds]uint32

	// Peak is the highest nextRegister per bank while it ran.
	Peak [isa.NumRegisterKinds]uint32

	// Reads holds the registers of the variables it read.
	Reads RegisterSet
}

// ScopeStack tracks nested lexical Scopes during compilation.
type ScopeStack struct {
	// Alloc is the register allocator shared by all Scopes.
	Alloc *RegisterAllocator

	// DebugVarTable is the variable debug table being built. Nil when debug info is
	// disabled.
	DebugVarTable *program.DebugVarTable

	// DebugBodyLenFunc returns len(function.body) for recording the current PC in var
	// entries. Nil when debug is disabled.
	DebugBodyLenFunc func() int

	// unspillable names captured locals whose registers must remain valid along every
	// control-flow path.
	unspillable map[string]bool

	// Scopes is the stack of active lexical Scopes.
	Scopes []lexicalScope

	// statementReads holds the registers of the variables looked up since the innermost open
	// statement began.
	statementReads RegisterSet
}

// NewScopeStack creates a scope stack with a fresh register allocator.
//
// Takes functionName (string) which identifies the function being compiled, used in
// diagnostic messages.
//
// Returns a new scope stack ready for use.
func NewScopeStack(functionName string) *ScopeStack {
	return new(ScopeStack{
		Alloc: new(RegisterAllocator{
			FunctionName:      functionName,
			OverflowErr:       nil,
			recycledRegisters: [isa.NumRegisterKinds][]uint32{},
			nextRegister:      [isa.NumRegisterKinds]uint32{},
			peakRegister:      [isa.NumRegisterKinds]uint32{},
			nextSpillSlot:     [isa.NumRegisterKinds]uint32{},
			peakSpillSlot:     [isa.NumRegisterKinds]uint32{},
			capturedFloor:     [isa.NumRegisterKinds]uint32{},
			refusedSpill:      false,
			spillLimits:       [isa.NumRegisterKinds]LoweredLimit{},
			statementPeak:     [isa.NumRegisterKinds]uint32{},
		}),
		DebugVarTable:    nil,
		DebugBodyLenFunc: nil,
		unspillable:      nil,
		Scopes:           nil,
		statementReads:   RegisterSet{},
	})
}

// BeginStatement opens a measurement window for one statement; windows nest.
//
// Returns StatementMark which EndStatement takes.
func (s *ScopeStack) BeginStatement() StatementMark {
	mark := StatementMark{start: s.Alloc.nextRegister, outerPeak: s.Alloc.statementPeak, outerReads: s.statementReads}
	s.Alloc.statementPeak = s.Alloc.nextRegister
	s.statementReads = RegisterSet{}
	return mark
}

// EndStatement closes the window BeginStatement opened, folding its usage into the
// enclosing statement's.
//
// Takes mark (*StatementMark) which BeginStatement returned.
//
// Returns StatementUsage which is how the statement used the registers.
func (s *ScopeStack) EndStatement(mark *StatementMark) StatementUsage {
	usage := StatementUsage{Start: mark.start, Peak: s.Alloc.statementPeak, Reads: s.statementReads}
	for kind := range s.Alloc.statementPeak {
		s.Alloc.statementPeak[kind] = max(mark.outerPeak[kind], usage.Peak[kind])
	}
	s.statementReads = mark.outerReads
	s.statementReads.union(&usage.Reads)
	return usage
}

// SetUnspillableNames marks the locals that DeclareVar must keep in registers.
//
// Takes names (map[string]bool) which are the names closures in the body capture; nil
// clears the set.
func (s *ScopeStack) SetUnspillableNames(names map[string]bool) {
	s.unspillable = names
}

// PushScope enters a new lexical scope.
func (s *ScopeStack) PushScope() {
	var recycledLengths [isa.NumRegisterKinds]int
	for i := range isa.NumRegisterKinds {
		recycledLengths[i] = len(s.Alloc.recycledRegisters[i])
	}
	s.Scopes = append(s.Scopes, lexicalScope{
		Vars:                 make(map[string]program.VarLocation),
		savedRegisters:       s.Alloc.Snapshot(),
		savedSpillSlots:      s.Alloc.spillSnapshot(),
		varRegisterEnd:       [isa.NumRegisterKinds]uint32{},
		savedRecycledLengths: recycledLengths,
	})
}

// PopScope exits the current lexical scope, freeing all registers allocated within it and
// restoring the recycled-register free lists to their state when the scope was entered.
func (s *ScopeStack) PopScope() {
	if len(s.Scopes) == 0 {
		return
	}
	top := s.Scopes[len(s.Scopes)-1]

	if s.DebugVarTable != nil && s.DebugBodyLenFunc != nil {
		endPC := s.DebugBodyLenFunc()
		for name := range top.Vars {
			for i := range slices.Backward(s.DebugVarTable.Entries) {
				entry := &s.DebugVarTable.Entries[i]
				if entry.Name == name && entry.EndPC == 0 {
					entry.EndPC = endPC
					break
				}
			}
		}
	}

	s.Alloc.Restore(top.savedRegisters)
	s.Alloc.spillRestore(top.savedSpillSlots)
	for i := range isa.NumRegisterKinds {
		s.Alloc.recycledRegisters[i] = s.Alloc.recycledRegisters[i][:top.savedRecycledLengths[i]]
	}
	s.Scopes = s.Scopes[:len(s.Scopes)-1]
}

// DeclareVar declares a variable in the current scope and allocates a register for it.
// Prefers recycled registers from dead-variable analysis over fresh sequential
// allocation.
//
// Takes name (string) which is the variable name.
// Takes kind (isa.RegisterKind) which specifies the register bank to allocate from.
//
// Returns the register location for the new variable.
func (s *ScopeStack) DeclareVar(name string, kind isa.RegisterKind) program.VarLocation {
	if s.spillsNewLocal(name, kind) {
		return s.declare(name, program.VarLocation{Kind: kind, IsSpilled: true, SpillSlot: s.Alloc.allocSpill(kind)})
	}
	return s.declare(name, s.newRegisterLocation(kind))
}

// DeclareParameter allocates a register for a parameter, receiver or named result. It
// never spills because the call sequence only addresses registers.
//
// Takes name (string) which is the parameter name.
// Takes kind (isa.RegisterKind) which specifies the register bank to allocate from.
//
// Returns the register location for the new parameter.
func (s *ScopeStack) DeclareParameter(name string, kind isa.RegisterKind) program.VarLocation {
	return s.declare(name, s.newRegisterLocation(kind))
}

// LookupVar searches for a variable in the scope stack, starting from the innermost
// scope. A variable held in a register is recorded in the open statement's read set.
//
// Takes name (string) which is the variable name to look up.
//
// Returns the location and true if found.
func (s *ScopeStack) LookupVar(name string) (program.VarLocation, bool) {
	for scopeIndex := range slices.Backward(s.Scopes) {
		if location, ok := s.Scopes[scopeIndex].Vars[name]; ok {
			if !location.IsSpilled {
				s.statementReads.add(location.Kind, location.Register)
			}
			return location, true
		}
	}
	return program.VarLocation{}, false
}

// MarkCaptured marks a variable as captured by a closure, so that subsequent writes to it
// will Emit isa.SubOpSetSharedCell.
//
// Takes name (string) which is the variable name to mark.
func (s *ScopeStack) MarkCaptured(name string) {
	for scopeIndex := range slices.Backward(s.Scopes) {
		if loc, ok := s.Scopes[scopeIndex].Vars[name]; ok {
			loc.IsCaptured = true
			s.Scopes[scopeIndex].bind(name, loc)
			if !loc.IsSpilled {
				s.Alloc.ReserveCaptured(loc.Kind, loc.Register)
			}
			return
		}
	}
}

// UpdateVar updates a variable's location in the scope stack.
//
// Takes name (string) which is the variable name to update.
// Takes location (VarLocation) which is the new register location.
//
// Returns true if the variable was found and updated.
func (s *ScopeStack) UpdateVar(name string, location program.VarLocation) bool {
	for scopeIndex := range slices.Backward(s.Scopes) {
		if _, ok := s.Scopes[scopeIndex].Vars[name]; ok {
			s.Scopes[scopeIndex].bind(name, location)
			s.patchDebugVarLocation(name, location)
			return true
		}
	}
	return false
}

// RestoreWatermark restores register allocation to a saved Snapshot while preserving
// registers referenced by scope variables.
//
// This prevents watermark restores from freeing registers allocated by variable promotion
// (e.g. compileAddressOfIdent promoting an int-bank variable to a general-bank indirect
// pointer).
//
// Takes saved ([NumRegisterKinds]uint32) which is the Snapshot to restore from.
func (s *ScopeStack) RestoreWatermark(saved [isa.NumRegisterKinds]uint32) {
	s.Alloc.Restore(saved)
	for i := range s.Scopes {
		for kind, end := range s.Scopes[i].varRegisterEnd {
			s.Alloc.keepBelow(isa.RegisterKind(kind), end)
		}
	}
}

// KeepLocation keeps the register of location allocated after a watermark restore, for a
// value the compiler tracks outside the scope stack. A spilled location needs nothing.
//
// Takes location (program.VarLocation) which must stay allocated.
func (s *ScopeStack) KeepLocation(location program.VarLocation) {
	if location.IsSpilled {
		return
	}
	s.Alloc.keepBelow(location.Kind, uint32(location.Register)+1)
}

// BindVar binds name to location in the innermost scope, as a declaration does, for a
// variable whose register the compiler chose itself.
//
// Takes name (string) which is the variable name.
// Takes location (program.VarLocation) which is where it lives.
func (s *ScopeStack) BindVar(name string, location program.VarLocation) {
	if len(s.Scopes) > 0 {
		s.Scopes[len(s.Scopes)-1].bind(name, location)
	}
}

// PeakRegisters returns the peak register usage per bank, which determines the register
// file size for the compiled function. When spilling is active for a bank, the register
// file must be large enough to hold both the directly-addressable registers (0-255) and
// the spill area (256+).
//
// Returns the peak register count array indexed by register kind.
func (s *ScopeStack) PeakRegisters() [isa.NumRegisterKinds]uint32 {
	var result [isa.NumRegisterKinds]uint32
	for i := range isa.NumRegisterKinds {
		result[i] = s.Alloc.peakRegister[i]
		if s.Alloc.peakSpillSlot[i] > 0 {
			spillTotal := isa.SpillAreaOffset + s.Alloc.peakSpillSlot[i]
			if spillTotal > result[i] {
				result[i] = spillTotal
			}
		}
	}
	return result
}

// OverflowError returns the register overflow error if any bank exceeded the 256-register
// limit during compilation.
//
// Returns error which is the overflow error, or nil when all banks are within limits.
func (s *ScopeStack) OverflowError() error {
	return s.Alloc.OverflowErr
}

// spillsNewLocal reports whether a local must spill because its bank reached the
// threshold and no recycled register is available.
//
// Takes name (string) which is the variable name.
// Takes kind (isa.RegisterKind) which is the bank.
//
// Returns bool which is true when the local spills.
func (s *ScopeStack) spillsNewLocal(name string, kind isa.RegisterKind) bool {
	hasRecycled := len(s.Alloc.recycledRegisters[kind]) > s.recycledBase(kind)
	return !hasRecycled && !s.unspillable[name] && s.Alloc.needsSpill(kind)
}

// newRegisterLocation allocates a register for a new variable, preferring one that
// dead-variable analysis recycled in the current scope.
//
// Takes kind (isa.RegisterKind) which is the bank.
//
// Returns program.VarLocation which is the register.
func (s *ScopeStack) newRegisterLocation(kind isa.RegisterKind) program.VarLocation {
	return program.VarLocation{Register: s.Alloc.allocOrRecycle(kind, s.recycledBase(kind)), Kind: kind}
}

// recycledBase returns how many recycled registers of kind's bank belong to enclosing
// scopes, which the current scope must not reuse.
//
// Takes kind (isa.RegisterKind) which is the bank.
//
// Returns int which is the length of the free list when the current scope began.
func (s *ScopeStack) recycledBase(kind isa.RegisterKind) int {
	if len(s.Scopes) == 0 {
		return 0
	}
	return s.Scopes[len(s.Scopes)-1].savedRecycledLengths[kind]
}

// declare binds a new variable in the current scope and opens its debug entry.
//
// Takes name (string) which is the variable name.
// Takes location (program.VarLocation) which is where it lives.
//
// Returns program.VarLocation which is location.
func (s *ScopeStack) declare(name string, location program.VarLocation) program.VarLocation {
	s.BindVar(name, location)
	if s.DebugVarTable != nil && s.DebugBodyLenFunc != nil && name != "_" && !strings.HasPrefix(name, SyntheticNamePrefix) {
		s.DebugVarTable.Entries = append(s.DebugVarTable.Entries, program.DebugVarEntry{Name: name,
			Location: location,
			StartPC:  s.DebugBodyLenFunc(), EndPC: 0})
	}
	return location
}

// lexicalScope represents a lexical scope during compilation. Variables declared in this
// scope are freed when the scope exits.
type lexicalScope struct {
	// Vars maps variable names to their register locations.
	Vars map[string]program.VarLocation

	// savedRegisters records the register allocation state when this scope was entered,
	// allowing restoration on exit.
	savedRegisters [isa.NumRegisterKinds]uint32

	// savedSpillSlots records the spill slot allocation state when this scope was entered,
	// allowing restoration on exit.
	savedSpillSlots [isa.NumRegisterKinds]uint32

	// varRegisterEnd records one past each bank's highest register holding a variable in
	// this scope.
	varRegisterEnd [isa.NumRegisterKinds]uint32

	// savedRecycledLengths records the length of each recycled-register free list when this
	// scope was entered, so scope exit can truncate back without discarding outer-scope
	// entries.
	savedRecycledLengths [isa.NumRegisterKinds]int
}

// bind sets a variable's location and updates varRegisterEnd.
//
// Takes name (string) which is the variable name.
// Takes location (program.VarLocation) which is where it lives now.
func (scope *lexicalScope) bind(name string, location program.VarLocation) {
	previous, existed := scope.Vars[name]
	scope.Vars[name] = location
	if !location.IsSpilled {
		scope.varRegisterEnd[location.Kind] = max(scope.varRegisterEnd[location.Kind], uint32(location.Register)+1)
	}
	if existed && setsRegisterEnd(scope, previous) && !sameRegister(previous, location) {
		scope.recomputeRegisterEnd(previous.Kind)
	}
}

// recomputeRegisterEnd works out varRegisterEnd of kind's bank from the variables.
//
// Takes kind (isa.RegisterKind) which is the bank.
func (scope *lexicalScope) recomputeRegisterEnd(kind isa.RegisterKind) {
	end := uint32(0)
	for _, location := range scope.Vars {
		if !location.IsSpilled && location.Kind == kind {
			end = max(end, uint32(location.Register)+1)
		}
	}
	scope.varRegisterEnd[kind] = end
}

// patchDebugVarLocation moves the newest open debug var-table entry of a variable to its
// new location, so a heap-promoted or reloaded variable is still found by the debugger.
//
// Takes name (string) which is the variable name.
// Takes location (VarLocation) which is the new location.
func (s *ScopeStack) patchDebugVarLocation(name string, location program.VarLocation) {
	if s.DebugVarTable == nil {
		return
	}
	for index := len(s.DebugVarTable.Entries) - 1; index >= 0; index-- {
		entry := &s.DebugVarTable.Entries[index]
		if entry.Name == name && entry.EndPC == 0 {
			entry.Location = location
			return
		}
	}
}

// parseForcedSpillLimit reads the PIPIT_FORCE_SPILL_LIMIT value.
//
// Takes value (string) which is the environment value.
//
// Returns uint32 which is the limit, or zero when the value is unset or invalid.
func parseForcedSpillLimit(value string) uint32 {
	limit, err := strconv.ParseUint(value, 10, 32)
	if err != nil || limit >= uint64(spillThreshold) {
		return 0
	}
	return uint32(limit)
}

// setsRegisterEnd reports whether location is a register variable at the top of its
// bank's varRegisterEnd in scope.
//
// Takes scope (*lexicalScope) which holds the variable.
// Takes location (program.VarLocation) which is where the variable was.
//
// Returns bool which is true when moving the variable may lower varRegisterEnd.
func setsRegisterEnd(scope *lexicalScope, location program.VarLocation) bool {
	return !location.IsSpilled && uint32(location.Register)+1 == scope.varRegisterEnd[location.Kind]
}

// sameRegister reports whether two locations name the same register.
//
// Takes first (program.VarLocation) which is one location.
// Takes second (program.VarLocation) which is the other.
//
// Returns bool which is true when both are the same register of the same bank.
func sameRegister(first, second program.VarLocation) bool {
	return !first.IsSpilled && !second.IsSpilled && first.Kind == second.Kind && first.Register == second.Register
}

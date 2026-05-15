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
	"slices"
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

	// maxSpillSlotsPerBank caps the per-bank spill area at the uint16 limit. The runtime
	// spillSlot field is uint16 and the physical register file index is 256 + spillSlot, so
	// allocating beyond this would silently wrap the uint16 cast and alias two distinct
	// logical slots onto the same runtime register, corrupting state.
	maxSpillSlotsPerBank = 1 << 16
)

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
}

// Alloc allocates a register in the given bank and returns its index.
//
// Takes kind (isa.RegisterKind) which specifies the register bank to allocate from.
//
// Returns the index of the newly allocated register.
func (r *RegisterAllocator) Alloc(kind isa.RegisterKind) uint8 {
	register := r.nextRegister[kind]
	r.nextRegister[kind]++
	if r.nextRegister[kind] > r.peakRegister[kind] {
		r.peakRegister[kind] = r.nextRegister[kind]
	}
	if register > maxRegisterIndex && r.OverflowErr == nil {
		r.OverflowErr = fmt.Errorf(
			"register overflow in function %q: %s bank requires more than %d registers "+
				"(instruction encoding is limited to uint8 indices); "+
				"peak registers: int=%d float=%d string=%d general=%d bool=%d uint=%d complex=%d; "+
				"recycled pool lengths: int=%d float=%d string=%d general=%d bool=%d uint=%d complex=%d",
			r.FunctionName, kind, maxRegisterIndex+1,
			r.peakRegister[isa.RegisterInt], r.peakRegister[isa.RegisterFloat], r.peakRegister[isa.RegisterString],
			r.peakRegister[isa.RegisterGeneral], r.peakRegister[isa.RegisterBool], r.peakRegister[isa.RegisterUint],
			r.peakRegister[isa.RegisterComplex],
			len(r.recycledRegisters[isa.RegisterInt]), len(r.recycledRegisters[isa.RegisterFloat]),
			len(r.recycledRegisters[isa.RegisterString]), len(r.recycledRegisters[isa.RegisterGeneral]),
			len(r.recycledRegisters[isa.RegisterBool]), len(r.recycledRegisters[isa.RegisterUint]),
			len(r.recycledRegisters[isa.RegisterComplex]),
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
// Used when exiting a scope to free all registers allocated within it.
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
	}
	r.nextRegister = saved
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
			if r.nextRegister[kind] > r.peakRegister[kind] {
				r.peakRegister[kind] = r.nextRegister[kind]
			}
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
		if r.OverflowErr == nil {
			r.OverflowErr = fmt.Errorf("%w: bank %s in function %q holds more than %d live locals",
				fault.ErrSpillUnsupportedBank, kind, r.FunctionName, spillThreshold)
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
	return r.nextRegister[kind] >= spillThreshold
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

	// Scopes is the stack of active lexical Scopes.
	Scopes []lexicalScope
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
		}),
		DebugVarTable:    nil,
		DebugBodyLenFunc: nil,
		Scopes:           nil,
	})
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
	baseFreeIndex := 0
	if len(s.Scopes) > 0 {
		baseFreeIndex = s.Scopes[len(s.Scopes)-1].savedRecycledLengths[kind]
	}

	var location program.VarLocation

	hasRecycled := len(s.Alloc.recycledRegisters[kind]) > baseFreeIndex
	if hasRecycled || !s.Alloc.needsSpill(kind) {
		register := s.Alloc.allocOrRecycle(kind, baseFreeIndex)
		location = program.VarLocation{Register: register, Kind: kind}
	} else {
		slot := s.Alloc.allocSpill(kind)
		location = program.VarLocation{Kind: kind, IsSpilled: true, SpillSlot: slot}
	}

	if len(s.Scopes) > 0 {
		s.Scopes[len(s.Scopes)-1].Vars[name] = location
	}
	if s.DebugVarTable != nil && s.DebugBodyLenFunc != nil && name != "_" && !strings.HasPrefix(name, SyntheticNamePrefix) {
		s.DebugVarTable.Entries = append(s.DebugVarTable.Entries, program.DebugVarEntry{Name: name,
			Location: location,
			StartPC:  s.DebugBodyLenFunc(), EndPC: 0})
	}
	return location
}

// LookupVar searches for a variable in the scope stack, starting from the innermost
// scope.
//
// Takes name (string) which is the variable name to look up.
//
// Returns the location and true if found.
func (s *ScopeStack) LookupVar(name string) (program.VarLocation, bool) {
	for scopeIndex := range slices.Backward(s.Scopes) {
		if location, ok := s.Scopes[scopeIndex].Vars[name]; ok {
			return location, true
		}
	}
	return program.VarLocation{}, false
}

// MarkCaptured marks a variable as captured by a closure, so that subsequent writes to it
// will Emit isa.SubOpWriteSharedCell.
//
// Takes name (string) which is the variable name to mark.
func (s *ScopeStack) MarkCaptured(name string) {
	for scopeIndex := range slices.Backward(s.Scopes) {
		if loc, ok := s.Scopes[scopeIndex].Vars[name]; ok {
			loc.IsCaptured = true
			s.Scopes[scopeIndex].Vars[name] = loc
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
			s.Scopes[scopeIndex].Vars[name] = location
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
		scope := &s.Scopes[i]
		for _, location := range scope.Vars {
			if location.IsSpilled {
				continue
			}
			needed := uint32(location.Register) + 1
			if needed > s.Alloc.nextRegister[location.Kind] {
				s.Alloc.nextRegister[location.Kind] = needed
				if s.Alloc.peakRegister[location.Kind] < needed {
					s.Alloc.peakRegister[location.Kind] = needed
				}
			}
		}
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

	// savedRecycledLengths records the length of each recycled-register free list when this
	// scope was entered, so scope exit can truncate back without discarding outer-scope
	// entries.
	savedRecycledLengths [isa.NumRegisterKinds]int
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

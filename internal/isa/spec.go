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

package isa

import (
	"fmt"
)

// tierCount is the number of encoding tiers.
const tierCount = 4

// Tier is the encoding depth an operation lives at: the byte position whose value selects
// it once every shallower position holds the "descend" marker.
type Tier uint8

const (
	// TierMain is the op byte itself: three-operand instructions.
	TierMain Tier = iota

	// TierSub1 is operand A when the op byte is OpDrillTier1: two-operand instructions.
	TierSub1

	// TierSub2 is operand B when A is also SubOpDrillTier2: one-operand instructions.
	TierSub2

	// TierSub3 is operand C when B is also SubOpTier2DrillTier3: zero-operand instructions.
	TierSub3
)

// specFlag carries the yes/no properties of an operation.
type specFlag uint16

const (
	// SpecJump marks an operation that carries a relative jump offset. JumpLayoutOf returns
	// where the offset is encoded.
	SpecJump specFlag = 1 << iota

	// specReserved marks a retired slot kept so the numbering of later operations does not
	// shift. It has no handler and no name.
	specReserved

	// specMeta marks a drill marker: index 0 of a tier, which means "descend a tier" rather
	// than an operation in its own right.
	specMeta

	// SpecShimNarrow selects the slim assembly shim for an operation lifted through a tier-2
	// shim: the handler neither reads nor writes the program counter, so the shim skips the
	// PC save and restore around the Go call.
	SpecShimNarrow

	// SpecShimSuppressed keeps the shim emitted as a fallback jump target while a pure
	// assembly body owns the operation's jump-table slot.
	SpecShimSuppressed

	// specMutatesMemory marks an operation that can mutate state reachable from outside its
	// own register writes. The optimiser's heap-effect predicate derives from this flag.
	specMutatesMemory

	// specMemoryPure marks an operation that provably mutates no reachable state. Stated
	// explicitly so a new operation carrying neither flag is a classification gap the ISA
	// spec test rejects.
	specMemoryPure
)

// OpSpec is the single record everything else about an operation derives from.
type OpSpec struct {
	// Name is the mnemonic the disassembler prints, for example "ADD_INT".
	Name string

	// Handler is the Go handler the flat dispatch switch calls, for example "handleAddInt".
	// Empty for drill markers and reserved slots.
	Handler string

	// AsmBody is the assembly handler symbol that owns the jump-table slot, for example
	// "handlerAddInt". Empty when the operation has no assembly body.
	AsmBody string

	// AsmTable names the jump table the body is installed in; empty means the main table.
	AsmTable string

	// Shim is the suffix of the tier-2 assembly-call shim, for example "TypeAssert", when
	// the operation reaches its Go handler through a shim. Empty otherwise.
	Shim string

	// ShimHandler is the exported Go trampoline target the shim calls, for example
	// "HandleTypeAssert".
	ShimHandler string

	// ExitStub is the assembly exit stub that routes the operation to Go through a dedicated
	// exit reason, for example "handlerSetFieldExit". Empty otherwise.
	ExitStub string

	// ExitReason names the exit constant the stub writes, for example "exitSetField".
	ExitReason string

	// Cost is the cost-table charge for one execution of the operation.
	Cost int64

	// Flags are the operation's yes/no properties.
	Flags specFlag

	// Tier is the encoding depth.
	Tier Tier

	// Code is the operation's value within its tier: the enum constant.
	Code uint8
}

// specIndex holds every row by tier and code for O(1) lookup, built once from the tier
// slices.
var specIndex [tierCount][256]*OpSpec

// Has reports whether the row carries every flag in f.
//
// Takes f (specFlag) which is the flag mask to test.
//
// Returns true when all bits in f are set.
func (s OpSpec) Has(f specFlag) bool { return s.Flags&f == f }

// Shape returns the operand Shape recorded for the operation.
//
// Every tier carries its own shapes, so a tier-1 row describes its own operands rather
// than borrowing the OpDrillTier1 wildcard.
//
// Returns OperandShape which is the recorded Shape, or the zero Shape when none is
// described.
func (s OpSpec) Shape() OperandShape {
	return OperandShapeAt(s.Tier, s.Code)
}

// MutatesMemory reports whether the operation can mutate memory beyond its register
// writes, and whether the operation was classified at all.
//
// Returns mutates (bool) which is true when specMutatesMemory is set.
// Returns classified (bool) which is false when the row carries neither memory flag, in
// which case callers must treat the operation as mutating.
func (s OpSpec) MutatesMemory() (mutates, classified bool) {
	if s.Has(specMutatesMemory) {
		return true, true
	}
	if s.Has(specMemoryPure) {
		return false, true
	}
	return true, false
}

// String renders the row for diagnostics.
//
// Returns a formatted string showing tier, code and name.
func (s OpSpec) String() string {
	return fmt.Sprintf("tier%d:%d %s", s.Tier, s.Code, s.Name)
}

// cost sets the cost-table charge.
//
// Takes charge (int64) which is the per-execution cost weight.
//
// Returns OpSpec with the updated cost.
func (s OpSpec) cost(charge int64) OpSpec { s.Cost = charge; return s }

// handler names the Go handler.
//
// Takes name (string) which is the handler function name.
//
// Returns OpSpec with the handler set.
func (s OpSpec) handler(name string) OpSpec { s.Handler = name; return s }

// asm names the assembly body that owns the main jump-table slot.
//
// Takes symbol (string) which is the assembly handler symbol.
//
// Returns OpSpec with the assembly body set.
func (s OpSpec) asm(symbol string) OpSpec { s.AsmBody = symbol; return s }

// asmIn names the assembly body and its target jump table.
//
// Takes table (string) which is the jump table name.
// Takes symbol (string) which is the assembly handler symbol.
//
// Returns OpSpec with both fields set.
func (s OpSpec) asmIn(table, symbol string) OpSpec { s.AsmBody = symbol; s.AsmTable = table; return s }

// shim binds the operation to a tier-2 assembly-call shim.
//
// Takes goHandler (string) which is the exported Go trampoline.
// Takes suffix (string) which is the shim name suffix.
//
// Returns OpSpec with shim fields set.
func (s OpSpec) shim(goHandler, suffix string) OpSpec {
	s.ShimHandler = goHandler
	s.Shim = suffix
	return s
}

// exit binds the operation to a direct exit stub.
//
// Takes stub (string) which is the assembly exit stub symbol.
// Takes reason (string) which is the exit constant name.
//
// Returns OpSpec with exit fields set.
func (s OpSpec) exit(stub, reason string) OpSpec { s.ExitStub = stub; s.ExitReason = reason; return s }

// flags ORs properties onto the row.
//
// Takes f (specFlag) which is the flag bits to add.
//
// Returns OpSpec with the additional flags.
func (s OpSpec) flags(f specFlag) OpSpec { s.Flags |= f; return s }

// mutates marks the operation as able to mutate memory beyond its own register writes.
//
// Returns OpSpec with specMutatesMemory set.
func (s OpSpec) mutates() OpSpec { s.Flags |= specMutatesMemory; return s }

// pure marks the operation as mutating no memory beyond its own register writes.
//
// Returns OpSpec with specMemoryPure set.
func (s OpSpec) pure() OpSpec { s.Flags |= specMemoryPure; return s }

// AllSpecs returns every row in tier order then code order.
//
// Returns []OpSpec containing all registered spec rows.
func AllSpecs() []OpSpec {
	all := make([]OpSpec, 0, len(tierMainSpecs)+len(tierSub1Specs)+len(tierSub2Specs)+len(tierSub3Specs))
	for tier := range specIndex {
		for code := range specIndex[tier] {
			if row := specIndex[tier][code]; row != nil {
				all = append(all, *row)
			}
		}
	}
	return all
}

// SpecAt returns the row for a tier and code, and whether one exists.
//
// Takes tier (Tier) which is the encoding depth.
// Takes code (uint8) which is the operation value within its tier.
//
// Returns OpSpec and true when a row exists, or the zero spec and false otherwise.
func SpecAt(tier Tier, code uint8) (OpSpec, bool) {
	if int(tier) >= tierCount {
		return OpSpec{}, false
	}
	row := specIndex[tier][code]
	if row == nil {
		return OpSpec{}, false
	}
	return *row, true
}

// SpecFor returns the row for a tier-0 opcode.
//
// Takes op (Opcode) which is the tier-0 opcode to look up.
//
// Returns OpSpec and true when a row exists, or the zero spec and false otherwise.
func SpecFor(op Opcode) (OpSpec, bool) { return SpecAt(TierMain, uint8(op)) }

// SpecForInstruction resolves the drill cascade and returns the row the instruction
// executes.
//
// Takes instr (Instruction) which is the encoded instruction word.
//
// Returns OpSpec and true when a row exists, or the zero spec and false otherwise.
func SpecForInstruction(instr Instruction) (OpSpec, bool) {
	if instr.Op != OpDrillTier1 {
		return SpecAt(TierMain, uint8(instr.Op))
	}
	if SubOpcode(instr.A) != SubOpDrillTier2 {
		return SpecAt(TierSub1, instr.A)
	}
	if SubOpcodeTier2(instr.B) != SubOpTier2DrillTier3 {
		return SpecAt(TierSub2, instr.B)
	}
	return SpecAt(TierSub3, instr.C)
}

// spec starts a row for an operation at a tier.
//
// Takes tier (Tier) which is the encoding depth.
// Takes code (uint8) which is the operation value within its tier.
// Takes name (string) which is the disassembly mnemonic.
//
// Returns OpSpec with the baseline fields populated.
func spec(tier Tier, code uint8, name string) OpSpec {
	return OpSpec{
		Name:        name,
		Handler:     "",
		AsmBody:     "",
		AsmTable:    "",
		Shim:        "",
		ShimHandler: "",
		ExitStub:    "",
		ExitReason:  "",
		Cost:        costCheap,
		Flags:       0,
		Tier:        tier,
		Code:        code,
	}
}

// main0 starts a tier-0 row.
//
// Takes op (Opcode) which is the tier-0 opcode.
// Takes name (string) which is the disassembly mnemonic.
//
// Returns OpSpec with tier set to TierMain.
func main0(op Opcode, name string) OpSpec { return spec(TierMain, uint8(op), name) }

// sub1 starts a tier-1 row.
//
// Takes op (SubOpcode) which is the tier-1 sub-opcode.
// Takes name (string) which is the disassembly mnemonic.
//
// Returns OpSpec with tier set to TierSub1.
func sub1(op SubOpcode, name string) OpSpec { return spec(TierSub1, uint8(op), name) }

// sub2 starts a tier-2 row.
//
// Takes op (SubOpcodeTier2) which is the tier-2 sub-opcode.
// Takes name (string) which is the disassembly mnemonic.
//
// Returns OpSpec with tier set to TierSub2.
func sub2(op SubOpcodeTier2, name string) OpSpec { return spec(TierSub2, uint8(op), name) }

// sub3 starts a tier-3 row.
//
// Takes op (SubOpcodeTier3) which is the tier-3 sub-opcode.
// Takes name (string) which is the disassembly mnemonic.
//
// Returns OpSpec with tier set to TierSub3.
func sub3(op SubOpcodeTier3, name string) OpSpec { return spec(TierSub3, uint8(op), name) }

func init() {
	for _, rows := range [][]OpSpec{tierMainSpecs, tierSub1Specs, tierSub2Specs, tierSub3Specs} {
		for index := range rows {
			row := &rows[index]
			if specIndex[row.Tier][row.Code] != nil {
				panic(fmt.Sprintf("isa: duplicate spec row for %s", row))
			}
			specIndex[row.Tier][row.Code] = row
		}
	}
}

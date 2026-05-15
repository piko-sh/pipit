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

const (
	// costFree represents zero cost for register moves, constant loads, and extensions.
	costFree int64 = 0

	// costCheap represents low cost for arithmetic, comparisons, jumps, and type
	// conversions.
	costCheap int64 = 1

	// costMedium represents medium cost for string ops, field access, and math intrinsics.
	costMedium int64 = 3

	// costModerate represents moderate cost for index/map access, closures, channels, and
	// defer/panic.
	costModerate int64 = 5

	// costExpensive represents high cost for function calls, goroutine spawn, append, and
	// select.
	costExpensive int64 = 10

	// costVeryHeavy represents very high cost for allocations (make slice/map/chan).
	costVeryHeavy int64 = 20
)

const (
	// SlotsPerTier is how many operation codes one tier holds: a whole byte's worth.
	SlotsPerTier = 256

	// FlatDispatchSlots is the size of the flat (tier, code) operation space: four tiers of
	// SlotsPerTier codes each. It matches the engine's 1024-entry flat dispatch table.
	FlatDispatchSlots = tierCount * SlotsPerTier
)

// CostTable holds the per-operation cost weights used by the runtime cost metering
// system, indexed by the flat (tier, code) slot.
type CostTable [FlatDispatchSlots]int64

// FlatIndexOf returns the flat slot of a tier-0 opcode, for tables keyed by operation
// identity whose entries are written out by name.
//
// Takes op (Opcode) which is the tier-0 opcode.
//
// Returns int which is the flat slot.
func FlatIndexOf(op Opcode) int { return int(TierMain)*SlotsPerTier + int(op) }

// FlatIndexOfSub1 returns the flat slot of a tier-1 sub-opcode.
//
// Takes op (SubOpcode) which is the tier-1 sub-opcode.
//
// Returns int which is the flat slot.
func FlatIndexOfSub1(op SubOpcode) int { return int(TierSub1)*SlotsPerTier + int(op) }

// FlatIndexOfSub2 returns the flat slot of a tier-2 sub-opcode.
//
// Takes op (SubOpcodeTier2) which is the tier-2 sub-opcode.
//
// Returns int which is the flat slot.
func FlatIndexOfSub2(op SubOpcodeTier2) int { return int(TierSub2)*SlotsPerTier + int(op) }

// FlatIndexOfSub3 returns the flat slot of a tier-3 sub-opcode.
//
// Takes op (SubOpcodeTier3) which is the tier-3 sub-opcode.
//
// Returns int which is the flat slot.
func FlatIndexOfSub3(op SubOpcodeTier3) int { return int(TierSub3)*SlotsPerTier + int(op) }

// FlatIndexFor resolves the drill cascade to the operation's flat (tier, code) slot.
//
// Takes instr (Instruction) which is the encoded instruction word.
//
// Returns int which is the flat slot in the range [0, FlatDispatchSlots).
func FlatIndexFor(instr Instruction) int {
	if instr.Op != OpDrillTier1 {
		return int(instr.Op)
	}
	if SubOpcode(instr.A) != SubOpDrillTier2 {
		return int(TierSub1)*SlotsPerTier + int(instr.A)
	}
	if SubOpcodeTier2(instr.B) != SubOpTier2DrillTier3 {
		return int(TierSub2)*SlotsPerTier + int(instr.B)
	}
	return int(TierSub3)*SlotsPerTier + int(instr.C)
}

var (
	// defaultCostTable is the package-level default used when no custom table is provided.
	defaultCostTable CostTable
)

// SharedDefaultCostTable returns a pointer to the process-wide default cost table.
// Callers must treat the returned table as read-only.
//
// Returns *CostTable which is the shared default, never nil.
func SharedDefaultCostTable() *CostTable {
	return &defaultCostTable
}

// DefaultCostTable returns a mutable copy of the default cost table.
//
// Returns CostTable populated with the default cost weights.
func DefaultCostTable() CostTable {
	var table CostTable
	initDefaultCostTable(&table)
	return table
}

func init() {
	initDefaultCostTable(&defaultCostTable)
}

// initDefaultCostTable fills the table from the spec rows: every operation at every tier
// charges the cost its row states, and slots without a row charge costCheap.
//
// Takes table (*CostTable) which is the cost table to fill.
func initDefaultCostTable(table *CostTable) {
	for i := range table {
		table[i] = costCheap
	}
	rows := AllSpecs()
	for index := range rows {
		table[int(rows[index].Tier)*SlotsPerTier+int(rows[index].Code)] = rows[index].Cost
	}
}

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
	"slices"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// snapshotFieldLimit bounds the number of captured fields per aggregate.
const snapshotFieldLimit = 8

// snapshotScanLimit bounds the control-flow search for one aggregate copy.
const snapshotScanLimit = 256

// snapshotField records a direct field read and its uses of a copied aggregate.
type snapshotField struct {
	// read is the original field-load instruction.
	read isa.Instruction

	// extension holds a wide field-layout index when present.
	extension isa.Instruction

	// kind identifies the captured field register bank.
	kind isa.RegisterKind

	// width counts the field-load instruction words.
	width int

	// uses lists every field read to replace.
	uses []int
}

// snapshotRegion contains uses reached before the copied register is replaced.
type snapshotRegion struct {
	// fields groups supported reads by field layout.
	fields []snapshotField

	// members records PCs reached before the copied register is replaced.
	members map[int]bool
}

// scalarizeSnapshotsPass replaces private aggregate copies with field captures.
type scalarizeSnapshotsPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the name used by pipeline diagnostics.
func (scalarizeSnapshotsPass) Name() string { return "scalarize-snapshots" }

// Run captures eligible fields and refreshes alias facts for subsequent passes.
//
// Takes state (*PassContext) which owns the shared control-flow analysis.
// Takes cf (*program.CompiledFunction) which contains the instructions and metadata to
// transform.
//
// Returns error when cancellation interrupts the pass, or nil after processing the
// function.
func (scalarizeSnapshotsPass) Run(ctx context.Context, state *PassContext, cf *program.CompiledFunction) error {
	changed := false
	for pc := 0; pc < len(cf.Body); pc++ {
		inst := cf.Body[pc]
		if inst.Op != isa.OpMoveGeneral || inst.C != engine.MoveGeneralModeSnapshot {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		region, ok := findSnapshotFields(cf, pc, state.Analysis)
		if !ok {
			continue
		}
		if captureSnapshotFields(cf, pc, region.fields) {
			changed = true
			state.Analysis.invalidate()
		}
	}
	if changed {
		state.Analysis.invalidate()
		cf.AliasInfo = nil
		cf.PrecomputedAllocCountsValid = false
		return RunPointerAliasAnalysis(ctx, cf)
	}
	return nil
}

// snapshotFieldAt recognizes direct scalar and byte-slice reads from receiver.
//
// Takes cf (*program.CompiledFunction) which supplies the instruction stream and field
// layouts.
// Takes pc (int) which identifies the possible field read.
// Takes receiver (uint8) which names the copied aggregate register.
//
// Returns snapshotField which describes the field read.
// Returns bool which is true when the field can be captured without a pointer traversal
// or another aggregate copy.
func snapshotFieldAt(cf *program.CompiledFunction, pc int, receiver uint8) (snapshotField, bool) {
	inst := cf.Body[pc]
	field := snapshotField{read: inst, width: 1}
	layout := int(inst.C)
	if isa.InstrIsTier1SubOp(inst, isa.SubOpGetStructFieldSliceByte) {
		if inst.C != receiver || pc+1 >= len(cf.Body) || cf.Body[pc+1].Op != isa.OpExt {
			return field, false
		}
		field.extension = cf.Body[pc+1]
		field.width = 2
		field.kind = isa.RegisterSliceByte
		layout = int(field.extension.A) | int(field.extension.B)<<8
	} else {
		if inst.B != receiver {
			return field, false
		}
		switch inst.Op {
		case isa.OpGetStructFieldIntT0, isa.OpGetStructFieldSliceLen:
			field.kind = isa.RegisterInt
		case isa.OpGetStructFieldUint:
			field.kind = isa.RegisterUint
		case isa.OpGetStructFieldFloat:
			field.kind = isa.RegisterFloat
		case isa.OpGetStructFieldBool:
			field.kind = isa.RegisterBool
		default:
			return field, false
		}
	}
	if layout >= len(cf.StructLayoutTable) || cf.StructLayoutTable[layout].PathLength != 1 {
		return field, false
	}
	return field, true
}

// findSnapshotFields proves a copied aggregate has only supported field uses.
//
// Takes cf (*program.CompiledFunction) which supplies the body and register metadata.
// Takes copyPC (int) which identifies the snapshot instruction.
// Takes analysis (*functionAnalysis) which supplies predecessor edges for the
// single-definition proof.
//
// Returns snapshotRegion which contains the captured fields and their uses.
// Returns bool which is false for escaping, opaque, ambiguous, or excessively large
// regions.
func findSnapshotFields(cf *program.CompiledFunction, copyPC int, analysis *functionAnalysis) (snapshotRegion, bool) {
	region := snapshotRegion{members: make(map[int]bool)}
	receiver := cf.Body[copyPC].A
	if resultSlotReadByRecover(cf, isa.RegisterGeneral, receiver) {
		return region, false
	}
	walk := newRegisterLivenessWalk(cf, cf.Body, isa.RegisterGeneral, receiver)
	pending := []int{copyPC + 1}
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		owner := -1
		for pc < len(cf.Body) {
			if pc < 0 || len(region.members) >= snapshotScanLimit {
				return region, false
			}
			if region.members[pc] {
				break
			}
			inst := cf.Body[pc]
			if IsCallInstruction(inst) || isa.InstrIsTier1SubOp(inst, isa.SubOpTypeSwitchJump) {
				return region, false
			}
			if field, ok := snapshotFieldAt(cf, pc, receiver); ok {
				index := slices.IndexFunc(region.fields, func(prior snapshotField) bool {
					return prior.read.Op == field.read.Op && prior.read.C == field.read.C && prior.extension == field.extension
				})
				if index < 0 {
					if len(region.fields) >= snapshotFieldLimit {
						return region, false
					}
					index = len(region.fields)
					region.fields = append(region.fields, field)
				}
				region.fields[index].uses = append(region.fields[index].uses, pc)
				for offset := 0; offset < field.width; offset++ {
					region.members[pc+offset] = true
				}
				pc += field.width
				owner = -1
				continue
			}
			verdict, next := walk.step(pc, &owner)
			if verdict == walkLive {
				return region, false
			}
			if verdict == walkDead {
				break
			}
			if !instructionShapeAllowsCseScan(inst) && inst.Op != isa.OpExt {
				return region, false
			}
			region.members[pc] = true
			if verdict == walkBranch {
				target, ok := program.JumpTargetAt(cf.Body, pc)
				if !ok {
					return region, false
				}
				pending = append(pending, target)
				if !isa.InstrIsTier1SubOp(inst, isa.SubOpJump) {
					pending = append(pending, pc+program.JumpFootprint(inst))
				}
				for ext := pc + 1; ext < pc+program.JumpFootprint(inst); ext++ {
					region.members[ext] = true
				}
				break
			}
			pc = next
		}
	}
	if len(region.fields) == 0 {
		return region, false
	}
	analysis.buildCFG()
	pending = pending[:0]
	for _, field := range region.fields {
		pending = append(pending, field.uses...)
	}
	checked := make(map[int]bool)
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if pc == copyPC || checked[pc] {
			continue
		}
		if pc == 0 || !region.members[pc] {
			return region, false
		}
		checked[pc] = true
		for _, pred := range analysis.predecessors[pc] {
			if analysis.reachable[pred] {
				pending = append(pending, pred)
			}
		}
	}
	for _, inst := range cf.Body {
		if isa.InstrIsTier1SubOp(inst, isa.SubOpTypeSwitchJump) {
			return region, false
		}
	}
	return region, true
}

// captureSnapshotFields replaces one copy with direct field captures.
//
// Takes cf (*program.CompiledFunction) which owns the body, register counts, and PC
// metadata.
// Takes pc (int) which identifies the copy to replace.
// Takes fields ([]snapshotField) which lists the proven field reads and their uses.
//
// Returns false without mutation if register or jump encodings cannot hold the
// transformed function.
func captureSnapshotFields(cf *program.CompiledFunction, pc int, fields []snapshotField) bool {
	old := cf.Body
	if sm := cf.DebugSourceMap; sm != nil && len(sm.Positions) != len(old) {
		return false
	}
	counts := cf.NumRegisters
	var captures []isa.Instruction
	receiver := old[pc].B
	if cf.DebugVarTable != nil {
		snapshot := old[pc]
		snapshot.C = engine.MoveGeneralModeDebugSnapshot
		captures = append(captures, snapshot)
		receiver = snapshot.A
	}
	replacements := make(map[int]isa.Instruction)
	for _, field := range fields {
		if counts[field.kind] >= 256 {
			return false
		}
		reg := uint8(counts[field.kind])
		counts[field.kind]++
		read := field.read
		if field.width == 2 {
			read.B = reg
			read.C = receiver
		} else {
			read.A = reg
			read.B = receiver
		}
		captures = append(captures, read)
		if field.width == 2 {
			captures = append(captures, field.extension)
		}
		for _, use := range field.uses {
			inst := old[use]
			if field.width == 2 {
				replacements[use] = isa.NewTier1Instruction(isa.SubOpMoveSliceByte, inst.B, reg)
				replacements[use+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
			} else if inst.Op == isa.OpGetStructFieldSliceLen {
				replacements[use] = isa.NewTier1Instruction(isa.SubOpMoveInt, inst.A, reg)
			} else {
				replacements[use] = emitMoveForTier0ReadOp(inst.Op, inst.A, reg)
			}
		}
	}
	growth := len(captures) - 1
	relocate := func(index int) int {
		if index > pc {
			return index + growth
		}
		return index
	}
	body := make([]isa.Instruction, 0, len(old)+growth)
	body = append(body, old[:pc]...)
	body = append(body, captures...)
	body = append(body, old[pc+1:]...)
	for index, inst := range replacements {
		body[relocate(index)] = inst
	}
	for index := range old {
		if target, ok := program.JumpTargetAt(old, index); ok && !program.SetJumpTarget(body, relocate(index), relocate(target)) {
			return false
		}
	}
	if sm := cf.DebugSourceMap; sm != nil {
		positions := append(sm.Positions[:0:0], sm.Positions[:pc]...)
		for range captures {
			positions = append(positions, sm.Positions[pc])
		}
		sm.Positions = append(positions, sm.Positions[pc+1:]...)
	}
	if table := cf.DebugVarTable; table != nil {
		for index := range table.Entries {
			entry := &table.Entries[index]
			entry.StartPC = relocate(entry.StartPC)
			if entry.EndPC != 0 {
				entry.EndPC = relocate(entry.EndPC)
			}
		}
	}
	cf.PeepholeProvenance = remapSnapshotEntries(cf.PeepholeProvenance, relocate)
	for index, ann := range cf.PeepholeProvenance {
		if ann.Origin >= 0 {
			ann.Origin = relocate(ann.Origin)
			cf.PeepholeProvenance[index] = ann
		}
	}
	cf.ArenaSafeAllocPCs = remapSnapshotEntries(cf.ArenaSafeAllocPCs, relocate)
	cf.FieldStoreArenaSafePCs = remapSnapshotEntries(cf.FieldStoreArenaSafePCs, relocate)
	cf.InPlaceHeaderReusePCs = remapSnapshotEntries(cf.InPlaceHeaderReusePCs, relocate)
	cf.GetMethodReceiverTypeNames = remapSnapshotEntries(cf.GetMethodReceiverTypeNames, relocate)
	cf.Body = body
	cf.NumRegisters = counts
	RecordPeepholeRewrite(cf, pc, peepholeRewriteSnapshotFields, pc)
	return true
}

// remapSnapshotEntries relocates PC-keyed annotations after inserting captures.
//
// Takes entries (map[K]V) which supplies the old annotations.
// Takes relocate (func(int) int) which maps original PCs to the expanded stream.
//
// Returns map[K]V containing relocated annotations, preserving nil input.
func remapSnapshotEntries[K ~int | ~uint32, V any](entries map[K]V, relocate func(int) int) map[K]V {
	if entries == nil {
		return nil
	}
	result := make(map[K]V, len(entries))
	for key, value := range entries {
		result[K(relocate(int(key)))] = value
	}
	return result
}

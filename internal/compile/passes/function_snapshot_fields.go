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

const (
	// snapshotFieldLimit bounds the number of captured fields per aggregate.
	snapshotFieldLimit = 8

	// snapshotScanLimit bounds the control-flow search for one aggregate copy.
	snapshotScanLimit = 256

	// wideFieldReadWords is the word count of a field read that carries its layout index in
	// an extension word.
	wideFieldReadWords = 2
)

// snapshotField records a direct field read and its uses of a copied aggregate.
type snapshotField struct {
	// uses lists every field read to replace.
	uses []int

	// width counts the field-load instruction words.
	width int

	// read is the original field-load instruction.
	read isa.Instruction

	// extension holds a wide field-layout index when present.
	extension isa.Instruction

	// kind identifies the captured field register bank.
	kind isa.RegisterKind
}

// sameField reports whether other reads the same field as field.
//
// Takes other (snapshotField) which is the field read to compare.
//
// Returns true when both reads name the same operation and layout.
func (field snapshotField) sameField(other snapshotField) bool {
	return field.read.Op == other.read.Op && field.read.C == other.read.C && field.extension == other.extension
}

// snapshotRegion contains uses reached before the copied register is replaced.
type snapshotRegion struct {
	// members records PCs reached before the copied register is replaced.
	members map[int]bool

	// fields groups supported reads by field layout.
	fields []snapshotField
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
		if ok && captureSnapshotFields(cf, pc, region.fields) {
			changed = true
			state.Analysis.invalidate()
		}
	}
	if !changed {
		return nil
	}
	cf.PrecomputedAllocCountsValid = false
	return RunPointerAliasAnalysis(ctx, cf)
}

// snapshotFieldAt recognises direct scalar and byte-slice reads from receiver.
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
	field := snapshotField{uses: nil, width: 1, read: inst, extension: isa.Instruction{}, kind: isa.RegisterInt}
	layout := int(inst.C)
	if isa.InstrIsTier1SubOp(inst, isa.SubOpGetStructFieldSliceByte) {
		if inst.C != receiver || pc+1 >= len(cf.Body) || cf.Body[pc+1].Op != isa.OpExt {
			return field, false
		}
		field.extension = cf.Body[pc+1]
		field.width = wideFieldReadWords
		field.kind = isa.RegisterSliceByte
		layout = int(isa.JoinWide(field.extension.A, field.extension.B))
	} else {
		kind, ok := scalarFieldReadKind(inst.Op)
		if !ok || inst.B != receiver {
			return field, false
		}
		field.kind = kind
	}
	if layout >= len(cf.StructLayoutTable) || cf.StructLayoutTable[layout].PathLength != 1 {
		return field, false
	}
	return field, true
}

// scalarFieldReadKind maps a tier-0 scalar field read to its destination bank.
//
// Takes op (isa.Opcode) which is the candidate field read.
//
// Returns the destination bank and whether op is a supported field read.
func scalarFieldReadKind(op isa.Opcode) (isa.RegisterKind, bool) {
	switch op {
	case isa.OpGetStructFieldIntT0, isa.OpGetStructFieldSliceLen:
		return isa.RegisterInt, true
	case isa.OpGetStructFieldUint:
		return isa.RegisterUint, true
	case isa.OpGetStructFieldFloat:
		return isa.RegisterFloat, true
	case isa.OpGetStructFieldBool:
		return isa.RegisterBool, true
	default:
		return isa.RegisterInt, false
	}
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
	region := snapshotRegion{members: make(map[int]bool), fields: nil}
	receiver := cf.Body[copyPC].A
	if analysis.HasTypeSwitch() || resultSlotReadByRecover(cf, isa.RegisterGeneral, receiver) {
		return region, false
	}
	walk := snapshotWalk{
		cf:       cf,
		liveness: newRegisterLivenessWalk(cf, cf.Body, isa.RegisterGeneral, receiver),
		region:   &region,
		pending:  []int{copyPC + 1},
		receiver: receiver,
	}
	if !walk.run() || len(region.fields) == 0 {
		return region, false
	}
	return region, snapshotUsesHaveSingleDefinition(region, copyPC, analysis)
}

// snapshotWalk follows every path from an aggregate copy until the copied register is
// replaced, collecting the field reads and refusing any other use.
type snapshotWalk struct {
	// cf supplies the body and field layouts.
	cf *program.CompiledFunction

	// liveness classifies the words that are not field reads.
	liveness *registerLivenessWalk

	// region collects the members and field reads.
	region *snapshotRegion

	// pending holds path starts still to follow.
	pending []int

	// receiver is the copied aggregate register.
	receiver uint8
}

// run follows every pending path.
//
// Returns false when some path uses the copy in an unsupported way.
func (w *snapshotWalk) run() bool {
	for len(w.pending) > 0 {
		pc := w.pending[len(w.pending)-1]
		w.pending = w.pending[:len(w.pending)-1]
		if !w.follow(pc) {
			return false
		}
	}
	return true
}

// follow walks one straight-line path from pc until the copy dies, the path joins a
// visited word, or a branch queues its successors.
//
// Takes pc (int) which is the first word of the path.
//
// Returns false when the path uses the copy in an unsupported way.
func (w *snapshotWalk) follow(pc int) bool {
	owner := -1
	for pc < len(w.cf.Body) && !w.region.members[pc] {
		if pc < 0 || len(w.region.members) >= snapshotScanLimit || IsCallInstruction(w.cf.Body[pc]) {
			return false
		}
		if field, ok := snapshotFieldAt(w.cf, pc, w.receiver); ok {
			if !w.recordField(pc, field) {
				return false
			}
			pc += field.width
			owner = -1
			continue
		}
		next, done, ok := w.step(pc, &owner)
		if !ok || done {
			return ok
		}
		pc = next
	}
	return true
}

// step classifies one word that is not a field read.
//
// Takes pc (int) which is the word's program counter.
// Takes owner (*int) which tracks the instruction owning later extension words.
//
// Returns the next PC, whether the path ended, and false when the word refutes the
// rewrite.
func (w *snapshotWalk) step(pc int, owner *int) (next int, done, ok bool) {
	inst := w.cf.Body[pc]
	verdict, next := w.liveness.step(pc, owner)
	switch {
	case verdict == walkLive:
		return 0, true, false
	case verdict == walkDead:
		return 0, true, true
	case !instructionShapeAllowsCseScan(inst) && inst.Op != isa.OpExt:
		return 0, true, false
	}
	w.region.members[pc] = true
	if verdict == walkBranch {
		return 0, true, w.branch(pc, inst)
	}
	return next, false, true
}

// branch queues the successors of the jump at pc and marks its footprint as visited.
//
// Takes pc (int) which is the jump's program counter.
// Takes inst (isa.Instruction) which is the jump.
//
// Returns false when the jump has no single decodable target.
func (w *snapshotWalk) branch(pc int, inst isa.Instruction) bool {
	target, ok := program.JumpTargetAt(w.cf.Body, pc)
	if !ok {
		return false
	}
	footprint := program.JumpFootprint(inst)
	w.pending = append(w.pending, target)
	if !isa.InstrIsTier1SubOp(inst, isa.SubOpJump) {
		w.pending = append(w.pending, pc+footprint)
	}
	for ext := pc + 1; ext < pc+footprint; ext++ {
		w.region.members[ext] = true
	}
	return true
}

// recordField adds a field read to the region, grouping reads of the same field.
//
// Takes pc (int) which is the read's program counter.
// Takes field (snapshotField) which describes the read.
//
// Returns false when the aggregate has too many distinct fields.
func (w *snapshotWalk) recordField(pc int, field snapshotField) bool {
	index := slices.IndexFunc(w.region.fields, field.sameField)
	if index < 0 {
		if len(w.region.fields) >= snapshotFieldLimit {
			return false
		}
		index = len(w.region.fields)
		w.region.fields = append(w.region.fields, field)
	}
	w.region.fields[index].uses = append(w.region.fields[index].uses, pc)
	for offset := range field.width {
		w.region.members[pc+offset] = true
	}
	return true
}

// snapshotUsesHaveSingleDefinition proves every reachable path to a field read starts at
// the copy, so no other definition of the copied register reaches it.
//
// Takes region (snapshotRegion) which holds the field reads and visited words.
// Takes copyPC (int) which is the copy instruction.
// Takes analysis (*functionAnalysis) which supplies predecessor edges.
//
// Returns true when every predecessor chain stays inside the region until the copy.
func snapshotUsesHaveSingleDefinition(region snapshotRegion, copyPC int, analysis *functionAnalysis) bool {
	var uses []int
	for _, field := range region.fields {
		uses = append(uses, field.uses...)
	}
	return usesReachedOnlyFrom(analysis, region.members, uses, copyPC, copyPC+1)
}

// snapshotCapture holds the words that replace one aggregate copy.
type snapshotCapture struct {
	// replacements maps each replaced field-read word to its new instruction.
	replacements map[int]isa.Instruction

	// captures are the words inserted in place of the copy.
	captures []isa.Instruction

	// counts are the register counts after allocating the capture registers.
	counts [isa.NumRegisterKinds]uint32
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
	capture, ok := buildSnapshotCapture(cf, pc, fields)
	if !ok {
		return false
	}
	old := cf.Body
	rewrite := newBodyRewrite(len(old), len(old)+len(capture.captures))
	for index := range old {
		if index != pc {
			if inst, replaced := capture.replacements[index]; replaced {
				rewrite.keepAs(inst, index)
			} else {
				rewrite.keep(old, index)
			}
			continue
		}
		rewrite.drop(index)
		for _, inst := range capture.captures {
			rewrite.insert(inst, index)
		}
	}
	if !rewrite.apply(cf) {
		return false
	}
	cf.NumRegisters = capture.counts
	RecordPeepholeRewrite(cf, pc, peepholeRewriteSnapshotFields, pc)
	return true
}

// buildSnapshotCapture allocates a register per field and builds the capture words and
// the moves that replace each field read.
//
// With debug variables, the copy is kept as a debugger-only snapshot so the aggregate
// stays inspectable, and the captures read from it.
//
// Takes cf (*program.CompiledFunction) which owns the body and register counts.
// Takes pc (int) which identifies the copy to replace.
// Takes fields ([]snapshotField) which lists the proven field reads and their uses.
//
// Returns the capture and false when a register bank is full.
func buildSnapshotCapture(cf *program.CompiledFunction, pc int, fields []snapshotField) (snapshotCapture, bool) {
	old := cf.Body
	capture := snapshotCapture{replacements: make(map[int]isa.Instruction), captures: nil, counts: cf.NumRegisters}
	receiver := old[pc].B
	if cf.DebugVarTable != nil {
		snapshot := old[pc]
		snapshot.C = engine.MoveGeneralModeDebugSnapshot
		capture.captures = append(capture.captures, snapshot)
		receiver = snapshot.A
	}
	for _, field := range fields {
		if capture.counts[field.kind] >= isa.GeneralRegisterBankSize {
			return capture, false
		}
		reg := uint8(capture.counts[field.kind])
		capture.counts[field.kind]++
		capture.captures = append(capture.captures, field.captureRead(reg, receiver)...)
		for _, use := range field.uses {
			field.replaceUse(capture.replacements, old, use, reg)
		}
	}
	return capture, true
}

// captureRead builds the words that read the captured field from receiver into reg.
//
// Takes reg (uint8) which is the capture register.
// Takes receiver (uint8) which is the general register holding the aggregate.
//
// Returns the capture words.
func (field snapshotField) captureRead(reg, receiver uint8) []isa.Instruction {
	read := field.read
	if field.width == wideFieldReadWords {
		read.B = reg
		read.C = receiver
		return []isa.Instruction{read, field.extension}
	}
	read.A = reg
	read.B = receiver
	return []isa.Instruction{read}
}

// replaceUse records the move that replaces the field read at use.
//
// Takes replacements (map[int]isa.Instruction) which receives the new words.
// Takes old ([]isa.Instruction) which is the body before the rewrite.
// Takes use (int) which is the field read's program counter.
// Takes reg (uint8) which is the capture register.
func (field snapshotField) replaceUse(replacements map[int]isa.Instruction, old []isa.Instruction, use int, reg uint8) {
	inst := old[use]
	switch {
	case field.width == wideFieldReadWords:
		replacements[use] = isa.NewTier1Instruction(isa.SubOpMoveSliceByte, inst.B, reg)
		replacements[use+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	case inst.Op == isa.OpGetStructFieldSliceLen:
		replacements[use] = isa.NewTier1Instruction(isa.SubOpMoveInt, inst.A, reg)
	default:
		replacements[use] = emitMoveForTier0ReadOp(inst.Op, inst.A, reg)
	}
}

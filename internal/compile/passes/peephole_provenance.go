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
	"fmt"

	"pipit.sh/pipit/internal/engine/program"
)

const (
	// peepholeRewriteCseTier0 marks a tier-0 read that was rewritten to a same-bank MOVE
	// because a prior matching read still held the cached value.
	peepholeRewriteCseTier0 program.PeepholeRewriteKind = iota + 1

	// peepholeRewriteCseTier1Umbrella marks the umbrella word of a tier-1 read that was
	// rewritten to a same-bank tier-1 MOVE.
	peepholeRewriteCseTier1Umbrella

	// peepholeRewriteCseTier1Ext marks the EXT word of a CSE'd tier-1 read that was nopped
	// because the umbrella was rewritten to a move and no longer consumes its layout
	// extension.
	peepholeRewriteCseTier1Ext

	// peepholeRewriteCsePostSet marks a GET that was rewritten to a MOVE because a preceding
	// SET to the same field carried the stored value in a register still live at the read
	// site.
	peepholeRewriteCsePostSet

	// peepholeRewriteLicmHoist marks an instruction inserted at a loop pre-header by the
	// loop-invariant code motion pass.
	peepholeRewriteLicmHoist

	// peepholeRewriteGvn marks an instruction that GVN rewrote to a same-bank MOVE from an
	// earlier dominator-validated equivalent computation.
	peepholeRewriteGvn

	// peepholeRewriteBce marks a slice / string index access whose runtime bounds check was
	// elided in favour of the unchecked opcode variant. origin records the PC of the proof
	// source (the range loop header, or the guarding LtInt comparison).
	peepholeRewriteBce

	// PeepholeRewriteUnroll marks the leading instruction of an inlined copy of a
	// self-recursive callee body. origin records the PC of the original isa.SubOpCall site
	// that the inliner replaced.
	PeepholeRewriteUnroll

	// PeepholeRewriteInline marks the leading instruction of an inlined callee body. origin
	// records the PC of the isa.SubOpCall site the inliner replaced and originFunction the
	// callee's index in the enclosing function table.
	PeepholeRewriteInline
)

// RecordPeepholeRewrite stores an annotation describing the rewrite applied at pc. Lazily
// allocates the side table on first use; cheap because the table is touched only at
// compile-time peephole rewrites.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes pc (int) which is the PC of the rewritten instruction.
// Takes kind (program.PeepholeRewriteKind) which classifies the rewrite.
// Takes origin (int) which is the PC the rewrite derives from (or any non-negative value
// when origin is not meaningful for the kind).
func RecordPeepholeRewrite(compiledFunction *program.CompiledFunction, pc int, kind program.PeepholeRewriteKind, origin int) {
	if compiledFunction.PeepholeProvenance == nil {
		compiledFunction.PeepholeProvenance = make(map[int]program.PeepholeAnnotation)
	}
	compiledFunction.PeepholeProvenance[pc] = program.PeepholeAnnotation{Kind: kind, Origin: origin, OriginFunction: -1}
}

// RecordInlineRewrite stores a PeepholeRewriteInline annotation at pc, naming the
// isa.SubOpCall site the inliner replaced and the callee's function-table index.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes pc (int) which is the first PC of the inlined body.
// Takes originPC (int) which is the replaced isa.SubOpCall PC.
// Takes originFunction (int) which is the callee's index in the enclosing function table
// (-1 when unknown).
func RecordInlineRewrite(compiledFunction *program.CompiledFunction, pc, originPC, originFunction int) {
	if compiledFunction.PeepholeProvenance == nil {
		compiledFunction.PeepholeProvenance = make(map[int]program.PeepholeAnnotation)
	}
	compiledFunction.PeepholeProvenance[pc] = program.PeepholeAnnotation{Kind: PeepholeRewriteInline, Origin: originPC, OriginFunction: originFunction}
}

// PeepholeAnnotationAt returns the annotation recorded for pc.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes pc (int) which is the program counter to look up.
//
// Returns the recorded annotation, or the zero value when no annotation exists for that
// PC.
func PeepholeAnnotationAt(compiledFunction *program.CompiledFunction, pc int) program.PeepholeAnnotation {
	if compiledFunction.PeepholeProvenance == nil {
		return program.PeepholeAnnotation{}
	}
	return compiledFunction.PeepholeProvenance[pc]
}

// FormatPeepholeAnnotation renders an annotation as the trailing comment text appended to
// a disassembled instruction line.
//
// Takes ann (PeepholeAnnotation) which is the annotation recorded by the peephole pass.
//
// Returns the trailing comment text, or an empty string for the zero annotation so
// callers can fall through to other comment producers.
func FormatPeepholeAnnotation(ann program.PeepholeAnnotation) string {
	switch ann.Kind {
	case peepholeRewriteCseTier0:
		return fmt.Sprintf("CSE'd from PC %d", ann.Origin)
	case peepholeRewriteCseTier1Umbrella:
		return fmt.Sprintf("CSE'd from PC %d (tier-1 read)", ann.Origin)
	case peepholeRewriteCseTier1Ext:
		return "CSE'd EXT word"
	case peepholeRewriteCsePostSet:
		return fmt.Sprintf("CSE'd from SET at PC %d", ann.Origin)
	case peepholeRewriteLicmHoist:
		return fmt.Sprintf("LICM hoist of PC %d", ann.Origin)
	case peepholeRewriteGvn:
		return fmt.Sprintf("GVN'd from PC %d", ann.Origin)
	case peepholeRewriteBce:
		return fmt.Sprintf("BCE: bounds-check elided (proof from PC %d)", ann.Origin)
	case PeepholeRewriteUnroll:
		return fmt.Sprintf("inlined self-recursive body of call at PC %d", ann.Origin)
	case PeepholeRewriteInline:
		return fmt.Sprintf("inlined body of function %d called at PC %d", ann.OriginFunction, ann.Origin)
	}
	return ""
}

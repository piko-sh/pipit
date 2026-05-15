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

package program

import (
	"math"

	"pipit.sh/pipit/internal/isa"
)

// InlineRefusal records why a callee was rejected for inlining, cached on the callee with
// the zero value (InlineRefusalUnknown) meaning unprobed.
type InlineRefusal uint8

const (
	// InlineRefusalUnknown is the zero value meaning the callee has not been probed.
	InlineRefusalUnknown InlineRefusal = iota

	// InlineEligible marks a callee that passed every refusal check.
	InlineEligible

	// InlineRefusalUpvalues refuses callees with closure-captured variables.
	InlineRefusalUpvalues

	// InlineRefusalDefer refuses callees whose body contains isa.OpDefer.
	InlineRefusalDefer

	// InlineRefusalGo refuses callees whose body contains isa.OpGo.
	InlineRefusalGo

	// InlineRefusalRecursion refuses callees that sit inside an SCC of the call graph.
	InlineRefusalRecursion

	// InlineRefusalMethodCall refuses callees whose body contains isa.SubOpCallMethod.
	InlineRefusalMethodCall

	// InlineRefusalNativeCall refuses callees whose body contains isa.SubOpCallNative.
	InlineRefusalNativeCall

	// InlineRefusalTailCall refuses callees whose body contains isa.SubOpTailCall.
	InlineRefusalTailCall

	// InlineRefusalGenericPlaceholder refuses uninstantiated generic placeholders.
	InlineRefusalGenericPlaceholder

	// InlineRefusalClosureOps refuses callees using makeClosure / get / set / sync upvalue
	// ops.
	InlineRefusalClosureOps

	// InlineRefusalChannelOps refuses callees using isa.SubOpSelect, isa.OpChannelSend, or
	// recv ops.
	InlineRefusalChannelOps

	// InlineRefusalVariadic refuses callees that cannot be packed at the call site.
	InlineRefusalVariadic

	// InlineRefusalNoBody refuses callees with an empty body (forward declarations).
	InlineRefusalNoBody

	// InlineRefusalSiteIndirect refuses sites that are native, closure, or method.
	InlineRefusalSiteIndirect

	// InlineRefusalOversize refuses callees whose hairiness score exceeds the budget.
	InlineRefusalOversize

	// InlineRefusalCapWatermark refuses splices that would push numRegisters past 256.
	InlineRefusalCapWatermark

	// InlineRefusalCallerCap refuses sites once the caller has hit maxInlinesPerCaller.
	InlineRefusalCallerCap

	// InlineRefusalAlreadyUnrolled refuses self-recursive sites that have already been
	// unrolled once (preventing infinite expansion).
	InlineRefusalAlreadyUnrolled

	// InlineRefusalSelfHairy refuses self-recursive sites whose callee hairiness exceeds
	// SelfUnrollBudget.
	InlineRefusalSelfHairy

	// InlineRefusalSelfInLoop refuses self-recursive unrolling at call sites inside a loop
	// body, avoiding inflated hot-loop bytecode.
	InlineRefusalSelfInLoop

	// InlineRefusalUnrollDisabled refuses self-recursive unrolling when the build-tag flag
	// turns it off.
	InlineRefusalUnrollDisabled

	// InlineRefusalSpecialisedMethod refuses the monomorphised body of a Go 1.27 generic
	// method.
	InlineRefusalSpecialisedMethod

	// InlineRefusalRecover refuses a body that calls recover (directly or in a nested
	// literal): inlined into a deferred callee it would run in the frame recover is eligible
	// for, while Go only honours recover called by the deferred function itself.
	InlineRefusalRecover
)

// BlockerForInstruction returns the refusal implied by an instruction appearing in a
// callee. Takes the whole instruction because sub-tier-0 ops share op byte 0 and need the
// drill cascade to identify.
//
// Takes inst (isa.Instruction) which is the instruction under inspection.
//
// Returns the InlineRefusal kind, or InlineRefusalUnknown when the instruction does not
// block inlining.
func BlockerForInstruction(inst isa.Instruction) InlineRefusal {
	if isa.InstrIsTier2SubOp(inst, isa.SubOpTier2SyncClosureUpvalues) ||
		isa.InstrIsTier3SubOp(inst, isa.SubOpTier3SyncIIFEUpvalues) {
		return InlineRefusalClosureOps
	}
	if inst.Op == isa.OpDrillTier1 {
		return tier1Blocker(isa.SubOpcode(inst.A))
	}
	switch inst.Op {
	case isa.OpDefer:
		return InlineRefusalDefer
	case isa.OpGo:
		return InlineRefusalGo
	case isa.OpMakeClosure, isa.OpGetUpvalue, isa.OpSetUpvalue:
		return InlineRefusalClosureOps
	case isa.OpChannelSend:
		return InlineRefusalChannelOps
	default:
	}
	return InlineRefusalUnknown
}

// tier1Blocker returns the refusal implied by a tier-1 sub-op appearing in a callee.
//
// Takes sub (isa.SubOpcode) which is the tier-1 sub-opcode.
//
// Returns the InlineRefusal kind, or InlineRefusalUnknown when the sub-op does not block
// inlining.
func tier1Blocker(sub isa.SubOpcode) InlineRefusal {
	switch sub {
	case isa.SubOpCallMethod, isa.SubOpCallMethodInlineable:
		return InlineRefusalMethodCall
	case isa.SubOpCallNative:
		return InlineRefusalNativeCall
	case isa.SubOpTailCall:
		return InlineRefusalTailCall
	case isa.SubOpCallIIFE:
		return InlineRefusalClosureOps
	case isa.SubOpSelect:
		return InlineRefusalChannelOps
	default:
	}
	return InlineRefusalUnknown
}

// FitsJumpOffset reports whether offset fits the signed 16-bit jump encoding.
//
// Takes offset (int) which is the prospective PC-relative jump offset.
//
// Returns true when the offset fits within [math.MinInt16, math.MaxInt16].
func FitsJumpOffset(offset int) bool {
	return offset >= math.MinInt16 && offset <= math.MaxInt16
}

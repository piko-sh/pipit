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

// Options selects which optimisations a compilation runs.
//
// Every optimisation is on under DefaultOptions. A host switches one off, through the
// service option that carries the Options into the compiler, to confirm whether that
// optimisation causes a suspected miscompile without rebuilding the binary. The zero
// value disables everything, so callers start from DefaultOptions and clear the fields
// they want off.
type Options struct {
	// BCE enables bounds-check elimination over typed-bank bytecode.
	BCE bool

	// CSE enables common-subexpression elimination on struct-field reads.
	CSE bool

	// GVN enables global value numbering.
	GVN bool

	// LICM enables loop-invariant struct-field read hoisting.
	LICM bool

	// UnrollSelfRecursive enables one-level self-recursive unrolling in the inliner.
	UnrollSelfRecursive bool

	// SIMDKernels enables the SIMD AST-pattern recogniser, which detects canonical numerical
	// loop shapes and emits SIMD sub-opcodes.
	SIMDKernels bool

	// LoopUnroll enables the constant-bound loop unroller recogniser.
	LoopUnroll bool

	// ArenaPromotion enables the escape pass's arena annotations. Off, every allocation
	// takes the heap path, isolating arena faults from compiler faults.
	ArenaPromotion bool
}

// DefaultOptions returns the production configuration: every optimisation enabled.
//
// Returns Options with every field set to true.
func DefaultOptions() Options {
	return Options{
		BCE:                 true,
		CSE:                 true,
		GVN:                 true,
		LICM:                true,
		UnrollSelfRecursive: true,
		SIMDKernels:         true,
		LoopUnroll:          true,
		ArenaPromotion:      true,
	}
}

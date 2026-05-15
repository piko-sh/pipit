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

// TinyLeafShape classifies trivial method callees that handleCallMethod can execute
// inline in the caller's frame, bypassing pushCompiledFrame.
type TinyLeafShape uint8

const (
	// TinyLeafNone marks a body that does not match any recognised shape; dispatch falls
	// back to pushCompiledFrame. Default for all functions.
	TinyLeafNone TinyLeafShape = iota

	// TinyLeafReturnUintField marks a `return recv.fieldX` body for a uint field.
	TinyLeafReturnUintField

	// TinyLeafReturnIntField marks a `return recv.fieldX` body where fieldX is an int-kind
	// field. Symmetric to TinyLeafReturnUintField.
	TinyLeafReturnIntField

	// TinyLeafReturnEnvUintAtIntFieldSlot marks a `return env[recv.slot]` body where slot is
	// an int-kind field and env is a uint slice parameter.
	TinyLeafReturnEnvUintAtIntFieldSlot

	// TinyLeafIncIntField marks a recv.fieldX++ body with no return value, emitted as
	// isa.SubOpIncStructFieldInt on the receiver.
	TinyLeafIncIntField

	// TinyLeafIncUintField is the uint-field sibling of TinyLeafIncIntField, emitted as
	// isa.SubOpIncStructFieldUint.
	TinyLeafIncUintField

	// TinyLeafSetScalarFieldFromArg marks a recv.fieldX = argument body with no return
	// value.
	TinyLeafSetScalarFieldFromArg
)

// FusedEvalShape classifies an interpreted method body as a recognised expression-tree
// evaluator shape executed by runFusedEvalShape with no frame pushes.
type FusedEvalShape uint8

const (
	// fusedEvalUnclassified is the zero value: the body has not been examined yet.
	// Classification is lazy (first dispatch) because the matchers validate call-site
	// indices and CallSites are appended after Optimise() runs.
	fusedEvalUnclassified FusedEvalShape = iota //nolint:unused // names the zero value

	// FusedEvalNone marks a body that does not match any recognised evaluator family.
	FusedEvalNone

	// FusedEvalAddMasked is uint32(childA.Eval + childB.Eval) with 32-bit truncation.
	FusedEvalAddMasked

	// FusedEvalSubMasked is the subtraction twin of FusedEvalAddMasked.
	FusedEvalSubMasked

	// FusedEvalMulMasked is the multiplication twin of FusedEvalAddMasked.
	FusedEvalMulMasked

	// FusedEvalModGuarded evaluates childA first; a zero childA yields zero, otherwise
	// childB.Eval % childA (the canonical source fetches the divisor child first).
	FusedEvalModGuarded

	// FusedEvalMin returns the smaller of the two child evaluations.
	FusedEvalMin

	// FusedEvalMax returns the larger of the two child evaluations.
	FusedEvalMax

	// FusedEvalIfPos evaluates childA; non-zero selects childB, zero selects childC.
	FusedEvalIfPos
)

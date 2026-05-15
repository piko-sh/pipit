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

package verify

import (
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// checkTypeSwitchCaseForward records a verification error when a type-switch table row
// jumps backwards. The jump-table dispatcher never polls the cancellation budget on the
// taken edge, so a backward target could form an unpollable loop; the Compiler only ever
// emits forward rows and hand-built bytecode must do the same.
//
// Takes compiledFunction (*CompiledFunction) which names the function in the report.
// Takes pc (int) which is the row's program counter.
// Takes instr (instruction) which is the instruction under inspection.
// Takes report (*VerificationReport) which collects errors.
func checkTypeSwitchCaseForward(compiledFunction *program.CompiledFunction, pc int, instr isa.Instruction, report *VerificationReport) {
	if instr.Op != isa.OpTypeSwitchCase || instr.SignedOffset() >= 0 {
		return
	}
	report.errors = append(report.errors, verificationError{functionName: compiledFunction.Name,
		pc:          pc,
		instruction: instr,
		operand:     "B",
		reason:      "type switch table row jumps backwards", bank: 0, slot: 0})
}

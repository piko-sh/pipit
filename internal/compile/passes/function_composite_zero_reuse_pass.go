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
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// PromoteCompositeZeroReuse rewrites struct and array zero-constant loads into
// isa.OpLoadCompositeZeroReuse wherever the destination register's storage is provably
// frame-confined (ArenaSafeAllocPCs) and unaliased by any other live general register.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
func PromoteCompositeZeroReuse(compiledFunction *program.CompiledFunction) {
	if compiledFunction == nil || len(compiledFunction.Body) == 0 {
		return
	}
	promoteCompositeZeroReuse(compiledFunction, newFunctionAnalysis(compiledFunction))
}

// promoteCompositeZeroReuse is PromoteCompositeZeroReuse with the jump-target set
// supplied by the caller, so the per-site liveness scans share one decode.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes analysis (*FunctionAnalysis) which supplies the jump-target set on demand.
func promoteCompositeZeroReuse(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) {
	if compiledFunction == nil || len(compiledFunction.Body) == 0 || compiledFunction.AliasInfo == nil {
		return
	}
	jumpTargets := analysis.JumpTargets()
	for index := range compiledFunction.Body {
		instruction := compiledFunction.Body[index]
		if instruction.Op != isa.OpLoadGeneralConst {
			continue
		}
		if !loadsCompositeZero(compiledFunction, instruction) {
			continue
		}
		if !compositeZeroReuseSiteIsSafe(compiledFunction, index, instruction.A, jumpTargets) {
			continue
		}
		compiledFunction.Body[index] = isa.NewInstruction(isa.OpLoadCompositeZeroReuse,
			instruction.A, instruction.B, instruction.C)
	}
}

// loadsCompositeZero reports whether the load's constant is a struct or array zero value.
//
// Takes compiledFunction (*program.CompiledFunction) which carries the constant pool.
// Takes instruction (isa.Instruction) which is the candidate load.
//
// Returns true when the referenced constant is a zero struct or array.
func loadsCompositeZero(compiledFunction *program.CompiledFunction, instruction isa.Instruction) bool {
	index := int(instruction.WideIndex())
	if index < 0 || index >= len(compiledFunction.GeneralConstants) {
		return false
	}
	constant := compiledFunction.GeneralConstants[index]
	if !constant.IsValid() {
		return false
	}
	switch constant.Kind() {
	case reflect.Struct, reflect.Array:
		return constant.IsZero()
	default:
		return false
	}
}

// compositeZeroReuseSiteIsSafe reports whether the load at pc may be promoted to the
// reuse form.
//
// Requires the escape pass's frame-local verdict for this site and the absence of any
// other live general register that may alias the destination. A dead register cannot
// observe the zeroing, so liveness is checked before the alias query, exactly as the
// in-place append gate does.
//
// Takes compiledFunction (*program.CompiledFunction) which carries the verdicts.
// Takes pc (int) which is the candidate site.
// Takes destinationRegister (uint8) which is the register whose storage would be reused.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true only when both conditions hold.
func compositeZeroReuseSiteIsSafe(compiledFunction *program.CompiledFunction, pc int, destinationRegister uint8, jumpTargets map[int]bool) bool {
	if !compiledFunction.ArenaSafeAllocPCs[pc] {
		return false
	}
	peak := compiledFunction.NumRegisters[isa.RegisterGeneral]
	if peak == 0 {
		return true
	}
	for register := range peak {
		other := uint8(register)
		if other == destinationRegister {
			continue
		}
		if generalRegisterIsDeadAfter(compiledFunction, compiledFunction.Body, len(compiledFunction.Body), pc, other, jumpTargets) {
			continue
		}
		if compiledFunction.AliasInfo.MayAlias(pc, destinationRegister, other) {
			return false
		}
	}
	return true
}

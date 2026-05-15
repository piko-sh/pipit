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

package inline

import (
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// RewriteInlineableMethodCalls promotes isa.SubOpCallMethod instructions whose call-site
// signature matches a known fused-dispatch shape to isa.SubOpCallMethodInlineable.
//
// Takes compiledFunction (*CompiledFunction) whose body is scanned and selectively
// promoted.
func RewriteInlineableMethodCalls(compiledFunction *program.CompiledFunction) {
	for index := range compiledFunction.Body {
		instr := compiledFunction.Body[index]
		if !isa.InstrIsTier1SubOp(instr, isa.SubOpCallMethod) {
			continue
		}
		siteIndex := instr.WideIndex()
		if int(siteIndex) >= len(compiledFunction.CallSites) {
			continue
		}
		site := &compiledFunction.CallSites[siteIndex]
		if !program.IsInlineableShape(site) {
			continue
		}

		compiledFunction.Body[index] = isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, instr.B, instr.C)
	}
}

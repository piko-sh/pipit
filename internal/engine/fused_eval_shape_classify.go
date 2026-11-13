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

package engine

import (
	"reflect"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// fusedChildFetchWords is the instruction span of one interface-child fetch sequence.
	fusedChildFetchWords = 3

	// fusedTruncateWidth is the operand bit width the masked-arithmetic families truncate
	// to.
	fusedTruncateWidth = 32

	// fusedReceiverRegister is the general register a method's receiver arrives in.
	fusedReceiverRegister = 0

	// fusedResultRegister is the uint register a node returns its value in.
	fusedResultRegister = 0

	// fusedParameterCount counts the receiver and environment parameters of a node method.
	fusedParameterCount = 2

	// fusedReturnedValues is the number of values a node method returns.
	fusedReturnedValues = 1
)

// fusedChild is one matched child of a node.
type fusedChild struct {
	// layout describes the interface field that holds the child.
	layout program.StructFieldLayout

	// site is the index of the call site that evaluates the child.
	site uint16

	// result is the uint register the call writes.
	result uint8
}

// fusedBody is a method body as the classifier reads it, with its padding words left out.
//
// A classified method is evaluated without running its bytecode, so a family is matched
// only when every word of the body, and every register those words name, is the one the
// family's evaluator stands in for.
type fusedBody struct {
	// function is the method being classified.
	function *program.CompiledFunction

	// words are the body's words other than padding.
	words []isa.Instruction

	// positions holds the program counter of each of words.
	positions []int

	// environmentKind is the bank the environment parameter arrives in.
	environmentKind isa.RegisterKind

	// environmentRegister is the register of that bank it arrives in.
	environmentRegister uint8
}

// child matches the three words that read an interface child of the receiver and call a
// method on it with the environment the node was given.
//
// Takes at (int) which is the position of the first of them.
//
// Returns fusedChild which is the matched child.
// Returns bool which is false when the words are anything else.
func (b *fusedBody) child(at int) (fusedChild, bool) {
	if at < 0 || at+fusedChildFetchWords > len(b.words) {
		return fusedChild{}, false
	}
	read, call, extension := b.words[at], b.words[at+1], b.words[at+2]
	if read.Op != isa.OpGetStructFieldGeneral || read.B != fusedReceiverRegister || b.holdsAParameter(read.A) {
		return fusedChild{}, false
	}
	if !isa.InstrIsTier1SubOp(call, isa.SubOpCallMethodInlineable) || extension.Op != isa.OpExtension {
		return fusedChild{}, false
	}
	if int(read.C) >= len(b.function.StructLayoutTable) {
		return fusedChild{}, false
	}
	layout := b.function.StructLayoutTable[read.C]
	if reflect.Kind(layout.Kind) != reflect.Interface {
		return fusedChild{}, false
	}
	siteIndex, isCall := isa.CallSiteIndex(call)
	if !isCall || int(siteIndex) >= len(b.function.CallSites) {
		return fusedChild{}, false
	}
	result, ok := b.callResult(&b.function.CallSites[siteIndex], read.A)
	if !ok {
		return fusedChild{}, false
	}
	return fusedChild{layout: layout, site: siteIndex, result: result}, true
}

// holdsAParameter reports whether a general register holds the receiver or the
// environment, which a child fetch may not overwrite.
//
// Takes register (uint8) which is the general register.
//
// Returns bool which is true for a parameter's register.
func (b *fusedBody) holdsAParameter(register uint8) bool {
	return register == fusedReceiverRegister ||
		b.environmentKind == isa.RegisterGeneral && register == b.environmentRegister
}

// callResult checks that a call site passes a fetched child and the node's own
// environment, and names the register it returns into.
//
// Takes site (*program.CallSite) which is the call site.
// Takes receiver (uint8) which is the general register holding the fetched child.
//
// Returns uint8 which is the uint register the call writes.
// Returns bool which is false when the site passes or returns anything else.
func (b *fusedBody) callResult(site *program.CallSite, receiver uint8) (uint8, bool) {
	if site.IsClosure || site.IsNative || len(site.Arguments) != fusedParameterCount || len(site.Returns) != fusedReturnedValues {
		return 0, false
	}
	if !namesRegister(site.Arguments[0], isa.RegisterGeneral, receiver) ||
		!namesRegister(site.Arguments[1], b.environmentKind, b.environmentRegister) {
		return 0, false
	}
	returned := site.Returns[0]
	if !isPlainRegister(returned, isa.RegisterUint64) {
		return 0, false
	}
	return returned.Register, true
}

// returnOf matches the words that return a uint register.
//
// Takes at (int) which is the position of the first of them.
// Takes register (uint8) which is the uint register returned.
//
// Returns next (int) which is the position after the return.
// Returns ok (bool) which is false when the words are anything else.
func (b *fusedBody) returnOf(at int, register uint8) (next int, ok bool) {
	if register != fusedResultRegister {
		if at >= len(b.words) || b.words[at] != isa.NewTier1Instruction(isa.SubOpMoveUint64, fusedResultRegister, register) {
			return 0, false
		}
		at++
	}
	if at >= len(b.words) || b.words[at] != isa.NewTier2Instruction(isa.SubOpTier2Return, fusedReturnedValues) {
		return 0, false
	}
	return at + 1, true
}

// truncatedReturnOf matches the truncation of a uint register to 32 bits and the words
// that return it.
//
// Takes at (int) which is the position of the truncation.
// Takes register (uint8) which is the uint register truncated and returned.
//
// Returns next (int) which is the position after the return.
// Returns ok (bool) which is false when the words are anything else.
func (b *fusedBody) truncatedReturnOf(at int, register uint8) (next int, ok bool) {
	truncation := isa.NewInstruction(isa.OpTruncateToWidth, register, fusedTruncateWidth, uint8(isa.RegisterUint64))
	if at >= len(b.words) || b.words[at] != truncation {
		return 0, false
	}
	return b.returnOf(at+1, register)
}

// zeroLoad matches the load of zero into a uint register.
//
// Takes at (int) which is the position of the load.
//
// Returns register (uint8) which is the register loaded.
// Returns ok (bool) which is false when the word is anything else.
func (b *fusedBody) zeroLoad(at int) (register uint8, ok bool) {
	if at >= len(b.words) {
		return 0, false
	}
	load := b.words[at]
	return load.B, load == isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, load.B, 0)
}

// jumpLandsOn reports whether the jump at a position lands on another one.
//
// A jump onto padding lands on the word after it.
//
// Takes at (int) which is the position of the jump.
// Takes target (int) which is the position it has to land on.
//
// Returns bool which is false for any other target and for a word that is not a jump with
// a single target.
func (b *fusedBody) jumpLandsOn(at, target int) bool {
	if at >= len(b.words) || target >= len(b.words) {
		return false
	}
	jump, programCounter := b.words[at], b.positions[at]
	var landing int
	switch layout, trailingPadding := isa.JumpLayoutOf(jump); layout {
	case isa.JumpLayoutWord:
		landing = programCounter + 1 + int(isa.JoinOffset(jump.B, jump.C))
	case isa.JumpLayoutExtension:
		if at+1 >= len(b.words) || b.positions[at+1] != programCounter+1 || b.words[at+1].Op != isa.OpExtension {
			return false
		}
		extension := b.words[at+1]
		landing = programCounter + 1 + 1 + trailingPadding + int(isa.JoinOffset(extension.A, extension.B))
	default:
		return false
	}
	index, _ := slices.BinarySearch(b.positions, landing)
	return index == target
}

// maskedBinary matches a node that adds, subtracts or multiplies its two children and
// keeps the low 32 bits.
//
// Returns program.FusedEvalShape which is the family, or FusedEvalNone.
// Returns [fusedEvalChildren]fusedChild which holds the two children.
func (b *fusedBody) maskedBinary() (program.FusedEvalShape, [fusedEvalChildren]fusedChild) {
	var children [fusedEvalChildren]fusedChild
	left, right, ok := b.twoChildren()
	if !ok || len(b.words) <= 2*fusedChildFetchWords {
		return program.FusedEvalNone, children
	}
	arithmetic := b.words[2*fusedChildFetchWords]
	var shape program.FusedEvalShape
	switch arithmetic.Op {
	case isa.OpAddUint64:
		shape = program.FusedEvalAddMasked
	case isa.OpSubtractUint64:
		shape = program.FusedEvalSubMasked
	case isa.OpMultiplyUint64:
		shape = program.FusedEvalMulMasked
	default:
		return program.FusedEvalNone, children
	}
	if arithmetic.B != left.result || arithmetic.C != right.result {
		return program.FusedEvalNone, children
	}
	if end, returned := b.truncatedReturnOf(2*fusedChildFetchWords+1, arithmetic.A); !returned || end != len(b.words) {
		return program.FusedEvalNone, children
	}
	children[0], children[1] = left, right
	return shape, children
}

// minMax matches a node that returns the smaller or the larger of its two children.
//
// Returns program.FusedEvalShape which is the family, or FusedEvalNone.
// Returns [fusedEvalChildren]fusedChild which holds the two children.
func (b *fusedBody) minMax() (program.FusedEvalShape, [fusedEvalChildren]fusedChild) {
	var children [fusedEvalChildren]fusedChild
	const comparisonAt, branchAt = 2 * fusedChildFetchWords, 2*fusedChildFetchWords + 1
	left, right, ok := b.twoChildren()
	if !ok || len(b.words) <= branchAt {
		return program.FusedEvalNone, children
	}
	comparison, branch := b.words[comparisonAt], b.words[branchAt]
	var shape program.FusedEvalShape
	switch comparison.Op {
	case isa.OpLessThanUint64:
		shape = program.FusedEvalMin
	case isa.OpGreaterThanUint64:
		shape = program.FusedEvalMax
	default:
		return program.FusedEvalNone, children
	}
	if comparison.B != left.result || comparison.C != right.result ||
		branch.Op != isa.OpJumpIfFalse || branch.A != comparison.A {
		return program.FusedEvalNone, children
	}
	otherwise, returned := b.returnOf(branchAt+1, left.result)
	if !returned || !b.jumpLandsOn(branchAt, otherwise) {
		return program.FusedEvalNone, children
	}
	if end, returned := b.returnOf(otherwise, right.result); !returned || end != len(b.words) {
		return program.FusedEvalNone, children
	}
	children[0], children[1] = left, right
	return shape, children
}

// twoChildren matches the fetches of two children at the start of the body, each
// evaluated into a register of its own.
//
// Returns left (fusedChild) which is the first child.
// Returns right (fusedChild) which is the second.
// Returns ok (bool) which is false when the body starts with anything else.
func (b *fusedBody) twoChildren() (left, right fusedChild, ok bool) {
	left, ok = b.child(0)
	if !ok {
		return left, right, false
	}
	right, ok = b.child(fusedChildFetchWords)
	return left, right, ok && left.result != right.result
}

// guardedModulo matches a node that returns zero when its first child is zero and
// otherwise the remainder of its second child by its first, either kept to 32 bits or
// returned whole.
//
// Returns program.FusedEvalShape which is FusedEvalModGuarded, FusedEvalModGuardedWhole
// or FusedEvalNone.
// Returns [fusedEvalChildren]fusedChild which holds the divisor and then the dividend.
func (b *fusedBody) guardedModulo() (program.FusedEvalShape, [fusedEvalChildren]fusedChild) {
	var children [fusedEvalChildren]fusedChild
	const guardAt, zeroAt = fusedChildFetchWords, fusedChildFetchWords + 2
	divisor, ok := b.child(0)
	if !ok || len(b.words) <= guardAt {
		return program.FusedEvalNone, children
	}
	if b.words[guardAt] != isa.NewTier1Instruction(isa.SubOpEqualUint64ImmediateThenJumpIfFalse, divisor.result, 0) {
		return program.FusedEvalNone, children
	}
	zero, loaded := b.zeroLoad(zeroAt)
	if !loaded {
		return program.FusedEvalNone, children
	}
	dividendAt, returned := b.returnOf(zeroAt+1, zero)
	if !returned || !b.jumpLandsOn(guardAt, dividendAt) {
		return program.FusedEvalNone, children
	}
	dividend, ok := b.child(dividendAt)
	remainderAt := dividendAt + fusedChildFetchWords
	if !ok || dividend.result == divisor.result || remainderAt >= len(b.words) {
		return program.FusedEvalNone, children
	}
	remainder := b.words[remainderAt]
	if remainder.Op != isa.OpRemainderUint64 || remainder.B != dividend.result || remainder.C != divisor.result {
		return program.FusedEvalNone, children
	}
	shape := program.FusedEvalNone
	if end, returned := b.truncatedReturnOf(remainderAt+1, remainder.A); returned && end == len(b.words) {
		shape = program.FusedEvalModGuarded
	} else if end, returned := b.returnOf(remainderAt+1, remainder.A); returned && end == len(b.words) {
		shape = program.FusedEvalModGuardedWhole
	}
	if shape == program.FusedEvalNone {
		return program.FusedEvalNone, children
	}
	children[0], children[1] = divisor, dividend
	return shape, children
}

// conditional matches a node that returns its second child when its first is not zero and
// its third child otherwise.
//
// Returns program.FusedEvalShape which is FusedEvalIfPos, or FusedEvalNone.
// Returns [fusedEvalChildren]fusedChild which holds the condition and the two arms.
func (b *fusedBody) conditional() (program.FusedEvalShape, [fusedEvalChildren]fusedChild) {
	var children [fusedEvalChildren]fusedChild
	const zeroAt, testAt = fusedChildFetchWords, fusedChildFetchWords + 1
	condition, ok := b.child(0)
	if !ok {
		return program.FusedEvalNone, children
	}
	zero, loaded := b.zeroLoad(zeroAt)
	if !loaded || zero == condition.result {
		return program.FusedEvalNone, children
	}
	jumpAt, tested := b.testForNotEqual(testAt, condition.result, zero)
	if !tested {
		return program.FusedEvalNone, children
	}
	const thenAt = testAt + 2
	then, ok := b.child(thenAt)
	if !ok {
		return program.FusedEvalNone, children
	}
	otherwiseAt, returned := b.returnOf(thenAt+fusedChildFetchWords, then.result)
	if !returned || !b.jumpLandsOn(jumpAt, otherwiseAt) {
		return program.FusedEvalNone, children
	}
	otherwise, ok := b.child(otherwiseAt)
	if !ok {
		return program.FusedEvalNone, children
	}
	if end, returned := b.returnOf(otherwiseAt+fusedChildFetchWords, otherwise.result); !returned || end != len(b.words) {
		return program.FusedEvalNone, children
	}
	children[0], children[1], children[2] = condition, then, otherwise
	return program.FusedEvalIfPos, children
}

// testForNotEqual matches the two words that test two uint registers for inequality and
// jump when they are equal, as one fused instruction or as a comparison and a jump.
//
// Takes at (int) which is the position of the first of them.
// Takes left (uint8) which is one register compared.
// Takes right (uint8) which is the other.
//
// Returns jumpAt (int) which is the position of the word that jumps.
// Returns ok (bool) which is false when the words are anything else.
func (b *fusedBody) testForNotEqual(at int, left, right uint8) (jumpAt int, ok bool) {
	if at+1 >= len(b.words) {
		return 0, false
	}
	test, branch := b.words[at], b.words[at+1]
	if test.B != left || test.C != right {
		return 0, false
	}
	if isa.InstrIsTier1SubOp(test, isa.SubOpNotEqualUint64ThenJumpIfFalse) {
		return at, branch.Op == isa.OpExtension
	}
	return at + 1, test.Op == isa.OpNotEqualUint64 && branch.Op == isa.OpJumpIfFalse && branch.A == test.A
}

// newFusedBody reads a method body for classification.
//
// Takes compiledFunction (*program.CompiledFunction) which is the method.
//
// Returns *fusedBody which holds the body without its padding words.
// Returns bool which is false when the method does not take a receiver and an environment
// and return one unsigned value, so no family can describe it.
func newFusedBody(compiledFunction *program.CompiledFunction) (*fusedBody, bool) {
	parameters, results := compiledFunction.ParameterKinds, compiledFunction.ResultKinds
	if len(parameters) != fusedParameterCount || parameters[0] != isa.RegisterGeneral ||
		len(results) != fusedReturnedValues || results[0] != isa.RegisterUint64 {
		return nil, false
	}
	body := &fusedBody{
		function:            compiledFunction,
		words:               make([]isa.Instruction, 0, len(compiledFunction.Body)),
		positions:           make([]int, 0, len(compiledFunction.Body)),
		environmentKind:     parameters[1],
		environmentRegister: 0,
	}
	if body.environmentKind == isa.RegisterGeneral {
		body.environmentRegister = fusedReceiverRegister + 1
	}
	for programCounter, word := range compiledFunction.Body {
		if word == (isa.Instruction{}) {
			continue
		}
		body.words = append(body.words, word)
		body.positions = append(body.positions, programCounter)
	}
	return body, true
}

// classifyFusedEvalShape classifies a compiled function's body against the recognised
// evaluator families.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function whose
// EvalShape fields are populated on a match.
func classifyFusedEvalShape(compiledFunction *program.CompiledFunction) {
	compiledFunction.EvalShapeOnce.Do(func() { classifyFusedEvalShapeOnce(compiledFunction) })
}

// classifyFusedEvalShapeOnce runs the classification body exactly once per function under
// EvalShapeOnce.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to classify.
func classifyFusedEvalShapeOnce(compiledFunction *program.CompiledFunction) {
	compiledFunction.EvalShape = program.FusedEvalNone
	body, ok := newFusedBody(compiledFunction)
	if !ok {
		return
	}
	families := [...]func() (program.FusedEvalShape, [fusedEvalChildren]fusedChild){
		body.maskedBinary, body.minMax, body.guardedModulo, body.conditional,
	}
	for _, family := range families {
		shape, children := family()
		if shape == program.FusedEvalNone {
			continue
		}
		compiledFunction.EvalChildALayout, compiledFunction.EvalSiteA = children[0].layout, children[0].site
		compiledFunction.EvalChildBLayout, compiledFunction.EvalSiteB = children[1].layout, children[1].site
		compiledFunction.EvalChildCLayout, compiledFunction.EvalSiteC = children[2].layout, children[2].site
		compiledFunction.EvalShape = shape
		return
	}
}

// namesRegister reports whether a call operand is a plain register of a bank.
//
// Takes location (program.VarLocation) which is the operand.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes register (uint8) which is the register.
//
// Returns bool which is false for any other register and for an operand that is an
// upvalue, a spilled value or one held indirectly.
func namesRegister(location program.VarLocation, kind isa.RegisterKind, register uint8) bool {
	return location.Register == register && isPlainRegister(location, kind)
}

// isPlainRegister reports whether a call operand is a register of a bank, read and
// written directly.
//
// Takes location (program.VarLocation) which is the operand.
// Takes kind (isa.RegisterKind) which is the bank.
//
// Returns bool which is false for another bank and for an operand that is an upvalue, a
// captured or spilled value or one held indirectly.
func isPlainRegister(location program.VarLocation, kind isa.RegisterKind) bool {
	return location.Kind == kind && !location.IsUpvalue && !location.IsIndirect && !location.IsCaptured && !location.IsSpilled
}

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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type bytecodeBuilder struct {
	generalConstants []reflect.Value
	callSites        []program.CallSite
	floatConstants   []float64
	stringConstants  []string
	boolConstants    []bool
	uintConstants    []uint64
	typeTable        []reflect.Type
	body             []isa.Instruction
	intConstants     []int64
	resultKinds      []isa.RegisterKind
	parameterKinds   []isa.RegisterKind
	functions        []*program.CompiledFunction
	complexConstants []complex128
	numRegisters     [isa.NumRegisterKinds]uint32
}

func newBytecodeBuilder() *bytecodeBuilder {
	return &bytecodeBuilder{}
}

func (b *bytecodeBuilder) Emit(op isa.Opcode, a, bc, c uint8) *bytecodeBuilder {
	b.body = append(b.body, isa.Instruction{Op: op, A: a, B: bc, C: c})
	return b
}

func (b *bytecodeBuilder) EmitJump(op isa.Opcode, a uint8, offset int16) *bytecodeBuilder {
	unsigned := uint16(offset)
	low := uint8(unsigned & 0xFF)
	high := uint8(unsigned >> 8)
	b.body = append(b.body, isa.Instruction{Op: op, A: a, B: low, C: high})
	return b
}

func (b *bytecodeBuilder) emitExt(payload int32) *bytecodeBuilder {
	unsigned := uint32(payload)
	a := uint8(unsigned & 0xFF)
	bc := uint8((unsigned >> 8) & 0xFF)
	c := uint8((unsigned >> 16) & 0xFF)
	b.body = append(b.body, isa.Instruction{Op: isa.OpExt, A: a, B: bc, C: c})
	return b
}

func (b *bytecodeBuilder) addIntConst(v int64) uint8 {
	index := len(b.intConstants)
	b.intConstants = append(b.intConstants, v)
	return uint8(index)
}

func (b *bytecodeBuilder) addFloatConst(v float64) uint8 {
	index := len(b.floatConstants)
	b.floatConstants = append(b.floatConstants, v)
	return uint8(index)
}

func (b *bytecodeBuilder) addStringConst(v string) uint8 {
	index := len(b.stringConstants)
	b.stringConstants = append(b.stringConstants, v)
	return uint8(index)
}

func (b *bytecodeBuilder) addBoolConst(v bool) uint8 {
	index := len(b.boolConstants)
	b.boolConstants = append(b.boolConstants, v)
	return uint8(index)
}

func (b *bytecodeBuilder) addGeneralConst(v reflect.Value) uint8 {
	index := len(b.generalConstants)
	b.generalConstants = append(b.generalConstants, v)
	return uint8(index)
}

func (b *bytecodeBuilder) addType(t reflect.Type) uint8 {
	index := len(b.typeTable)
	b.typeTable = append(b.typeTable, t)
	return uint8(index)
}

func (b *bytecodeBuilder) intRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterInt] = n
	return b
}

func (b *bytecodeBuilder) floatRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterFloat] = n
	return b
}

func (b *bytecodeBuilder) stringRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterString] = n
	return b
}

func (b *bytecodeBuilder) generalRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterGeneral] = n
	return b
}

func (b *bytecodeBuilder) boolRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterBool] = n
	return b
}

func (b *bytecodeBuilder) uintRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterUint] = n
	return b
}

func (b *bytecodeBuilder) sliceIntRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterSliceInt] = n
	return b
}

func (b *bytecodeBuilder) sliceFloatRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterSliceFloat] = n
	return b
}

func (b *bytecodeBuilder) sliceStringRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterSliceString] = n
	return b
}

func (b *bytecodeBuilder) sliceBoolRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterSliceBool] = n
	return b
}

func (b *bytecodeBuilder) sliceUintRegisters(n uint32) *bytecodeBuilder {
	b.numRegisters[isa.RegisterSliceUint] = n
	return b
}

func (b *bytecodeBuilder) returnInt() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterInt}
	return b
}

func (b *bytecodeBuilder) returnFloat() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterFloat}
	return b
}

func (b *bytecodeBuilder) returnString() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterString}
	return b
}

func (b *bytecodeBuilder) returnBool() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterBool}
	return b
}

func (b *bytecodeBuilder) returnGeneral() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterGeneral}
	return b
}

func (b *bytecodeBuilder) AddCallSite(cs *program.CallSite) uint16 {
	index := len(b.callSites)
	b.callSites = append(b.callSites, *cs)
	return uint16(index)
}

func (b *bytecodeBuilder) addSubFunction(compiledFunction *program.CompiledFunction) uint16 {
	index := len(b.functions)
	b.functions = append(b.functions, compiledFunction)
	return uint16(index)
}

func (b *bytecodeBuilder) CurrentPC() int {
	return len(b.body)
}

func (b *bytecodeBuilder) build() *program.CompiledFunction {
	function := program.NewNamedFunction("test")
	function.Body = b.body
	function.IntConstants = b.intConstants
	function.FloatConstants = b.floatConstants
	function.StringConstants = b.stringConstants
	function.BoolConstants = b.boolConstants
	function.UintConstants = b.uintConstants
	function.GeneralConstants = b.generalConstants
	function.ComplexConstants = b.complexConstants
	function.NumRegisters = b.numRegisters
	function.ResultKinds = b.resultKinds
	function.ParameterKinds = b.parameterKinds
	function.Functions = b.functions
	function.CallSites = b.callSites
	function.TypeTable = b.typeTable

	return function
}

func findOpcode(compiledFunction *program.CompiledFunction, op isa.Opcode) int {
	for i, instr := range compiledFunction.Body {
		if instr.Op == op {
			return i
		}
	}
	return -1
}

func makeOpCallSlot(siteIdx uint16) isa.Instruction {
	return isa.NewTier1Instruction(isa.SubOpCall, byte(siteIdx&0xFF), byte(siteIdx>>8))
}

func requireContainsOpcode(t *testing.T, compiledFunction *program.CompiledFunction, op isa.Opcode) {
	t.Helper()
	for _, instr := range compiledFunction.Body {
		if instructionMatchesOpcode(instr, op) {
			return
		}
	}
	require.Failf(t, "opcode not found",
		"expected %s in bytecode:\n%s", op, compiledFunction.Disassemble())
}

func requireContainsTier1SubOp(t *testing.T, compiledFunction *program.CompiledFunction, subOp isa.SubOpcode) {
	t.Helper()
	for _, instr := range compiledFunction.Body {
		if isa.InstrIsTier1SubOp(instr, subOp) {
			return
		}
	}
	require.Failf(t, "tier-1 sub-op not found",
		"expected {opDrillTier1, %s, *, *} in bytecode:\n%s",
		subOp, compiledFunction.Disassemble())
}

func requireNoOpcode(t *testing.T, compiledFunction *program.CompiledFunction, op isa.Opcode) {
	t.Helper()
	for i, instr := range compiledFunction.Body {
		if instr.Op == op {
			require.Failf(t, "unexpected opcode found",
				"found %s at PC %d in bytecode:\n%s", op, i, compiledFunction.Disassemble())
		}
	}
}

func instructionMatchesOpcode(instr isa.Instruction, op isa.Opcode) bool {
	return instr.Op == op
}

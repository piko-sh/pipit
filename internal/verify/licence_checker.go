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
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// CheckerInstrKind is the kind of one abstract instruction.
type CheckerInstrKind uint8

const (
	// checkerNop reads and writes no general register.
	checkerNop CheckerInstrKind = iota

	// CheckerAlloc is a granted allocation.
	CheckerAlloc

	// CheckerMove copies a register.
	CheckerMove

	// CheckerStore stores a register into an object.
	CheckerStore

	// CheckerOp hands its sources off and redefines its destinations.
	CheckerOp

	// CheckerRead reads through its receiver without handing the receiver off.
	CheckerRead

	// checkerHavoc hands its sources off; any register may change.
	checkerHavoc

	// checkerUnclassified is a reference-carrying operation with no declared route.
	checkerUnclassified

	// checkerStoreValue stores a value held in no general register.
	checkerStoreValue
)

// CheckerInstr is one abstract instruction.
type CheckerInstr struct {
	// Sources and Destinations are the general registers read and written.
	Sources, Destinations []int

	// Receiver is a store's receiver register; Destination a move's or alloc's destination;
	// Source a move's or store's source.
	Receiver, Destination, Source int

	// Kind is the instruction's kind.
	Kind CheckerInstrKind

	// Granted is true for a granted store.
	Granted bool

	// Container marks a map allocation or a granted map store.
	Container bool
}

// CheckerProgram is a compiled function in the checker's abstract bytecode.
type CheckerProgram struct {
	// Name is the compiled function's name.
	Name string

	// Code holds one abstract instruction per pc.
	Code []CheckerInstr

	// Successors holds each pc's successors.
	Successors [][]int

	// Registers is the general register count.
	Registers int

	// Granted counts the granted stores.
	Granted int
}

// localContainerAllocOps marks operations that can allocate a map without other writes.
var localContainerAllocOps = flatOperationSet(isa.FlatIndexOfSub2(isa.SubOpTier2MakeMap))

// localContainerStoreOps marks map store operations eligible for local-container grants.
var localContainerStoreOps = flatOperationSet(
	isa.FlatIndexOf(isa.OpSetMapStringGeneral),
	isa.FlatIndexOf(isa.OpSetMapInt64General),
)

// localContainerAppendOps marks the map stores that grow the slice a map entry holds and
// store it back, so the value stored is in no general register.
var localContainerAppendOps = flatOperationSet(isa.FlatIndexOf(isa.OpAppendStringToMapString))

// localContainerReadOps marks map lookups whose operand B is the map.
//
// The comma-ok forms write an int register named by the extension word. The extension
// word is checked for naming no general register.
var localContainerReadOps = flatOperationSet(
	isa.FlatIndexOf(isa.OpGetMap), isa.FlatIndexOf(isa.OpGetMapOk),
	isa.FlatIndexOf(isa.OpGetMapInt64Int64), isa.FlatIndexOf(isa.OpGetMapInt64Int64Ok),
	isa.FlatIndexOf(isa.OpGetMapStringInt64), isa.FlatIndexOf(isa.OpGetMapStringInt64Ok),
	isa.FlatIndexOf(isa.OpGetMapStringString), isa.FlatIndexOf(isa.OpGetMapStringStringOk),
	isa.FlatIndexOf(isa.OpGetMapInt64String), isa.FlatIndexOf(isa.OpGetMapInt64StringOk),
	isa.FlatIndexOf(isa.OpGetMapInt64General), isa.FlatIndexOf(isa.OpGetMapInt64GeneralOk),
	isa.FlatIndexOf(isa.OpGetMapStringGeneral), isa.FlatIndexOf(isa.OpGetMapStringGeneralOk),
	isa.FlatIndexOf(isa.OpGetMapInt64Int64OkThenJumpIfFalse), isa.FlatIndexOf(isa.OpGetMapStringInt64OkThenJumpIfFalse),
	isa.FlatIndexOf(isa.OpGetMapStringStringOkThenJumpIfFalse), isa.FlatIndexOf(isa.OpGetMapInt64StringOkThenJumpIfFalse),
	isa.FlatIndexOf(isa.OpGetMapInt64GeneralOkThenJumpIfFalse), isa.FlatIndexOf(isa.OpGetMapStringGeneralOkThenJumpIfFalse),
)

// checkerGrants says which local-container grants a conversion applies.
type checkerGrants struct {
	// allocs lists the PCs converted as container allocations.
	allocs program.PCSet

	// stores lists the PCs converted as granted container stores.
	stores program.PCSet

	// every selects grant inference for all eligible map allocations and stores.
	every bool
}

// pureMoveOps marks the operations that copy one general register into another and do
// nothing else, indexed by flat (tier, code) slot so that renaming an operation cannot
// change the set.
var pureMoveOps = flatOperationSet(isa.FlatIndexOf(isa.OpMoveGeneral))

// flatOperationMarks is a set of operations indexed by flat (tier, code) slot.
type flatOperationMarks [isa.FlatDispatchSlots]bool

// has reports whether the operation at a flat slot is in the set.
//
// Takes flatIndex (int) which is a flat slot, possibly out of range.
//
// Returns bool which is true for a member.
func (marks *flatOperationMarks) has(flatIndex int) bool {
	return flatIndex >= 0 && flatIndex < len(marks) && marks[flatIndex]
}

// describedOperands is an instruction whose operand shape describes every general
// register it reads and writes.
type describedOperands struct {
	// reads lists the general registers the instruction reads.
	reads []int

	// writes lists the general registers the instruction writes.
	writes []int

	// shape describes the instruction's operands.
	shape isa.OperandShape

	// instr is the instruction.
	instr isa.Instruction
}

// siteCallOps marks the calls convertCall models over their call site, indexed by flat
// (tier, code) slot.
var siteCallOps = flatOperationSet(
	isa.FlatIndexOfSub1(isa.SubOpCall),
	isa.FlatIndexOfSub1(isa.SubOpCallNative),
	isa.FlatIndexOfSub1(isa.SubOpCallMethod),
	isa.FlatIndexOfSub1(isa.SubOpCallScalar),
	isa.FlatIndexOfSub1(isa.SubOpCallMethodInlineable),
)

// ConvertForLicenceChecker maps a compiled function to the checker's abstract bytecode.
//
// Takes function (*program.CompiledFunction) which is the function.
//
// Returns *CheckerProgram which is the conversion.
func ConvertForLicenceChecker(function *program.CompiledFunction) *CheckerProgram {
	return convertForLicenceChecker(function, checkerGrants{
		allocs: function.LocalContainerAllocPCs,
		stores: function.LocalContainerStorePCs,
		every:  false,
	})
}

// convertForLicenceChecker is ConvertForLicenceChecker under the given local-container
// grants.
//
// Takes function (*program.CompiledFunction) which is the function.
// Takes grants (checkerGrants) which says which map allocations and stores are converted
// as container allocations and granted container stores.
//
// Returns *CheckerProgram which is the conversion.
func convertForLicenceChecker(function *program.CompiledFunction, grants checkerGrants) *CheckerProgram {
	body := function.Body
	registerCount := int(function.NumRegisters[isa.RegisterGeneral])
	converted := &CheckerProgram{Name: function.Name, Registers: registerCount, Code: make([]CheckerInstr, len(body)), Successors: make([][]int, len(body)), Granted: 0}
	all := make([]int, registerCount)
	for r := range all {
		all[r] = r
	}
	for programCounter := 0; programCounter < len(body); {
		instr := body[programCounter]
		shape := isa.ShapeForInstruction(instr)
		converted.Code[programCounter] = convertOne(function, grants, programCounter, instr, shape, all)
		if converted.Code[programCounter].isStore() && converted.Code[programCounter].Granted {
			converted.Granted++
		}
		next := programCounter + 1
		if layout, _ := isa.JumpLayoutOf(instr); layout != isa.JumpLayoutNone {
			next = programCounter + program.JumpFootprint(instr)
		} else if shape.Flags&isa.ShapeFlagFollowsExtension != 0 {
			next = skipExtensionWords(body, next)
		}
		switch {
		case shape.Flags&isa.ShapeFlagTerminator != 0:
			converted.Successors[programCounter] = terminatorSuccessors(body, programCounter, instr)
		case shape.Flags&isa.ShapeFlagControlFlow != 0:
			converted.Successors[programCounter] = controlFlowSuccessors(body, programCounter, next)
		case next < len(body):
			converted.Successors[programCounter] = []int{next}
		}

		programCounter = max(next, programCounter+1)
	}
	return converted
}

// convertOne maps one instruction.
//
// Takes function (*program.CompiledFunction) which holds the grant tables.
// Takes grants (checkerGrants) which holds the local-container grants.
// Takes programCounter (int) which is the instruction's programCounter.
// Takes instr (isa.Instruction) which is the instruction.
// Takes shape (isa.OperandShape) which describes its operands.
// Takes all ([]int) which lists every general register.
//
// Returns CheckerInstr which is the abstract instruction.
func convertOne(
	function *program.CompiledFunction, grants checkerGrants, programCounter int, instr isa.Instruction, shape isa.OperandShape, all []int,
) CheckerInstr {
	if instr.Op == isa.OpExtension {
		return checkerInstrOf(checkerNop)
	}
	row, known := isa.SpecForInstruction(instr)
	if !known {
		return checkerInstrOf(checkerUnclassified)
	}
	if row.RequiresHandOff() && len(row.HandOff) == 0 {
		return checkerInstrOf(checkerUnclassified)
	}
	if row.Calls() && siteCallOps.has(isa.FlatIndexFor(instr)) {
		if call, ok := convertCall(function, instr, len(all)); ok {
			return call
		}
	}
	opaque := shape.Flags&isa.ShapeFlagOpaqueWrites != 0 && !localContainerReadOps.has(isa.FlatIndexFor(instr))
	extensionReads, extensionWrites, extensionKnown := extensionGeneralOperands(function.Body, programCounter, instr, shape, len(all))
	if shape.Flags&isa.ShapeFlagDescribed == 0 || opaque || !extensionKnown {
		return CheckerInstr{Kind: checkerHavoc, Sources: all, Destinations: nil, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}
	}
	reads, writes := generalOperands(instr, shape, len(all))
	operands := describedOperands{instr: instr, shape: shape, reads: append(reads, extensionReads...), writes: append(writes, extensionWrites...)}
	if container, ok := convertLocalContainer(function, grants, programCounter, operands); ok {
		return container
	}
	return convertDescribed(function, programCounter, row, operands)
}

// extensionGeneralOperands lists the general registers an instruction's extension words
// read and write.
//
// Takes body ([]isa.Instruction) which is the function's body.
// Takes programCounter (int) which is the instruction's programCounter.
// Takes instr (isa.Instruction) which is the instruction.
// Takes shape (isa.OperandShape) which describes its operands.
// Takes generals (int) which is the number of general registers.
//
// Returns reads ([]int) which lists the registers the words read.
// Returns writes ([]int) which lists the registers the words write.
// Returns known (bool) which is false when a word is not described for the general bank,
// or names a register outside it, so that the words may name any register.
func extensionGeneralOperands(
	body []isa.Instruction, programCounter int, instr isa.Instruction, shape isa.OperandShape, generals int,
) (reads, writes []int, known bool) {
	if shape.Flags&isa.ShapeFlagFollowsExtension == 0 || programCounter+1 >= len(body) {
		return nil, nil, true
	}
	end := skipExtensionWords(body, programCounter+1)
	for word := programCounter + 1; word < end; word++ {
		wordReads, wordWrites, described := isa.ExtensionRegisterUse(instr, body[programCounter+1:word+1], isa.RegisterGeneral)
		if !described {
			return nil, nil, false
		}
		reads, known = appendNamedRegisters(reads, body[word], wordReads, generals)
		if !known {
			return nil, nil, false
		}
		writes, known = appendNamedRegisters(writes, body[word], wordWrites, generals)
		if !known {
			return nil, nil, false
		}
	}
	return reads, writes, true
}

// appendNamedRegisters appends the general registers the marked bytes of one extension
// word name.
//
// Takes registers ([]int) which is the list to extend.
// Takes word (isa.Instruction) which is the extension word.
// Takes named (isa.ExtensionIntMask) which marks the bytes that name a register.
// Takes generals (int) which is the number of general registers.
//
// Returns []int which is the extended list.
// Returns bool which is false when a marked byte names a register outside the bank.
func appendNamedRegisters(registers []int, word isa.Instruction, named isa.ExtensionIntMask, generals int) ([]int, bool) {
	for index, register := range [isa.NumInstructionOperands]uint8{word.A, word.B, word.C} {
		if !named[index] {
			continue
		}
		if int(register) >= generals {
			return nil, false
		}
		registers = append(registers, int(register))
	}
	return registers, true
}

// convertLocalContainer maps map allocations, stores and lookups.
//
// A grant on an instruction that is not the operation it must be, or whose operands are
// not the shape the grant assumes, converts to an unclassified instruction, so a wrong
// table is rejected and never trusted.
//
// Takes function (*program.CompiledFunction) which holds the type table.
// Takes grants (checkerGrants) which holds the local-container grants.
// Takes programCounter (int) which is the instruction's programCounter.
// Takes operands (describedOperands) which is the instruction and its registers.
//
// Returns CheckerInstr which is the abstract instruction.
// Returns bool which is false when the instruction is none of these.
func convertLocalContainer(
	function *program.CompiledFunction, grants checkerGrants, programCounter int, operands describedOperands,
) (CheckerInstr, bool) {
	flatIndex := isa.FlatIndexFor(operands.instr)
	appends := localContainerAppendOps.has(flatIndex)
	allocates, stores := allocatesMap(function, programCounter, flatIndex), localContainerStoreOps.has(flatIndex) || appends
	switch {
	case grants.allocs.Has(programCounter) && !allocates, grants.stores.Has(programCounter) && !stores:
		return checkerInstrOf(checkerUnclassified), true
	case allocates && (grants.every || grants.allocs.Has(programCounter)):
		if len(operands.writes) != 1 || len(operands.reads) != 0 {
			return checkerInstrOf(checkerUnclassified), grants.allocs.Has(programCounter)
		}
		return CheckerInstr{Kind: CheckerAlloc, Destination: operands.writes[0], Container: true, Sources: nil, Destinations: nil, Receiver: 0, Source: 0, Granted: false}, true
	case stores:
		convert := convertGrantedStore
		if appends {
			convert = convertValueStore
		}
		store := convert(operands)
		if !store.isStore() {
			return store, grants.stores.Has(programCounter)
		}
		store.Granted = grants.every || grants.stores.Has(programCounter)
		store.Container = store.Granted
		return store, true
	case localContainerReadOps.has(flatIndex):
		return convertContainerRead(operands)
	default:
		return CheckerInstr{}, false
	}
}

// allocatesMap reports whether the instruction allocates a map.
//
// Takes function (*program.CompiledFunction) which holds the body and the type table.
// Takes programCounter (int) which is the instruction's programCounter.
// Takes flatIndex (int) which is the instruction's flat slot.
//
// Returns bool which is true when the instruction allocates a map.
func allocatesMap(function *program.CompiledFunction, programCounter, flatIndex int) bool {
	if !localContainerAllocOps.has(flatIndex) || programCounter+1 >= len(function.Body) {
		return false
	}
	extension := function.Body[programCounter+1]
	typeIndex := int(extension.A) | int(extension.B)<<isa.WideBitShift
	if extension.Op != isa.OpExtension || typeIndex >= len(function.TypeTable) {
		return false
	}
	allocated := function.TypeTable[typeIndex]
	return allocated != nil && allocated.Kind() == reflect.Map
}

// convertContainerRead maps a lookup through the map in operand B.
//
// Takes operands (describedOperands) which is the instruction and its registers.
//
// Returns CheckerInstr which is the read.
// Returns bool which is false when operand B is not a general register the lookup reads,
// in which case the caller converts the instruction as any other.
func convertContainerRead(operands describedOperands) (CheckerInstr, bool) {
	if operands.shape.B != isa.RoleRegGeneral || !operands.shape.Reads[1] {
		return CheckerInstr{}, false
	}
	receiver := int(operands.instr.B)
	return CheckerInstr{
		Kind: CheckerRead, Receiver: receiver, Sources: sourcesExcept(operands.reads, receiver), Destinations: operands.writes,
		Destination: 0, Source: 0, Granted: false, Container: false,
	}, true
}

// convertDescribed maps an instruction whose operands are described, by what its grants
// and opcode say it does.
//
// Takes function (*program.CompiledFunction) which holds the grant tables.
// Takes programCounter (int) which is the instruction's programCounter.
// Takes row (isa.OpSpec) which is the opcode's specification.
// Takes operands (describedOperands) which is the instruction and its registers.
//
// Returns CheckerInstr which is the abstract instruction.
func convertDescribed(function *program.CompiledFunction, programCounter int, row isa.OpSpec, operands describedOperands) CheckerInstr {
	reads, writes := operands.reads, operands.writes
	switch {
	case function.FieldStoreArenaSafePCs[programCounter]:
		return convertGrantedStore(operands)
	case function.ArenaSafeAllocPCs[programCounter] && len(writes) == 1 && len(reads) == 0:
		return CheckerInstr{Kind: CheckerAlloc, Destination: writes[0], Sources: nil, Destinations: nil, Receiver: 0, Source: 0, Granted: false, Container: false}
	case function.ArenaSafeAllocPCs[programCounter]:
		return CheckerInstr{Kind: CheckerOp, Sources: reads, Destinations: writes, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}
	case row.Calls():
		return CheckerInstr{Kind: checkerHavoc, Sources: reads, Destinations: nil, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}
	case pureMoveOps.has(isa.FlatIndexFor(operands.instr)) && len(reads) == 1 && len(writes) == 1:
		return CheckerInstr{Kind: CheckerMove, Destination: writes[0], Source: reads[0], Sources: nil, Destinations: nil, Receiver: 0, Granted: false, Container: false}
	case storesIntoContainer(row):
		return convertContainerStore(operands)
	case len(reads) == 0 && len(writes) == 0:
		return checkerInstrOf(checkerNop)
	default:
		return CheckerInstr{Kind: CheckerOp, Sources: reads, Destinations: writes, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}
	}
}

// convertGrantedStore maps a field store granted as arena safe.
//
// Takes operands (describedOperands) which is the instruction and its registers.
//
// Returns CheckerInstr which is the granted store, or unclassified when the operands are
// not one receiver, one source and at most one write.
func convertGrantedStore(operands describedOperands) CheckerInstr {
	receiver, ok := receiverOperand(operands.instr, operands.shape)
	sources := sourcesExcept(operands.reads, receiver)
	if !ok || len(sources) != 1 || len(operands.writes) > 1 {
		return checkerInstrOf(checkerUnclassified)
	}
	return CheckerInstr{
		Kind: CheckerStore, Receiver: receiver, Source: sources[0], Granted: true,
		Sources: nil, Destinations: nil, Destination: 0, Container: false,
	}
}

// convertValueStore maps a store whose value is held in no general register.
//
// Takes operands (describedOperands) which is the instruction and its registers.
//
// Returns CheckerInstr which is the granted value store, or unclassified when the
// instruction reads a general register other than its receiver or writes one.
func convertValueStore(operands describedOperands) CheckerInstr {
	receiver, ok := receiverOperand(operands.instr, operands.shape)
	if !ok || len(sourcesExcept(operands.reads, receiver)) != 0 || len(operands.writes) != 0 {
		return checkerInstrOf(checkerUnclassified)
	}
	return CheckerInstr{
		Kind: checkerStoreValue, Receiver: receiver, Granted: true,
		Sources: nil, Destinations: nil, Destination: 0, Source: 0, Container: false,
	}
}

// convertContainerStore maps an opcode that stores into a container: a store of one
// source into its receiver when that is all it does, otherwise an operation.
//
// Takes operands (describedOperands) which is the instruction and its registers.
//
// Returns CheckerInstr which is the store or the operation.
func convertContainerStore(operands describedOperands) CheckerInstr {
	if recv, ok := receiverOperand(operands.instr, operands.shape); ok && len(operands.writes) == 0 {
		if sources := sourcesExcept(operands.reads, recv); len(sources) == 1 {
			return CheckerInstr{Kind: CheckerStore, Receiver: recv, Source: sources[0], Sources: nil, Destinations: nil, Destination: 0, Granted: false, Container: false}
		}
	}
	return CheckerInstr{Kind: CheckerOp, Sources: operands.reads, Destinations: operands.writes, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}
}

// sourcesExcept lists the registers read other than the receiver.
//
// Takes reads ([]int) which lists the registers read.
// Takes recv (int) which is the receiver.
//
// Returns []int which holds the other registers, or nil.
func sourcesExcept(reads []int, recv int) []int {
	var sources []int
	for _, r := range reads {
		if r != recv {
			sources = append(sources, r)
		}
	}
	return sources
}

// checkerInstrOf returns an abstract instruction of kind with no operands.
//
// Takes kind (CheckerInstrKind) which is the instruction's kind.
//
// Returns CheckerInstr which is the instruction.
func checkerInstrOf(kind CheckerInstrKind) CheckerInstr {
	return CheckerInstr{Kind: kind, Sources: nil, Destinations: nil, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}
}

// convertCall models a call using the general-register operands recorded at its call
// site.
//
// Takes function (*program.CompiledFunction) which holds the call sites.
// Takes instr (isa.Instruction) which is the call.
// Takes generals (int) which is the function's general register count.
//
// Returns CheckerInstr which models the call when its operands can be represented
// exactly.
// Returns bool which is false when the caller must model the call as havoc instead.
func convertCall(function *program.CompiledFunction, instr isa.Instruction, generals int) (CheckerInstr, bool) {
	index := int(instr.WideIndex())
	if index >= len(function.CallSites) {
		return CheckerInstr{}, false
	}
	site := &function.CallSites[index]
	srcs, ok := appendGeneralLocations(nil, site.Arguments, generals)
	if !ok {
		return CheckerInstr{}, false
	}
	if srcs, ok = appendCallees(srcs, site, generals); !ok {
		return CheckerInstr{}, false
	}
	dsts, ok := appendGeneralLocations(nil, site.Returns, generals)
	if !ok {
		return CheckerInstr{}, false
	}
	return CheckerInstr{Kind: CheckerOp, Sources: srcs, Destinations: dsts, Receiver: 0, Destination: 0, Source: 0, Granted: false, Container: false}, true
}

// appendGeneralLocations appends the general registers among a call site's locations.
//
// Takes list ([]int) which receives the registers.
// Takes locations ([]program.VarLocation) which are the arguments or results.
// Takes generals (int) which is the function's general register count.
//
// Returns []int which is list with the registers appended.
// Returns bool which is false when a location is spilled, an upvalue, or a general
// register out of range.
func appendGeneralLocations(list []int, locations []program.VarLocation, generals int) ([]int, bool) {
	for _, location := range locations {
		switch {
		case location.IsUpvalue || location.IsSpilled:
			return list, false
		case location.Kind != isa.RegisterGeneral:
		case int(location.Register) >= generals:
			return list, false
		default:
			list = append(list, int(location.Register))
		}
	}
	return list, true
}

// appendCallees appends the registers holding a call site's native function, closure and
// method receiver, those the site uses.
//
// Takes list ([]int) which receives the registers.
// Takes site (*program.CallSite) which is the call site.
// Takes generals (int) which is the function's general register count.
//
// Returns []int which is list with the registers appended.
// Returns bool which is false when a register is out of range.
func appendCallees(list []int, site *program.CallSite, generals int) ([]int, bool) {
	registers := [...]struct {
		used     bool
		register uint8
	}{
		{used: site.IsNative, register: site.NativeRegister},
		{used: site.IsClosure, register: site.ClosureRegister},
		{used: site.IsMethod, register: site.MethodReceiverRegister},
	}
	for _, callee := range registers {
		if !callee.used {
			continue
		}
		if int(callee.register) >= generals {
			return list, false
		}
		list = append(list, int(callee.register))
	}
	return list, true
}

// kindMarkedGlobalOps marks loads and stores of package-level variables.
var kindMarkedGlobalOps = flatOperationSet(
	isa.FlatIndexOf(isa.OpGetGlobal), isa.FlatIndexOf(isa.OpSetGlobal),
	isa.FlatIndexOfSub1(isa.SubOpGetGlobalWide), isa.FlatIndexOfSub1(isa.SubOpSetGlobalWide),
)

// generalOperands lists the general registers an instruction reads and writes.
//
// A dynamically typed operand names a register of a bank chosen at run time. It counts as
// a general register whenever its index can be one (below the general count), which only
// over-approximates. The one exception is a global load or store whose kind marker names
// another bank, which never touches a general register.
//
// Takes instr (isa.Instruction) which is the instruction.
// Takes shape (isa.OperandShape) which describes its operands.
// Takes generals (int) which is the function's general register count.
//
// Returns reads and writes ([]int).
func generalOperands(instr isa.Instruction, shape isa.OperandShape, generals int) (reads, writes []int) {
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{instr.A, instr.B, instr.C}
	dynamicIsGeneral := !markedAsAnotherBank(instr, roles, operands)
	for index, role := range roles {
		dynamic := role == isa.RoleRegDynamic && dynamicIsGeneral && int(operands[index]) < generals
		if role != isa.RoleRegGeneral && !dynamic {
			continue
		}
		if shape.Reads[index] {
			reads = append(reads, int(operands[index]))
		}
		if shape.Writes[index] {
			writes = append(writes, int(operands[index]))
		}
	}
	return reads, writes
}

// markedAsAnotherBank reports whether instr is a global load or store whose kind marker
// names a bank other than the general one.
//
// Takes instr (isa.Instruction) which is the instruction.
// Takes roles ([isa.NumInstructionOperands]isa.OperandRole) which are its operand roles.
// Takes operands ([isa.NumInstructionOperands]uint8) which are its operand bytes.
//
// Returns bool which is true when its register operand is not a general register.
func markedAsAnotherBank(
	instr isa.Instruction, roles [isa.NumInstructionOperands]isa.OperandRole, operands [isa.NumInstructionOperands]uint8,
) bool {
	if !kindMarkedGlobalOps.has(isa.FlatIndexFor(instr)) {
		return false
	}
	for index, role := range roles {
		if role == isa.RoleKindMarker {
			return isa.RegisterKind(operands[index]) != isa.RegisterGeneral
		}
	}
	return false
}

// receiverOperand returns operand A when it reads the general register receiving a
// container store.
//
// Takes instr (isa.Instruction) which is the instruction.
// Takes shape (isa.OperandShape) which describes its operands.
//
// Returns the register and true when operand A is a general read.
func receiverOperand(instr isa.Instruction, shape isa.OperandShape) (int, bool) {
	if (shape.A == isa.RoleRegGeneral || shape.A == isa.RoleRegDynamic) && shape.Reads[0] {
		return int(instr.A), true
	}
	return 0, false
}

// storesIntoContainer reports whether the row's only routes store into a container or
// append to a field (the value is stored, the receiver is not handed off).
//
// Takes row (isa.OpSpec) which is the operation's row.
//
// Returns bool which is true when it does.
func storesIntoContainer(row isa.OpSpec) bool {
	if len(row.HandOff) == 0 {
		return false
	}
	for _, entry := range row.HandOff {
		switch entry.Route {
		case isa.RouteContainerStore, isa.RouteFieldAppend, isa.RouteArenaInternal, isa.RouteNone:
		default:
			return false
		}
	}
	return true
}

// flatOperationSet builds a set of operations from their flat slots.
//
// Takes flatIndices (...int) which are the members' flat slots.
//
// Returns *flatOperationMarks which holds them.
func flatOperationSet(flatIndices ...int) *flatOperationMarks {
	marks := new(flatOperationMarks)
	for _, flatIndex := range flatIndices {
		marks[flatIndex] = true
	}
	return marks
}

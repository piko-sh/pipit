package handlers

import (
	"os"
	"reflect"
)

type VM struct {
	globals []any
	count   int
}
type CallFrame struct{ pc int }
type Registers struct{ general []any }
type Instruction struct{ A, B uint8 }
type OpResult int

func barrier(v any) any { return v }

func handleRegisterOnly(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	registers.general[instruction.A] = registers.general[instruction.B]
	return 0
}

func handleBarriered(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	vm.globals[0] = barrier(registers.general[instruction.B])
	return 0
}

func handleUnbarriered(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	vm.globals[0] = registers.general[instruction.B] // want `licencecheck handler-barriers: handlers.handleUnbarriered makes 1 reference stores into memory and calls no barrier`
	return 0
}

func handleReviewed(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	vm.globals[0] = registers.general[instruction.B]
	return 0
}

func handleReviewedGrew(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	vm.globals[0] = registers.general[instruction.B] // want `licencecheck handler-barriers: handlers.handleReviewedGrew makes 2 reference stores`
	vm.globals[1] = registers.general[instruction.A]
	return 0
}

func handleFresh(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	local := make([]any, 1)
	local[0] = registers.general[instruction.B]
	registers.general[instruction.A] = local
	return 0
}

type pair struct {
	left  *int
	right int
}

type counts struct{ a, b int }

var shared = make([]any, 1)

func lookup(vm *VM) []any { return vm.globals }

func two() ([]any, []any) { return nil, nil }

func handleReflective(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	target := reflect.ValueOf(vm.globals).Index(0)
	target.Set(reflect.ValueOf(registers.general[instruction.B])) // want `licencecheck handler-barriers: handlers.handleReflective makes 1 reference stores`
	return 0
}

func handleScalars(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	vm.count = int(instruction.A)
	frame.pc = 1
	scalar := counts{}
	scalar.a = vm.count
	var grid [2]int
	grid[0] = 1
	return 0
}

func handleThroughCall(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	lookup(vm)[0] = registers.general[instruction.B] // want `licencecheck handler-barriers: handlers.handleThroughCall makes 1 reference stores`
	return 0
}

func handlePackageVariable(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	os.Args[0] = "pipit" // want `licencecheck handler-barriers: handlers.handlePackageVariable makes 1 reference stores`
	return 0
}

func handleGlobal(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	shared[0] = registers.general[instruction.B] // want `licencecheck handler-barriers: handlers.handleGlobal makes 1 reference stores`
	return 0
}

func handleFreshForms(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	var declared = make([]any, 1)
	declared[0] = registers.general[instruction.B]
	literal := &pair{}
	literal.left = new(int)
	allocated := new([2]*int)
	allocated[0] = literal.left
	wrapped := (pair{})
	wrapped.left = allocated[1]
	registers.general[instruction.A] = declared
	return 0
}

func handleReused(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) OpResult {
	reused := vm.globals
	reused[0] = registers.general[instruction.B] // want `licencecheck handler-barriers: handlers.handleReused makes 3 reference stores`
	left, right := two()
	left[0], right[0] = registers.general[0], registers.general[1]
	return 0
}

func handleWrongShape(vm *VM, registers *Registers) OpResult {
	vm.globals[0] = registers.general[0]
	return 0
}

func handleWrongResult(vm *VM, frame *CallFrame, registers *Registers, instruction Instruction) int {
	vm.globals[0] = registers.general[0]
	return 0
}

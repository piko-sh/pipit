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

	"pipit.sh/pipit/internal/isa"
)

// Registers holds the typed register banks for a call frame. Bank ordering must match the
// isa.RegisterKind iota values, and new banks must be appended so ASM-pinned offsets
// remain stable.
type Registers struct {
	// Ints stores int64 values for the integer register bank.
	Ints []int64

	// Floats stores float64 values for the float register bank.
	Floats []float64

	// Strings stores string values for the string register bank.
	Strings []string

	// General stores reflect.Value values for the general register bank.
	General []reflect.Value

	// Bools stores bool values for the boolean register bank.
	Bools []bool

	// Uints stores uint64 values for the unsigned integer register bank.
	Uints []uint64

	// Complex stores complex128 values for the complex register bank.
	Complex []complex128

	// SlicesInt stores []int64 slice headers for the typed-slice-int register bank. The
	// Compiler routes make([]T, ...) where T resolves to int-kind into this bank instead of
	// boxing a reflect.Value into the general bank, removing per-element reflect.Value
	// allocations on get/set.
	SlicesInt [][]int64

	// slicesFloat stores []float64 slice headers for the typed slice float bank.
	slicesFloat [][]float64

	// slicesString stores []string slice headers for the typed slice string bank.
	slicesString [][]string

	// slicesBool stores []bool slice headers for the typed slice bool bank.
	slicesBool [][]bool

	// slicesUint stores []uint64 slice headers for the typed slice uint bank.
	slicesUint [][]uint64

	// slicesByte stores []byte slice headers for the typed slice byte bank.
	slicesByte [][]byte

	// lastAllocMask records the nonZeroBankMask of the previous frame-slot occupant so the
	// frame allocator clears only the banks that need clearing.
	lastAllocMask uint16
}

// NewRegistersForBench is an exported wrapper for benchmarking direct allocation vs arena
// allocation.
//
// Takes numRegs ([NumRegisterKinds]uint32) which is the number of registers per bank.
//
// Returns a freshly allocated register file.
func NewRegistersForBench(numRegs [isa.NumRegisterKinds]uint32) Registers {
	return NewRegisters(numRegs)
}

// NewRegisters creates a register file sized for a compiled function.
//
// Takes numRegs ([NumRegisterKinds]uint32) which is the number of registers per bank.
//
// Returns a freshly allocated register file.
func NewRegisters(numRegs [isa.NumRegisterKinds]uint32) Registers {
	return Registers{Ints: make([]int64, numRegs[isa.RegisterInt]),
		Floats:       make([]float64, numRegs[isa.RegisterFloat]),
		Strings:      make([]string, numRegs[isa.RegisterString]),
		General:      make([]reflect.Value, numRegs[isa.RegisterGeneral]),
		Bools:        make([]bool, numRegs[isa.RegisterBool]),
		Uints:        make([]uint64, numRegs[isa.RegisterUint]),
		Complex:      make([]complex128, numRegs[isa.RegisterComplex]),
		SlicesInt:    make([][]int64, numRegs[isa.RegisterSliceInt]),
		slicesFloat:  make([][]float64, numRegs[isa.RegisterSliceFloat]),
		slicesString: make([][]string, numRegs[isa.RegisterSliceString]),
		slicesBool:   make([][]bool, numRegs[isa.RegisterSliceBool]),
		slicesUint:   make([][]uint64, numRegs[isa.RegisterSliceUint]),
		slicesByte:   make([][]byte, numRegs[isa.RegisterSliceByte]), lastAllocMask: 0}
}

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
	"unsafe"

	"piko.sh/vectormaths"
)

// elementwiseFloat64Plan bundles the resolved destination plus two operand slices and the
// validated element count for a SIMD elementwise float64 kernel. The ok field is false
// when the count validation failed and an interpreted panic was raised.
type elementwiseFloat64Plan struct {
	// destination is the destination typed-slice float64 bank.
	destination []float64

	// a is the first operand typed-slice float64 bank.
	a []float64

	// b is the second operand typed-slice float64 bank.
	b []float64

	// count is the validated element count to operate on.
	count int

	// ok reports whether the plan is valid; false when validation raised an interpreted
	// panic.
	ok bool
}

// float64SlicesOverlap reports whether the backing arrays of x and y share any element
// address. Two SIMD-add operands that overlap cannot go through the batch-reading
// vectormaths.AddF64 kernel without diverging from Go's element-by-element evaluation
// order, so the caller falls back to a forward scalar loop when this returns true.
//
// Takes x ([]float64) which is the first already count-bounded operand window.
// Takes y ([]float64) which is the second already count-bounded operand window.
//
// Returns true when the two windows intersect in memory.
func float64SlicesOverlap(x, y []float64) bool {
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	xStart := uintptr(unsafe.Pointer(unsafe.SliceData(x)))
	xEnd := xStart + uintptr(len(x))*float64ElementSize
	yStart := uintptr(unsafe.Pointer(unsafe.SliceData(y)))
	yEnd := yStart + uintptr(len(y))*float64ElementSize
	return xStart < yEnd && yStart < xEnd
}

// addF64ScalarForward writes destination[i] = a[i] + b[i] in ascending index order,
// reproducing the exact evaluation order of the scalar Go loop the SIMD add kernel
// replaced. Used when a backing-store overlap between destination and an operand makes
// the batch vectormaths.AddF64 kernel (which reads ahead) observably diverge from Go
// semantics.
//
// Takes destination ([]float64) which is the destination window.
// Takes a ([]float64) which is the first operand window.
// Takes b ([]float64) which is the second operand window.
func addF64ScalarForward(destination, a, b []float64) {
	for i := range destination {
		destination[i] = a[i] + b[i]
	}
}

// simdReadSliceWithCount resolves a typed-bank slice and validates the count against
// slice length, raising an interpreted panic on out-of-range.
//
// Takes vm (*VM).
// Takes registers (*Registers) which holds the register banks.
// Takes sliceFloatRegister (uint8) which is the typed slicesFloat index.
// Takes countIntRegister (uint8) which is the int-bank register holding the count.
//
// Returns the slice, the validated count, and ok=true; nil/0/false on error.
func simdReadSliceWithCount(vm *VM, registers *Registers, sliceFloatRegister, countIntRegister uint8) ([]float64, int, bool) {
	slice := registers.slicesFloat[sliceFloatRegister]
	count := int(registers.Ints[countIntRegister])
	if count < 0 || count > len(slice) {
		raiseNativePanicAsInterpreted(vm, newRuntimePanicError("simd kernel: count %d out of range [0, %d]", count, len(slice)))
		return nil, 0, false
	}
	return slice, count, true
}

// simdReadDualSliceWithCount resolves two typed-bank float64 slices and validates a count
// against both. Bounds-checks mirror what the original scalar loop would have done at
// index `count-1`.
//
// Takes vm (*VM).
// Takes registers (*Registers) which holds the register banks.
// Takes firstRegister (uint8) which is the first slicesFloat index.
// Takes secondRegister (uint8) which is the second slicesFloat index.
// Takes countIntRegister (uint8) which is the int-bank count.
//
// Returns both slices, the validated count, and ok=true; nil/nil/0/ false after raising
// an interpreted panic.
func simdReadDualSliceWithCount(vm *VM, registers *Registers, firstRegister, secondRegister, countIntRegister uint8) (firstSlice, secondSlice []float64, count int, ok bool) {
	firstSlice = registers.slicesFloat[firstRegister]
	secondSlice = registers.slicesFloat[secondRegister]
	count = int(registers.Ints[countIntRegister])
	if count < 0 || count > len(firstSlice) || count > len(secondSlice) {
		raiseNativePanicAsInterpreted(vm, newRuntimePanicError("simd kernel: count %d out of range (len(a)=%d, len(b)=%d)", count, len(firstSlice), len(secondSlice)))
		return nil, nil, 0, false
	}
	return firstSlice, secondSlice, count, true
}

// simdReadElementwiseFloat64 resolves the destination + two operand typed-bank float64
// slices and validates a shared count against all three lengths, so the bounds-checking
// logic of the element-wise add dispatcher lives in one place.
//
// Takes vm (*VM).
// Takes registers (*Registers) which holds the register banks.
// Takes destinationRegister (uint8) which is the slicesFloat index of the destination
// slice.
// Takes firstRegister (uint8) which is the first operand slicesFloat index.
// Takes secondRegister (uint8) which is the second operand slicesFloat index.
// Takes countIntRegister (uint8) which is the int-bank count.
//
// Returns an elementwiseFloat64Plan; the ok field is false after raising an interpreted
// panic for an out-of-range count.
func simdReadElementwiseFloat64(vm *VM, registers *Registers, destinationRegister, firstRegister, secondRegister, countIntRegister uint8) elementwiseFloat64Plan {
	destination := registers.slicesFloat[destinationRegister]
	a := registers.slicesFloat[firstRegister]
	b := registers.slicesFloat[secondRegister]
	count := int(registers.Ints[countIntRegister])
	if count < 0 || count > len(destination) || count > len(a) || count > len(b) {
		raiseNativePanicAsInterpreted(vm, newRuntimePanicError("simd kernel: count %d out of range (len(destination)=%d, len(a)=%d, len(b)=%d)", count, len(destination), len(a), len(b)))
		return elementwiseFloat64Plan{}
	}
	return elementwiseFloat64Plan{destination: destination, count: count, a: a, b: b, ok: true}
}

// simdDotProductFloat64 computes the scalar dot product of two equal-length []float64
// slices, dispatching to the architecture-tuned kernel in internal/vectormaths (SSE2 /
// AVX2 on amd64, scalar fallback elsewhere). The Go-side helper gives SIMD dispatch a
// single named entry point regardless of how vectormaths picks its implementation.
//
// Takes a ([]float64) which is the first operand slice.
// Takes b ([]float64) which is the second operand slice.
//
// Returns the scalar sum sum(a[i]*b[i]).
func simdDotProductFloat64(a, b []float64) float64 {
	return vectormaths.DotF64(a, b)
}

// simdSumFloat64 computes the scalar sum of a []float64, dispatching to the
// architecture-tuned kernel in internal/vectormaths.
//
// Takes a ([]float64) which is the operand slice.
//
// Returns sum(a[i]).
func simdSumFloat64(a []float64) float64 {
	return vectormaths.SumF64(a)
}

// simdScaleFloat64 multiplies every element of a []float64 by a scalar k in place,
// dispatching to the architecture-tuned kernel in internal/vectormaths.
//
// Takes slice ([]float64) which is mutated in place.
// Takes k (float64) which is the scalar coefficient.
func simdScaleFloat64(slice []float64, k float64) {
	vectormaths.ScaleF64(slice, k)
}

// simdSubFloat64 and simdMulFloat64 are the binary operations passed to
// simdElementwiseFloat64. Kept as standalone named functions so the Go Compiler can
// inline them into the element-wise loop body without indirect-call overhead.

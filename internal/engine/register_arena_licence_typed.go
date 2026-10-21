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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
)

// licenceTypedWalkOn is true in the builds with the typed walk. A walk with typedOff set
// still takes the reflection walk, which the typed-walk differential compares it with.
const licenceTypedWalkOn = true

// licenceTypedKind is what a plan does with a value of its type.
type licenceTypedKind uint8

const (
	// licenceTypedNoPointers is a pointer-free value.
	//
	// It costs a leaf and holds nothing.
	licenceTypedNoPointers licenceTypedKind = iota

	// licenceTypedString revokes the string's bytes.
	licenceTypedString

	// licenceTypedSlice revokes the backing and walks its elements to capacity.
	licenceTypedSlice

	// licenceTypedArray walks the elements.
	licenceTypedArray

	// licenceTypedStruct walks the fields.
	licenceTypedStruct

	// licenceTypedPointer revokes the pointee and walks it.
	licenceTypedPointer

	// licenceTypedInterface walks the dynamic value.
	licenceTypedInterface

	// licenceTypedMap leaves the map to visitMap.
	licenceTypedMap

	// licenceTypedReflectValue leaves a reflect.Value's held value to visit.
	licenceTypedReflectValue

	// licenceTypedUnsafePointer revokes the address.
	licenceTypedUnsafePointer

	// licenceTypedFunc applies visitFunc's handling to a registered wrapper's closure or
	// receiver.
	licenceTypedFunc

	// licenceTypedChan costs a leaf and is not followed, as in visit.
	licenceTypedChan
)

// licenceTypedPlan is the compiled walk of one type. Plans are immutable once published.
type licenceTypedPlan struct {
	// element is the plan of a slice's, array's or pointer's element (nil when the element
	// is pointer-free, for a slice or pointer).
	element *licenceTypedPlan

	// abi is the type's *abi.Type.
	abi unsafe.Pointer

	// elemABI is the *abi.Type of a slice's or pointer's element, the memo's type key.
	elemABI unsafe.Pointer

	// steps lists a struct's pointer-bearing fields and runs of consecutive pointer-free
	// fields in field order.
	steps []licenceTypedStep

	// stride is an array's or slice's element size.
	stride uintptr

	// size is the type's size, the extent page-granular revocation revokes for a boxed value
	// (revokeExtent).
	size uintptr

	// elemSize is a pointer's element size, the extent page-granular revocation revokes for
	// its pointee.
	elemSize uintptr

	// length is an array's length.
	length int

	// kind is what the plan does.
	kind licenceTypedKind

	// elemNoPointers is true when a slice's or pointer's element is pointer-free.
	elemNoPointers bool

	// emptyInterface is true for an interface without methods (its first word is the type,
	// not an itab).
	emptyInterface bool

	// direct is true when a value of the type is stored in an interface's data word itself
	// rather than boxed (abi.TFlagDirectIface).
	direct bool

	// closure and cell are true for *RuntimeClosure and *program.UpvalueCell, which the walk
	// follows by their fields' meaning (closure wrappers and cell walks,
	// visitClosureOrCell).
	closure, cell bool
}

// licenceTypedStep is one struct field of a plan, or a run of pointer-free fields.
type licenceTypedStep struct {
	// plan is the field's plan, or nil for a run.
	plan *licenceTypedPlan

	// offset is the field's offset.
	offset uintptr

	// run is the number of consecutive pointer-free fields, or zero for a field.
	run int
}

// licenceTypedWordSize is the size of a pointer word.
const licenceTypedWordSize = unsafe.Sizeof(uintptr(0))

var (
	// licenceTypedPlans caches the plan of each type, keyed by *abi.Type.
	licenceTypedPlans typedCache[unsafe.Pointer, *licenceTypedPlan]

	// licenceTypedReflectValueABI is reflect.Value's *abi.Type.
	licenceTypedReflectValueABI = reflectValueABIType(reflect.TypeFor[reflect.Value]())

	// licenceTypedDescriptorABI is the *abi.Type of a reflect.Type's dynamic value, at which
	// the walk stops (isTypeDescriptor).
	licenceTypedDescriptorABI = reflectValueABIType(reflectTypeDescriptorType)
)

// licenceTypedPlanFor returns the plan of valueType, compiling it (and the plans it
// refers to) on first use.
//
// Takes valueType (reflect.Type) which is the type.
//
// Returns *licenceTypedPlan which is the plan.
func licenceTypedPlanFor(valueType reflect.Type) *licenceTypedPlan {
	if cached, found := licenceTypedPlans.load(reflectValueABIType(valueType)); found {
		return cached
	}
	building := map[unsafe.Pointer]*licenceTypedPlan{}
	plan := buildLicenceTypedPlan(valueType, building)
	for key, built := range building {
		licenceTypedPlans.loadOrStore(key, built)
	}
	return plan
}

// buildLicenceTypedPlan compiles the plan of valueType. A recursive type refers to its
// own plan through building, which holds the plans made by this compilation.
//
// Takes valueType (reflect.Type) which is the type.
// Takes building (map[unsafe.Pointer]*licenceTypedPlan) which holds the plans in
// progress.
//
// Returns *licenceTypedPlan which is the plan, possibly still being filled by a caller.
func buildLicenceTypedPlan(valueType reflect.Type, building map[unsafe.Pointer]*licenceTypedPlan) *licenceTypedPlan {
	key := reflectValueABIType(valueType)
	if plan, found := building[key]; found {
		return plan
	}
	if cached, found := licenceTypedPlans.load(key); found {
		return cached
	}
	plan := &licenceTypedPlan{abi: key, kind: licenceTypedNoPointers, size: valueType.Size(), element: nil, elemABI: nil, steps: nil, stride: 0, elemSize: 0, length: 0, elemNoPointers: false,
		emptyInterface: false, direct: false, closure: false, cell: false}
	building[key] = plan
	ptrBytes := *(*uintptr)(unsafe.Add(key, abiTypePtrBytesOffset))
	plan.direct = valueType.Size() == licenceTypedWordSize && ptrBytes == licenceTypedWordSize
	if ptrBytes == 0 {
		return plan
	}
	if key == licenceTypedReflectValueABI {
		plan.kind = licenceTypedReflectValue
		return plan
	}
	switch valueType.Kind() {
	case reflect.String:
		plan.kind = licenceTypedString
	case reflect.Slice:
		plan.kind = licenceTypedSlice
		plan.elemABI, plan.stride = reflectValueABIType(valueType.Elem()), valueType.Elem().Size()
		plan.elemNoPointers = licenceTypeHoldsNoPointers(valueType.Elem())
		if !plan.elemNoPointers {
			plan.element = buildLicenceTypedPlan(valueType.Elem(), building)
		}
	case reflect.Array:
		plan.kind = licenceTypedArray
		plan.length, plan.stride = valueType.Len(), valueType.Elem().Size()
		plan.element = buildLicenceTypedPlan(valueType.Elem(), building)
	case reflect.Struct:
		plan.kind = licenceTypedStruct
		plan.steps = buildLicenceTypedSteps(valueType, building)
	case reflect.Pointer:
		plan.kind = licenceTypedPointer
		plan.closure, plan.cell = valueType == runtimeClosurePointerType, valueType == upvalueCellPointerType
		plan.elemABI, plan.elemSize = reflectValueABIType(valueType.Elem()), valueType.Elem().Size()
		plan.elemNoPointers = licenceTypeHoldsNoPointers(valueType.Elem())
		if !plan.elemNoPointers {
			plan.element = buildLicenceTypedPlan(valueType.Elem(), building)
		}
	case reflect.Interface:
		plan.kind = licenceTypedInterface
		plan.emptyInterface = valueType.NumMethod() == 0
	case reflect.Map:
		plan.kind = licenceTypedMap
	case reflect.UnsafePointer:
		plan.kind = licenceTypedUnsafePointer
	case reflect.Func:
		plan.kind = licenceTypedFunc
	case reflect.Chan:
		plan.kind = licenceTypedChan
	default:
	}
	return plan
}

// buildLicenceTypedSteps compiles a struct type's steps: one per field that holds
// pointers, and one skip per run of fields that hold none.
//
// Takes valueType (reflect.Type) which is the struct type.
// Takes building (map[unsafe.Pointer]*licenceTypedPlan) which holds the plans in
// progress.
//
// Returns []licenceTypedStep which holds the steps in field order.
func buildLicenceTypedSteps(valueType reflect.Type, building map[unsafe.Pointer]*licenceTypedPlan) []licenceTypedStep {
	var steps []licenceTypedStep
	for index := range valueType.NumField() {
		field := structFieldType(valueType, index)
		if !licenceTypeHoldsNoPointers(field.Type) {
			steps = append(steps, licenceTypedStep{plan: buildLicenceTypedPlan(field.Type, building), offset: field.Offset, run: 0})
			continue
		}
		if last := len(steps) - 1; last >= 0 && steps[last].plan == nil {
			steps[last].run++
		} else {
			steps = append(steps, licenceTypedStep{run: 1, plan: nil, offset: 0})
		}
	}
	return steps
}

// visitTyped is visit's typed path.
//
// It walks value by its type's plan when value's storage can be addressed (or value is a
// pointer), and reports whether it did. visit has already checked value valid and the
// walk not exhausted.
//
// Takes value (reflect.Value) which is the value.
// Takes boxed (bool) which is visit's argument.
// Takes depth (int) which is the remaining nesting allowance.
//
// Returns bool which is false when the reflect walk must visit value instead.
func (w *licenceWalk) visitTyped(value reflect.Value, boxed bool, depth int) bool {
	raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	kind := reflect.Kind(raw.flag & flagKindMask)
	if raw.flag&flagIndir == 0 {
		if kind != reflect.Pointer {
			return false
		}
		plan := w.typedPlan(raw.typ, value.Type)
		if w.typedEnter(depth) {
			w.typedPointer(plan, raw.ptr, nil, value, depth)
		}
		return true
	}
	if kind == reflect.Map {
		return false
	}
	box := false
	if boxed && raw.flag&flagAddr == 0 {
		switch kind {
		case reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Interface:
		default:
			box = true
		}
	}
	w.typedVisit(w.typedPlan(raw.typ, value.Type), raw.ptr, box, depth)
	return true
}

// typedPlan returns the plan of the type at typ, from the walk's last plan, the arena's,
// the cache, or by compiling the type that of returns, which is called only on a miss.
//
// Takes typ (unsafe.Pointer) which is the *abi.Type.
// Takes of (func() reflect.Type) which returns the type.
//
// Returns *licenceTypedPlan which is the plan.
func (w *licenceWalk) typedPlan(typ unsafe.Pointer, of func() reflect.Type) *licenceTypedPlan {
	if typ == w.typedLastABI {
		return w.typedLastPlan
	}
	state := &w.arena.licenceState
	plan := state.typedPlan
	if typ != state.typedPlanABI {
		if cached, found := licenceTypedPlans.load(typ); found {
			plan = cached
		} else {
			plan = licenceTypedPlanFor(of())
		}
	}
	w.typedLastABI, w.typedLastPlan = typ, plan
	state.typedPlanABI, state.typedPlan = typ, plan
	return plan
}

// typedEnter is visit's prologue for a value the typed walk visits.
//
// It stops the walk at a revoked epoch or past the depth or leaf budget, and otherwise
// spends a leaf.
//
// Takes depth (int) which is the remaining nesting allowance.
//
// Returns bool which is true when the value is to be visited.
func (w *licenceWalk) typedEnter(depth int) bool {
	if w.exhausted {
		return false
	}
	if w.arena.licenceState.epochRevoked {
		w.epochStop, w.exhausted = true, true
		return false
	}
	if depth <= 0 || w.budget <= 0 {
		w.exhausted = true
		return false
	}
	w.budget--
	return true
}

// typedSkip accounts for count visits to pointer-free values at depth.
//
// Each visit costs a leaf, but the values hold no references and cannot revoke the epoch,
// so they are settled immediately.
//
// Takes count (int) which is the number of values.
// Takes depth (int) which is their nesting allowance.
func (w *licenceWalk) typedSkip(count int, depth int) {
	if w.exhausted {
		return
	}
	if w.arena.licenceState.epochRevoked {
		w.epochStop, w.exhausted = true, true
		return
	}
	if depth <= 0 {
		w.exhausted = true
		return
	}
	if w.budget < count {
		w.budget, w.exhausted = 0, true
		return
	}
	w.budget -= count
}

// typedVisit is visit for the value at pointer of plan's type.
//
// Takes plan (*licenceTypedPlan) which is the value's plan.
// Takes pointer (unsafe.Pointer) which is the value's address.
// Takes box (bool) which is true when pointer is an interface's box, which
// visitBoxedStorage revokes.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) typedVisit(plan *licenceTypedPlan, pointer unsafe.Pointer, box bool, depth int) {
	if !w.typedEnter(depth) {
		return
	}
	if box {
		w.revokeExtent(uintptr(pointer), plan.size)
	}
	switch plan.kind {
	case licenceTypedString:
		if s := *(*string)(pointer); len(s) > 0 {
			w.revoke(uintptr(unsafe.Pointer(unsafe.StringData(s))))
		}
	case licenceTypedSlice:
		w.typedSlice(plan, pointer, depth)
	case licenceTypedArray:
		w.typedArray(plan, pointer, depth)
	case licenceTypedStruct:
		w.typedStruct(plan, pointer, depth)
	case licenceTypedPointer:
		w.typedPointer(plan, *(*unsafe.Pointer)(pointer), pointer, reflect.Value{}, depth)
	case licenceTypedInterface:
		w.typedInterface(plan, pointer, depth)
	case licenceTypedMap:
		w.visitMap(unsafeNewAt(plan.abi, pointer, reflect.Map), depth)
	case licenceTypedReflectValue:
		w.visit(reflectValueHeld(unsafeNewAt(plan.abi, pointer, reflect.Struct)), true, depth-1)
	case licenceTypedUnsafePointer:
		w.revoke(uintptr(*(*unsafe.Pointer)(pointer)))
	case licenceTypedFunc:
		if *(*unsafe.Pointer)(pointer) != nil {
			w.visitFunc(unsafeNewAt(plan.abi, pointer, reflect.Func), depth)
		}
	default:
	}
}

// typedArray is visit's array case for the array at pointer, element by element until the
// walk is exhausted.
//
// Takes plan (*licenceTypedPlan) which is the array type's plan.
// Takes pointer (unsafe.Pointer) which is the array's address.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) typedArray(plan *licenceTypedPlan, pointer unsafe.Pointer, depth int) {
	for index := range plan.length {
		if w.exhausted {
			return
		}
		w.typedVisit(plan.element, unsafe.Add(pointer, uintptr(index)*plan.stride), false, depth-1)
	}
}

// typedStruct is visit's struct case for the struct at pointer, step by step.
//
// Takes plan (*licenceTypedPlan) which is the struct type's plan.
// Takes pointer (unsafe.Pointer) which is the struct's address.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) typedStruct(plan *licenceTypedPlan, pointer unsafe.Pointer, depth int) {
	for index := range plan.steps {
		step := &plan.steps[index]
		if step.plan == nil {
			w.typedSkip(step.run, depth-1)
			continue
		}
		w.typedVisit(step.plan, unsafe.Add(pointer, step.offset), false, depth-1)
	}
}

// typedPointer is visit's pointer case for the pointer value pointer.
//
// Takes plan (*licenceTypedPlan) which is the pointer type's plan.
// Takes pointer (unsafe.Pointer) which is the pointer.
// Takes at (unsafe.Pointer) which is the pointer word's address, or nil when value is
// given.
// Takes value (reflect.Value) which is the pointer value when at is nil; only the census
// build reads either (typedMemoCovers).
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) typedPointer(plan *licenceTypedPlan, pointer, at unsafe.Pointer, value reflect.Value, depth int) {
	if pointer == nil {
		return
	}
	switch {
	case plan.closure && closureWrapperWalksApply(w.arena):
		w.visitClosure((*RuntimeClosure)(pointer), depth)
		return
	case plan.cell && cellWalksApply(w.arena):
		w.visitCell((*program.UpvalueCell)(pointer), depth)
		return
	default:
	}
	if plan.elemNoPointers {
		w.revokeExtent(uintptr(pointer), plan.elemSize)
		return
	}
	if w.typedMemoCovers(plan, at, value, pointer, plan.elemABI, 0, depth) {
		return
	}
	w.revokeExtent(uintptr(pointer), plan.elemSize)
	w.typedVisit(plan.element, pointer, false, depth-1)
	w.memoNote(pointer, plan.elemABI, 0)
}

// typedMemoCovers applies memoCovers to the private typed walk, which never uses
// memoReadOnly.
//
// Takes plan (*licenceTypedPlan) which is the container's plan.
// Takes at (unsafe.Pointer) which is the container value's address, or nil when value is
// given.
// Takes value (reflect.Value) which is the container value when at is nil.
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its element type.
// Takes extent (int) which is the slice capacity to walk, or zero.
// Takes depth (int) which is the walk's remaining depth at the container.
//
// Returns bool which is true when the walk may stop at the container.
func (w *licenceWalk) typedMemoCovers(plan *licenceTypedPlan, at unsafe.Pointer, value reflect.Value, pointer, typ unsafe.Pointer, extent int, depth int) bool {
	if w.memo == nil || depth == w.unmemoisedDepth {
		return false
	}
	covered := w.memo.covers(pointer, typ, extent) && w.memoEligible(pointer)
	if storeCensusBuilt && covered {
		if !value.IsValid() {
			value = unsafeNewAt(plan.abi, at, plan.containerKind())
		}
		censusNoteMemoSettled(w.arena, value)
	}
	return covered
}

// containerKind is the reflect kind of a container plan: a slice or a pointer.
//
// Returns reflect.Kind which is reflect.Slice for a slice plan and reflect.Pointer
// otherwise.
func (plan *licenceTypedPlan) containerKind() reflect.Kind {
	if plan.kind == licenceTypedSlice {
		return reflect.Slice
	}
	return reflect.Pointer
}

// typedSlice is visitSlice for the slice header at pointer (the walk is never shared
// here).
//
// Takes plan (*licenceTypedPlan) which is the slice type's plan.
// Takes pointer (unsafe.Pointer) which is the slice header's address.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) typedSlice(plan *licenceTypedPlan, pointer unsafe.Pointer, depth int) {
	header := (*snapshotSliceHeader)(pointer)
	data, capacity := header.Data, header.Cap
	if capacity == 0 {
		return
	}
	extentBytes := uintptr(capacity) * plan.stride
	if plan.elemNoPointers {
		w.revokeExtent(uintptr(data), extentBytes)
		return
	}
	if w.typedMemoCovers(plan, pointer, reflect.Value{}, data, plan.elemABI, capacity, depth) {
		return
	}
	w.revokeExtent(uintptr(data), extentBytes)
	for index := range capacity {
		if w.exhausted {
			break
		}
		w.typedVisit(plan.element, unsafe.Add(data, uintptr(index)*plan.stride), false, depth-1)
	}
	w.memoNote(data, plan.elemABI, capacity)
}

// typedInterface is visit's interface case for the interface at pointer.
//
// Takes plan (*licenceTypedPlan) which is the interface type's plan.
// Takes pointer (unsafe.Pointer) which is the interface's address.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) typedInterface(plan *licenceTypedPlan, pointer unsafe.Pointer, depth int) {
	word := *(*unsafe.Pointer)(pointer)
	if word == nil {
		return
	}
	typ := word
	if !plan.emptyInterface {
		typ = *(*unsafe.Pointer)(unsafe.Add(word, licenceTypedWordSize))
	}
	if typ == licenceTypedDescriptorABI {
		return
	}
	dynamic := w.typedPlan(typ, func() reflect.Type { return unsafeNewAt(plan.abi, pointer, reflect.Interface).Elem().Type() })
	data := unsafe.Add(pointer, licenceTypedWordSize)
	if dynamic.direct {
		w.typedVisit(dynamic, data, false, depth-1)
		return
	}
	w.typedVisit(dynamic, *(*unsafe.Pointer)(data), true, depth-1)
}

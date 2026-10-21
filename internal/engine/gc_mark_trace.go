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
	"unsafe"
)

const (
	// gcMapHolderSlots bounds the map walk holders a mark state keeps.
	gcMapHolderSlots = 16

	// gcMapHolderMaxBytes is the largest key or element type whose holder a mark state
	// keeps; a larger one is made for its walk and dropped.
	gcMapHolderMaxBytes = 256

	// gcVisitedInitialSize is the number of slots a mark state's visited set starts with. It
	// is a power of two.
	gcVisitedInitialSize = 256

	// gcVisitSetLoadNumerator and gcVisitSetLoadDenominator bound the share of a visited
	// set's slots in use, three in four, before it grows.
	gcVisitSetLoadNumerator = 3

	// gcVisitSetLoadDenominator is the denominator of that share.
	gcVisitSetLoadDenominator = 4

	// gcVisitExtentShift places a key's extent in the high bits of its hash input, apart
	// from the address bits that vary.
	gcVisitExtentShift = 40

	// gcVisitHashDropBits drops the hash's low bits, which Fibonacci hashing mixes least,
	// leaving a non-negative int.
	gcVisitHashDropBits = 33

	// gcVisitedKeptSize is the most slots of a visited set a pooled mark state keeps between
	// cycles, enough for 98,304 referents. A larger set is dropped.
	gcVisitedKeptSize = 1 << 17

	// gcMarkWorkKeptSize is the largest work list a pooled mark state keeps between cycles.
	// A larger one is dropped.
	gcMarkWorkKeptSize = 1 << 14

	// gcMarkWorkLimit bounds the steps the mark's work list holds at once (queueWork).
	gcMarkWorkLimit = 1 << 20

	// gcTraceMaxDepth bounds the reference hops the mark phase of an arena without a licence
	// follows.
	//
	// Arena slabs hold only pointer-free values and slice headers, so cycles cannot form and
	// a small bound covers every real chain while guarding against malformed values. A
	// licensed mark has no depth bound, because its visited set and step budget end every
	// walk (licenceMarkStepBudget).
	gcTraceMaxDepth = 8
)

const (
	// gcMarkValue is a value to mark.
	gcMarkValue gcMarkWorkKind = iota

	// gcMarkArrayElements is an array's walk, at its next element.
	gcMarkArrayElements

	// gcMarkCapacityElements is a slice backing's walk up to its capacity, at its next
	// element.
	gcMarkCapacityElements

	// gcMarkMapEntries is a map's walk through key and element holders, at its next entry.
	gcMarkMapEntries

	// gcMarkMapEntryCopies is a map's walk through the iterator's copies, at its next entry.
	gcMarkMapEntryCopies
)

var (
	// typeInt64Slice is the reflect type for []int64, used by the collector.
	typeInt64Slice = reflect.TypeFor[[]int64]()

	// typeFloat64Slice is the reflect type for []float64, used by the collector.
	typeFloat64Slice = reflect.TypeFor[[]float64]()

	// typeStringSlice is the reflect type for []string, used by the collector.
	typeStringSlice = reflect.TypeFor[[]string]()

	// typeBoolSlice is the reflect type for []bool, used by the collector.
	typeBoolSlice = reflect.TypeFor[[]bool]()

	// typeUint64Slice is the reflect type for []uint64, used by the collector.
	typeUint64Slice = reflect.TypeFor[[]uint64]()

	// typeByteSlice is the reflect type for []byte, used by the collector.
	typeByteSlice = reflect.TypeFor[[]byte]()
)

// gcMarkWorkKind says what a work list entry is.
type gcMarkWorkKind uint8

// gcMarkWork is one pending step of the mark, which is a value to mark or a sequence or
// map walk at its next element or entry.
type gcMarkWork struct {
	// iterator is a map walk's position.
	iterator *reflect.MapIter

	// keyHolder holds a map walk's current key, when keys are traced through a holder.
	keyHolder *gcMapHolder

	// elementHolder holds a map walk's current element, when elements are traced through a
	// holder.
	elementHolder *gcMapHolder

	// value is the value to mark, or the array, slice elements or map a walk reads.
	value reflect.Value

	// index is a sequence walk's next element.
	index int

	// depth is the value's hops from its root, or for a walk, the container's.
	depth int

	// kind says what the entry is.
	kind gcMarkWorkKind

	// traceKey is true when a map walk marks keys.
	traceKey bool

	// traceElement is true when a map walk marks elements.
	traceElement bool
}

// pushMark queues v, depth hops from its root, for the mark to take (drainMark).
//
// Takes v (reflect.Value) which is the value.
// Takes depth (int) which is its hops from its root.
func (state *gcMarkState) pushMark(v reflect.Value, depth int) {
	state.queueWork(newMarkWalk(gcMarkValue, v, depth))
}

// queueWork appends entry to the work list.
//
// Takes entry (gcMarkWork) which is the step.
//
// Returns bool which is false when entry was dropped.
func (state *gcMarkState) queueWork(entry gcMarkWork) bool {
	limit := state.workLimit
	if limit == 0 {
		limit = gcMarkWorkLimit
	}
	if len(state.work) >= limit {
		state.failClose(failClosedBudget)
		state.endWalk(&entry)
		return false
	}
	state.work = append(state.work, entry)
	return true
}

// newMarkWalk returns a work list entry of kind for value, depth hops from its root, at
// its first element and with no iterator or holders.
//
// Takes kind (gcMarkWorkKind) which says what the entry is.
// Takes value (reflect.Value) which is the value, or what the walk reads.
// Takes depth (int) which is its hops from its root.
//
// Returns gcMarkWork which is the entry.
func newMarkWalk(kind gcMarkWorkKind, value reflect.Value, depth int) gcMarkWork {
	return gcMarkWork{iterator: nil, keyHolder: nil, elementHolder: nil, value: value, index: 0, depth: depth, kind: kind, traceKey: false, traceElement: false}
}

// drainMark takes the mark's queued steps, last in first out, until the work list is back
// to length base.
//
// A step queues what it reaches above itself, so a walk's next step waits below
// everything its current element reaches. So the memory a map walk's holders lend is
// marked before the walk overwrites it. Once the mark has settled, the remaining steps
// are dropped and their holders released.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes base (int) which is the work list length to stop at.
func drainMark(arena *RegisterArena, state *gcMarkState, base int) {
	for len(state.work) > base {
		last := len(state.work) - 1
		entry := state.work[last]
		state.work[last] = gcMarkWork{}
		state.work = state.work[:last]
		if !state.takeMarkStep() {
			state.endWalk(&entry)
			continue
		}
		switch entry.kind {
		case gcMarkValue:
			markValue(arena, state, entry.value, entry.depth)
		case gcMarkArrayElements:
			markNextArrayElement(arena, state, entry)
		case gcMarkCapacityElements:
			markNextCapacityElement(state, entry)
		case gcMarkMapEntries:
			markNextMapEntry(state, entry)
		case gcMarkMapEntryCopies:
			markNextMapEntryCopy(state, entry)
		default:
		}
	}
}

// takeMarkStep spends one step of the licensed mark's allowance (licenceMarkStepBudget).
//
// Returns bool which is false once the mark has settled, or once the allowance is spent,
// which fails the cycle closed.
func (state *gcMarkState) takeMarkStep() bool {
	if state.markSettled() {
		return false
	}
	if !state.licensed {
		return true
	}
	if state.stepBudget <= 0 {
		state.failClose(failClosedBudget)
		return false
	}
	state.stepBudget--
	return true
}

// endWalk hands back the map iterator and holders of a walk that has ended or that the
// mark drops.
//
// Takes entry (*gcMarkWork) which is the entry.
func (state *gcMarkState) endWalk(entry *gcMarkWork) {
	if entry.iterator != nil {
		state.releaseMapIterator(entry.iterator)
	}
	if entry.keyHolder != nil {
		entry.keyHolder.release()
	}
	if entry.elementHolder != nil {
		entry.elementHolder.release()
	}
}

// takeMapIterator returns an iterator over value, reusing one the mark state keeps.
//
// Takes value (reflect.Value) which is a non-nil map.
//
// Returns *reflect.MapIter which the caller hands back through releaseMapIterator.
func (state *gcMarkState) takeMapIterator(value reflect.Value) *reflect.MapIter {
	var iterator *reflect.MapIter
	if last := len(state.mapIterators) - 1; last >= 0 {
		iterator = state.mapIterators[last]
		state.mapIterators[last] = nil
		state.mapIterators = state.mapIterators[:last]
	} else {
		iterator = new(reflect.MapIter)
	}
	iterator.Reset(value)
	return iterator
}

// releaseMapIterator resets an iterator takeMapIterator returned, so it keeps no map
// reachable, and keeps it for the next walk while fewer than gcMapHolderSlots are kept.
//
// Takes iterator (*reflect.MapIter) which is the iterator.
func (state *gcMarkState) releaseMapIterator(iterator *reflect.MapIter) {
	iterator.Reset(reflect.Value{})
	if len(state.mapIterators) < gcMapHolderSlots {
		state.mapIterators = append(state.mapIterators, iterator)
	}
}

// clearWork drops every queued step, handing back what its walks hold (endWalk), and the
// work list itself once a cycle has grown it past gcMarkWorkKeptSize.
func (state *gcMarkState) clearWork() {
	for index := range state.work {
		state.endWalk(&state.work[index])
	}
	clear(state.work)
	state.work = state.work[:0]
	if cap(state.work) > gcMarkWorkKeptSize {
		state.work = nil
	}
}

// markValue marks one value.
//
// Classifies v by kind and follows references that can lead from one arena slab into
// another (pointers to arena pointees, interface roots wrapping arena values, slice
// headers whose backing lives in a slab), queueing each value it reaches (pushMark).
// Every hop that reaches a slab marks its generation live.
//
// Soundness rests on two invariants. First, only pointer-free values and slice headers
// are carved from the slabs, so a slab's contents never reference another slab except
// through a header's Data word. Second, the materialise barrier keeps heap objects from
// retaining slab pointers, so every live slab object is reachable from a VM root.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which is the value to inspect and mark.
// Takes depth (int) which is the number of hops already taken from the root.
func markValue(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if !v.IsValid() || state.markStopsAt(depth) || !state.readsValuePrivately(arena, v) {
		return
	}
	if state.licensed && v.Kind() == reflect.Struct && v.Type() == reflectValueReflectType {
		state.pushMark(reflectValueHeld(v), depth+1)
		return
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Array:
		markGenericBytesPointer(arena, state, ReflectValuePtr(v))
		markCompositeFields(arena, state, v, depth)
	case reflect.Slice:
		markSliceHeaderAt(arena, state, v, depth)
	case reflect.String:
		markString(arena, state, v.String())
	case reflect.Pointer:
		markPointer(arena, state, v, depth)
	case reflect.Interface:
		if !v.IsNil() && (!state.licensed || !isTypeDescriptor(v.Elem())) {
			state.pushMark(v.Elem(), depth+1)
		}
	default:
		if state.licensed {
			markUntracedKind(arena, state, v, depth)
		}
	}
}

// failClose records the first reason the licensed mark cannot vouch for every live
// reference, which makes the cycle compact nothing.
//
// Takes reason (licenceFailClosedReason) which says why.
func (state *gcMarkState) failClose(reason licenceFailClosedReason) {
	if state.failClosed == 0 {
		state.failClosed = reason
	}
}

// markStopsAt reports whether the mark stops before a value depth hops from its root. The
// mark has settled (markSettled), or, for a mark without a licence, the value lies past
// gcTraceMaxDepth.
//
// Takes depth (int) which is the value's distance from its root.
//
// Returns bool which is true when the value is not visited.
func (state *gcMarkState) markStopsAt(depth int) bool {
	if state.markSettled() {
		return true
	}
	return !state.licensed && depth > gcTraceMaxDepth
}

// firstVisit reports whether the licensed mark traces the referent at pointer, viewed as
// referentType with extent elements, for the first time this cycle, and records it.
//
// Takes pointer (unsafe.Pointer) which is the referent's address.
// Takes referentType (reflect.Type) which is the type it is traced as.
// Takes extent (int) which is the elements traced from pointer, or zero.
//
// Returns bool which is true when the caller traces the referent; false when the mark has
// traced it this cycle, or has failed closed with its visit budget spent.
func (state *gcMarkState) firstVisit(pointer unsafe.Pointer, referentType reflect.Type, extent int) bool {
	if pointer == nil {
		return false
	}
	key := gcVisitKey{pointer: pointer, referentType: referentType, extent: extent}
	if state.visited.contains(key) {
		return false
	}
	if state.visitBudget <= 0 {
		state.failClose(failClosedBudget)
		return false
	}
	state.visitBudget--
	state.visited.insert(key)
	return true
}

// clearVisited forgets the referents a cycle traced, dropping the set once a cycle has
// grown it past gcVisitedKeptSize so a pooled mark state keeps no large table.
func (state *gcMarkState) clearVisited() {
	state.visited.reset(gcVisitedKeptSize)
}

// markSettled reports whether the rest of a licensed mark can change nothing, so the mark
// may stop.
//
// Returns bool which is true when the mark may stop.
func (state *gcMarkState) markSettled() bool {
	return state.licensed && state.failClosed != 0
}

// spendTraceBudget takes one visit from the licensed mark's tracing allowance.
//
// Returns bool which is false, with the cycle failed closed, once the allowance is spent,
// or once the mark has settled (markSettled).
func (state *gcMarkState) spendTraceBudget() bool {
	if state.markSettled() {
		return false
	}
	if state.traceBudget <= 0 {
		state.failClose(failClosedBudget)
		return false
	}
	state.traceBudget--
	return true
}

// markUntracedKind is the licensed mark's handling of the kinds the default mark skips.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes value (reflect.Value) which is a map, channel, function or unsafe pointer.
// Takes depth (int) which is the current trace depth.
func markUntracedKind(arena *RegisterArena, state *gcMarkState, value reflect.Value, depth int) {
	switch value.Kind() {
	case reflect.Map:
		if !value.IsNil() && state.shared {
			state.failClose(failClosedShared)
			return
		}
		if !value.IsNil() && state.firstVisit(value.UnsafePointer(), value.Type(), 0) {
			markMapEntries(state, value, depth)
		}
	case reflect.Chan:
		if !value.IsNil() && value.Len() > 0 && !typeIsPointerFree(value.Type().Elem()) {
			state.failClose(failClosedChannel)
		}
	case reflect.Func:
		if !value.IsNil() && !markFunctionValue(arena, state, value, depth) {
			state.failClose(failClosedFunction)
		}
	case reflect.UnsafePointer:
		pointer := value.UnsafePointer()
		markSliceHeaderSlab(arena, state, pointer)
		markBackingPointer(arena, state, pointer)
	default:
	}
}

// markMapEntries starts the licensed mark's walk of a live map's entries, which
// markNextMapEntry or, for a map read through an unexported field, markNextMapEntryCopy
// takes one entry at a time.
//
// Takes state (*gcMarkState) which records the marks.
// Takes value (reflect.Value) which is a non-nil map.
// Takes depth (int) which is the current trace depth. Entries are traced one deeper.
func markMapEntries(state *gcMarkState, value reflect.Value, depth int) {
	mapType := value.Type()
	traceKey := !typeIsPointerFree(mapType.Key())
	traceElement := !typeIsPointerFree(mapType.Elem())
	if !traceKey && !traceElement {
		spendTraceBudgetFor(state, value.Len())
		return
	}
	walk := newMarkWalk(gcMarkMapEntryCopies, value, depth)
	walk.iterator, walk.traceKey, walk.traceElement = state.takeMapIterator(value), traceKey, traceElement
	if value.CanInterface() {
		walk.kind = gcMarkMapEntries
		if traceKey {
			walk.keyHolder = state.takeMapHolder(mapType.Key())
		}
		if traceElement {
			walk.elementHolder = state.takeMapHolder(mapType.Elem())
		}
	}
	state.queueWork(walk)
}

// markNextMapEntry takes a map walk's next entry.
//
// Takes state (*gcMarkState) which records the marks.
// Takes walk (gcMarkWork) which is the walk.
func markNextMapEntry(state *gcMarkState, walk gcMarkWork) {
	if !walk.iterator.Next() || !state.spendTraceBudget() {
		state.endWalk(&walk)
		return
	}
	if !state.queueWork(walk) {
		return
	}
	if walk.traceKey {
		walk.keyHolder.value.SetIterKey(walk.iterator)
		state.pushMark(walk.keyHolder.value, walk.depth+1)
	}
	if walk.traceElement {
		walk.elementHolder.value.SetIterValue(walk.iterator)
		state.pushMark(walk.elementHolder.value, walk.depth+1)
	}
}

// gcVisitKey names one referent the licensed mark traced (firstVisit).
type gcVisitKey struct {
	// pointer is the referent's address.
	pointer unsafe.Pointer

	// referentType is the type the referent is traced as.
	referentType reflect.Type

	// extent is the elements traced from pointer, for a slice backing; zero otherwise.
	extent int
}

// gcMapHolder is a settable value the map walk copies one key or element into.
type gcMapHolder struct {
	// value is the holder, addressable, of the key or element type.
	value reflect.Value

	// inUse is true while a walk holds it.
	inUse bool

	// kept is true when the holder belongs to the mark state's mapHolders.
	kept bool
}

// takeMapHolder returns a zero holder of type valueType for one map walk, reusing one the
// mark state keeps when one of that type is free.
//
// Takes valueType (reflect.Type) which is the key or element type.
//
// Returns *gcMapHolder which the caller releases after its walk.
func (state *gcMarkState) takeMapHolder(valueType reflect.Type) *gcMapHolder {
	for index := range state.mapHolders {
		holder := &state.mapHolders[index]
		if !holder.inUse && holder.value.Type() == valueType {
			holder.inUse = true
			return holder
		}
	}
	holder := gcMapHolder{value: reflect.New(valueType).Elem(), inUse: true, kept: false}
	if valueType.Size() > gcMapHolderMaxBytes || len(state.mapHolders) >= gcMapHolderSlots {
		return &holder
	}
	if state.mapHolders == nil {
		state.mapHolders = make([]gcMapHolder, 0, gcMapHolderSlots)
	}
	holder.kept = true
	state.mapHolders = append(state.mapHolders, holder)
	return &state.mapHolders[len(state.mapHolders)-1]
}

// release zeroes a holder takeMapHolder returned, after its walk, and frees it for the
// next.
func (holder *gcMapHolder) release() {
	if !holder.kept {
		return
	}
	holder.value.SetZero()
	holder.inUse = false
}

// markNextMapEntryCopy is markNextMapEntry for a map read through an unexported field,
// which queues the iterator's copy of each traced side.
//
// Takes state (*gcMarkState) which records the marks.
// Takes walk (gcMarkWork) which is the walk.
func markNextMapEntryCopy(state *gcMarkState, walk gcMarkWork) {
	if !walk.iterator.Next() || !state.spendTraceBudget() {
		state.endWalk(&walk)
		return
	}
	if !state.queueWork(walk) {
		return
	}
	if walk.traceKey {
		state.pushMark(walk.iterator.Key(), walk.depth+1)
	}
	if walk.traceElement {
		state.pushMark(walk.iterator.Value(), walk.depth+1)
	}
}

// spendTraceBudgetFor takes count visits from the licensed mark's tracing allowance at
// once, with the outcome of count calls to spendTraceBudget: the cycle fails closed, with
// the allowance spent, when count exceeds it.
//
// Takes state (*gcMarkState) which holds the allowance.
// Takes count (int) which is the number of visits.
func spendTraceBudgetFor(state *gcMarkState, count int) {
	if count > state.traceBudget {
		state.traceBudget = min(state.traceBudget, 0)
		state.failClose(failClosedBudget)
		return
	}
	state.traceBudget -= count
}

// markCompositeFields queues pointer-bearing struct fields, last first so the first is
// taken first, or starts the walk of an array's elements (markNextArrayElement), so that
// slice backings and strings reachable only through them stay alive.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes v (reflect.Value) which is the struct or array.
// Takes depth (int) which is the current trace depth. Fields are traced one level deeper.
func markCompositeFields(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if typeIsPointerFree(v.Type()) {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := v.NumField() - 1; i >= 0; i-- {
			if field := v.Field(i); innerValueMarks(arena, state, field) {
				state.pushMark(field, depth+1)
			}
		}
	case reflect.Array:
		if v.Len() != 0 {
			state.queueWork(newMarkWalk(gcMarkArrayElements, v, depth))
		}
	default:
	}
}

// markNextArrayElement takes an array walk's next element, queueing the walk's next step
// below it.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes walk (gcMarkWork) which is the walk.
func markNextArrayElement(arena *RegisterArena, state *gcMarkState, walk gcMarkWork) {
	element := walk.value.Index(walk.index)
	if walk.index++; walk.index < walk.value.Len() {
		state.queueWork(walk)
	}
	if innerValueMarks(arena, state, element) {
		state.pushMark(element, walk.depth+1)
	}
}

// innerValueMarks reports whether a field or element can add to its container's marks.
//
// The value must hold pointers, the mark must be allowed to read it (readsValuePrivately,
// which otherwise fails the mark closed), and it must not be a nil pointer, interface,
// map, channel or function, nor a slice of no capacity. A pointer-free inner value lies
// in the memory its container's mark already classified (markGenericBytesPointer), as
// does the header of a slice of no capacity, since no field lies in a slice-header slab.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes value (reflect.Value) which is the field or element.
//
// Returns bool which is false when the mark can skip it.
func innerValueMarks(arena *RegisterArena, state *gcMarkState, value reflect.Value) bool {
	if typeIsPointerFree(value.Type()) || !state.readsValuePrivately(arena, value) {
		return false
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Chan, reflect.Func:
		return !value.IsNil()
	case reflect.Slice:
		return value.Cap() != 0
	default:
		return true
	}
}

// markNextCapacityElement takes a slice backing walk's next element (capacityElement),
// queueing the walk's next step below it.
//
// Takes state (*gcMarkState) which records the marks.
// Takes walk (gcMarkWork) which is the walk.
func markNextCapacityElement(state *gcMarkState, walk gcMarkWork) {
	if !state.spendTraceBudget() {
		return
	}
	element := capacityElement(walk.value, walk.index)
	if walk.index++; walk.index < walk.value.Cap() {
		state.queueWork(walk)
	}
	state.pushMark(element, walk.depth+1)
}

// markPointer marks what a pointer reaches. A licensed mark traces a pointer to a closure
// or upvalue cell by its fields' meaning (markClosureOrCellPointer), and any other
// pointee once per cycle (firstVisit).
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes v (reflect.Value) which must be Pointer-kinded.
// Takes depth (int) which is the current trace depth from the root.
func markPointer(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if state.licensed {
		if markClosureOrCellPointer(arena, state, v, depth) {
			return
		}
		if !v.IsNil() && !state.firstVisit(v.UnsafePointer(), v.Type(), 0) {
			return
		}
	}
	markPointerTarget(arena, state, v, depth)
}

// markPointerTarget marks the slab holding a pointer's pointee.
//
// A pointer to a slice lands in the slice-header slab and its header is followed like any
// other slice. A pointer to a pointer-free value lands in the generic bytes slab.
// Pointers to values that can hold references (maps, channels, functions, further
// pointers) are always heap allocated and need no marking.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which must be Pointer-kinded.
// Takes depth (int) which is the current trace depth from the root.
func markPointerTarget(arena *RegisterArena, state *gcMarkState, v reflect.Value, depth int) {
	if v.IsNil() {
		return
	}
	pointee := v.UnsafePointer()
	switch v.Type().Elem().Kind() {
	case reflect.Slice:
		markSliceHeaderSlab(arena, state, pointee)
		markSliceHeaderAt(arena, state, v.Elem(), depth)
	case reflect.Map, reflect.Chan, reflect.Func, reflect.Pointer, reflect.Interface, reflect.UnsafePointer:
		if state.licensed {
			state.pushMark(v.Elem(), depth+1)
		}
	case reflect.Struct, reflect.Array:
		markGenericBytesPointer(arena, state, pointee)
		markCompositeFields(arena, state, v.Elem(), depth)
	default:
		markGenericBytesPointer(arena, state, pointee)
	}
}

// markSliceHeaderAt follows a Slice-kinded reflect.Value.
//
// It marks the header's home slab generation, then the backing that the header's Data
// word addresses, including spare capacity beyond Len, and finally recurses into string
// elements so their bytes are marked too.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes value (reflect.Value) which must be Slice-kinded.
// Takes depth (int) which is the current trace depth. Elements are traced one deeper.
func markSliceHeaderAt(arena *RegisterArena, state *gcMarkState, value reflect.Value, depth int) {
	if !state.readsValuePrivately(arena, value) {
		return
	}
	if headerPtr := ReflectValuePtr(value); headerPtr != nil {
		markSliceHeaderSlab(arena, state, headerPtr)
	}
	if value.Cap() == 0 {
		return
	}
	if markTypedSliceBacking(arena, state, value) {
		return
	}
	markBackingPointer(arena, state, value.UnsafePointer())
	if value.Type().Elem().Kind() == reflect.String {
		if value.Len() != 0 && !state.readsPrivately(arena, value.Pointer()) {
			return
		}
		for i := range value.Len() {
			markString(arena, state, value.Index(i).String())
		}
		return
	}
	if state.licensed && !typeIsPointerFree(value.Type().Elem()) && state.firstVisit(value.UnsafePointer(), value.Type(), value.Cap()) {
		queueCapacityElements(state, value, depth)
	}
}

// queueCapacityElements starts the licensed mark's walk of a pointer-bearing slice
// backing, one element at a time (markNextCapacityElement).
//
// Takes state (*gcMarkState) which records the marks.
// Takes value (reflect.Value) which is the slice.
// Takes depth (int) which is the current trace depth. Elements are traced one deeper.
func queueCapacityElements(state *gcMarkState, value reflect.Value, depth int) {
	if value.Cap() > state.traceBudget {
		spendTraceBudgetFor(state, value.Cap())
		return
	}
	state.queueWork(newMarkWalk(gcMarkCapacityElements, capacityElements(value), depth))
}

// markTypedSliceBacking marks the backing of a slice whose static type is one of the
// typed-bank element types, reusing the per-bank markers.
//
// The slice header is rebuilt from the value's data pointer, length and capacity rather
// than through reflect.TypeAssert, because a value read from an unexported struct field
// carries reflect's read-only flag, which TypeAssert refuses but the header accessors do
// not. The collector reaches such values when it traces struct fields.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes v (reflect.Value) which must be Slice-kinded.
//
// Returns true when the slice matched a typed bank and has been marked.
func markTypedSliceBacking(arena *RegisterArena, state *gcMarkState, v reflect.Value) bool {
	switch v.Type() {
	case typeInt64Slice:
		markIntSlice(arena, state, sliceHeaderOf[int64](v))
	case typeFloat64Slice:
		markFloatSlice(arena, state, sliceHeaderOf[float64](v))
	case typeStringSlice:
		markStringSlice(arena, state, sliceHeaderOf[string](v))
	case typeBoolSlice:
		markBoolSlice(arena, state, sliceHeaderOf[bool](v))
	case typeUint64Slice:
		markUintSlice(arena, state, sliceHeaderOf[uint64](v))
	case typeByteSlice:
		markByteSlice(arena, state, sliceHeaderOf[byte](v))
	default:
		return false
	}
	return true
}

// sliceHeaderOf rebuilds a []T from a slice-kinded reflect.Value of that exact type using
// only the accessors reflect permits on read-only values.
//
// Takes v (reflect.Value) which holds a []T.
//
// Returns the slice sharing v's backing store.
func sliceHeaderOf[T any](v reflect.Value) []T {
	capacity := v.Cap()
	if capacity == 0 {
		return nil
	}
	return unsafe.Slice((*T)(v.UnsafePointer()), capacity)[:v.Len()]
}

// markBackingPointer marks the slab generation holding an untyped slice backing.
//
// Takes arena (*RegisterArena) which owns the slabs being tested.
// Takes state (*gcMarkState) which records the liveness flags.
// Takes data (unsafe.Pointer) which is the backing's first element address.
func markBackingPointer(arena *RegisterArena, state *gcMarkState, data unsafe.Pointer) {
	if data == nil {
		return
	}
	markGenericBytesPointer(arena, state, data)
	markBytePointer(arena, state, data, 1)
	pointer := uintptr(data)
	markOldBackingHolding(pointer, arena.oldIntBackings, int64ElementSize, state.OldIntBackingLive)
	markOldBackingHolding(pointer, arena.oldFloatBackings, float64ElementSize, state.OldFloatBackingLive)
	markOldBackingHolding(pointer, arena.oldStringBackings, stringHeaderSize, state.OldStringBackingLive)
	markOldBackingHolding(pointer, arena.oldBoolBackings, boolElementSize, state.OldBoolBackingLive)
	markOldBackingHolding(pointer, arena.oldUintBackings, uint64ElementSize, state.OldUintBackingLive)
}

// markOldBackingHolding flags the retained backing slab of one typed class that contains
// pointer, if any.
//
// Takes pointer (uintptr) which is the address being classified.
// Takes backings ([][]T) which is the class's retention list.
// Takes stride (uintptr) which is the byte size of one element.
// Takes live ([]bool) which is the class's liveness bitmap.
func markOldBackingHolding[T any](pointer uintptr, backings [][]T, stride uintptr, live []bool) {
	for i, backing := range backings {
		if i >= len(live) || len(backing) == 0 {
			continue
		}
		base := uintptr(unsafe.Pointer(&backing[0]))
		if pointer >= base && pointer < base+uintptr(len(backing))*stride {
			live[i] = true
		}
	}
}

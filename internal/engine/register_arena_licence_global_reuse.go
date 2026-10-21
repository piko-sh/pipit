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
	"fmt"
	"math"
	"reflect"
	"slices"
	"sync/atomic"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// watchRange is one span of storage a watched global reaches.
type watchRange struct {
	// low and high bound the span, high exclusive.
	low, high uintptr
}

// GlobalReuseWatch watches the storage a file set's reference-typed globals reach, from
// the end of a run's variable initialisers (global reuse). A native call handed an
// argument that reaches it sets the dirty bit, unless the native is reads-only; the owner
// then re-runs the initialisers.
type GlobalReuseWatch struct {
	// store is the watched GlobalStore.
	store *GlobalStore

	// reason records why the watch is dirty.
	reason atomic.Pointer[string]

	// ranges are the watched spans, sorted by low; replaced, never written.
	ranges []watchRange

	// dirty is the dirty bit.
	dirty atomic.Bool
}

// watchCollector gathers the spans a global reaches.
type watchCollector struct {
	// seen holds the pointers already followed.
	seen map[uintptr]bool

	// ranges are the gathered spans.
	ranges []watchRange

	// incomplete is set when a bound was hit.
	incomplete bool
}

// watchCheck walks a native argument for watched storage.
type watchCheck struct {
	// watch is the watch consulted.
	watch *GlobalReuseWatch

	// visited counts the values walked.
	visited int

	// found is set when the argument reaches watched storage, or the walk ran out.
	found bool
}

// GlobalSlotImage is the value of every global slot of a file set at the end of a run's
// variable initialisers (global reuse).
type GlobalSlotImage struct {
	// slots are the file set's globals.
	slots []program.GlobalVariableInfo

	// values hold each slot's value.
	values []globalSlotValueImage

	// lengths are the store's bank lengths when the image was taken.
	lengths program.SlotAllocation
}

// globalSlotValueImage is one slot's value in an image.
type globalSlotValueImage struct {
	// general is a general slot's value, a copy for by-value aggregates.
	general reflect.Value

	// text is a string slot's value.
	text string

	// complexValue is a complex slot's value.
	complexValue complex128

	// bits is an int, uint, float or bool slot's value.
	bits uint64

	// present is false when the slot lay outside its bank.
	present bool
}

// WatchGlobalsForReuse arms a watch over the storage the general-bank slots reach, and
// installs it for store, replacing any earlier one.
//
// Takes store (*GlobalStore) which holds the globals.
// Takes slots ([]program.GlobalVariableInfo) which are the file set's globals.
//
// Returns *GlobalReuseWatch which starts clean, or dirty when the storage could not be
// recorded within the bounds.
func WatchGlobalsForReuse(store *GlobalStore, slots []program.GlobalVariableInfo) *GlobalReuseWatch {
	watch := &GlobalReuseWatch{store: store, reason: atomic.Pointer[string]{}, ranges: nil, dirty: atomic.Bool{}}
	collector := watchCollector{seen: map[uintptr]bool{}, ranges: nil, incomplete: false}
	for _, slot := range slots {
		if slot.IsIndirect {
			watch.markDirty("indirect global")
			continue
		}
		if slot.Kind != isa.RegisterGeneral {
			continue
		}
		collector.collect(store.GetGeneral(slot.Index), globalWatchDepth)
	}
	if collector.incomplete {
		watch.markDirty("global image exceeds the watch's bounds")
	}
	slices.SortFunc(collector.ranges, func(left, right watchRange) int {
		switch {
		case left.low < right.low:
			return -1
		case left.low > right.low:
			return 1
		default:
			return 0
		}
	})
	watch.ranges = collector.ranges
	if previous, loaded := globalReuseWatches.swap(store, watch); loaded {
		if previous != nil {
			previous.markDirty("replaced by another file set's watch on the same globals")
		}
	} else {
		globalReuseWatchCount.Add(1)
	}
	return watch
}

// Stop removes the watch from its store.
func (w *GlobalReuseWatch) Stop() {
	if w == nil {
		return
	}
	if globalReuseWatches.compareAndDelete(w.store, w) {
		globalReuseWatchCount.Add(-1)
	}
}

// Installed reports whether the watch is still the one installed for its store.
//
// Returns bool which is false once the watch was stopped or replaced.
func (w *GlobalReuseWatch) Installed() bool {
	if w == nil {
		return false
	}
	installed, ok := globalReuseWatches.load(w.store)
	return ok && installed == w
}

// StopGlobalReuseWatch removes whatever watch is installed for store.
//
// A service calls it when it closes, or once it is collected, so a watch never outlives
// the service whose globals it watches.
//
// Takes store (*GlobalStore) which holds the watched globals.
func StopGlobalReuseWatch(store *GlobalStore) {
	if store == nil {
		return
	}
	if _, loaded := globalReuseWatches.loadAndDelete(store); loaded {
		globalReuseWatchCount.Add(-1)
	}
}

// Dirty reports the dirty bit and why it was set.
//
// Returns bool which is true when the watch is nil or has been marked dirty.
// Returns string which is "no watch" for a nil receiver, the recorded dirty reason, or ""
// when clean.
func (w *GlobalReuseWatch) Dirty() (bool, string) {
	if w == nil {
		return true, "no watch"
	}
	if !w.dirty.Load() {
		return false, ""
	}
	reason := ""
	if r := w.reason.Load(); r != nil {
		reason = *r
	}
	return true, reason
}

// markDirty sets the dirty bit.
//
// Takes reason (string) which says why.
func (w *GlobalReuseWatch) markDirty(reason string) {
	if w == nil || w.dirty.Load() {
		return
	}
	w.reason.Store(&reason)
	w.dirty.Store(true)
}

// reaches reports whether address falls in a watched span.
//
// Takes address (uintptr) which is an address.
//
// Returns bool which is true when it does.
func (w *GlobalReuseWatch) reaches(address uintptr) bool {
	index, _ := slices.BinarySearchFunc(w.ranges, address, func(candidate watchRange, target uintptr) int {
		switch {
		case candidate.high <= target:
			return -1
		case candidate.low > target:
			return 1
		default:
			return 0
		}
	})
	return index < len(w.ranges) && w.ranges[index].low <= address && address < w.ranges[index].high
}

// add records a span.
//
// Takes low (uintptr) which is the span's starting address.
// Takes size (uintptr) which is the span's size in bytes.
//
// Returns bool which is false when the span was already recorded.
func (c *watchCollector) add(low, size uintptr) bool {
	if low == 0 || c.seen[low] {
		return false
	}
	c.seen[low] = true
	if len(c.ranges) >= globalWatchRangeLimit {
		c.incomplete = true
		return false
	}
	c.ranges = append(c.ranges, watchRange{low: low, high: low + max(size, 1)})
	return true
}

// collect follows value's references, recording what they reach.
//
// Takes value (reflect.Value) which is a global's value or a part of it.
// Takes depth (int) which bounds the walk.
func (c *watchCollector) collect(value reflect.Value, depth int) {
	if !value.IsValid() || c.incomplete {
		return
	}
	if depth <= 0 {
		if !typeIsPointerFree(value.Type()) && value.Kind() != reflect.String {
			c.incomplete = true
		}
		return
	}
	c.collectKind(value, depth)
}

// collectKind is collect's step by value's kind, once value is valid and within depth.
//
// Takes value (reflect.Value) which is a global's value or a part of it.
// Takes depth (int) which bounds the walk.
func (c *watchCollector) collectKind(value reflect.Value, depth int) {
	switch value.Kind() {
	case reflect.Slice:
		if value.Cap() != 0 && c.add(uintptr(value.UnsafePointer()), uintptr(value.Cap())*value.Type().Elem().Size()) {
			c.collectElements(value, depth)
		}
	case reflect.Array:
		c.collectElements(value, depth)
	case reflect.Map:
		if !value.IsNil() && c.add(uintptr(value.UnsafePointer()), 1) {
			c.collectEntries(value, depth)
		}
	case reflect.Pointer:
		if value.IsNil() || !c.add(uintptr(value.UnsafePointer()), value.Type().Elem().Size()) {
			return
		}
		c.collect(value.Elem(), depth-1)
	case reflect.Interface:
		c.collect(value.Elem(), depth)
	case reflect.Struct:
		for index := range value.NumField() {
			c.collect(structFieldValue(value, index), depth-1)
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if !value.IsNil() {
			c.add(uintptr(value.Pointer()), 1)
		}
	default:
	}
}

// collectEntries follows the keys and values of a map that may hold pointers, up to
// globalWatchElementLimit entries.
//
// Takes value (reflect.Value) which is the map, not nil.
// Takes depth (int) which bounds the walk.
func (c *watchCollector) collectEntries(value reflect.Value, depth int) {
	if typeIsPointerFree(value.Type().Key()) && typeIsPointerFree(value.Type().Elem()) {
		return
	}
	count := 0
	iterator := value.MapRange()
	for iterator.Next() {
		if count++; count > globalWatchElementLimit {
			c.incomplete = true
			return
		}
		c.collect(iterator.Key(), depth-1)
		c.collect(iterator.Value(), depth-1)
	}
}

// collectElements follows the elements of a slice or array.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which bounds the walk.
func (c *watchCollector) collectElements(value reflect.Value, depth int) {
	if element := value.Type().Elem(); element.Kind() == reflect.String || typeIsPointerFree(element) {
		return
	}
	if value.Len() > globalWatchElementLimit {
		c.incomplete = true
		return
	}
	for index := range value.Len() {
		c.collect(value.Index(index), depth-1)
	}
}

// noteNativeCallForGlobalReuse sets the dirty bit of vm's store's watch when a native
// call's argument, or its method receiver, reaches watched storage, unless the callee is
// reads-only (global reuse). handleCallNative calls it before any path runs the native.
//
// Takes vm (*VM) which makes the call.
// Takes registers (*Registers) which hold the arguments.
// Takes site (*program.CallSite) which locates them.
func noteNativeCallForGlobalReuse(vm *VM, registers *Registers, site *program.CallSite) {
	if vm == nil || vm.Globals == nil {
		return
	}
	watch, ok := globalReuseWatches.load(vm.Globals)
	if !ok || watch == nil || watch.dirty.Load() {
		return
	}
	function := registers.General[site.NativeRegister]
	if vm.Arena.nativeReadsOnly(function) {
		return
	}
	check := watchCheck{watch: watch, visited: 0, found: false}
	if site.IsMethod {
		check.visit(registers.General[site.MethodReceiverRegister], globalWatchDepth)
	}
	check.visitArguments(registers, site)
	if check.found {
		if licenceCounting {
			hostDeclarationCounters.dirtyByNative.Add(1)
		}
		watch.markDirty("native " + borrowDeclarationName(function) + " was handed a global's storage")
	}
}

// visitArguments walks a native call's reference-carrying arguments until one reaches
// watched storage.
//
// Takes registers (*Registers) which hold the arguments.
// Takes site (*program.CallSite) which locates them.
func (c *watchCheck) visitArguments(registers *Registers, site *program.CallSite) {
	for _, location := range site.Arguments {
		if c.found {
			return
		}
		if isScalarRegisterKind(location.Kind) || location.Kind == isa.RegisterString || location.IsSpilled || location.IsUpvalue {
			continue
		}
		if location.Kind == isa.RegisterGeneral && !location.IsIndirect {
			c.visit(registers.General[location.Register], globalWatchDepth)
		} else {
			c.visit(registerToReflectValue(nil, registers, location.Kind, location.Register), globalWatchDepth)
		}
	}
}

// visit walks value.
//
// Takes value (reflect.Value) which is the argument or a part of it.
// Takes depth (int) which bounds the walk.
func (c *watchCheck) visit(value reflect.Value, depth int) {
	if c.found || !value.IsValid() {
		return
	}
	if c.visited++; c.visited > globalWatchElementLimit || depth <= 0 {
		if value.Kind() != reflect.String && !typeIsPointerFree(value.Type()) {
			c.found = true
		}
		return
	}
	c.visitKind(value, depth)
}

// visitKind is visit's step by value's kind, once value is valid and within the walk's
// bounds.
//
// Takes value (reflect.Value) which is the argument or a part of it.
// Takes depth (int) which bounds the walk.
func (c *watchCheck) visitKind(value reflect.Value, depth int) {
	switch value.Kind() {
	case reflect.Slice:
		if value.Cap() != 0 && !c.reaches(uintptr(value.UnsafePointer())) {
			c.visitElements(value, depth)
		}
	case reflect.Array:
		c.visitElements(value, depth)
	case reflect.Map:
		if !value.IsNil() && !c.reaches(uintptr(value.UnsafePointer())) {
			c.visitEntries(value, depth)
		}
	case reflect.Pointer:
		if !value.IsNil() && !c.reaches(uintptr(value.UnsafePointer())) {
			c.visit(value.Elem(), depth-1)
		}
	case reflect.Interface:
		c.visit(value.Elem(), depth)
	case reflect.Struct:
		for index := range value.NumField() {
			c.visit(structFieldValue(value, index), depth-1)
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if !value.IsNil() {
			c.reaches(uintptr(value.Pointer()))
		}
	default:
	}
}

// reaches reports whether p is watched storage, and records the finding when it is.
//
// Takes p (uintptr) which is the address.
//
// Returns bool which is true when the watch reaches p.
func (c *watchCheck) reaches(p uintptr) bool {
	if c.watch.reaches(p) {
		c.found = true
	}
	return c.found
}

// visitEntries walks the keys and values of a map that may hold pointers, until one
// reaches watched storage.
//
// Takes value (reflect.Value) which is the map, not nil.
// Takes depth (int) which bounds the walk.
func (c *watchCheck) visitEntries(value reflect.Value, depth int) {
	if typeIsPointerFree(value.Type().Key()) && typeIsPointerFree(value.Type().Elem()) {
		return
	}
	iterator := value.MapRange()
	for iterator.Next() && !c.found {
		c.visit(iterator.Key(), depth-1)
		c.visit(iterator.Value(), depth-1)
	}
}

// visitElements walks the elements of a slice or array.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which bounds the walk.
func (c *watchCheck) visitElements(value reflect.Value, depth int) {
	if element := value.Type().Elem(); element.Kind() == reflect.String || typeIsPointerFree(element) {
		return
	}
	for index := range value.Len() {
		if c.found {
			return
		}
		c.visit(value.Index(index), depth-1)
	}
}

// CaptureGlobalSlotImage records slots' current values in store.
//
// Takes store (*GlobalStore) which holds the globals.
// Takes slots ([]program.GlobalVariableInfo) which are the file set's globals; the image
// keeps the slice, which the caller must not change.
//
// Returns *GlobalSlotImage which Matches compares later.
func CaptureGlobalSlotImage(store *GlobalStore, slots []program.GlobalVariableInfo) *GlobalSlotImage {
	image := &GlobalSlotImage{slots: slots, values: make([]globalSlotValueImage, len(slots)), lengths: store.Lengths()}
	for index, slot := range slots {
		value := readGlobalSlotImage(store, slot)
		if value.general.IsValid() && (value.general.Kind() == reflect.Struct || value.general.Kind() == reflect.Array) {
			copied := reflect.New(value.general.Type()).Elem()
			copied.Set(value.general)
			value.general = copied
		}
		image.values[index] = value
	}
	return image
}

// Matches reports whether every slot still holds its image value.
//
// Takes store (*GlobalStore) which holds the globals.
//
// Returns bool which is true when nothing differs.
// Returns string which names the first difference.
func (image *GlobalSlotImage) Matches(store *GlobalStore) (bool, string) {
	if image == nil {
		return false, "no image"
	}
	if store.Lengths() != image.lengths {
		return false, "global banks changed"
	}
	for index, slot := range image.slots {
		current, want := readGlobalSlotImage(store, slot), image.values[index]
		same := current.present == want.present && current.bits == want.bits && current.text == want.text &&
			current.complexValue == want.complexValue && sameGlobalValue(current.general, want.general, 2)
		if !same {
			return false, fmt.Sprintf("global slot %d (bank %d) changed", slot.Index, slot.SlotKind())
		}
	}
	return true, ""
}

// readGlobalSlotImage reads one global slot.
//
// Takes store (*GlobalStore) which holds the globals.
// Takes slot (program.GlobalVariableInfo) which names the slot.
//
// Returns globalSlotValueImage which is the slot's value.
func readGlobalSlotImage(store *GlobalStore, slot program.GlobalVariableInfo) globalSlotValueImage {
	if !store.indexInBank(slot) {
		return globalSlotValueImage{}
	}
	value := globalSlotValueImage{present: true, general: reflect.Value{}, text: "", complexValue: 0, bits: 0}
	switch slot.SlotKind() {
	case isa.RegisterInt64:
		value.bits = safeconv.Int64ToUint64Reinterpret(store.GetInt(slot.Index))
	case isa.RegisterFloat64:
		value.bits = math.Float64bits(store.GetFloat(slot.Index))
	case isa.RegisterString:
		value.text = store.GetString(slot.Index)
	case isa.RegisterBool:
		if store.GetBool(slot.Index) {
			value.bits = 1
		}
	case isa.RegisterUint64:
		value.bits = store.GetUint(slot.Index)
	case isa.RegisterComplex128:
		value.complexValue = store.getComplex(slot.Index)
	default:
		value.general = store.GetGeneral(slot.Index)
	}
	return value
}

// sameGlobalValue compares a slot's value with its image.
//
// Takes current (reflect.Value) which is the slot's value now.
// Takes image (reflect.Value) which is its value at the image.
// Takes depth (int) which bounds the look through interfaces.
//
// Returns bool which is true when the slot is unchanged.
func sameGlobalValue(current, image reflect.Value, depth int) bool {
	if current.IsValid() != image.IsValid() {
		return false
	}
	if !current.IsValid() {
		return true
	}
	if current.Type() != image.Type() {
		return false
	}
	switch current.Kind() {
	case reflect.Slice:
		return current.Len() == image.Len() && current.Cap() == image.Cap() &&
			(current.Cap() == 0 || current.UnsafePointer() == image.UnsafePointer())
	case reflect.Map, reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
		return current.UnsafePointer() == image.UnsafePointer()
	case reflect.Func:
		return current.Pointer() == image.Pointer() && current.IsNil() == image.IsNil()
	case reflect.Interface:
		if current.IsNil() || image.IsNil() {
			return current.IsNil() == image.IsNil()
		}
		return depth > 0 && sameGlobalValue(current.Elem(), image.Elem(), depth-1)
	case reflect.Struct, reflect.Array:
		if !current.Type().Comparable() {
			return false
		}
		return current.Equal(image)
	default:
		return current.Equal(image)
	}
}

// noteNativeCallIfGlobalsWatched is noteNativeCallForGlobalReuse when any global reuse
// watch is installed, and nothing otherwise.
//
// Takes vm (*VM) which makes the call.
// Takes registers (*Registers) which hold the arguments.
// Takes site (*program.CallSite) which locates them.
func noteNativeCallIfGlobalsWatched(vm *VM, registers *Registers, site *program.CallSite) {
	if globalReuseWatchCount.Load() != 0 {
		noteNativeCallForGlobalReuse(vm, registers, site)
	}
}

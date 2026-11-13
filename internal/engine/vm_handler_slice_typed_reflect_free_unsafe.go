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

//go:build go1.27 && !safe

package engine

import (
	"maps"
	"math"
	"reflect"
	"sync"
	"unsafe"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
)

const (
	// typedElementShapeSlotBits is the number of hash bits that pick a typedElementShapes
	// slot.
	typedElementShapeSlotBits = 10

	// typedElementShapeSlots is the number of direct-mapped slots in typedElementShapes.
	typedElementShapeSlots = 1 << typedElementShapeSlotBits

	// typedElementShapeHashBits is the width of the hash typedElementShapeSlot takes the
	// slot's bits from the top of.
	typedElementShapeHashBits = 64

	// typedElementShapeHashMultiplier spreads ABI type addresses over the slots (the
	// Fibonacci hashing constant, 2^64 divided by the golden ratio).
	typedElementShapeHashMultiplier = 0x9E3779B97F4A7C15

	// typedElementShapeTableLimit bounds the types typedElementShapeTable keeps; a type past
	// it has its shape worked out on each front miss instead.
	typedElementShapeTableLimit = 1 << 14

	// elementWidthByte is the size of an 8-bit integer element.
	elementWidthByte = 1

	// elementWidthHalf is the size of a 16-bit integer element.
	elementWidthHalf = 2

	// elementWidthWord is the size of a 32-bit integer element.
	elementWidthWord = 4
)

// typedContainer is how a typed collection register holds its elements.
type typedContainer uint8

const (
	// typedContainerNone is a collection whose elements the reflection-free routes decline.
	typedContainerNone typedContainer = iota

	// typedContainerSlice is a slice; the value's storage word addresses its header.
	typedContainerSlice

	// typedContainerArray is an array held in the value's storage.
	typedContainerArray

	// typedContainerArrayPointer is a pointer to an array.
	typedContainerArrayPointer
)

// typedElementShape is how one collection type lays out its elements, for the
// reflection-free element routes of the typed slice handlers.
type typedElementShape struct {
	// collectionType is the collection's ABI type, the key.
	collectionType unsafe.Pointer

	// elementSize is the element's size in bytes.
	elementSize int64

	// arrayLength is the array's length, for an array or a pointer to one.
	arrayLength int64

	// elementKind is the element's kind: a bool, integer, float or string kind, or
	// reflect.Invalid for any other element.
	elementKind reflect.Kind

	// container is how the collection holds its elements.
	container typedContainer

	// structElement is true when the element is a struct, whose storage
	// structElementStorage() addresses. elementSize is set for such an element too.
	structElement bool
}

// typedElementLocation is an element the reflection-free routes may read or write.
type typedElementLocation struct {
	// address is the element's address.
	address unsafe.Pointer

	// backing is the slice's backing, or nil for an array.
	backing unsafe.Pointer

	// size is the element's size in bytes.
	size int64

	// kind is the element's kind.
	kind reflect.Kind
}

// plainSliceHeader mirrors the runtime layout of a slice header.
type plainSliceHeader struct {
	// data is the backing's first element.
	data unsafe.Pointer

	// length is the slice's length.
	length int

	// capacity is the slice's capacity.
	capacity int
}

var (
	// typedElementShapes is a direct-mapped, lock-free front for typedElementShapeTable,
	// keyed by the collection's ABI type. Types are never freed, so an entry never goes
	// stale; a collision replaces the slot's entry with the table's, which costs a lookup
	// but no allocation.
	typedElementShapes [typedElementShapeSlots]frontPointer[typedElementShape]

	// typedElementShapeTable holds every shape worked out, one per collection type, as a
	// copy-on-write map read without a lock.
	typedElementShapeTable frontPointer[map[unsafe.Pointer]*typedElementShape]

	// typedElementShapeTableMu serialises additions to typedElementShapeTable.
	typedElementShapeTableMu sync.Mutex

	// plainByteSliceType is the ABI type pointer for []byte.
	plainByteSliceType = func() unsafe.Pointer {
		probe := reflect.ValueOf([]byte(nil))
		return (*unsafeReflectValue)(unsafe.Pointer(&probe)).typ
	}()
)

// locate finds an element once typedElementAt knows the collection's shape.
//
// typedElementAt locates a slice itself on a front hit and calls this for the rest. A
// write into an array needs it addressable, as reflect's setters do. A nil pointer to an
// array is left to the reflect route, which reports it.
//
// Takes raw (*unsafeReflectValue) which is the collection register's words, with a
// non-nil type and storage word, and not read-only when write is set.
// Takes index (int64) which is the element index.
// Takes write (bool) which is true when the element is about to be written.
//
// Returns location (typedElementLocation) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func (shape *typedElementShape) locate(raw *unsafeReflectValue, index int64, write bool) (location typedElementLocation, ok bool) {
	if shape.elementKind == reflect.Invalid || index < 0 {
		return typedElementLocation{}, false
	}
	var data, backing unsafe.Pointer
	length := shape.arrayLength
	switch shape.container {
	case typedContainerSlice:
		if raw.flag&flagIndir == 0 {
			return typedElementLocation{}, false
		}
		header := (*plainSliceHeader)(raw.ptr)
		data, backing, length = header.data, header.data, int64(header.length)
	case typedContainerArray:
		if raw.flag&flagIndir == 0 || (write && raw.flag&flagAddr == 0) {
			return typedElementLocation{}, false
		}
		data = raw.ptr
	case typedContainerArrayPointer:
		data = raw.ptr
		if raw.flag&flagIndir != 0 {
			data = *(*unsafe.Pointer)(raw.ptr)
		}
	default:
		return typedElementLocation{}, false
	}
	if data == nil || index >= length {
		return typedElementLocation{}, false
	}
	return typedElementLocation{address: unsafe.Add(data, index*shape.elementSize), backing: backing, size: shape.elementSize, kind: shape.elementKind}, true
}

// loadUnsigned reads an integer element as its bits, zero-extended from its width.
//
// Returns uint64 which is the element.
func (element typedElementLocation) loadUnsigned() uint64 {
	switch element.size {
	case elementWidthByte:
		return uint64(*(*uint8)(element.address))
	case elementWidthHalf:
		return uint64(*(*uint16)(element.address))
	case elementWidthWord:
		return uint64(*(*uint32)(element.address))
	default:
		return *(*uint64)(element.address)
	}
}

// loadSigned reads an integer element sign-extended from its width.
//
// Returns int64 which is the element.
func (element typedElementLocation) loadSigned() int64 {
	switch element.size {
	case elementWidthByte:
		return int64(*(*int8)(element.address))
	case elementWidthHalf:
		return int64(*(*int16)(element.address))
	case elementWidthWord:
		return int64(*(*int32)(element.address))
	default:
		return *(*int64)(element.address)
	}
}

// storeInteger writes the low bits of an integer into an integer element, truncated to
// its width as SetInt and SetUint truncate. Signed and unsigned elements of one width
// store the same bits.
//
// Takes bits (uint64) which is the value, a signed one reinterpreted.
func (element typedElementLocation) storeInteger(bits uint64) {
	switch element.size {
	case elementWidthByte:
		*(*uint8)(element.address) = uint8(bits & math.MaxUint8)
	case elementWidthHalf:
		*(*uint16)(element.address) = uint16(bits & math.MaxUint16)
	case elementWidthWord:
		*(*uint32)(element.address) = uint32(bits & math.MaxUint32)
	default:
		*(*uint64)(element.address) = bits
	}
}

// newTypedElementShape works out the shape of collectionType.
//
// Takes collectionType (reflect.Type) which is the collection's type.
// Takes key (unsafe.Pointer) which is its ABI type.
//
// Returns *typedElementShape which is the shape. Its element kind is reflect.Invalid for
// a collection the routes decline.
func newTypedElementShape(collectionType reflect.Type, key unsafe.Pointer) *typedElementShape {
	shape := &typedElementShape{
		collectionType: key, elementSize: 0, arrayLength: 0, elementKind: reflect.Invalid, container: typedContainerNone, structElement: false,
	}
	var elementType reflect.Type
	switch collectionType.Kind() {
	case reflect.Slice:
		shape.container, elementType = typedContainerSlice, collectionType.Elem()
	case reflect.Array:
		shape.container, elementType = typedContainerArray, collectionType.Elem()
		shape.arrayLength = int64(collectionType.Len())
	case reflect.Pointer:
		arrayType := collectionType.Elem()
		if arrayType.Kind() != reflect.Array {
			return shape
		}
		shape.container, elementType = typedContainerArrayPointer, arrayType.Elem()
		shape.arrayLength = int64(arrayType.Len())
	default:
		return shape
	}
	if typedElementKind(elementType.Kind()) {
		shape.elementKind = elementType.Kind()
		shape.elementSize = safeconv.UintptrToInt64(elementType.Size())
	}
	if elementType.Kind() == reflect.Struct {
		shape.structElement = true
		shape.elementSize = safeconv.UintptrToInt64(elementType.Size())
	}
	return shape
}

// byteSliceElements returns the bytes of a general register holding []byte.
//
// It answers only for the type []byte itself. A slice of any other type, named ones
// included, is left to typedElementAt.
//
// Takes collection (*reflect.Value) which is the collection register.
//
// Returns []byte which is the slice the register holds, or nil when it holds anything
// else.
func byteSliceElements(collection *reflect.Value) []byte {
	raw := (*unsafeReflectValue)(unsafe.Pointer(collection))
	if raw.typ != plainByteSliceType || raw.flag&flagIndir == 0 || raw.ptr == nil {
		return nil
	}
	return *(*[]byte)(raw.ptr)
}

// writableByteSliceElements is byteSliceElements for a store, which a read-only value
// refuses as typedElementAt refuses it.
//
// Takes collection (*reflect.Value) which is the collection register.
//
// Returns []byte which is the slice the register holds, or nil when it holds anything
// else or may not be written through.
func writableByteSliceElements(collection *reflect.Value) []byte {
	raw := (*unsafeReflectValue)(unsafe.Pointer(collection))
	if raw.typ != plainByteSliceType || raw.flag&(flagIndir|licenceFlagRO) != flagIndir || raw.ptr == nil {
		return nil
	}
	return *(*[]byte)(raw.ptr)
}

// typedElementAt locates element index of a typed collection register for a
// reflection-free read or write.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes write (bool) which is true when the element is about to be written.
//
// Returns location (typedElementLocation) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func typedElementAt(collection reflect.Value, index int64, write bool) (location typedElementLocation, ok bool) {
	raw := (*unsafeReflectValue)(unsafe.Pointer(&collection))
	if raw.typ == nil || raw.ptr == nil || (write && raw.flag&licenceFlagRO != 0) {
		return typedElementLocation{}, false
	}
	slot := typedElementShapeSlot(raw.typ)
	shape := slot.Load()
	if shape == nil || shape.collectionType != raw.typ {
		return typedElementShapeMiss(collection, raw.typ, slot).locate(raw, index, write)
	}
	if shape.container != typedContainerSlice || raw.flag&flagIndir == 0 {
		return shape.locate(raw, index, write)
	}
	header := (*plainSliceHeader)(raw.ptr)
	if shape.elementKind == reflect.Invalid || index < 0 || index >= int64(header.length) {
		return typedElementLocation{}, false
	}
	return typedElementLocation{address: unsafe.Add(header.data, index*shape.elementSize), backing: header.data, size: shape.elementSize, kind: shape.elementKind}, true
}

// typedElementShapeSlot returns the front slot of the collection type whose ABI type is
// key.
//
// Takes key (unsafe.Pointer) which is the collection's ABI type.
//
// Returns *frontPointer[typedElementShape] which is the slot.
func typedElementShapeSlot(key unsafe.Pointer) *frontPointer[typedElementShape] {
	return &typedElementShapes[(uint64(uintptr(key))*typedElementShapeHashMultiplier)>>(typedElementShapeHashBits-typedElementShapeSlotBits)]
}

// typedElementShapeMiss resolves a missing collection shape and fills its front slot.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes key (unsafe.Pointer) which is its ABI type.
// Takes slot (*frontPointer[typedElementShape]) which is the key's front slot.
//
// Returns *typedElementShape which is the shape, never nil.
func typedElementShapeMiss(collection reflect.Value, key unsafe.Pointer, slot *frontPointer[typedElementShape]) *typedElementShape {
	shape := typedElementShapeFromTable(collection, key)
	slot.Store(shape)
	return shape
}

// typedElementShapeFromTable returns the shape of collection's type from the table,
// working it out and adding it on the type's first use.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes key (unsafe.Pointer) which is its ABI type.
//
// Returns *typedElementShape which is the shape, never nil.
//
// Concurrency: Reads the table without a lock; additions copy it under
// typedElementShapeTableMu.
func typedElementShapeFromTable(collection reflect.Value, key unsafe.Pointer) *typedElementShape {
	if table := typedElementShapeTable.Load(); table != nil {
		if shape, found := (*table)[key]; found {
			return shape
		}
	}
	typedElementShapeTableMu.Lock()
	defer typedElementShapeTableMu.Unlock()
	current := typedElementShapeTable.Load()
	if current != nil {
		if shape, found := (*current)[key]; found {
			return shape
		}
	}
	shape := newTypedElementShape(collection.Type(), key)
	if current != nil && len(*current) >= typedElementShapeTableLimit {
		return shape
	}
	next := make(map[unsafe.Pointer]*typedElementShape, 1)
	if current != nil {
		next = maps.Clone(*current)
	}
	next[key] = shape
	typedElementShapeTable.Store(&next)
	return shape
}

// structElementStorage addresses a struct element of a slice, array or pointer to an
// array.
//
// Takes collection (*reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns unsafe.Pointer which addresses the element, or nil when the collection is
// anything else, is nil or the index is out of range, which the caller leaves to the
// element read that reports it.
func structElementStorage(collection *reflect.Value, index int64) unsafe.Pointer {
	raw := (*unsafeReflectValue)(unsafe.Pointer(collection))
	if raw.typ == nil || raw.ptr == nil || index < 0 {
		return nil
	}
	slot := typedElementShapeSlot(raw.typ)
	shape := slot.Load()
	if shape == nil || shape.collectionType != raw.typ {
		shape = typedElementShapeMiss(*collection, raw.typ, slot)
	}
	if !shape.structElement {
		return nil
	}
	data, length := raw.ptr, shape.arrayLength
	switch shape.container {
	case typedContainerSlice:
		if raw.flag&flagIndir == 0 {
			return nil
		}
		header := (*plainSliceHeader)(raw.ptr)
		data, length = header.data, int64(header.length)
	case typedContainerArray:
		if raw.flag&flagIndir == 0 {
			return nil
		}
	case typedContainerArrayPointer:
		if raw.flag&flagIndir != 0 {
			data = *(*unsafe.Pointer)(raw.ptr)
		}
	default:
		return nil
	}
	if data == nil || index >= length {
		return nil
	}
	return unsafe.Add(data, index*shape.elementSize)
}

// readStructElementUintFields makes the unsigned field reads of a run straight from the
// storage of a struct element, where structElementStorage() can address it.
//
// Takes frame (*CallFrame) which holds the layout table and the program counter.
// Takes registers (*Registers) which receives the fields.
// Takes collection (*reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes words ([]isa.Instruction) which are the run's extension words, one for each read.
// Takes base (int) which is the program counter of the first of them.
//
// Returns bool which is false, with nothing read, when the element cannot be addressed.
func readStructElementUintFields(frame *CallFrame, registers *Registers, collection *reflect.Value, index int64, words []isa.Instruction, base int) bool {
	storage := structElementStorage(collection, index)
	if storage == nil {
		return false
	}
	readUintFieldsAt(frame, registers, storage, words, base)
	return true
}

// typedElementKind reports whether the reflection-free routes read and write elements of
// kind.
//
// Takes kind (reflect.Kind) which is the element's kind.
//
// Returns bool which is true for bool, integer, float and string kinds.
func typedElementKind(kind reflect.Kind) bool {
	return kind == reflect.Bool || kind == reflect.String || isSignedIntKind(kind) || isUnsignedIntKind(kind) ||
		kind == reflect.Float32 || kind == reflect.Float64
}

// getElementInt64WithoutReflect is OpGetElementInt64's reflection-free route, with a
// signed element read as Index(index).Int(), an unsigned one reinterpreted as the reflect
// route does.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (int64) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func getElementInt64WithoutReflect(collection reflect.Value, index int64) (value int64, ok bool) {
	element, ok := typedElementAt(collection, index, false)
	switch {
	case !ok:
		return 0, false
	case isSignedIntKind(element.kind):
		return element.loadSigned(), true
	case isUnsignedIntKind(element.kind):
		return safeconv.Uint64ToInt64Reinterpret(element.loadUnsigned()), true
	default:
		return 0, false
	}
}

// setElementInt64WithoutReflect is OpSetElementInt64's reflection-free route, with a
// signed element written as Index(index).SetInt(value), an unsigned one as SetUint of the
// reinterpreted value, each truncated to the element's width as the setters truncate.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (int64) which is the value stored.
//
// Returns bool which is false when the caller must take the reflect route.
func setElementInt64WithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value int64) bool {
	element, ok := typedElementAt(collection, index, true)
	if !ok || (!isSignedIntKind(element.kind) && !isUnsignedIntKind(element.kind)) {
		return false
	}
	element.storeInteger(safeconv.Int64ToUint64Reinterpret(value))
	return true
}

// getElementUint64WithoutReflect is OpGetElementUint64's reflection-free route, with an
// unsigned element read as Index(index).Uint().
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (uint64) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func getElementUint64WithoutReflect(collection reflect.Value, index int64) (value uint64, ok bool) {
	element, ok := typedElementAt(collection, index, false)
	if !ok || !isUnsignedIntKind(element.kind) {
		return 0, false
	}
	return element.loadUnsigned(), true
}

// setElementUint64WithoutReflect is OpSetElementUint64's reflection-free route, with an
// unsigned element written as Index(index).SetUint(value), truncated to its width as
// SetUint truncates.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (uint64) which is the value stored.
//
// Returns bool which is false when the caller must take the reflect route.
func setElementUint64WithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value uint64) bool {
	element, ok := typedElementAt(collection, index, true)
	if !ok || !isUnsignedIntKind(element.kind) {
		return false
	}
	element.storeInteger(value)
	return true
}

// getElementFloat64WithoutReflect is OpGetElementFloat64's reflection-free route, with a
// float element read as Index(index).Float().
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (float64) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func getElementFloat64WithoutReflect(collection reflect.Value, index int64) (value float64, ok bool) {
	element, ok := typedElementAt(collection, index, false)
	switch {
	case !ok:
		return 0, false
	case element.kind == reflect.Float32:
		return float64(*(*float32)(element.address)), true
	case element.kind == reflect.Float64:
		return *(*float64)(element.address), true
	default:
		return 0, false
	}
}

// setElementFloat64WithoutReflect is OpSetElementFloat64's reflection-free route, with a
// float element written as Index(index).SetFloat(value), rounded to float32 as SetFloat
// rounds.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (float64) which is the value stored.
//
// Returns bool which is false when the caller must take the reflect route.
func setElementFloat64WithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value float64) bool {
	element, ok := typedElementAt(collection, index, true)
	if !ok || (element.kind != reflect.Float32 && element.kind != reflect.Float64) {
		return false
	}
	if element.kind == reflect.Float32 {
		*(*float32)(element.address) = float32(value)
		return true
	}
	*(*float64)(element.address) = value
	return true
}

// getElementBoolWithoutReflect is OpGetElementBool's reflection-free route, with a bool
// element read as Index(index).Bool().
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (bool) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func getElementBoolWithoutReflect(collection reflect.Value, index int64) (value bool, ok bool) {
	element, ok := typedElementAt(collection, index, false)
	if !ok || element.kind != reflect.Bool {
		return false, false
	}
	return *(*bool)(element.address), true
}

// setElementBoolWithoutReflect is OpSetElementBool's reflection-free route, with a bool
// element written as Index(index).SetBool(value).
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (bool) which is the value stored.
//
// Returns bool which is false when the caller must take the reflect route.
func setElementBoolWithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value bool) bool {
	element, ok := typedElementAt(collection, index, true)
	if !ok || element.kind != reflect.Bool {
		return false
	}
	*(*bool)(element.address) = value
	return true
}

// getElementStringWithoutReflect is OpGetElementString's reflection-free route, with a
// string element read as Index(index).String().
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (string) which is the element.
// Returns ok (bool) which is false when the caller must take the reflect route.
func getElementStringWithoutReflect(collection reflect.Value, index int64) (value string, ok bool) {
	element, ok := typedElementAt(collection, index, false)
	if !ok || element.kind != reflect.String {
		return "", false
	}
	return *(*string)(element.address), true
}

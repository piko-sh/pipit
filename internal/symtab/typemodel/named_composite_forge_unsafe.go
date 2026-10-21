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

package typemodel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"piko.sh/safeconv"
)

const (
	// namedCompositeForging is true in builds that forge runtime type descriptors, so named
	// composite types get runtime types of their own.
	namedCompositeForging = true

	// forgeGoRelease is the Go release whose internal/abi layout the mirror structs below
	// copy. Forging is refused on any other release until the mirrors are checked against
	// it.
	forgeGoRelease = "go1.27"

	// kindMask selects the reflect kind from a descriptor's kind byte (abi.KindMask).
	kindMask uint8 = 1<<5 - 1

	// variadicFlag marks a variadic function in a descriptor's outCount.
	variadicFlag uint16 = 1 << 15

	// tflagUncommon marks a type descriptor followed by an uncommon section
	// (abi.TFlagUncommon).
	tflagUncommon uint8 = 1 << 0

	// tflagExtraStar marks a descriptor whose name string carries a leading '*' that String
	// strips (abi.TFlagExtraStar).
	tflagExtraStar uint8 = 1 << 1

	// tflagNamed marks a named type (abi.TFlagNamed).
	tflagNamed uint8 = 1 << 2

	// tflagGCMaskOnDemand marks a descriptor whose pointer mask the runtime builds on first
	// use from the type's structure (abi.TFlagGCMaskOnDemand).
	tflagGCMaskOnDemand uint8 = 1 << 4

	// tflagIdentityBits are the flag bits that record a type's name rather than its layout,
	// which a forged named type and its underlying type may differ in.
	tflagIdentityBits = tflagUncommon | tflagExtraStar | tflagNamed

	// selfCheckEntries is how many entries the layout check stores in a forged map, enough
	// to fill several groups so the copied group layout and hasher are exercised.
	selfCheckEntries = 64

	// selfCheckLength is the length of the slice and array values the layout check makes.
	selfCheckLength = 3

	// selfCheckCapacity is the capacity of the slice the layout check makes.
	selfCheckCapacity = 5

	// selfCheckMarker is the value the layout check stores and expects to read back.
	selfCheckMarker = 7
)

// forgeLayoutError runs the layout check once per process and returns its verdict, which
// is nil when forging is sound on this Go release or an error wrapping
// ErrNamedCompositeUnsupported. A check that panics panics again on every later call
// instead of being reported as sound.
var forgeLayoutError = sync.OnceValue(checkForgeLayout)

// abiTypeHeader mirrors the leading abi.Type every runtime type descriptor starts with.
// The field order and widths match Go 1.27's internal/abi.Type.
type abiTypeHeader struct {
	// size is the size of a value of the type in bytes.
	size uintptr

	// ptrBytes is the length of the prefix of a value that can hold pointers.
	ptrBytes uintptr

	// hash is the type's hash, used by the runtime's interface tables.
	hash uint32

	// tflag holds the abi.TFlag bits.
	tflag uint8

	// align is a value's alignment.
	align uint8

	// fieldAlign is a struct field's alignment.
	fieldAlign uint8

	// kind is the reflect kind.
	kind uint8

	// equal is the equality function, nil for an incomparable type.
	equal unsafe.Pointer

	// gcData describes which words hold pointers.
	gcData unsafe.Pointer

	// str is the name offset of the type's string form.
	str int32

	// ptrToThis is the type offset of the pointer type, zero when none was compiled.
	ptrToThis int32
}

// abiUncommon mirrors abi.UncommonType, the section that records a named type's package
// path and methods.
type abiUncommon struct {
	// pkgPath is the name offset of the import path.
	pkgPath int32

	// mcount is the number of methods.
	mcount uint16

	// xcount is the number of exported methods.
	xcount uint16

	// moff is the offset from this section to the method array.
	moff uint32

	// unused pads the section as the runtime's does.
	unused uint32
}

// sliceTypeMirror mirrors abi.SliceType.
type sliceTypeMirror struct {
	// abiTypeHeader is the common descriptor.
	abiTypeHeader

	// elem is the element type.
	elem unsafe.Pointer
}

// arrayTypeMirror mirrors abi.ArrayType.
type arrayTypeMirror struct {
	// abiTypeHeader is the common descriptor.
	abiTypeHeader

	// elem is the element type.
	elem unsafe.Pointer

	// slice is the type of a slice of the element type.
	slice unsafe.Pointer

	// length is the array length.
	length uintptr
}

// chanTypeMirror mirrors abi.ChanType.
type chanTypeMirror struct {
	// abiTypeHeader is the common descriptor.
	abiTypeHeader

	// elem is the element type.
	elem unsafe.Pointer

	// dir is the channel direction.
	dir int
}

// mapTypeMirror mirrors abi.MapType.
type mapTypeMirror struct {
	// abiTypeHeader is the common descriptor.
	abiTypeHeader

	// key is the key type.
	key unsafe.Pointer

	// elem is the element type.
	elem unsafe.Pointer

	// group is the internal slot-group type.
	group unsafe.Pointer

	// hasher hashes a key.
	hasher unsafe.Pointer

	// groupSize is the size of a group.
	groupSize uintptr

	// keysOff is the offset of the first key in a group.
	keysOff uintptr

	// keyStride is the distance between keys.
	keyStride uintptr

	// elemsOff is the offset of the first element in a group.
	elemsOff uintptr

	// elemStride is the distance between elements.
	elemStride uintptr

	// elemOff is the offset of an element within its slot.
	elemOff uintptr

	// flags holds the map's flags.
	flags uint32
}

// funcTypeMirror mirrors abi.FuncType. The parameter and result types follow it, after
// the uncommon section when there is one.
type funcTypeMirror struct {
	// abiTypeHeader is the common descriptor.
	abiTypeHeader

	// inCount is the number of parameters.
	inCount uint16

	// outCount is the number of results, with the top bit set for a variadic function.
	outCount uint16
}

// namedSliceType is a forged named slice type, the descriptor followed by its uncommon
// section.
type namedSliceType struct {
	// sliceTypeMirror is the copied descriptor.
	sliceTypeMirror

	// uncommon records the package path.
	uncommon abiUncommon
}

// namedArrayType is a forged named array type.
type namedArrayType struct {
	// arrayTypeMirror is the copied descriptor.
	arrayTypeMirror

	// uncommon records the package path.
	uncommon abiUncommon
}

// namedChanType is a forged named channel type.
type namedChanType struct {
	// chanTypeMirror is the copied descriptor.
	chanTypeMirror

	// uncommon records the package path.
	uncommon abiUncommon
}

// namedMapType is a forged named map type.
type namedMapType struct {
	// mapTypeMirror is the copied descriptor.
	mapTypeMirror

	// uncommon records the package path.
	uncommon abiUncommon
}

// namedFuncPrefix is the head of a forged named function type. The parameter and result
// types follow it.
type namedFuncPrefix struct {
	// funcTypeMirror is the copied descriptor.
	funcTypeMirror

	// uncommon records the package path.
	uncommon abiUncommon
}

// forgedIdentity is what forgeNamedType writes over the copied descriptor.
type forgedIdentity struct {
	// uncommon is the uncommon section recording the package path.
	uncommon abiUncommon

	// str is the name offset of the qualified name.
	str int32

	// hashMix is folded into the copied hash so the named type hashes apart from its
	// underlying type in the runtime's interface tables.
	hashMix uint32
}

// apply marks header as the named type.
//
// Takes header (*abiTypeHeader) which is the copied descriptor's header.
func (identity forgedIdentity) apply(header *abiTypeHeader) {
	header.tflag = header.tflag&^tflagExtraStar | tflagNamed | tflagUncommon
	header.str = identity.str
	header.ptrToThis = 0
	header.hash ^= identity.hashMix
}

// addReflectOff registers a pointer in the runtime's table of names and types made at run
// time and returns the offset that resolves to it. reflect.StructOf names its types the
// same way.
//
// Takes pointer (unsafe.Pointer) which is the runtime name or type to register.
//
// Returns int32 which is the runtime table offset that resolves to pointer.
//
//go:linkname addReflectOff reflect.addReflectOff
func addReflectOff(pointer unsafe.Pointer) int32

// forgeNamedType builds the runtime type of a named type whose underlying type is
// underlying, as described on namedCompositeRegistry.
//
// The descriptor of the underlying type is copied into fresh memory typed so that the
// collector traces its pointers. The copy is marked named and uncommon, its string form
// is the qualified name, its uncommon section records the package path and no methods,
// and its pointer-to-this offset is cleared so reflect.PointerTo builds *T on demand
// rather than returning the underlying type's pointer type. Everything else, including
// the size, the pointer layout, the equality and hash functions and the element, key and
// parameter types, is the underlying type's own. The memory is never freed, as the
// registry holds the type for the life of the process, and the runtime's name table holds
// the names.
//
// Takes underlying (reflect.Type) which is an unnamed array, slice, map, channel or
// function type.
// Takes qualifiedName (string) which is the type's string form, e.g. "main.Points".
// Takes pkgPath (string) which is the declaring package's import path.
//
// Returns reflect.Type which is the named type.
// Returns error which wraps ErrNamedCompositeUnsupported for any other underlying type.
func forgeNamedType(underlying reflect.Type, qualifiedName, pkgPath string) (reflect.Type, error) {
	base := typeDescriptor(underlying)
	header := (*abiTypeHeader)(base)
	if header.tflag&(tflagNamed|tflagUncommon) != 0 {
		return nil, fmt.Errorf("%w: underlying type %s is already named", ErrNamedCompositeUnsupported, underlying)
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(pkgPath + "\x00" + qualifiedName))
	identity := forgedIdentity{
		str:      runtimeName(qualifiedName),
		hashMix:  hasher.Sum32(),
		uncommon: abiUncommon{pkgPath: runtimeName(pkgPath), mcount: 0, xcount: 0, moff: uint32(unsafe.Sizeof(abiUncommon{})), unused: 0},
	}
	var forged unsafe.Pointer
	switch underlying.Kind() {
	case reflect.Slice:
		named := &namedSliceType{sliceTypeMirror: *(*sliceTypeMirror)(base), uncommon: identity.uncommon}
		identity.apply(&named.abiTypeHeader)
		forged = unsafe.Pointer(named)
	case reflect.Array:
		named := &namedArrayType{arrayTypeMirror: *(*arrayTypeMirror)(base), uncommon: identity.uncommon}
		identity.apply(&named.abiTypeHeader)
		forged = unsafe.Pointer(named)
	case reflect.Chan:
		named := &namedChanType{chanTypeMirror: *(*chanTypeMirror)(base), uncommon: identity.uncommon}
		identity.apply(&named.abiTypeHeader)
		forged = unsafe.Pointer(named)
	case reflect.Map:
		named := &namedMapType{mapTypeMirror: *(*mapTypeMirror)(base), uncommon: identity.uncommon}
		identity.apply(&named.abiTypeHeader)
		forged = unsafe.Pointer(named)
	case reflect.Func:
		forged = forgeNamedFuncType(underlying, base, identity)
	default:
		return nil, fmt.Errorf("%w: %s has kind %s", ErrNamedCompositeUnsupported, qualifiedName, underlying.Kind())
	}
	return typeFromDescriptor(forged), nil
}

// forgeNamedFuncType copies a function type's descriptor together with its parameter and
// result types, which sit after the uncommon section in the named copy.
//
// Takes underlying (reflect.Type) which is the function type.
// Takes base (unsafe.Pointer) which is its descriptor.
// Takes identity (forgedIdentity) which is written over the copy.
//
// Returns unsafe.Pointer which is the forged descriptor.
func forgeNamedFuncType(underlying reflect.Type, base unsafe.Pointer, identity forgedIdentity) unsafe.Pointer {
	count := underlying.NumIn() + underlying.NumOut()
	layout := reflect.StructOf([]reflect.StructField{
		{Name: "Prefix", Type: reflect.TypeFor[namedFuncPrefix]()},
		{Name: "Types", Type: reflect.ArrayOf(count, reflect.TypeFor[unsafe.Pointer]())},
	})
	holder := reflect.New(layout).UnsafePointer()
	prefix := (*namedFuncPrefix)(holder)
	prefix.funcTypeMirror = *(*funcTypeMirror)(base)
	prefix.uncommon = identity.uncommon
	identity.apply(&prefix.abiTypeHeader)
	forgedTypes := unsafe.Slice((*unsafe.Pointer)(unsafe.Add(holder, layout.Field(1).Offset)), count)
	baseTypes := unsafe.Slice((*unsafe.Pointer)(unsafe.Add(base, unsafe.Sizeof(funcTypeMirror{}))), count)
	copy(forgedTypes, baseTypes)
	return holder
}

// runtimeName encodes s as a runtime name (abi.Name, which is a flags byte, a varint
// length and the bytes) and registers it, returning the name offset that resolves to it.
//
// Takes s (string) which is the name.
//
// Returns int32 which is the name offset.
func runtimeName(s string) int32 {
	var length [binary.MaxVarintLen64]byte
	lengthBytes := binary.PutUvarint(length[:], uint64(len(s)))
	encoded := make([]byte, 1+lengthBytes+len(s))
	copy(encoded[1:], length[:lengthBytes])
	copy(encoded[1+lengthBytes:], s)
	return addReflectOff(unsafe.Pointer(&encoded[0]))
}

// patchCycleMember gives a forged member the element, key, parameter and result types,
// the equality function and the pointer mask of its underlying type, once the two agree
// in size, alignment, kind, pointer layout, comparability and every flag that describes
// memory.
//
// Takes named (reflect.Type) which is the forged member, not yet visible to any value.
// Takes underlying (reflect.Type) which is its underlying type.
//
// Returns error naming the first disagreement.
func patchCycleMember(named, underlying reflect.Type) error {
	if underlying == nil || named.Kind() != underlying.Kind() {
		return fmt.Errorf("underlying type %v is not a %s", underlying, named.Kind())
	}
	target := typeDescriptor(named)
	source := typeDescriptor(underlying)
	if err := checkCycleHeaders((*abiTypeHeader)(target), (*abiTypeHeader)(source)); err != nil {
		return err
	}
	if err := patchCycleParts(named.Kind(), target, source); err != nil {
		return err
	}
	(*abiTypeHeader)(target).equal = (*abiTypeHeader)(source).equal
	(*abiTypeHeader)(target).gcData = (*abiTypeHeader)(source).gcData
	return nil
}

// patchCycleParts copies the kind-specific parts of a descriptor into a forged member.
//
// Takes kind (reflect.Kind) which is the member's kind.
// Takes target (unsafe.Pointer) which is the forged descriptor.
// Takes source (unsafe.Pointer) which is the underlying type's descriptor.
//
// Returns error naming the first disagreement.
func patchCycleParts(kind reflect.Kind, target, source unsafe.Pointer) error {
	switch kind {
	case reflect.Slice:
		(*sliceTypeMirror)(target).elem = (*sliceTypeMirror)(source).elem
	case reflect.Array:
		into, from := (*arrayTypeMirror)(target), (*arrayTypeMirror)(source)
		if into.length != from.length {
			return fmt.Errorf("array length %d differs from %d", into.length, from.length)
		}
		into.elem, into.slice = from.elem, from.slice
	case reflect.Chan:
		into, from := (*chanTypeMirror)(target), (*chanTypeMirror)(source)
		into.elem, into.dir = from.elem, from.dir
	case reflect.Map:
		into, from := (*mapTypeMirror)(target), (*mapTypeMirror)(source)
		header := into.abiTypeHeader
		*into = *from
		into.abiTypeHeader = header
	case reflect.Func:
		return patchCycleFunc(target, source)
	default:
		return fmt.Errorf("kind %s cannot be a member", kind)
	}
	return nil
}

// patchCycleFunc copies a function type's parameter and result types into a forged member
// with the same counts.
//
// Takes target (unsafe.Pointer) which is the forged descriptor, laid out as
// namedFuncPrefix followed by the types.
// Takes source (unsafe.Pointer) which is the unnamed function type's descriptor, laid out
// as funcTypeMirror followed by the types.
//
// Returns error when the counts differ.
func patchCycleFunc(target, source unsafe.Pointer) error {
	into, from := (*funcTypeMirror)(target), (*funcTypeMirror)(source)
	if into.inCount != from.inCount || into.outCount != from.outCount {
		return fmt.Errorf("signature has %d/%d parameters and results, want %d/%d", from.inCount, from.outCount, into.inCount, into.outCount)
	}
	count := int(from.inCount) + int(from.outCount&^variadicFlag)
	forgedTypes := unsafe.Slice((*unsafe.Pointer)(unsafe.Add(target, unsafe.Sizeof(namedFuncPrefix{}))), count)
	sourceTypes := unsafe.Slice((*unsafe.Pointer)(unsafe.Add(source, unsafe.Sizeof(funcTypeMirror{}))), count)
	copy(forgedTypes, sourceTypes)
	return nil
}

// checkCycleHeaders checks that a forged member's header, copied from its placeholder,
// describes values of its underlying type exactly.
//
// A pointer mask the runtime builds on demand is not compared here, since it may not
// exist yet. both types then build it from their own structure, and patchCycleMember
// makes the member read the underlying type's.
//
// Takes named (*abiTypeHeader) which is the forged header.
// Takes underlying (*abiTypeHeader) which is the underlying type's header.
//
// Returns error naming the first disagreement.
func checkCycleHeaders(named, underlying *abiTypeHeader) error {
	switch {
	case named.size != underlying.size, named.ptrBytes != underlying.ptrBytes,
		named.align != underlying.align, named.fieldAlign != underlying.fieldAlign, named.kind != underlying.kind:
		return fmt.Errorf("layout %d/%d/%d differs from %d/%d/%d", named.size, named.ptrBytes, named.align,
			underlying.size, underlying.ptrBytes, underlying.align)
	case (named.equal == nil) != (underlying.equal == nil):
		return errors.New("comparability differs")
	case named.tflag&^tflagIdentityBits != underlying.tflag&^tflagIdentityBits:
		return fmt.Errorf("type flags %#x differ from %#x", named.tflag, underlying.tflag)
	}
	if named.tflag&tflagGCMaskOnDemand != 0 || named.ptrBytes == 0 {
		return nil
	}
	maskBytes := int((named.ptrBytes/unsafe.Sizeof(uintptr(0)) + 7) / 8)
	if !bytes.Equal(unsafe.Slice((*byte)(named.gcData), maskBytes), unsafe.Slice((*byte)(underlying.gcData), maskBytes)) {
		return errors.New("pointer layout differs")
	}
	return nil
}

// typeDescriptor returns the runtime descriptor a reflect.Type points at.
//
// Takes t (reflect.Type) which must be non-nil.
//
// Returns unsafe.Pointer which is the descriptor.
func typeDescriptor(t reflect.Type) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&t))[1]
}

// typeFromDescriptor returns the reflect.Type of a runtime descriptor by reading the type
// word of an interface value that names it.
//
// Takes descriptor (unsafe.Pointer) which is a complete type descriptor.
//
// Returns reflect.Type which is the type.
func typeFromDescriptor(descriptor unsafe.Pointer) reflect.Type {
	var carrier any
	(*[2]unsafe.Pointer)(unsafe.Pointer(&carrier))[0] = descriptor
	return reflect.TypeOf(carrier)
}

// checkForgeLayout decides whether forging is sound on the running Go release.
//
// It first refuses any release other than forgeGoRelease, then reads (without writing)
// the descriptors of compiled types through the mirror structs and compares what it reads
// with reflect, so a moved field is caught before anything is forged. Only then does it
// forge one type of each kind and check the forged type against its underlying type
// through reflect. That covers its name and package path, its element, key, length,
// direction and signature, values made, stored, read and converted through it, and that
// it is a type of its own.
//
// Returns error which wraps ErrNamedCompositeUnsupported naming the first mismatch.
func checkForgeLayout() error {
	checks := []func() error{
		checkGoRelease, checkDescriptorMirrors,
		checkForgedSlice, checkForgedArray, checkForgedMap, checkForgedChan, checkForgedFunc,
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return fmt.Errorf("%w: %w", ErrNamedCompositeUnsupported, err)
		}
	}
	return nil
}

// checkGoRelease refuses a Go release whose type descriptor layout the mirror structs
// were not written for.
//
// Returns error naming the running release when it is not forgeGoRelease.
func checkGoRelease() error {
	return goReleaseError(runtime.Version())
}

// goReleaseError reports whether version is a release of forgeGoRelease, such as "go1.27"
// or "go1.27.1". A development build is refused, since its layout may have moved.
//
// Takes version (string) which is a runtime.Version string.
//
// Returns error naming version when it is another release.
func goReleaseError(version string) error {
	rest, found := strings.CutPrefix(version, forgeGoRelease)
	if !found || (rest != "" && !strings.HasPrefix(rest, ".")) {
		return fmt.Errorf("descriptor layout is mirrored from %s, running %s", forgeGoRelease, version)
	}
	return nil
}

// checkDescriptorMirrors reads compiled types' descriptors through the mirror structs and
// compares every field it reads with what reflect reports, without writing anything.
//
// Returns error naming the first field that disagrees.
func checkDescriptorMirrors() error {
	checks := []struct {
		check func() bool
		name  string
	}{
		{name: "abi.Type", check: mirroredHeaderMatches},
		{name: "abi.SliceType", check: func() bool {
			return (*sliceTypeMirror)(typeDescriptor(reflect.TypeFor[[]uint16]())).elem == typeDescriptor(reflect.TypeFor[uint16]())
		}},
		{name: "abi.ArrayType", check: func() bool {
			array := (*arrayTypeMirror)(typeDescriptor(reflect.TypeFor[[selfCheckLength]uint32]()))
			return array.length == selfCheckLength && array.elem == typeDescriptor(reflect.TypeFor[uint32]()) &&
				array.slice == typeDescriptor(reflect.TypeFor[[]uint32]())
		}},
		{name: "abi.ChanType", check: func() bool {
			channel := (*chanTypeMirror)(typeDescriptor(reflect.TypeFor[<-chan int8]()))
			return channel.elem == typeDescriptor(reflect.TypeFor[int8]()) && channel.dir == int(reflect.RecvDir)
		}},
		{name: "abi.MapType", check: func() bool {
			mapType := (*mapTypeMirror)(typeDescriptor(reflect.TypeFor[map[int16]string]()))
			return mapType.key == typeDescriptor(reflect.TypeFor[int16]()) && mapType.elem == typeDescriptor(reflect.TypeFor[string]()) &&
				mapType.group != nil && (*abiTypeHeader)(mapType.group).size == mapType.groupSize
		}},
		{name: "abi.FuncType", check: func() bool {
			function := (*funcTypeMirror)(typeDescriptor(reflect.TypeFor[func(int, ...string) bool]()))
			return function.inCount == 2 && function.outCount == 1|variadicFlag
		}},
	}
	for _, mirror := range checks {
		if !mirror.check() {
			return fmt.Errorf("the %s mirror does not match this release's descriptors", mirror.name)
		}
	}
	return nil
}

// mirroredHeaderMatches compares the common header of a few compiled types with reflect.
//
// Returns bool which is true when every field read agrees.
func mirroredHeaderMatches() bool {
	for _, probe := range []reflect.Type{reflect.TypeFor[[]int](), reflect.TypeFor[[3]uint16](), reflect.TypeFor[map[string]bool](), reflect.TypeFor[func()]()} {
		header := (*abiTypeHeader)(typeDescriptor(probe))
		if header.size != probe.Size() || int(header.align) != probe.Align() || int(header.fieldAlign) != probe.FieldAlign() ||
			header.kind&kindMask != safeconv.UintToUint8(uint(probe.Kind())) || (header.equal != nil) != probe.Comparable() {
			return false
		}
	}
	return true
}

// forgeForCheck forges a type for the layout check under a reserved package path.
//
// Takes underlying (reflect.Type) which is the type to name.
// Takes name (string) which is the type's name.
//
// Returns reflect.Type which is the forged type.
// Returns error when forging or the common identity checks fail.
func forgeForCheck(underlying reflect.Type, name string) (reflect.Type, error) {
	named, err := forgeNamedType(underlying, "layoutcheck."+name, "pipit.sh/layoutcheck")
	if err != nil {
		return nil, err
	}
	switch {
	case named == underlying:
		return nil, fmt.Errorf("forged %s is its underlying type", name)
	case named.Kind() != underlying.Kind(), named.Size() != underlying.Size(), named.Align() != underlying.Align():
		return nil, fmt.Errorf("forged %s differs from %s in kind, size or alignment", name, underlying)
	case named.Name() != name, named.String() != "layoutcheck."+name, named.PkgPath() != "pipit.sh/layoutcheck":
		return nil, fmt.Errorf("forged %s names itself %q, %q in %q", name, named.Name(), named.String(), named.PkgPath())
	case named.NumMethod() != 0, named.Comparable() != underlying.Comparable():
		return nil, fmt.Errorf("forged %s differs from %s in methods or comparability", name, underlying)
	case !underlying.ConvertibleTo(named), !named.ConvertibleTo(underlying), !named.AssignableTo(underlying):
		return nil, fmt.Errorf("forged %s does not convert to and from %s", name, underlying)
	case reflect.PointerTo(named) == reflect.PointerTo(underlying), reflect.PointerTo(named).Elem() != named:
		return nil, fmt.Errorf("forged %s shares its pointer type with %s", name, underlying)
	}
	return named, nil
}

// checkForgedSlice checks a forged []int.
//
// Returns error naming the first mismatch.
func checkForgedSlice() error {
	underlying := reflect.TypeFor[[]int]()
	named, err := forgeForCheck(underlying, "Ints")
	if err != nil {
		return err
	}
	value := reflect.MakeSlice(named, selfCheckLength, selfCheckCapacity)
	value.Index(selfCheckLength - 1).SetInt(selfCheckMarker)
	plain, ok := reflect.TypeAssert[[]int](value.Convert(underlying))
	if named.Elem() != underlying.Elem() || !ok || len(plain) != selfCheckLength || cap(plain) != selfCheckCapacity ||
		plain[selfCheckLength-1] != selfCheckMarker {
		return fmt.Errorf("forged slice holds %v, want its last element %d", plain, selfCheckMarker)
	}
	if _, isPlain := reflect.TypeAssert[[]int](reflect.ValueOf(value.Interface())); isPlain {
		return fmt.Errorf("forged slice asserts to %s", underlying)
	}
	return nil
}

// checkForgedArray checks a forged array of uint16.
//
// Returns error naming the first mismatch.
func checkForgedArray() error {
	underlying := reflect.TypeFor[[selfCheckLength]uint16]()
	named, err := forgeForCheck(underlying, "Triple")
	if err != nil {
		return err
	}
	value := reflect.New(named).Elem()
	value.Index(selfCheckLength - 1).SetUint(selfCheckMarker)
	plain, ok := reflect.TypeAssert[[selfCheckLength]uint16](value.Convert(underlying))
	if named.Len() != selfCheckLength || named.Elem() != underlying.Elem() || !ok || plain[selfCheckLength-1] != selfCheckMarker {
		return fmt.Errorf("forged array holds %v, want its last element %d", plain, selfCheckMarker)
	}
	if value.Interface() == any(plain) {
		return fmt.Errorf("forged array equals %s in an interface", underlying)
	}
	return nil
}

// checkForgedMap checks a forged map[int]int through enough entries to span groups.
//
// Returns error naming the first mismatch.
func checkForgedMap() error {
	underlying := reflect.TypeFor[map[int]int]()
	named, err := forgeForCheck(underlying, "Counts")
	if err != nil {
		return err
	}
	value := reflect.MakeMap(named)
	for index := range selfCheckEntries {
		value.SetMapIndex(reflect.ValueOf(index), reflect.ValueOf(-index))
	}
	plain, ok := reflect.TypeAssert[map[int]int](value.Convert(underlying))
	if named.Key() != underlying.Key() || named.Elem() != underlying.Elem() || !ok ||
		len(plain) != selfCheckEntries || plain[selfCheckMarker] != -selfCheckMarker ||
		value.MapIndex(reflect.ValueOf(selfCheckMarker)).Int() != -selfCheckMarker {
		return fmt.Errorf("forged map holds %d entries, want %d", len(plain), selfCheckEntries)
	}
	return nil
}

// checkForgedChan checks a forged chan int.
//
// Returns error naming the first mismatch.
func checkForgedChan() error {
	underlying := reflect.TypeFor[chan int]()
	named, err := forgeForCheck(underlying, "Queue")
	if err != nil {
		return err
	}
	value := reflect.MakeChan(named, 1)
	value.Send(reflect.ValueOf(selfCheckMarker))
	received, ok := value.Recv()
	if named.ChanDir() != reflect.BothDir || named.Elem() != underlying.Elem() || !ok || received.Int() != selfCheckMarker {
		return fmt.Errorf("forged channel received %v, want %d", received, selfCheckMarker)
	}
	return nil
}

// checkForgedFunc checks a forged variadic function type and a call through it.
//
// Returns error naming the first mismatch.
func checkForgedFunc() error {
	underlying := reflect.TypeFor[func(string, ...int) (int, error)]()
	named, err := forgeForCheck(underlying, "Counter")
	if err != nil {
		return err
	}
	if named.NumIn() != 2 || named.NumOut() != 2 || !named.IsVariadic() ||
		named.In(0) != underlying.In(0) || named.In(1) != underlying.In(1) || named.Out(1) != underlying.Out(1) {
		return fmt.Errorf("forged function type has signature %s, want %s", named, underlying)
	}
	function := reflect.MakeFunc(named, func(arguments []reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(len(arguments[0].String()) + arguments[1].Len()), reflect.Zero(underlying.Out(1))}
	})
	results := function.Call([]reflect.Value{reflect.ValueOf("ab"), reflect.ValueOf(1), reflect.ValueOf(2)})
	if len(results) != named.NumOut() || results[0].Int() != int64(len("ab")+2) {
		return fmt.Errorf("forged function returned %v, want %d", results, len("ab")+2)
	}
	return nil
}

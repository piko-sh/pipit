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

package program

import (
	"fmt"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

const (
	// defaultMaxConstantPoolSize is the default upper bound on entries in each per-function
	// constant pool (int, float, string, bool, uint, complex, general, type, callSites).
	// uint16 indices saturate at 65535, so this also bounds the encoding.
	defaultMaxConstantPoolSize = 65535

	// defaultMaxSpecialisations is the default upper bound on the number of generic-function
	// specialisations registered per generic callee.
	defaultMaxSpecialisations = 1000

	// defaultMaxMethods is the default upper bound on the size of methodTable on the root
	// CompiledFunction.
	defaultMaxMethods = 10000

	// MaxSpecialisationsPerFunction caps how many instantiations a generic function may
	// accumulate. Beyond this, new call sites fall back to the type-erased dispatch path.
	MaxSpecialisationsPerFunction = 32
)

// typeRefMethodsKey identifies a type-table entry that carries interface method
// requirements: the reflect type together with the encoded requirement list.
type typeRefMethodsKey struct {
	// reflectType is the Go reflect type of the entry.
	reflectType reflect.Type

	// methods is the encoded requirement list joined by null bytes.
	methods string
}

// rememberTypeRefMethods records the type-table index for a method-bearing entry.
//
// Takes key (typeRefMethodsKey) which identifies the entry.
// Takes index (uint16) which is its type-table index.
func (compiledFunction *CompiledFunction) rememberTypeRefMethods(key typeRefMethodsKey, index uint16) {
	if compiledFunction.typeRefMethodsIndex == nil {
		compiledFunction.typeRefMethodsIndex = make(map[typeRefMethodsKey]uint16)
	}
	compiledFunction.typeRefMethodsIndex[key] = index
}

// SpecialisationsCap returns the per-generic specialisation ceiling, substituting the
// package default when none was set.
//
// Takes compiledFunction (*CompiledFunction) which carries the per-function cap.
//
// Returns int which is the maximum number of specialisations permitted.
func SpecialisationsCap(compiledFunction *CompiledFunction) int {
	if compiledFunction.maxSpecialisations > 0 {
		return compiledFunction.maxSpecialisations
	}
	return defaultMaxSpecialisations
}

// RegisterMethod records functionIndex under tableName in the receiver's methodTable,
// defending against pathological method declarations by capping the table size.
//
// Takes compiledFunction (*CompiledFunction) which owns the method table.
// Takes tableName (string) which is the "TypeName.MethodName" key.
// Takes functionIndex (uint16) which is the position of the method's CompiledFunction in
// rootFunction.functions.
//
// Returns nil on success, or errMethodTableExhausted when adding the entry would exceed
// the configured ceiling.
func RegisterMethod(compiledFunction *CompiledFunction, tableName string, functionIndex uint16) error {
	if _, ok := compiledFunction.methodTable[tableName]; ok {
		compiledFunction.methodTable[tableName] = functionIndex
		return nil
	}
	if len(compiledFunction.methodTable) >= methodsCap(compiledFunction) {
		return fmt.Errorf("%w: %s", errMethodTableExhausted, tableName)
	}
	if compiledFunction.methodTable == nil {
		compiledFunction.methodTable = make(map[string]uint16)
	}
	compiledFunction.methodTable[tableName] = functionIndex
	return nil
}

// LookupSpecialisation returns the function index of an existing specialisation matching
// key, or false when no specialisation has been registered for that type-args tuple.
//
// Takes compiledFunction (*CompiledFunction) which holds the specialisation map.
// Takes key (*SpecialisationKey) which is the type-args tuple to look up. Taken by
// pointer because the widened Go 1.27 key is 256 bytes and this runs per call site.
//
// Returns the function index of the specialised CompiledFunction in
// rootFunction.functions, and a bool indicating whether a match was found.
func LookupSpecialisation(compiledFunction *CompiledFunction, key *SpecialisationKey) (uint16, bool) {
	if compiledFunction.Specialisations == nil {
		return 0, false
	}
	index, ok := compiledFunction.Specialisations[*key]
	return index, ok
}

// RegisterSpecialisation records the function index of a fresh specialised body for the
// given type-args tuple. Must be called BEFORE the body is emitted so recursive generic
// calls within the body find the reserved index and emit a normal isa.SubOpCall rather
// than triggering an infinite re-specialisation cascade.
//
// Takes compiledFunction (*CompiledFunction) which holds the specialisation map.
// Takes key (*SpecialisationKey) which is the type-args tuple. Taken by pointer for the
// same reason LookupSpecialisation does.
// Takes functionIndex (uint16) which is the index of the specialised CompiledFunction in
// rootFunction.functions.
//
// Returns nil on success, or errSpecialisationLimitReached when the configured ceiling
// for this generic callee is exhausted; callers should fall back to the generic reflect
// path on this error.
func RegisterSpecialisation(compiledFunction *CompiledFunction, key *SpecialisationKey, functionIndex uint16) error {
	if _, ok := compiledFunction.Specialisations[*key]; ok {
		compiledFunction.Specialisations[*key] = functionIndex
		return nil
	}
	if len(compiledFunction.Specialisations) >= SpecialisationsCap(compiledFunction) {
		return fmt.Errorf("%w: %s", errSpecialisationLimitReached, compiledFunction.Name)
	}
	if compiledFunction.Specialisations == nil {
		compiledFunction.Specialisations = make(map[SpecialisationKey]uint16, 1)
	}
	compiledFunction.Specialisations[*key] = functionIndex
	return nil
}

// AddCallSite adds a call site and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes site (*CallSite) which is the call-site descriptor to append. Pointer rather than
// value because CallSite is large (~456 bytes); the receiver still gets a fresh copy in
// the slice.
//
// Returns the index of the newly added call site, or errConstantPoolExhausted when the
// call-site table has reached its configured ceiling.
func AddCallSite(compiledFunction *CompiledFunction, site *CallSite) (uint16, error) {
	if len(compiledFunction.CallSites) >= constantPoolCap(compiledFunction) {
		return 0, fmt.Errorf("%w: callSites", errConstantPoolExhausted)
	}
	index := len(compiledFunction.CallSites)
	compiledFunction.CallSites = append(compiledFunction.CallSites, *site)
	return safeconv.IntToUint16(index), nil
}

// AddIntConstant adds an int64 constant and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (int64) which is the constant value to add or look up.
//
// Returns the index of the constant in the IntConstants pool, or errConstantPoolExhausted
// when the pool has reached its ceiling.
func AddIntConstant(compiledFunction *CompiledFunction, v int64) (uint16, error) {
	return addDedupedConstant(&compiledFunction.IntConstants, &compiledFunction.IntConstIndex, v, constantPoolCap(compiledFunction))
}

// AddFloatConstant adds a float64 constant and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (float64) which is the constant value to add or look up.
//
// Returns the index of the constant in the FloatConstants pool, or
// errConstantPoolExhausted when the pool has reached its ceiling.
func AddFloatConstant(compiledFunction *CompiledFunction, v float64) (uint16, error) {
	return addDedupedConstant(&compiledFunction.FloatConstants, &compiledFunction.FloatConstIndex, v, constantPoolCap(compiledFunction))
}

// AddStringConstant adds a string constant and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (string) which is the constant value to add or look up.
//
// Returns the index of the constant in the StringConstants pool, or
// errConstantPoolExhausted when the pool has reached its ceiling.
func AddStringConstant(compiledFunction *CompiledFunction, v string) (uint16, error) {
	return addDedupedConstant(&compiledFunction.StringConstants, &compiledFunction.StringConstIndex, v, constantPoolCap(compiledFunction))
}

// AddGeneralConstant adds a reflect.Value constant with its reconstruction descriptor and
// returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (reflect.Value) which is the constant value to add.
// Takes constantDescriptor (descriptor.GeneralConstantDescriptor) which records how to
// reconstruct the value from a serialised form.
//
// Returns the index of the constant in the GeneralConstants pool, or
// errConstantPoolExhausted when the pool has reached its ceiling.
func AddGeneralConstant(compiledFunction *CompiledFunction, v reflect.Value, constantDescriptor descriptor.GeneralConstantDescriptor) (uint16, error) {
	if len(compiledFunction.GeneralConstants) >= constantPoolCap(compiledFunction) {
		return 0, fmt.Errorf("%w: generalConstants", errConstantPoolExhausted)
	}
	index := len(compiledFunction.GeneralConstants)
	compiledFunction.GeneralConstants = append(compiledFunction.GeneralConstants, v)
	compiledFunction.GeneralConstantDescriptors = append(compiledFunction.GeneralConstantDescriptors, constantDescriptor)
	return safeconv.IntToUint16(index), nil
}

// AddBoolConstant adds a bool constant and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (bool) which is the constant value to add or look up.
//
// Returns the index of the constant in the BoolConstants pool, or
// errConstantPoolExhausted when the pool has reached its ceiling.
func AddBoolConstant(compiledFunction *CompiledFunction, v bool) (uint16, error) {
	for i, c := range compiledFunction.BoolConstants {
		if c == v {
			return safeconv.IntToUint16(i), nil
		}
	}
	if len(compiledFunction.BoolConstants) >= constantPoolCap(compiledFunction) {
		return 0, fmt.Errorf("%w: boolConstants", errConstantPoolExhausted)
	}
	index := len(compiledFunction.BoolConstants)
	compiledFunction.BoolConstants = append(compiledFunction.BoolConstants, v)
	return safeconv.IntToUint16(index), nil
}

// AddUintConstant adds a uint64 constant and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (uint64) which is the constant value to add or look up.
//
// Returns the index of the constant in the UintConstants pool, or
// errConstantPoolExhausted when the pool has reached its ceiling.
func AddUintConstant(compiledFunction *CompiledFunction, v uint64) (uint16, error) {
	return addDedupedConstant(&compiledFunction.UintConstants, &compiledFunction.UintConstIndex, v, constantPoolCap(compiledFunction))
}

// AddComplexConstant adds a complex128 constant and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the constant pool.
// Takes v (complex128) which is the constant value to add or look up.
//
// Returns the index of the constant in the ComplexConstants pool, or
// errConstantPoolExhausted when the pool has reached its ceiling.
func AddComplexConstant(compiledFunction *CompiledFunction, v complex128) (uint16, error) {
	return addDedupedConstant(&compiledFunction.ComplexConstants, &compiledFunction.ComplexConstIndex, v, constantPoolCap(compiledFunction))
}

// AddTypeRef adds a reflect.Type to the type table and returns its index.
//
// Takes compiledFunction (*CompiledFunction) which owns the type table.
// Takes t (reflect.Type) which is the type to add or look up.
//
// Returns the index of the type in the TypeTable, or errConstantPoolExhausted when the
// table has reached its ceiling.
func AddTypeRef(compiledFunction *CompiledFunction, t reflect.Type) (uint16, error) {
	return AddTypeRefWithMethods(compiledFunction, t, nil)
}

// AddTypeRefWithMethods registers a reflect.Type entry with method-set constraints. A nil
// methods slice means no constraints (empty interface / any).
//
// Takes compiledFunction (*CompiledFunction) which owns the type table.
// Takes t (reflect.Type) which is the type to register.
// Takes methods ([]string) which lists required method names, or nil.
//
// Returns uint16 which is the typeTable index of t (existing or fresh).
// Returns error when the table is full (errConstantPoolExhausted).
func AddTypeRefWithMethods(compiledFunction *CompiledFunction, t reflect.Type, methods []string) (uint16, error) {
	if compiledFunction.TypeRefIndex == nil && len(compiledFunction.TypeTable) > 0 {
		compiledFunction.TypeRefIndex = make(map[reflect.Type]uint16, len(compiledFunction.TypeTable))
		for i, c := range compiledFunction.TypeTable {
			if _, ok := compiledFunction.TypeRefIndex[c]; !ok {
				compiledFunction.TypeRefIndex[c] = uint16(i)
			}
		}
	}

	if len(methods) > 0 {
		key := typeRefMethodsKey{reflectType: t, methods: strings.Join(methods, "\x00")}
		if index, ok := compiledFunction.typeRefMethodsIndex[key]; ok {
			return index, nil
		}
		index, err := appendTypeRef(compiledFunction, t, methods)
		if err != nil {
			return 0, err
		}
		compiledFunction.rememberTypeRefMethods(key, index)
		return index, nil
	}
	if index, ok := compiledFunction.TypeRefIndex[t]; ok {
		return index, nil
	}
	index, err := appendTypeRef(compiledFunction, t, nil)
	if err != nil {
		return 0, err
	}
	if compiledFunction.TypeRefIndex == nil {
		compiledFunction.TypeRefIndex = make(map[reflect.Type]uint16)
	}
	compiledFunction.TypeRefIndex[t] = index
	return index, nil
}

// Emit appends an instruction to the function body and returns its offset for later
// patching.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes op (isa.Opcode) which is the opcode.
// Takes a (uint8) which is the first instruction operand.
// Takes b (uint8) which is the second instruction operand.
// Takes c (uint8) which is the third instruction operand.
//
// Returns the instruction offset in the body.
func Emit(compiledFunction *CompiledFunction, op isa.Opcode, a, b, c uint8) int {
	pc := len(compiledFunction.Body)
	instruction := isa.NewInstruction(op, a, b, c)
	compiledFunction.Body = append(compiledFunction.Body, instruction)
	if compiledFunction.EmittedInlineBlocker == InlineRefusalUnknown {
		if r := BlockerForInstruction(instruction); r != InlineRefusalUnknown {
			compiledFunction.EmittedInlineBlocker = r
		}
	}
	if compiledFunction.DebugEmitHook != nil {
		compiledFunction.DebugEmitHook(pc)
	}
	return pc
}

// EmitWide emits an instruction with a 16-bit wide index split across B and C operands.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes op (isa.Opcode) which is the opcode.
// Takes a (uint8) which is the first operand.
// Takes wide (uint16) which is the 16-bit index to encode in B (low byte) and C (high
// byte).
//
// Returns the instruction offset in the body.
func EmitWide(compiledFunction *CompiledFunction, op isa.Opcode, a uint8, wide uint16) int {
	lo, hi := isa.SplitWide(wide)
	return Emit(compiledFunction, op, a, lo, hi)
}

// EmitExtension emits an isa.OpExt instruction with a 16-bit payload in A (low byte) and
// B (high byte), plus an extra byte in C.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes wide (uint16) which is the 16-bit payload.
// Takes c (uint8) which is the extra operand in C.
//
// Returns the instruction offset in the body.
func EmitExtension(compiledFunction *CompiledFunction, wide uint16, c uint8) int {
	lo, hi := isa.SplitWide(wide)
	return Emit(compiledFunction, isa.OpExt, lo, hi, c)
}

// EmitJump emits a jump instruction whose offset is patched later by PatchJump.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes op (isa.Opcode) which is the jump opcode.
// Takes conditionRegister (uint8) which is the condition register index.
//
// Returns the instruction offset for later patching with PatchJump.
func EmitJump(compiledFunction *CompiledFunction, op isa.Opcode, conditionRegister uint8) int {
	return Emit(compiledFunction, op, conditionRegister, 0, 0)
}

// EmitTier1Jump emits an unconditional tier-1 jump whose offset is patched later by
// PatchJump.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
//
// Returns the instruction offset for later patching with PatchJump.
func EmitTier1Jump(compiledFunction *CompiledFunction) int {
	return EmitTier1(compiledFunction, isa.SubOpJump, 0, 0)
}

// EmitTier1 appends a tier-1 instruction to the function body and returns its offset.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes subOp (isa.SubOpcode) which selects the tier-1 operation.
// Takes b (uint8) which is the first operand.
// Takes c (uint8) which is the second operand.
//
// Returns the instruction offset in the body.
func EmitTier1(compiledFunction *CompiledFunction, subOp isa.SubOpcode, b, c uint8) int {
	return Emit(compiledFunction, isa.OpDrillTier1, uint8(subOp), b, c)
}

// EmitTier1Wide emits a tier-1 instruction carrying a 16-bit index split across B and C.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes subOp (isa.SubOpcode) which selects the tier-1 operation.
// Takes wide (uint16) which is the 16-bit index to encode.
//
// Returns the instruction offset in the body.
func EmitTier1Wide(compiledFunction *CompiledFunction, subOp isa.SubOpcode, wide uint16) int {
	lo, hi := isa.SplitWide(wide)
	return EmitTier1(compiledFunction, subOp, lo, hi)
}

// EmitTier2 appends a tier-2 instruction {isa.OpDrillTier1, isa.SubOpDrillTier2, subOp,
// c} to the function body and returns its offset. The single operand is carried in C.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes subOp (isa.SubOpcodeTier2) which selects the tier-2 operation.
// Takes c (uint8) which is the sole operand.
//
// Returns the instruction offset in the body.
func EmitTier2(compiledFunction *CompiledFunction, subOp isa.SubOpcodeTier2, c uint8) int {
	return Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(subOp), c)
}

// EmitTier3 appends a tier-3 instruction {isa.OpDrillTier1, isa.SubOpDrillTier2,
// isa.SubOpTier2DrillTier3, subOp} to the function body and returns its offset. All three
// operand bytes are drill discriminators.
//
// Takes compiledFunction (*CompiledFunction) which receives the instruction.
// Takes subOp (isa.SubOpcodeTier3) which selects the tier-3 operation.
//
// Returns the instruction offset in the body.
func EmitTier3(compiledFunction *CompiledFunction, subOp isa.SubOpcodeTier3) int {
	return Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(subOp))
}

// PatchJump patches an earlier emitted jump instruction at patchPC to jump to the current
// instruction offset.
//
// Takes compiledFunction (*CompiledFunction) whose body is patched.
// Takes patchPC (int) which is the instruction offset to patch.
func PatchJump(compiledFunction *CompiledFunction, patchPC int) {
	lo, hi := EncodeJumpOffset(compiledFunction, len(compiledFunction.Body)-patchPC-1)
	compiledFunction.Body[patchPC].B = lo
	compiledFunction.Body[patchPC].C = hi
}

// EncodeJumpOffset splits a PC-relative jump distance into operand bytes. Sets
// JumpRangeExceeded on overflow so Optimise rejects the function cleanly.
//
// Takes compiledFunction (*CompiledFunction) which is flagged on overflow.
// Takes delta (int) which is the PC-relative jump distance.
//
// Returns lowByte (uint8) which is the low operand byte.
// Returns highByte (uint8) which is the high operand byte.
func EncodeJumpOffset(compiledFunction *CompiledFunction, delta int) (lowByte, highByte uint8) {
	if !FitsJumpOffset(delta) {
		compiledFunction.JumpRangeExceeded = true
		return 0, 0
	}
	return isa.SplitOffset(safeconv.MustIntToInt16(delta))
}

// CurrentPC returns the offset for the next instruction to be emitted.
//
// Takes compiledFunction (*CompiledFunction) whose body length is returned.
//
// Returns the current program counter offset.
func CurrentPC(compiledFunction *CompiledFunction) int {
	return len(compiledFunction.Body)
}

// MayCreateSharedCells reports whether the body contains an isa.OpMakeClosure. When
// false, every SyncClosureUpvalues in the function is provably a no-op.
//
// Takes compiledFunction (*CompiledFunction) whose body is scanned.
//
// Returns true when an isa.OpMakeClosure exists in the body.
func MayCreateSharedCells(compiledFunction *CompiledFunction) bool {
	for i := range compiledFunction.Body {
		if compiledFunction.Body[i].Op == isa.OpMakeClosure {
			return true
		}
	}
	return false
}

// constantPoolCap returns the configured constant-pool ceiling, substituting the package
// default when none was set at compile time.
//
// Takes compiledFunction (*CompiledFunction) which carries the per-function cap.
//
// Returns int which is the maximum number of pool entries permitted.
func constantPoolCap(compiledFunction *CompiledFunction) int {
	if compiledFunction.maxConstantPoolSize > 0 {
		return compiledFunction.maxConstantPoolSize
	}
	return defaultMaxConstantPoolSize
}

// methodsCap returns the configured methodTable ceiling, substituting the package default
// when none was set.
//
// Takes compiledFunction (*CompiledFunction) which carries the per-function cap.
//
// Returns int which is the maximum number of method entries permitted.
func methodsCap(compiledFunction *CompiledFunction) int {
	if compiledFunction.maxMethods > 0 {
		return compiledFunction.maxMethods
	}
	return defaultMaxMethods
}

// appendTypeRef adds a fresh type-table entry for t with the given interface method
// requirements and returns its index. It never records the plain type index: an entry
// with requirements must not serve a method-free reference to the same reflect type, or
// `x.(any)` would check another interface's methods.
//
// Takes compiledFunction (*CompiledFunction) which owns the type table.
// Takes t (reflect.Type) which is the type to add.
// Takes methods ([]string) which are the encoded interface method requirements.
//
// Returns uint16 which is the new entry's index.
// Returns error when the type table is exhausted.
func appendTypeRef(compiledFunction *CompiledFunction, t reflect.Type, methods []string) (uint16, error) {
	if len(compiledFunction.TypeTable) >= constantPoolCap(compiledFunction) {
		return 0, fmt.Errorf("%w: typeTable", errConstantPoolExhausted)
	}
	index := safeconv.IntToUint16(len(compiledFunction.TypeTable))
	compiledFunction.TypeTable = append(compiledFunction.TypeTable, t)
	for len(compiledFunction.TypeTableInterfaceMethods) < len(compiledFunction.TypeTable)-1 {
		compiledFunction.TypeTableInterfaceMethods = append(compiledFunction.TypeTableInterfaceMethods, nil)
	}
	compiledFunction.TypeTableInterfaceMethods = append(compiledFunction.TypeTableInterfaceMethods, methods)
	return index, nil
}

// TypeTableDescriptorAt returns the serialisable descriptor of one TypeTable entry.
// Prefers a stored descriptor from a bytecode-loaded function, otherwise derives one from
// the reflect.Type at serialisation time.
//
// Takes compiledFunction (*CompiledFunction) which owns the type table.
// Takes index (int) which is a valid TypeTable index.
//
// Returns descriptor.TypeDescriptor which describes TypeTable[index].
func TypeTableDescriptorAt(compiledFunction *CompiledFunction, index int) descriptor.TypeDescriptor {
	if index < len(compiledFunction.TypeTableDescriptors) {
		return compiledFunction.TypeTableDescriptors[index]
	}
	return descriptor.ReflectTypeToDescriptor(compiledFunction.TypeTable[index])
}

// typeTableDescriptorsOf returns one descriptor per TypeTable entry, stored entries first
// and the rest derived, without caching the result on the function.
//
// Takes compiledFunction (*CompiledFunction) which owns the type table.
//
// Returns []descriptor.TypeDescriptor which is parallel to compiledFunction.TypeTable.
func typeTableDescriptorsOf(compiledFunction *CompiledFunction) []descriptor.TypeDescriptor {
	descriptors := make([]descriptor.TypeDescriptor, len(compiledFunction.TypeTable))
	for i := range compiledFunction.TypeTable {
		descriptors[i] = TypeTableDescriptorAt(compiledFunction, i)
	}
	return descriptors
}

// addDedupedConstant appends v to the pool when no equal entry is already present,
// returning the existing or freshly-added index.
//
// Lazily rebuilds the dedup map from the pool when the map is nil but the pool is
// non-empty. Optimise() nils the index to release memory once compile is done;
// post-Optimise adders (e.g. the bytecode inliner merging constant pools across
// functions) must reseed the dedup map from existing entries, otherwise additions would
// duplicate pre-existing constants.
//
// Takes pool (*[]K) which points to the parallel pool slice.
// Takes index (*map[K]uint16) which points to the dedup index map.
// Takes v (K) which is the value to add or look up.
// Takes maxPoolSize (int) which caps total entries; on overflow returns (0,
// errConstantPoolExhausted) rather than panicking.
//
// Returns the existing index when an equal entry is present, otherwise the index of the
// freshly-appended entry, or the sentinel error when the pool would exceed maxPoolSize.
func addDedupedConstant[K comparable](pool *[]K, index *map[K]uint16, v K, maxPoolSize int) (uint16, error) {
	if *index == nil && len(*pool) > 0 {
		seeded := make(map[K]uint16, len(*pool))
		for i, c := range *pool {
			if _, ok := seeded[c]; !ok {
				seeded[c] = uint16(i)
			}
		}
		*index = seeded
	}
	if existing, ok := (*index)[v]; ok {
		return existing, nil
	}
	if len(*pool) >= maxPoolSize {
		return 0, fmt.Errorf("%w: %T pool", errConstantPoolExhausted, v)
	}
	newIndex := safeconv.IntToUint16(len(*pool))
	*pool = append(*pool, v)
	if *index == nil {
		*index = make(map[K]uint16)
	}
	(*index)[v] = newIndex
	return newIndex, nil
}

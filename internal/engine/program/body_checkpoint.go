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
	"pipit.sh/pipit/internal/isa"
)

// BodyCheckpoint records mutable compilation state so a body can be retried. It stores
// lengths and values without copying the function's synchronisation fields.
type BodyCheckpoint struct {
	// debugSourceMap is the source-position map to restore.
	debugSourceMap *SourceMap

	// debugVarTable is the variable debug table to restore.
	debugVarTable *DebugVarTable

	// debugEmitHook is the instruction-emission hook to restore.
	debugEmitHook func(pc int)

	// aliasInfo is the pointer-alias analysis to restore.
	aliasInfo *PointerAliasInfo

	// lengths records the slice boundaries before body compilation.
	lengths bodyLengths

	// numRegisters records peak register usage per bank before body compilation.
	numRegisters [isa.NumRegisterKinds]uint32

	// emittedInlineBlocker is the previously recorded inline-blocking opcode verdict.
	emittedInlineBlocker InlineRefusal

	// cachedInlineRefusal is the cached inliner verdict to restore.
	cachedInlineRefusal InlineRefusal

	// hasRecover records whether the function was marked as calling recover.
	hasRecover bool

	// inspectsCallStack records whether the function was marked as reading its call stack.
	inspectsCallStack bool

	// jumpRangeExceeded records whether a jump had already exceeded its encoded range.
	jumpRangeExceeded bool
}

// bodyLengths records slice lengths for restoring a CompiledFunction after a discarded
// compilation attempt.
type bodyLengths struct {
	// body is the number of instructions to retain.
	body int

	// callSites is the number of call-site records to retain.
	callSites int

	// pools records the constant pools and type tables to retain.
	pools PoolMarks

	// structLiteralTable is the number of struct literal records to retain.
	structLiteralTable int

	// parameterRegisters is the number of parameter register assignments to retain.
	parameterRegisters int

	// parameterEscapes is the number of parameter escape flags to retain.
	parameterEscapes int

	// upvalueDescriptors is the number of captured-variable descriptions to retain.
	upvalueDescriptors int

	// namedResultLocations is the number of named result locations to retain.
	namedResultLocations int

	// namedResultNames is the number of named result identifiers to retain.
	namedResultNames int

	// parameterKinds is the number of parameter register kinds to retain.
	parameterKinds int

	// parameterTypeRefs is the number of parameter type references to retain.
	parameterTypeRefs int

	// parameterIsGeneric is the number of parameter type-parameter flags to retain.
	parameterIsGeneric int

	// parameterTypedSlicePromoted is the number of typed-slice parameter flags to retain.
	parameterTypedSlicePromoted int

	// resultKinds is the number of result register kinds to retain.
	resultKinds int

	// resultTypeRefs is the number of result type references to retain.
	resultTypeRefs int

	// resultReflectTypes is the number of exact reflected result types to retain.
	resultReflectTypes int
}

// CaptureBodyCheckpoint records the body-dependent state of compiledFunction.
//
// Takes compiledFunction (*CompiledFunction) which is about to have its body compiled.
//
// Returns BodyCheckpoint which RestoreBodyCheckpoint can return the function to.
func CaptureBodyCheckpoint(compiledFunction *CompiledFunction) BodyCheckpoint {
	return BodyCheckpoint{
		debugSourceMap: compiledFunction.DebugSourceMap,
		debugVarTable:  compiledFunction.DebugVarTable,
		debugEmitHook:  compiledFunction.DebugEmitHook,
		aliasInfo:      compiledFunction.AliasInfo,
		lengths: bodyLengths{
			body:                        len(compiledFunction.Body),
			callSites:                   len(compiledFunction.CallSites),
			pools:                       CapturePoolMarks(compiledFunction),
			structLiteralTable:          len(compiledFunction.StructLiteralTable),
			parameterRegisters:          len(compiledFunction.ParameterRegisters),
			parameterEscapes:            len(compiledFunction.ParameterEscapes),
			upvalueDescriptors:          len(compiledFunction.UpvalueDescriptors),
			namedResultLocations:        len(compiledFunction.NamedResultLocations),
			namedResultNames:            len(compiledFunction.NamedResultNames),
			parameterKinds:              len(compiledFunction.ParameterKinds),
			parameterTypeRefs:           len(compiledFunction.ParameterTypeRefs),
			parameterIsGeneric:          len(compiledFunction.ParameterIsGeneric),
			parameterTypedSlicePromoted: len(compiledFunction.ParameterTypedSlicePromoted),
			resultKinds:                 len(compiledFunction.ResultKinds),
			resultTypeRefs:              len(compiledFunction.ResultTypeRefs),
			resultReflectTypes:          len(compiledFunction.ResultReflectTypes),
		},
		numRegisters:         compiledFunction.NumRegisters,
		emittedInlineBlocker: compiledFunction.EmittedInlineBlocker,
		cachedInlineRefusal:  compiledFunction.CachedInlineRefusal,
		hasRecover:           compiledFunction.HasRecover,
		inspectsCallStack:    compiledFunction.InspectsCallStack,
		jumpRangeExceeded:    compiledFunction.JumpRangeExceeded,
	}
}

// RestoreBodyCheckpoint discards changes made to compiledFunction since the checkpoint.
//
// Takes compiledFunction (*CompiledFunction) which is the function to rewind.
// Takes checkpoint (*BodyCheckpoint) which CaptureBodyCheckpoint recorded for it.
func RestoreBodyCheckpoint(compiledFunction *CompiledFunction, checkpoint *BodyCheckpoint) {
	lengths := checkpoint.lengths
	compiledFunction.Body = compiledFunction.Body[:lengths.body]
	compiledFunction.CallSites = compiledFunction.CallSites[:lengths.callSites]
	lengths.pools.Restore(compiledFunction)
	compiledFunction.StructLiteralTable = compiledFunction.StructLiteralTable[:lengths.structLiteralTable]
	compiledFunction.ParameterRegisters = compiledFunction.ParameterRegisters[:lengths.parameterRegisters]
	compiledFunction.ParameterEscapes = compiledFunction.ParameterEscapes[:lengths.parameterEscapes]
	compiledFunction.UpvalueDescriptors = compiledFunction.UpvalueDescriptors[:lengths.upvalueDescriptors]
	compiledFunction.NamedResultLocations = compiledFunction.NamedResultLocations[:lengths.namedResultLocations]
	compiledFunction.NamedResultNames = compiledFunction.NamedResultNames[:lengths.namedResultNames]
	compiledFunction.ParameterKinds = compiledFunction.ParameterKinds[:lengths.parameterKinds]
	compiledFunction.ParameterTypeRefs = compiledFunction.ParameterTypeRefs[:lengths.parameterTypeRefs]
	compiledFunction.ParameterIsGeneric = compiledFunction.ParameterIsGeneric[:lengths.parameterIsGeneric]
	compiledFunction.ParameterTypedSlicePromoted = compiledFunction.ParameterTypedSlicePromoted[:lengths.parameterTypedSlicePromoted]
	compiledFunction.ResultKinds = compiledFunction.ResultKinds[:lengths.resultKinds]
	compiledFunction.ResultTypeRefs = compiledFunction.ResultTypeRefs[:lengths.resultTypeRefs]
	compiledFunction.ResultReflectTypes = compiledFunction.ResultReflectTypes[:lengths.resultReflectTypes]

	prunePCs(compiledFunction.FieldStoreArenaSafePCs, lengths.body)
	prunePCs(compiledFunction.ArenaSafeAllocPCs, lengths.body)
	prunePCs(compiledFunction.InPlaceHeaderReusePCs, lengths.body)
	prunePCs(compiledFunction.PeepholeProvenance, lengths.body)
	for pc := range compiledFunction.GetMethodReceiverTypeNames {
		if int(pc) >= lengths.body {
			delete(compiledFunction.GetMethodReceiverTypeNames, pc)
		}
	}

	compiledFunction.DebugSourceMap = checkpoint.debugSourceMap
	compiledFunction.DebugVarTable = checkpoint.debugVarTable
	compiledFunction.DebugEmitHook = checkpoint.debugEmitHook
	compiledFunction.AliasInfo = checkpoint.aliasInfo
	compiledFunction.NumRegisters = checkpoint.numRegisters
	compiledFunction.EmittedInlineBlocker = checkpoint.emittedInlineBlocker
	compiledFunction.CachedInlineRefusal = checkpoint.cachedInlineRefusal
	compiledFunction.HasRecover = checkpoint.hasRecover
	compiledFunction.InspectsCallStack = checkpoint.inspectsCallStack
	compiledFunction.JumpRangeExceeded = checkpoint.jumpRangeExceeded
}

// pruneIndex deletes the dedup entries that point at or past length.
//
// Takes index (map[K]uint16) which maps values to pool positions; nil is allowed.
// Takes length (int) which is the pool's restored length.
func pruneIndex[K comparable](index map[K]uint16, length int) {
	for key, position := range index {
		if int(position) >= length {
			delete(index, key)
		}
	}
}

// prunePCs deletes the PC-keyed entries at or past length.
//
// Takes annotations (map[int]V) which is keyed by instruction index; nil is allowed.
// Takes length (int) which is the body's restored length.
func prunePCs[V any](annotations map[int]V, length int) {
	for pc := range annotations {
		if pc >= length {
			delete(annotations, pc)
		}
	}
}

// NewDiscardedFunction returns an unused stand-in that preserves a discarded function's
// table index.
//
// Returns *CompiledFunction which is the stand-in.
func NewDiscardedFunction() *CompiledFunction {
	function := NewNamedFunction("<discarded>")
	Emit(function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	return function
}

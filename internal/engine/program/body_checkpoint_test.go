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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

var checkpointRewoundFields = []string{
	"Body", "CallSites", "IntConstants", "FloatConstants", "StringConstants", "BoolConstants",
	"UintConstants", "ComplexConstants", "GeneralConstants", "GeneralConstantDescriptors",
	"TypeTable", "TypeTableDescriptors", "TypeTableInterfaceMethods", "StructLayoutTable",
	"StructLiteralTable", "ParameterRegisters", "ParameterEscapes", "UpvalueDescriptors",
	"NamedResultLocations", "NamedResultNames", "ParameterKinds", "ParameterTypeRefs",
	"ParameterIsGeneric", "ParameterTypedSlicePromoted", "ResultKinds", "ResultTypeRefs",
	"ResultReflectTypes", "IntConstIndex", "FloatConstIndex", "StringConstIndex",
	"UintConstIndex", "ComplexConstIndex", "TypeRefIndex", "typeRefMethodsIndex",
	"FieldStoreArenaSafePCs", "ArenaSafeAllocPCs", "InPlaceHeaderReusePCs", "PeepholeProvenance",
	"LocalContainerAllocPCs", "LocalContainerStorePCs",
	"BindMethodByNameReceiverTypeNames", "DebugSourceMap", "DebugVarTable", "DebugEmitHook", "AliasInfo",
	"NumRegisters", "EmittedInlineBlocker", "CachedInlineRefusal", "HasRecover",
	"InspectsCallStack", "JumpRangeExceeded",
}

var checkpointUntouchedFields = []string{
	"VariableInitFunction", "GlobalBases", "GenericTypeParams", "GenericRecvTypeParams",
	"ASMCallInfoTables", "TypeNames", "GenericDeclaration", "methodTable", "receiverMethods",
	"receiverBodies", "receiverMethodOwners", "SpecialisationOrigin",
	"Specialisations", "LayoutTypeWords", "MarshalShadowMemo", "marshalShadowCount", "SourceFile", "Name", "Functions",
	"PrecomputedAllocCounts", "maxConstantPoolSize", "maxSpecialisations", "ASMCallInfoBase",
	"StructLiteralTableBase", "StructLiteralTableLength", "maxMethods", "EvalChildALayout",
	"TinyLeafLayout", "EvalChildBLayout", "EvalChildCLayout", "EvalShapeOnce",
	"ASMCallInfoTablesOnce", "ASMCallInfoBases", "LayoutTypeWordsOnce", "StructLiteralTableOnce",
	"cachedMaxCallDepth", "observedCallDepth", "optimisticMakePlaced", "optimisticMakeVetoed", "hostTopLevelCalls", "licenceRejected", "finaliserUse", "conversionArenaVetoes", "NonZeroBankMask", "NonEmptyConstantMask",
	"EvalSiteC", "EvalSiteB", "EvalSiteA", "RequiresSpecialisation", "HasReceiver", "TinyLeafShape",
	"HeapMutationClass", "EvalShape", "IsVariadic", "IsPointerReceiver", "IsGenericFunction",
	"InRecursionCycle", "CannotObserveCallers", "PrecomputedAllocCountsValid", "MethodSignatures", "SignatureReflectType",
	"VariadicSliceType", "MethodReceiverReflectType", "RuntimeName",
}

func TestBodyCheckpointClassifiesEveryField(t *testing.T) {
	t.Parallel()

	known := make(map[string]bool)
	for _, name := range checkpointRewoundFields {
		known[name] = true
	}
	for _, name := range checkpointUntouchedFields {
		require.False(t, known[name], "%s is listed twice", name)
		known[name] = true
	}
	fields := reflect.TypeFor[CompiledFunction]()
	for field := range fields.Fields() {
		require.True(t, known[field.Name],
			"CompiledFunction.%s is new: decide whether a body compile changes it, and rewind it in RestoreBodyCheckpoint if so", field.Name)
	}
}

func TestBodyCheckpointRestoresTheFunction(t *testing.T) {
	t.Parallel()

	function := NewNamedFunction("f")
	Emit(function, isa.OpNoOperation, 0, 0, 0)
	first, err := AddIntConstant(function, 7)
	require.NoError(t, err)
	stringType := reflect.TypeFor[string]()
	_, err = AddTypeRefWithMethods(function, stringType, []string{"Len"})
	require.NoError(t, err)
	function.ParameterRegisters = append(function.ParameterRegisters, 0)
	checkpoint := CaptureBodyCheckpoint(function)

	Emit(function, isa.OpNoOperation, 1, 2, 3)
	_, err = AddIntConstant(function, 9)
	require.NoError(t, err)
	_, err = AddTypeRefWithMethods(function, reflect.TypeFor[int](), []string{"Len"})
	require.NoError(t, err)
	_, err = AddCallSite(function, &CallSite{})
	require.NoError(t, err)
	function.ParameterRegisters = append(function.ParameterRegisters, 4)
	function.NamedResultNames = append(function.NamedResultNames, "r")
	function.BindMethodByNameReceiverTypeNames = map[uint32]string{1: "T"}
	function.EmittedInlineBlocker = InlineRefusal(1)

	RestoreBodyCheckpoint(function, &checkpoint)

	require.Len(t, function.Body, 1)
	require.Equal(t, []int64{7}, function.IntConstants)
	require.Empty(t, function.CallSites)
	require.Equal(t, []uint8{0}, function.ParameterRegisters)
	require.Empty(t, function.NamedResultNames)
	require.Empty(t, function.BindMethodByNameReceiverTypeNames)
	require.Zero(t, function.EmittedInlineBlocker)

	again, err := AddIntConstant(function, 7)
	require.NoError(t, err)
	require.Equal(t, first, again, "a kept constant keeps its index")
	nine, err := AddIntConstant(function, 9)
	require.NoError(t, err)
	require.Equal(t, uint16(1), nine, "a discarded constant is added again at the next index")
	intIndex, err := AddTypeRefWithMethods(function, reflect.TypeFor[int](), []string{"Len"})
	require.NoError(t, err)
	require.Equal(t, uint16(len(function.TypeTable)-1), intIndex, "a discarded type entry is added again")
}

func TestNewDiscardedFunctionReturnsImmediately(t *testing.T) {
	t.Parallel()

	function := NewDiscardedFunction()
	require.Equal(t, "<discarded>", function.Name)
	require.Len(t, function.Body, 1)
}

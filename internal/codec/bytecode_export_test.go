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

package codec

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/frontend"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func TestALoadedFunctionsResultTypesComeFromItsSignature(t *testing.T) {
	t.Parallel()

	signature := reflect.TypeFor[func(int) ([]int, error)]()
	require.Equal(t, []reflect.Type{reflect.TypeFor[[]int](), reflect.TypeFor[error]()}, resultTypesOfSignature(signature, 2),
		"a []int result held as []int64 in its bank leaves the function as []int")
	require.Nil(t, resultTypesOfSignature(signature, 1), "a signature that disagrees with the result count is not trusted")
	require.Nil(t, resultTypesOfSignature(nil, 2))
	require.Nil(t, resultTypesOfSignature(reflect.TypeFor[int](), 0))
}

const roundTripProgram = `package main

type Celsius []float64

type List []List

type Shape interface{ Area() float64 }

type Square struct {
	Side  float64
	Label string
}

func (s Square) Area() float64 { return s.Side * s.Side }

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(value T) { s.items = append(s.items, value) }

func (s *Stack[T]) Len() int { return len(s.items) }

var counter int

var label = "start"

func sum(values ...int) (total int) {
	defer func() {
		if recover() != nil {
			total = -1
		}
	}()
	for _, value := range values {
		total += value
	}
	return
}

func main() {
	var shape Shape = Square{Side: 2, Label: "sq"}
	stack := &Stack[int]{}
	stack.Push(1)
	adder := func(x uint) uint { return x + uint(counter) }
	celsius := Celsius{1.5}
	var list List
	list = append(list, nil)
	number := complex(1, 2)
	flag := shape.Area() > 1
	square := shape.(Square)
	square.Side = 3.25
	label = label + "!"
	counter = sum(1, 2, 3) + int(adder(3)) + stack.Len() + len(celsius) + len(list)
	_, _, _ = number, flag, square
}
`

func compileRoundTripProgram(t *testing.T) *program.CompiledFunction {
	t.Helper()
	symbols := symtab.NewSymbolRegistry(nil)
	result, err := frontend.Typecheck("main", map[string]string{"main.go": roundTripProgram}, symbols.Importer(t.Context()))
	require.NoError(t, err)
	root := program.NewNamedFunction("<root>")
	config := compile.CompilerConfig{
		FileSet:            result.FileSet,
		Info:               result.Info,
		Function:           root,
		RootFunction:       root,
		FunctionTable:      nil,
		GlobalVariables:    nil,
		Symbols:            symbols,
		Globals:            engine.NewGlobalStore(),
		Patterns:           nil,
		ScopeName:          "<root>",
		Features:           policy.InterpFeaturesAll,
		Passes:             passes.DefaultOptions(),
		DebugEnabled:       false,
		MaxLiteralElements: 0,
		MaxExpressionDepth: 0,
	}
	_, err = compile.CompileParsedPackage(t.Context(), config, result.Files, "main", nil)
	require.NoError(t, err)
	return root
}

func roundTrip(t *testing.T, compiledFunction *program.CompiledFunction) *program.CompiledFunction {
	t.Helper()
	body := Body(compiledFunction)
	instructions := make([]isa.Instruction, len(body))
	for index, word := range body {
		instructions[index] = isa.Instruction{Op: isa.Opcode(word.Operation), A: word.A, B: word.B, C: word.C}
	}
	var registers [isa.NumRegisterKinds]uint32
	copy(registers[:], NumRegistersSlice(compiledFunction))
	parameterKinds := make([]isa.RegisterKind, 0, len(ParamKinds(compiledFunction)))
	for _, kind := range ParamKinds(compiledFunction) {
		parameterKinds = append(parameterKinds, MakeRegisterKind(kind))
	}
	resultKinds := make([]isa.RegisterKind, 0, len(ResultKinds(compiledFunction)))
	for _, kind := range ResultKinds(compiledFunction) {
		resultKinds = append(resultKinds, MakeRegisterKind(kind))
	}
	constantData, err := GeneralConstantDescriptors(compiledFunction)
	require.NoError(t, err)
	constants := make([]descriptor.GeneralConstantDescriptor, len(constantData))
	for index, data := range constantData {
		constants[index] = ImportGeneralConstantDescriptor(data)
	}
	typeTable := make([]reflect.Type, 0, len(compiledFunction.TypeTable))
	typeDescriptors := make([]descriptor.TypeDescriptor, 0, len(compiledFunction.TypeTable))
	for _, data := range TypeTableDescriptors(compiledFunction) {
		resolved, resolveErr := DescriptorToReflectType(t.Context(), data, symtab.NewSymbolRegistry(nil))
		require.NoError(t, resolveErr)
		typeTable = append(typeTable, resolved)
		typeDescriptors = append(typeDescriptors, ImportTypeDescriptor(data))
	}
	typeNames := make(map[reflect.Type]string)
	for reflectType, data := range TypeNames(compiledFunction) {
		resolved, resolveErr := DescriptorToReflectType(t.Context(), data.TypeDescriptor, symtab.NewSymbolRegistry(nil))
		require.NoError(t, resolveErr)
		require.Equal(t, reflectType, resolved, "%s keeps its runtime type", data.Name)
		typeNames[resolved] = data.Name
	}
	callSites := make([]program.CallSite, 0, len(compiledFunction.CallSites))
	for _, data := range CallSites(compiledFunction) {
		callSites = append(callSites, MakeCallSite(data))
	}
	upvalueData := UpvalueDescriptors(compiledFunction)
	upvalues := make([]program.UpvalueDescriptor, 0, len(upvalueData))
	for _, data := range upvalueData {
		upvalues = append(upvalues, MakeUpvalueDescriptor(data))
	}
	namedResultData := NamedResultLocations(compiledFunction)
	namedResults := make([]program.VarLocation, 0, len(namedResultData))
	for _, data := range namedResultData {
		namedResults = append(namedResults, MakeVarLocation(data))
	}
	receiverMethods := make(map[program.ReceiverMethodKey]uint16)
	for _, data := range ReceiverMethods(compiledFunction) {
		receiver, resolveErr := DescriptorToReflectType(t.Context(), data.ReceiverTypeDescriptor, symtab.NewSymbolRegistry(nil))
		require.NoError(t, resolveErr)
		receiverMethods[program.ReceiverMethodKey{Receiver: receiver, Method: data.Method}] = data.Function
	}
	functions := make([]*program.CompiledFunction, 0, len(compiledFunction.Functions))
	for _, child := range compiledFunction.Functions {
		functions = append(functions, roundTrip(t, child))
	}
	return NewCompiledFunctionFromData(&CompiledFunctionData{
		VariableInitFunction:       nil,
		SignatureReflectType:       ExportSignatureReflectType(compiledFunction),
		MethodTable:                MethodTable(compiledFunction),
		TypeNames:                  typeNames,
		ReceiverMethods:            receiverMethods,
		Name:                       ExportName(compiledFunction),
		SourceFile:                 ExportSourceFile(compiledFunction),
		ResultKinds:                resultKinds,
		Body:                       instructions,
		BoolConstants:              BoolConstants(compiledFunction),
		IntConstants:               IntConstants(compiledFunction),
		FloatConstants:             FloatConstants(compiledFunction),
		UintConstants:              UintConstants(compiledFunction),
		ComplexConstants:           ComplexConstants(compiledFunction),
		StringConstants:            StringConstants(compiledFunction),
		GeneralConstants:           compiledFunction.GeneralConstants,
		GeneralConstantDescriptors: constants,
		TypeTable:                  typeTable,
		TypeTableDescriptors:       typeDescriptors,
		TypeTableInterfaceMethods:  TypeTableInterfaceMethods(compiledFunction),
		ParamKinds:                 parameterKinds,
		ParamRegisters:             ParamRegisters(compiledFunction),
		CallSites:                  callSites,
		UpvalueDescriptors:         upvalues,
		Functions:                  functions,
		NamedResultLocations:       namedResults,
		StructLayoutTable:          StructLayoutTable(compiledFunction),
		NumRegisters:               registers,
		IsVariadic:                 ExportIsVariadic(compiledFunction),
		HasRecover:                 ExportHasRecover(compiledFunction),
		IsPointerReceiver:          ExportIsPointerReceiver(compiledFunction),
		HasReceiver:                ExportHasReceiver(compiledFunction),
	})
}

func requireSameFunction(t *testing.T, want, got *program.CompiledFunction) {
	t.Helper()
	require.Equal(t, want.Name, got.Name)
	requireSameElements(t, want.Body, got.Body, want.Name)
	require.Equal(t, want.NumRegisters, got.NumRegisters, want.Name)
	require.Equal(t, want.IsVariadic, got.IsVariadic, want.Name)
	require.Equal(t, want.HasRecover, got.HasRecover, want.Name)
	require.Equal(t, want.HasReceiver, got.HasReceiver, want.Name)
	require.Equal(t, want.IsPointerReceiver, got.IsPointerReceiver, want.Name)
	require.Equal(t, want.SignatureReflectType, got.SignatureReflectType, want.Name)
	requireSameElements(t, want.ResultKinds, got.ResultKinds, want.Name)
	requireSameElements(t, want.ParameterRegisters, got.ParameterRegisters, want.Name)
	requireSameElements(t, want.IntConstants, got.IntConstants, want.Name)
	requireSameElements(t, want.FloatConstants, got.FloatConstants, want.Name)
	requireSameElements(t, want.StringConstants, got.StringConstants, want.Name)
	requireSameElements(t, want.ComplexConstants, got.ComplexConstants, want.Name)
	requireSameElements(t, want.TypeTable, got.TypeTable, want.Name)
	requireSameElements(t, want.UpvalueDescriptors, got.UpvalueDescriptors, want.Name)
	requireSameElements(t, exportVarLocations(want.NamedResultLocations), exportVarLocations(got.NamedResultLocations), want.Name)
	require.Equal(t, want.MethodTable(), got.MethodTable(), want.Name)
	requireSameElements(t, want.StructLayoutTable, got.StructLayoutTable, want.Name)
	require.Len(t, got.CallSites, len(want.CallSites), want.Name)
	for index := range want.CallSites {
		require.Equal(t, exportVarLocations(want.CallSites[index].Arguments), exportVarLocations(got.CallSites[index].Arguments), want.Name)
		require.Equal(t, want.CallSites[index].FunctionIndex, got.CallSites[index].FunctionIndex, want.Name)
	}
	require.Equal(t, len(want.ReceiverMethods()), len(got.ReceiverMethods()), want.Name)
	require.Len(t, got.Functions, len(want.Functions), want.Name)
	for index := range want.Functions {
		requireSameFunction(t, want.Functions[index], got.Functions[index])
	}
}

func requireSameElements[T any](t *testing.T, want, got []T, name string) {
	t.Helper()
	require.Len(t, got, len(want), name)
	for index := range want {
		require.Equal(t, want[index], got[index], "%s element %d", name, index)
	}
}

func TestACompiledProgramSurvivesTheCodecsRoundTrip(t *testing.T) {
	t.Parallel()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("the round trip compares named composite types, which this build gives no runtime types of their own")
	}

	root := compileRoundTripProgram(t)
	rebuilt := roundTrip(t, root)
	requireSameFunction(t, root, rebuilt)

	fileSet := NewCompiledFileSetFromData(rebuilt, nil, map[string]uint16{"main": 0}, []uint16{1})
	require.NotNil(t, fileSet)
	withVariables := NewCompiledFileSetFromDataWithVars(rebuilt, nil, nil, nil, program.SlotAllocation{}, nil)
	require.NotNil(t, withVariables)
}

func TestReceiverMethodsAreOrderedWhateverTheMapOrder(t *testing.T) {
	t.Parallel()

	first := reflect.TypeFor[struct{ A int }]()
	second := reflect.TypeFor[struct{ B int }]()
	function := program.NewNamedFunction("<root>")
	for index := range 5 {
		function.Functions = append(function.Functions, program.NewNamedFunction("body"+strconv.Itoa(index)))
	}
	function.SetReceiverMethods(map[program.ReceiverMethodKey]uint16{
		{Receiver: second, Method: 1}: 4,
		{Receiver: first, Method: 1}:  4,
		{Receiver: first, Method: 0}:  2,
	})
	entries := ReceiverMethods(function)
	require.Len(t, entries, 3)
	require.Equal(t, uint16(0), entries[0].Method)
	receiver, err := DescriptorToReflectType(t.Context(), entries[1].ReceiverTypeDescriptor, nil)
	require.NoError(t, err)
	require.Equal(t, first, receiver, "receivers that tie on method and body sort by their printed form")
	for range 20 {
		require.Equal(t, entries, ReceiverMethods(function), "the encoding is the same every time")
	}
	require.Nil(t, ReceiverMethods(program.NewNamedFunction("empty")))
}

func TestLinkedTypeArgumentsRoundTrip(t *testing.T) {
	t.Parallel()

	typeArgs := []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[map[string][]bool]()}
	data := exportLinkedTypeArgs(typeArgs)
	resolved, err := ResolveLinkedTypeArgs(t.Context(), data, symtab.NewSymbolRegistry(nil))
	require.NoError(t, err)
	require.Equal(t, typeArgs, resolved)
	require.Nil(t, exportLinkedTypeArgs(nil))
	none, err := ResolveLinkedTypeArgs(t.Context(), nil, nil)
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = ResolveLinkedTypeArgs(t.Context(), []descriptor.TypeDescriptorData{{Kind: uint8(descriptor.KindNamed), PackagePath: "example.com/missing", Name: "Gone"}}, symtab.NewSymbolRegistry(nil))
	require.ErrorContains(t, err, "linked type argument 0")
}

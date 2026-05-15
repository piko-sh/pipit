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

package schema

import (
	"errors"
	"fmt"

	"pipit.sh/pipit/internal/schema/schemagen"
)

const (
	// maxInspectedFunctionNestingDepth caps the recursion depth when walking nested compiled
	// functions (closures within closures) in an untrusted payload. It mirrors the unpack
	// path's maxBytecodeFunctionNestingDepth so a tampered payload cannot exhaust the stack.
	maxInspectedFunctionNestingDepth = 256
)

var (
	// registerBankNames maps register bank indices to their display names.
	registerBankNames = [...]string{
		"int", "float", "string", "general", "bool", "uint", "complex",
	}

	// errFunctionNestingTooDeep signals a payload whose closure nesting exceeds
	// maxInspectedFunctionNestingDepth.
	errFunctionNestingTooDeep = errors.New("bytecode function nesting depth exceeded")
)

// BytecodeInspection is a JSON-serialisable summary of a compiled bytecode file set.
type BytecodeInspection struct {
	// Root is the top-level compiled function.
	Root *FunctionInspection `json:"root"`

	// VarInit is the package-level variable initialiser, or nil when none exists.
	VarInit *FunctionInspection `json:"var_init,omitempty"`

	// Entrypoints maps function names to their indices.
	Entrypoints map[string]uint16 `json:"entrypoints"`

	// InitFunctions holds the init function indices in declaration order.
	InitFunctions []uint16 `json:"init_functions,omitempty"`
}

// FunctionInspection is a JSON-serialisable summary of a single compiled function.
type FunctionInspection struct {
	// NumRegisters maps register bank names to their peak usage counts.
	NumRegisters map[string]uint32 `json:"num_registers"`

	// Constants maps constant-pool bank names to their entry counts.
	Constants map[string]int `json:"constants"`

	// Name is the qualified name of the function.
	Name string `json:"name"`

	// SourceFile is the path where the function was defined.
	SourceFile string `json:"source_file,omitempty"`

	// Functions holds summaries of nested child functions.
	Functions []*FunctionInspection `json:"functions,omitempty"`

	// Instructions is the number of bytecode instructions.
	Instructions int `json:"instructions"`

	// CallSites is the number of call site entries.
	CallSites int `json:"call_sites"`

	// Upvalues is the number of captured upvalue descriptors.
	Upvalues int `json:"upvalues"`

	// IsVariadic is true when variadic arguments are accepted.
	IsVariadic bool `json:"is_variadic,omitempty"`
}

// ConvertBytecode reads a raw FlatBuffer payload and returns a JSON-serialisable
// inspection summary.
//
// Takes payload ([]byte) which is the raw FlatBuffer bytes after Unpack strips the
// version header.
//
// Returns *BytecodeInspection which holds the structural metadata.
// Returns error when the payload is empty or malformed.
func ConvertBytecode(payload []byte) (inspection *BytecodeInspection, err error) {
	if len(payload) > MaximumBytecodePayloadBytes {
		return nil, ErrBytecodeTooLarge
	}
	if len(payload) == 0 {
		return nil, errors.New("empty bytecode payload")
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			inspection = nil
			err = fmt.Errorf("malformed bytecode payload: %v", recovered)
		}
	}()

	fileSet := schemagen.GetRootAsCompiledFileSet(payload, 0)
	if err := validateInspectionExpansion(fileSet, len(payload)); err != nil {
		return nil, err
	}

	inspection = &BytecodeInspection{Entrypoints: make(map[string]uint16), Root: nil, VarInit: nil, InitFunctions: nil}

	var rootFunction schemagen.CompiledFunction
	if fileSet.Root(&rootFunction) != nil {
		if inspection.Root, err = convertFunction(&rootFunction, 0); err != nil {
			return nil, err
		}
	}

	var varInitFunction schemagen.CompiledFunction
	if fileSet.VariableInitFunction(&varInitFunction) != nil {
		if inspection.VarInit, err = convertFunction(&varInitFunction, 0); err != nil {
			return nil, err
		}
	}

	var entrypoint schemagen.EntrypointEntry
	for i := range fileSet.EntrypointsLength() {
		if fileSet.Entrypoints(&entrypoint, i) {
			inspection.Entrypoints[string(entrypoint.Name())] = entrypoint.FunctionIndex()
		}
	}

	for i := range fileSet.InitialisationFunctionsLength() {
		inspection.InitFunctions = append(inspection.InitFunctions, fileSet.InitialisationFunctions(i))
	}

	return inspection, nil
}

// convertFunction summarises one compiled function and its nested closures.
//
// Takes function (*schemagen.CompiledFunction) which is the serialised function.
// Takes depth (int) which is the current closure nesting depth.
//
// Returns *FunctionInspection which holds the summary.
// Returns error when the nesting depth exceeds maxInspectedFunctionNestingDepth.
func convertFunction(function *schemagen.CompiledFunction, depth int) (*FunctionInspection, error) {
	if depth > maxInspectedFunctionNestingDepth {
		return nil, fmt.Errorf("%w: depth %d exceeds %d", errFunctionNestingTooDeep, depth, maxInspectedFunctionNestingDepth)
	}
	inspection := &FunctionInspection{Name: string(function.Name()),
		SourceFile:   string(function.SourceFile()),
		NumRegisters: make(map[string]uint32),
		Instructions: function.BodyLength(),
		Constants:    make(map[string]int),
		CallSites:    function.CallSitesLength(),
		Upvalues:     function.UpvalueDescriptorsLength(),
		IsVariadic:   function.IsVariadic(), Functions: nil}

	for i := range function.RegisterCountsLength() {
		count := function.RegisterCounts(i)
		if count > 0 && i < len(registerBankNames) {
			inspection.NumRegisters[registerBankNames[i]] = count
		}
	}

	addConstantCount(inspection.Constants, "int", function.IntConstantsLength())
	addConstantCount(inspection.Constants, "float", function.FloatConstantsLength())
	addConstantCount(inspection.Constants, "string", function.StringConstantsLength())
	addConstantCount(inspection.Constants, "bool", function.BoolConstantsLength())
	addConstantCount(inspection.Constants, "uint", function.UintConstantsLength())
	addConstantCount(inspection.Constants, "complex", function.ComplexConstantsLength())
	addConstantCount(inspection.Constants, "general", function.GeneralConstantDescriptorsLength())

	var childFunction schemagen.CompiledFunction
	for i := range function.FunctionsLength() {
		if function.Functions(&childFunction, i) {
			child, err := convertFunction(&childFunction, depth+1)
			if err != nil {
				return nil, err
			}
			inspection.Functions = append(inspection.Functions, child)
		}
	}

	return inspection, nil
}

// addConstantCount records a non-zero constant pool entry count.
//
// Takes constants (map[string]int) which accumulates pool counts.
// Takes name (string) which is the pool bank name.
// Takes count (int) which is the number of entries.
func addConstantCount(constants map[string]int, name string, count int) {
	if count > 0 {
		constants[name] = count
	}
}

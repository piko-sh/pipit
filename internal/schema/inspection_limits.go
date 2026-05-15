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
	maximumInspectionFunctions = 1 << 16

	maximumInspectionElements = 1 << 20

	maximumInspectionStringBytes = 8 << 20
)

var errInspectionLimit = errors.New("bytecode inspection expansion limit exceeded")

type inspectionBudget struct {
	functions int

	elements int

	strings int

	payloadLen int
}

// function charges the budget for a compiled function and its nested closures.
//
// Takes function (*schemagen.CompiledFunction) which is the compiled function to account
// for.
// Takes depth (int) which is the current closure nesting depth.
//
// Returns error without creating maps, strings, or summary nodes.
func (budget *inspectionBudget) function(function *schemagen.CompiledFunction, depth int) error {
	if function == nil {
		return nil
	}
	if depth > maxInspectedFunctionNestingDepth {
		return errFunctionNestingTooDeep
	}
	if budget.functions == 0 {
		return fmt.Errorf("%w: functions", errInspectionLimit)
	}
	budget.functions--
	if err := budget.chargeStrings(function.Name(), function.SourceFile()); err != nil {
		return err
	}
	if err := budget.chargeVectors(function.RegisterCountsLength(), function.FunctionsLength(),
		function.BodyLength(), function.CallSitesLength(), function.UpvalueDescriptorsLength(),
		function.IntConstantsLength(), function.FloatConstantsLength(), function.StringConstantsLength(),
		function.BoolConstantsLength(), function.UintConstantsLength(), function.ComplexConstantsLength(),
		function.GeneralConstantDescriptorsLength()); err != nil {
		return err
	}
	var child schemagen.CompiledFunction
	for index := range function.FunctionsLength() {
		if function.Functions(&child, index) {
			if err := budget.function(&child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// chargeVectors rejects impossible lengths and bounds total inspected entries.
//
// Takes counts (...int) which are the declared vector lengths from generated accessors.
//
// Returns error before unsigned lengths can drive excessive iteration.
func (budget *inspectionBudget) chargeVectors(counts ...int) error {
	for _, count := range counts {
		if count < 0 || count > budget.payloadLen || count > budget.elements {
			return fmt.Errorf("%w: vector elements", errInspectionLimit)
		}
		budget.elements -= count
	}
	return nil
}

// chargeStrings counts copied bytes per reference, including aliases.
//
// Takes values (...[]byte) which are the unconverted byte slices from the original
// payload.
//
// Returns error before cumulative string copying exceeds the summary allowance.
func (budget *inspectionBudget) chargeStrings(values ...[]byte) error {
	for _, value := range values {
		if len(value) > budget.strings {
			return fmt.Errorf("%w: string bytes", errInspectionLimit)
		}
		budget.strings -= len(value)
	}
	return nil
}

// validateInspectionExpansion bounds the metadata tree before materialising it.
// Inspection is not verification of executable bytecode or unused descriptor fields.
//
// Takes fileSet (*schemagen.CompiledFileSet) which is the deserialised top-level
// metadata.
// Takes payloadLen (int) which is the byte length of the backing FlatBuffer payload.
//
// Returns error on excessive tree expansion, declared lengths, or string copying.
func validateInspectionExpansion(fileSet *schemagen.CompiledFileSet, payloadLen int) error {
	budget := inspectionBudget{
		functions: maximumInspectionFunctions, elements: maximumInspectionElements,
		strings: maximumInspectionStringBytes, payloadLen: payloadLen,
	}
	if err := budget.chargeVectors(fileSet.EntrypointsLength(), fileSet.InitialisationFunctionsLength()); err != nil {
		return err
	}
	var entry schemagen.EntrypointEntry
	for index := range fileSet.EntrypointsLength() {
		if fileSet.Entrypoints(&entry, index) {
			if err := budget.chargeStrings(entry.Name()); err != nil {
				return err
			}
		}
	}
	for _, root := range []*schemagen.CompiledFunction{fileSet.Root(nil), fileSet.VariableInitFunction(nil)} {
		if err := budget.function(root, 0); err != nil {
			return err
		}
	}
	return nil
}

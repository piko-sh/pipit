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

package adapters

import (
	"context"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"pipit.sh/pipit/internal/schema/schemagen"
)

// maximumDecodedFunctionOccurrences is the upper bound on function entries before
// expansion is refused.
const maximumDecodedFunctionOccurrences = 1 << 16

// bytecodeExpansionBudget tracks remaining allocation allowances during function
// reconstruction.
type bytecodeExpansionBudget struct {
	// visitedTypes records the descriptor tables already charged, by payload offset. The
	// budget prices each table once.
	visitedTypes map[flatbuffers.UOffsetT]struct{}

	// typeAllowance is the type-unit budget this payload started with, for messages.
	typeAllowance int

	// functions is the remaining function occurrence allowance.
	functions int

	// types is the remaining type-unit allowance.
	types int

	// vectors is the remaining vector element allowance.
	vectors int

	// strings is the remaining string byte allowance.
	strings int
}

// alreadyVisitedType reports whether a descriptor table has been charged, recording it
// when it has not.
//
// Takes offset (flatbuffers.UOffsetT) which is the table's position in the payload.
//
// Returns bool which is true when the table was charged earlier in this validation.
func (budget *bytecodeExpansionBudget) alreadyVisitedType(offset flatbuffers.UOffsetT) bool {
	if budget.visitedTypes == nil {
		budget.visitedTypes = make(map[flatbuffers.UOffsetT]struct{})
	}
	if _, seen := budget.visitedTypes[offset]; seen {
		return true
	}
	budget.visitedTypes[offset] = struct{}{}
	return false
}

// validateFunctionExpansion bounds reconstruction before allocating compiled functions.
//
// Takes fileSet (*schemagen.CompiledFileSet) which is the untrusted file set.
// Takes payloadLen (int) which is the total payload size.
//
// Returns error on excessive expansion or cancellation.
func validateFunctionExpansion(ctx context.Context, fileSet *schemagen.CompiledFileSet, payloadLen int) error {
	typeAllowance := typeUnitAllowance(payloadLen)
	remaining := bytecodeExpansionBudget{
		visitedTypes: make(map[flatbuffers.UOffsetT]struct{}),
		functions:    maximumDecodedFunctionOccurrences, types: typeAllowance, typeAllowance: typeAllowance,
		vectors: maximumDecodedVectorElements, strings: maximumDecodedStringBytes,
	}
	if err := remaining.chargeVectors(payloadLen, fileSet.EntrypointsLength(), fileSet.InitialisationFunctionsLength(),
		fileSet.PackageVariablesLength()); err != nil {
		return err
	}
	if err := validateFileStrings(ctx, fileSet, &remaining); err != nil {
		return err
	}
	for _, root := range []*schemagen.CompiledFunction{fileSet.Root(nil), fileSet.VariableInitFunction(nil)} {
		if err := validateFunctionTree(ctx, root, 0, payloadLen, &remaining); err != nil {
			return err
		}
	}
	return validateTypeEntries(ctx, fileSet.PackageVariablesLength(), func(index int) *schemagen.TypeDescriptor {
		var entry schemagen.PackageVariableEntry
		if !fileSet.PackageVariables(&entry, index) {
			return nil
		}
		return entry.TypeDescriptor(nil)
	}, 0, payloadLen, &remaining)
}

// validateFunctionTree charges every recursively reconstructed function occurrence.
//
// Takes function (*schemagen.CompiledFunction) which is the untrusted function to
// validate.
// Takes depth (int) which is the current nesting depth.
// Takes payloadLen (int) which is the total payload size.
// Takes remaining (*bytecodeExpansionBudget) which tracks the shared work budget.
//
// Returns error without allocating reconstructed function state.
func validateFunctionTree(ctx context.Context, function *schemagen.CompiledFunction, depth, payloadLen int, remaining *bytecodeExpansionBudget) error {
	if function == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxBytecodeFunctionNestingDepth {
		return fmt.Errorf("%w: function expansion nesting", errBytecodeRecursionDepthExceeded)
	}
	if remaining.functions == 0 {
		return fmt.Errorf("%w: function expansion exceeds %d occurrences", errCorruptBytecodePayload, maximumDecodedFunctionOccurrences)
	}
	remaining.functions--
	if err := validateFunctionVectors(ctx, function, payloadLen, remaining); err != nil {
		return err
	}
	if err := validateFunctionStrings(ctx, function, remaining); err != nil {
		return err
	}
	if err := validateFunctionTypes(ctx, function, payloadLen, remaining); err != nil {
		return err
	}
	count, err := boundedCount(function.FunctionsLength(), payloadLen, "function expansion")
	if err != nil {
		return err
	}
	var child schemagen.CompiledFunction
	for index := range count {
		if function.Functions(&child, index) {
			if err := validateFunctionTree(ctx, &child, depth+1, payloadLen, remaining); err != nil {
				return err
			}
		}
	}
	return validateFunctionTree(ctx, function.VariableInitFunction(nil), depth+1, payloadLen, remaining)
}

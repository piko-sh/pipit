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
	"reflect"

	"pipit.sh/pipit/internal/schema/schemagen"
)

const (
	// maximumDecodedTypeUnits is the unit budget for decoded type descriptors.
	maximumDecodedTypeUnits = 1 << 16

	// typeUnitPayloadBytes is the payload size that earns one decoded type unit beyond the
	// floor: a well-formed module spends roughly fifty payload bytes per unit, so a payload
	// that needs more than one unit per thirty-two bytes is expanding, not describing.
	typeUnitPayloadBytes = 32
)

// chargeTypes reserves descriptor occurrences and allocated descriptor-vector slots.
//
// Takes count (int) which is the non-negative number of expansion units.
//
// Returns error when cumulative expansion would exceed the file-wide bound.
func (budget *bytecodeExpansionBudget) chargeTypes(count int) error {
	if count < 0 || count > budget.types {
		return fmt.Errorf("%w: type expansion exceeds %d units", errCorruptBytecodePayload, budget.typeAllowance)
	}
	budget.types -= count
	return nil
}

// typeUnitAllowance returns the decoded type units a payload may expand into.
//
// Takes payloadLen (int) which is the payload size in bytes.
//
// Returns int which is the floor or the payload-proportional allowance, whichever is
// larger.
func typeUnitAllowance(payloadLen int) int {
	return max(maximumDecodedTypeUnits, payloadLen/typeUnitPayloadBytes)
}

// validateFunctionTypes checks every descriptor-bearing function table.
//
// Takes function (*schemagen.CompiledFunction) which holds the untrusted function
// metadata.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the file-wide expansion budget.
//
// Returns error before reconstructing any function or runtime type.
func validateFunctionTypes(ctx context.Context, function *schemagen.CompiledFunction, payloadLen int, budget *bytecodeExpansionBudget) error {
	if err := validateTypeEntries(ctx, function.TypeTableDescriptorsLength(), func(index int) *schemagen.TypeDescriptor {
		var entry schemagen.TypeDescriptor
		if !function.TypeTableDescriptors(&entry, index) {
			return nil
		}
		return &entry
	}, 0, payloadLen, budget); err != nil {
		return err
	}
	if err := validateTypeEntries(ctx, function.GeneralConstantDescriptorsLength(), func(index int) *schemagen.TypeDescriptor {
		var entry schemagen.GeneralConstantDescriptor
		if !function.GeneralConstantDescriptors(&entry, index) {
			return nil
		}
		return entry.TypeDescriptor(nil)
	}, 0, payloadLen, budget); err != nil {
		return err
	}
	return validateTypeEntries(ctx, function.TypeNamesLength(), func(index int) *schemagen.TypeDescriptor {
		var entry schemagen.TypeNameEntry
		if !function.TypeNames(&entry, index) {
			return nil
		}
		return entry.TypeDescriptor(nil)
	}, 0, payloadLen, budget)
}

// validateTypeEntries charges slots even when their descriptor is absent. The getter is a
// private generated-accessor adapter, never a script callback.
//
// Takes declared (int) which is the untrusted vector length.
// Takes getter (func(int) *schemagen.TypeDescriptor) which retrieves each descriptor by
// index.
// Takes depth (int) which is the current nesting depth.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared expansion budget.
//
// Returns error before any decoded vector allocation.
func validateTypeEntries(ctx context.Context, declared int, getter func(int) *schemagen.TypeDescriptor,
	depth, payloadLen int, budget *bytecodeExpansionBudget,
) error {
	count, err := boundedCount(declared, payloadLen, "type expansion entries")
	if err != nil {
		return err
	}
	if err := budget.chargeTypes(count); err != nil {
		return err
	}
	for index := range count {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateTypeTree(ctx, getter(index), depth, payloadLen, budget); err != nil {
			return err
		}
	}
	return nil
}

// validateTypeTree visits every descriptor edge that unpacking reconstructs. Edges are
// checked regardless of the claimed type kind.
//
// Takes descriptor (*schemagen.TypeDescriptor) which is the untrusted type descriptor.
// Takes depth (int) which is the current nesting depth.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the file-wide work budget.
//
// Returns error for excessive depth, expansion or cancelled work.
func validateTypeTree(ctx context.Context, descriptor *schemagen.TypeDescriptor, depth, payloadLen int, budget *bytecodeExpansionBudget) error {
	if descriptor == nil {
		return nil
	}
	if descriptor.Kind() < schemagen.TypeDescKindBasic || descriptor.Kind() > schemagen.TypeDescKindNil {
		return fmt.Errorf("%w: unknown type descriptor kind", errCorruptBytecodePayload)
	}
	if descriptor.Kind() == schemagen.TypeDescKindNil && depth > 0 {
		return fmt.Errorf("%w: nil type nested inside a composite type", errCorruptBytecodePayload)
	}
	if descriptor.Kind() == schemagen.TypeDescKindBasic && !validBytecodeBasicKind(descriptor.BasicKind()) {
		return fmt.Errorf("%w: invalid basic type kind", errCorruptBytecodePayload)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxBytecodeTypeDescriptorDepth {
		return fmt.Errorf("%w: type expansion nesting", errBytecodeRecursionDepthExceeded)
	}

	if budget.alreadyVisitedType(descriptor.Table().Pos) {
		return nil
	}
	if err := budget.chargeTypes(1); err != nil {
		return err
	}
	if err := budget.chargeStrings(descriptor.Name(), descriptor.PackagePath()); err != nil {
		return err
	}
	for _, child := range []*schemagen.TypeDescriptor{descriptor.Element(nil), descriptor.Key(nil), descriptor.Value(nil)} {
		if err := validateTypeTree(ctx, child, depth+1, payloadLen, budget); err != nil {
			return err
		}
	}
	return validateTypeChildren(ctx, descriptor, depth+1, payloadLen, budget)
}

// validBytecodeBasicKind admits primitive encodings and the zero placeholder. The zero
// placeholder occurs in unused descriptor slots emitted by the writer.
//
// Takes kind (byte) which is the untrusted one-byte basic kind.
//
// Returns bool which is false for composite kinds and unknown encodings.
func validBytecodeBasicKind(kind byte) bool {
	switch reflect.Kind(kind) {
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16,
		reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32,
		reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String,
		reflect.UnsafePointer:
		return true
	default:
		return false
	}
}

// validateTypeChildren checks field, parameter and result vectors with one budget.
//
// Takes descriptor (*schemagen.TypeDescriptor) which is the parent type descriptor.
// Takes depth (int) which is the nesting depth of the children.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared expansion budget.
//
// Returns error without treating vector aliases as fresh allocation budgets.
func validateTypeChildren(ctx context.Context, descriptor *schemagen.TypeDescriptor, depth, payloadLen int, budget *bytecodeExpansionBudget) error {
	if err := validateTypeEntries(ctx, descriptor.FieldsLength(), func(index int) *schemagen.TypeDescriptor {
		var field schemagen.TypeDescField
		if !descriptor.Fields(&field, index) {
			return nil
		}
		return field.TypeDescriptor(nil)
	}, depth, payloadLen, budget); err != nil {
		return err
	}
	if err := validateTypeFieldStrings(ctx, descriptor, budget); err != nil {
		return err
	}
	if err := validateTypeEntries(ctx, descriptor.ParamsLength(), func(index int) *schemagen.TypeDescriptor {
		var parameter schemagen.TypeDescriptor
		if !descriptor.Params(&parameter, index) {
			return nil
		}
		return &parameter
	}, depth, payloadLen, budget); err != nil {
		return err
	}
	return validateTypeEntries(ctx, descriptor.ResultsLength(), func(index int) *schemagen.TypeDescriptor {
		var result schemagen.TypeDescriptor
		if !descriptor.Results(&result, index) {
			return nil
		}
		return &result
	}, depth, payloadLen, budget)
}

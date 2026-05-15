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

	"pipit.sh/pipit/internal/schema/schemagen"
)

// maximumDecodedStringBytes is the byte budget for decoded string constants.
const maximumDecodedStringBytes = 8 << 20

// chargeStrings counts referenced bytes without converting them to Go strings. Aliases
// are charged on every occurrence, including in safe-copy builds.
//
// Takes values (...[]byte) which are the byte slices returned by generated accessors.
//
// Returns error before arithmetic underflow or expanded string allocation.
func (budget *bytecodeExpansionBudget) chargeStrings(values ...[]byte) error {
	for _, value := range values {
		if len(value) > budget.strings {
			return fmt.Errorf("%w: string expansion exceeds %d bytes", errCorruptBytecodePayload, maximumDecodedStringBytes)
		}
		budget.strings -= len(value)
	}
	return nil
}

// validateFileStrings accounts for file metadata after vector admission.
//
// Takes fileSet (*schemagen.CompiledFileSet) which holds the untrusted file metadata.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared reconstruction budget.
//
// Returns error before copying or hashing expanded strings.
func validateFileStrings(ctx context.Context, fileSet *schemagen.CompiledFileSet, budget *bytecodeExpansionBudget) error {
	if err := checkStringEntries(ctx, fileSet.EntrypointsLength(), func(index int) error {
		var entry schemagen.EntrypointEntry
		if !fileSet.Entrypoints(&entry, index) {
			return nil
		}
		return budget.chargeStrings(entry.Name())
	}); err != nil {
		return err
	}
	return checkStringEntries(ctx, fileSet.PackageVariablesLength(), func(index int) error {
		var entry schemagen.PackageVariableEntry
		if !fileSet.PackageVariables(&entry, index) {
			return nil
		}
		return budget.chargeStrings(entry.Name(), entry.PackagePath())
	})
}

// validateFunctionStrings charges every function occurrence, including aliases.
//
// Takes function (*schemagen.CompiledFunction) which has already had its vectors
// admitted.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared string byte budget.
//
// Returns error before reconstructing string pools or metadata.
func validateFunctionStrings(ctx context.Context, function *schemagen.CompiledFunction, budget *bytecodeExpansionBudget) error {
	if err := budget.chargeStrings(function.Name(), function.SourceFile()); err != nil {
		return err
	}
	if err := checkStringEntries(ctx, function.StringConstantsLength(), func(index int) error {
		return budget.chargeStrings(function.StringConstants(index))
	}); err != nil {
		return err
	}
	if err := validateFunctionMetadataStrings(ctx, function, budget); err != nil {
		return err
	}
	return validateInterfaceStrings(ctx, function, budget)
}

// validateFunctionMetadataStrings accounts for constant, type and method names.
//
// Takes function (*schemagen.CompiledFunction) which has already had its vectors
// admitted.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared string byte budget.
//
// Returns error when metadata alone exceeds cumulative expansion limits.
func validateFunctionMetadataStrings(ctx context.Context, function *schemagen.CompiledFunction, budget *bytecodeExpansionBudget) error {
	if err := checkStringEntries(ctx, function.GeneralConstantDescriptorsLength(), func(index int) error {
		var entry schemagen.GeneralConstantDescriptor
		if !function.GeneralConstantDescriptors(&entry, index) {
			return nil
		}
		return budget.chargeStrings(entry.PackagePath(), entry.SymbolName())
	}); err != nil {
		return err
	}
	if err := checkStringEntries(ctx, function.TypeNamesLength(), func(index int) error {
		var entry schemagen.TypeNameEntry
		if !function.TypeNames(&entry, index) {
			return nil
		}
		return budget.chargeStrings(entry.Name())
	}); err != nil {
		return err
	}
	return checkStringEntries(ctx, function.MethodTableLength(), func(index int) error {
		var entry schemagen.MethodTableEntry
		if !function.MethodTable(&entry, index) {
			return nil
		}
		return budget.chargeStrings(entry.Name())
	})
}

// validateInterfaceStrings charges names copied from each interface-method list.
//
// Takes function (*schemagen.CompiledFunction) which has already had its vectors
// admitted.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared string byte budget.
//
// Returns error without allocating reconstructed method strings.
func validateInterfaceStrings(ctx context.Context, function *schemagen.CompiledFunction, budget *bytecodeExpansionBudget) error {
	return checkStringEntries(ctx, function.TypeTableInterfaceMethodsLength(), func(index int) error {
		var entry schemagen.InterfaceMethodSet
		if !function.TypeTableInterfaceMethods(&entry, index) {
			return nil
		}
		return checkStringEntries(ctx, entry.MethodsLength(), func(method int) error {
			return budget.chargeStrings(entry.Methods(method))
		})
	})
}

// validateTypeFieldStrings accounts for field names, tags and package paths.
//
// Takes descriptor (*schemagen.TypeDescriptor) which has already had its field vector
// admitted.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared string byte budget.
//
// Returns error before decoded field metadata is allocated.
func validateTypeFieldStrings(ctx context.Context, descriptor *schemagen.TypeDescriptor, budget *bytecodeExpansionBudget) error {
	return checkStringEntries(ctx, descriptor.FieldsLength(), func(index int) error {
		var field schemagen.TypeDescField
		if !descriptor.Fields(&field, index) {
			return nil
		}
		return budget.chargeStrings(field.Name(), field.Tag(), field.PackagePath())
	})
}

// checkStringEntries checks cancellation while visiting an already admitted vector. The
// accessor is a private parser closure, never a script-provided callback.
//
// Takes count (int) which is the bounded vector length.
// Takes accessor (func(int) error) which charges each entry.
//
// Returns the first cancellation, accessor or accounting failure.
func checkStringEntries(ctx context.Context, count int, accessor func(int) error) error {
	for index := range count {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := accessor(index); err != nil {
			return err
		}
	}
	return nil
}

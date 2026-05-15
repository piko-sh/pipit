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

// maximumDecodedVectorElements is the element budget for decoded vectors.
const maximumDecodedVectorElements = 1 << 20

// chargeVectors bounds individual lengths and their cumulative expansion.
//
// Takes payloadLen (int) which is the total payload size.
// Takes counts (...int) which are the declared vector lengths to charge.
//
// Returns error before subtraction can underflow. Admitted elements are not refunded.
func (budget *bytecodeExpansionBudget) chargeVectors(payloadLen int, counts ...int) error {
	for _, declared := range counts {
		count, err := boundedCount(declared, payloadLen, "vector expansion")
		if err != nil {
			return err
		}
		if count > budget.vectors {
			return fmt.Errorf("%w: vector expansion exceeds %d elements", errCorruptBytecodePayload, maximumDecodedVectorElements)
		}
		budget.vectors -= count
	}
	return nil
}

// validateFunctionVectors charges all function vectors before reconstruction. Repeated
// functions and aliased vectors consume the same file-wide allowance.
//
// Takes function (*schemagen.CompiledFunction) which holds the untrusted function
// metadata.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared expansion budget.
//
// Returns error before allocating decoded slices or maps.
func validateFunctionVectors(ctx context.Context, function *schemagen.CompiledFunction, payloadLen int, budget *bytecodeExpansionBudget) error {
	if err := budget.chargeVectors(payloadLen,
		function.RegisterCountsLength(), function.ParameterKindsLength(), function.ParameterRegistersLength(),
		function.ResultKindsLength(), function.BodyLength(), function.BoolConstantsLength(),
		function.IntConstantsLength(), function.FloatConstantsLength(), function.UintConstantsLength(),
		function.ComplexConstantsLength(), function.StringConstantsLength(), function.GeneralConstantDescriptorsLength(),
		function.TypeTableDescriptorsLength(), function.TypeNamesLength(), function.CallSitesLength(),
		function.UpvalueDescriptorsLength(), function.FunctionsLength(), function.NamedResultLocationsLength(),
		function.MethodTableLength(), function.StructLayoutTableLength(), function.TypeTableInterfaceMethodsLength(),
	); err != nil {
		return err
	}
	if err := validateCallVectors(ctx, function, payloadLen, budget); err != nil {
		return err
	}
	return validateMethodVectors(ctx, function, payloadLen, budget)
}

// validateCallVectors charges nested arguments and results, including aliases.
//
// Takes function (*schemagen.CompiledFunction) which has already had its outer vectors
// admitted.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared expansion budget.
//
// Returns error before materialising any call-site data.
func validateCallVectors(ctx context.Context, function *schemagen.CompiledFunction, payloadLen int, budget *bytecodeExpansionBudget) error {
	var site schemagen.CallSite
	for index := range function.CallSitesLength() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if function.CallSites(&site, index) {
			if err := budget.chargeVectors(payloadLen, site.ArgumentsLength(), site.ReturnsLength()); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateMethodVectors charges each reconstructed interface method list.
//
// Takes function (*schemagen.CompiledFunction) which has already had its outer vectors
// admitted.
// Takes payloadLen (int) which is the total payload size.
// Takes budget (*bytecodeExpansionBudget) which tracks the shared expansion budget.
//
// Returns error when nested vector expansion exceeds the remaining allowance.
func validateMethodVectors(ctx context.Context, function *schemagen.CompiledFunction, payloadLen int, budget *bytecodeExpansionBudget) error {
	var methods schemagen.InterfaceMethodSet
	for index := range function.TypeTableInterfaceMethodsLength() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if function.TypeTableInterfaceMethods(&methods, index) {
			if err := budget.chargeVectors(payloadLen, methods.MethodsLength()); err != nil {
				return err
			}
		}
	}
	return nil
}

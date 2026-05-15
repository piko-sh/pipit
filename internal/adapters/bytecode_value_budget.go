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
	"fmt"
	"reflect"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

// maximumDecodedValueBytes is the byte budget for decoded general-constant storage.
const maximumDecodedValueBytes = 64 << 20

// bytecodeValueBudget tracks the remaining allocation budget during general-constant
// reconstruction.
type bytecodeValueBudget struct {
	// remaining is the number of bytes still available.
	remaining uintptr
}

// reconstruct charges fresh zero-value storage before allocating it. Borrowed package
// symbols remain host-owned and do not allocate another object.
//
// Takes data (codec.GeneralConstantDescriptorData) which is the preflighted descriptor.
// Takes registry (*symtab.SymbolRegistry) which provides the host's symbol lookups.
//
// Returns reflect.Value which is the reconstructed value.
// Returns error when the constant kind is unknown or the budget is exceeded.
func (budget *bytecodeValueBudget) reconstruct(data codec.GeneralConstantDescriptorData, registry *symtab.SymbolRegistry) (reflect.Value, error) {
	switch descriptor.GeneralConstantKind(data.Kind) {
	case descriptor.GeneralConstantPackageSymbol, descriptor.GeneralConstantPackageSymbolAddress:
		return codec.ReconstructGeneralConstant(data, registry)
	case descriptor.GeneralConstantNamedTypeZero:
		return budget.namedZero(data.PackagePath, data.SymbolName, registry)
	case descriptor.GeneralConstantCompositeZero:
		resolved, err := codec.DescriptorToReflectType(data.TypeDescriptor, registry)
		if err != nil {
			return reflect.Value{}, err
		}
		return budget.zero(resolved)
	default:
		return reflect.Value{}, fmt.Errorf("%w: unknown general constant kind", errCorruptBytecodePayload)
	}
}

// namedZero resolves the registry's nil-pointer type marker without allocating.
//
// Takes packagePath (string) which is the preflighted package import path.
// Takes name (string) which is the preflighted symbol name.
// Takes registry (*symtab.SymbolRegistry) which provides the host's symbol lookups.
//
// Returns reflect.Value which is a fresh addressable zero.
// Returns error when the type is not found or the budget is exceeded.
func (budget *bytecodeValueBudget) namedZero(packagePath, name string, registry *symtab.SymbolRegistry) (reflect.Value, error) {
	value, exists := registry.Lookup(packagePath, name)
	if !exists || value.Kind() != reflect.Pointer || !value.IsNil() {
		return reflect.Value{}, fmt.Errorf("named type %s.%s not found in registry", packagePath, name)
	}
	return budget.zero(value.Type().Elem())
}

// zero reserves complete object storage before reflect.New can allocate it.
//
// Zero-size values still consume one unit. Reservations are never refunded.
//
// Takes resolved (reflect.Type) which is the type to allocate, including host-registered
// named types.
//
// Returns reflect.Value which is the fresh zero value.
// Returns error when the budget is exhausted.
func (budget *bytecodeValueBudget) zero(resolved reflect.Type) (reflect.Value, error) {
	size := max(uintptr(1), resolved.Size())
	if size > budget.remaining {
		return reflect.Value{}, fmt.Errorf("%w: zero-value storage exceeds %d bytes", errCorruptBytecodePayload, maximumDecodedValueBytes)
	}
	budget.remaining -= size
	return reflect.New(resolved).Elem(), nil
}

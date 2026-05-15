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

package symtab

import (
	"go/types"
	"maps"
	"reflect"
)

// CompositeSymbolProvider merges symbol exports from multiple providers. Later providers
// override earlier ones for the same package and symbol.
type CompositeSymbolProvider struct {
	// providers holds the ordered list of symbol providers to merge.
	providers []SymbolProviderPort
}

// NewCompositeSymbolProvider creates a provider that merges exports from the given
// providers in order.
//
// Takes providers (SymbolProviderPort variadic) which are the symbol providers to merge.
//
// Returns *CompositeSymbolProvider which merges all providers.
func NewCompositeSymbolProvider(providers ...SymbolProviderPort) *CompositeSymbolProvider {
	return &CompositeSymbolProvider{providers: providers}
}

// Exports returns the merged symbol table from all providers.
//
// Later providers override earlier ones for the same symbol.
//
// Returns SymbolExports which maps import paths to symbol maps.
func (c *CompositeSymbolProvider) Exports() SymbolExports {
	merged := make(map[string]map[string]reflect.Value)
	for _, p := range c.providers {
		for pkg, symbols := range p.Exports() {
			if merged[pkg] == nil {
				merged[pkg] = make(map[string]reflect.Value, len(symbols))
			}
			maps.Copy(merged[pkg], symbols)
		}
	}
	return merged
}

// TypesPackages returns the merged type information of every provider that carries any.
//
// Composing providers must not lose the generic signatures the type checker needs, so a
// composite answers for its members. Later providers override earlier ones, as for
// symbols.
//
// Returns the package set keyed by import path.
func (c *CompositeSymbolProvider) TypesPackages() map[string]*types.Package {
	merged := make(map[string]*types.Package)
	for _, p := range c.providers {
		typed, ok := p.(interface {
			TypesPackages() map[string]*types.Package
		})
		if !ok {
			continue
		}
		maps.Copy(merged, typed.TypesPackages())
	}
	return merged
}

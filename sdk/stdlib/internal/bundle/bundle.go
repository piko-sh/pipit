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

// Package bundle holds the provider every standard-library bundle is built from.
package bundle

import (
	"go/types"

	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib/typeinfo"
)

// Provider serves one bundle's symbol tables.
type Provider struct {
	// exports holds the bundle's generated symbol table.
	exports symtab.SymbolExports
}

// New wraps a bundle's generated symbol table as a provider.
//
// Takes exports (symtab.SymbolExports) which is the bundle's table.
//
// Returns *Provider serving that table.
func New(exports symtab.SymbolExports) *Provider {
	return &Provider{exports: exports}
}

// Exports returns the bundle's symbol table.
//
// Returns symtab.SymbolExports keyed by import path.
func (p *Provider) Exports() symtab.SymbolExports {
	return p.exports
}

// TypesPackages returns the shared standard-library type information.
//
// Returns the package set keyed by import path.
func (*Provider) TypesPackages() map[string]*types.Package {
	return typeinfo.TypesPackages()
}

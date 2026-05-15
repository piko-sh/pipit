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

// Package selfhost exposes pipit's own compiler and program packages as native symbols,
// so the self-hosting lane can run the compiler under the interpreter.
package selfhost

import (
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
)

// Provider serves the symbol tables of pipit's own packages that internal/compile depends
// on, generated from pipit-symbols-selfhost.yaml, so the compiler's source can run under
// the interpreter in the self-hosting lane.
type Provider struct{}

var (
	_ symtab.SymbolProviderPort = (*Provider)(nil)

	_ program.TypesPackageProviderPort = (*Provider)(nil)
)

// NewProvider constructs the provider.
//
// Returns *Provider which serves the generated tables.
func NewProvider() *Provider {
	return &Provider{}
}

// Exports returns the generated symbol table.
//
// Returns symtab.SymbolExports keyed by import path.
func (*Provider) Exports() symtab.SymbolExports {
	return Symbols
}

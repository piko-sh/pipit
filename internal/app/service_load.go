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

package app

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

// UseSymbols sets the pre-registered native symbols for import resolution, protecting the
// "unsafe" package from override.
//
// Takes symbols (*symtab.SymbolRegistry) which is the registry to use.
func (s *Service) UseSymbols(symbols *symtab.SymbolRegistry) {
	symbols.ProtectPackage(policy.PkgUnsafe)
	s.symbols = symbols
	s.applyClockOverrides()
}

// UseSymbolProviders builds a symtab.SymbolRegistry from one or more symbol providers.
//
// Later providers override earlier ones for the same package/symbol. The "unsafe" package
// is always protected.
//
// Takes providers (symtab.SymbolProviderPort variadic) which are the symbol providers to
// compose.
func (s *Service) UseSymbolProviders(providers ...symtab.SymbolProviderPort) {
	composite := symtab.NewCompositeSymbolProvider(providers...)
	exports := composite.Exports()
	delete(exports, policy.PkgUnsafe)
	s.symbols = symtab.NewSymbolRegistry(exports)
	s.symbols.ProtectPackage(policy.PkgUnsafe)

	for _, p := range providers {
		if tp, ok := p.(program.TypesPackageProviderPort); ok {
			for path, pkg := range tp.TypesPackages() {
				s.symbols.RegisterTypesPackage(path, pkg)
			}
		}
	}

	s.symbols.SynthesiseAll()
	s.applyClockOverrides()
}

// RegisterPackage registers symbols under a package path in the symbol registry. This is
// useful for creating package aliases by registering the same symbol set under a shorter
// import path.
//
// Takes packagePath (string) which is the import path to register.
// Takes symbols (map[string]reflect.Value) which maps symbol names to their reflected
// values.
func (s *Service) RegisterPackage(packagePath string, symbols map[string]reflect.Value) {
	s.symbols.RegisterPackage(packagePath, symbols)
}

// HasRegisteredPackage reports whether the given import path is available in the symbol
// registry.
//
// Takes importPath (string) which is the full Go import path to check.
//
// Returns true if the package is already available via the symbol registry.
func (s *Service) HasRegisteredPackage(importPath string) bool {
	return s.symbols.HasPackage(importPath)
}

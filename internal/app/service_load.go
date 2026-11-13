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
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

// typesPackagesLoadErrorReporter is a symbol provider that reports why some of its
// pre-built types packages could not be decoded. Such a provider still supplies the
// packages that decoded, and generic type checking of the others is degraded.
type typesPackagesLoadErrorReporter interface {
	// TypesPackagesLoadError reports the decode failures.
	//
	// Returns error which is nil when every package decoded.
	TypesPackagesLoadError() error
}

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
//
// Returns error which joins, per rejected package, an error wrapping
// symtab.ErrTypesPackageLayout for a pre-built types package whose layout disagrees with
// the native types, and the decode failures a provider reports.
func (s *Service) UseSymbolProviders(providers ...symtab.SymbolProviderPort) error {
	composite := symtab.NewCompositeSymbolProvider(providers...)
	exports := composite.Exports()
	delete(exports, policy.PkgUnsafe)
	s.symbols = symtab.NewSymbolRegistry(exports)
	s.symbols.SetNamedCompositeQuota(s.namedCompositeQuota)
	s.symbols.ProtectPackage(policy.PkgUnsafe)

	rejected := make([]error, 0, len(providers))
	for _, p := range providers {
		rejected = append(rejected, s.registerProviderTypes(p)...)
	}

	if s.constructionLog == nil {
		s.constructionLog = logging.NewDeferred()
	}
	s.symbols.SynthesiseAll(s.constructionLog.Context())
	s.applyClockOverrides()
	return errors.Join(rejected...)
}

// registerProviderTypes registers the pre-built types packages one provider supplies.
//
// Takes provider (symtab.SymbolProviderPort) which may supply types packages and report
// that some failed to decode.
//
// Returns []error which holds the provider's decode failure and each rejected package.
func (s *Service) registerProviderTypes(provider symtab.SymbolProviderPort) []error {
	var rejected []error
	if reporter, reports := provider.(typesPackagesLoadErrorReporter); reports {
		if err := reporter.TypesPackagesLoadError(); err != nil {
			rejected = append(rejected, fmt.Errorf("decoding a symbol provider's types packages: %w", err))
		}
	}
	typed, ok := provider.(program.TypesPackageProviderPort)
	if !ok {
		return rejected
	}
	packages := typed.TypesPackages()
	for _, path := range slices.Sorted(maps.Keys(packages)) {
		if err := s.symbols.RegisterTypesPackage(path, packages[path]); err != nil {
			rejected = append(rejected, fmt.Errorf("registering the types of package %q: %w", path, err))
		}
	}
	return rejected
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

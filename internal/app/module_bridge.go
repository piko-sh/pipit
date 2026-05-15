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
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"golang.org/x/tools/go/gcexportdata"

	"pipit.sh/pipit/internal/module"
)

// bridgeBytecodeModule bridges a bytecode-loaded module's exports.
//
// Builds the runtime export set from the CompiledFileSet's entrypoints and republishes
// external methods so cross-module method dispatch resolves. Constants and vars are not
// registered because the bytecode wire format does not carry them.
//
// Takes entry (module.TypesExportEntry) which carries the serialised types-package data
// and per-package FunctionTable.
// Takes cfs (*CompiledFileSet) which is the reconstructed file set.
//
// Returns error when decoding or registration fails.
func (s *Service) bridgeBytecodeModule(entry module.TypesExportEntry, cfs *program.CompiledFileSet) error {
	if cfs == nil || cfs.Root() == nil {
		return errors.New("app: bridgeBytecodeModule needs a non-nil CompiledFileSet root")
	}

	typesPkg, err := s.decodeTypesExportEntry(entry)
	if err != nil {
		return fmt.Errorf("app: decoding TypesExport for %q: %w", entry.ImportPath, err)
	}

	exports := buildExportsFromFunctionTable(cfs, entry.FunctionTable)

	for _, pending := range s.pendingVarBridges {
		if pending.importPath != entry.ImportPath {
			continue
		}
		if exports == nil {
			exports = make(map[string]reflect.Value, len(pending.vars))
		}
		for _, v := range pending.vars {
			exports[v.name] = v.storage
		}
	}

	s.symbols.RegisterPackage(entry.ImportPath, exports)
	s.symbols.RegisterTypesPackage(entry.ImportPath, typesPkg)
	publishExternalMethods(s.globals, cfs.Root())
	return nil
}

// decodeTypesExportEntry rebuilds a [go/types.Package].
//
// Seeds the gcexportdata import cache with packages already registered in the Service's
// SymbolRegistry so cross-module type references resolve to the same *types.Package
// instances rather than distinct stubs.
//
// Takes entry (module.TypesExportEntry) which carries the serialised types-package data.
//
// Returns *types.Package which is the rebuilt go/types package.
// Returns error when the entry is empty or decoding fails.
func (s *Service) decodeTypesExportEntry(entry module.TypesExportEntry) (*types.Package, error) {
	if len(entry.Data) == 0 {
		return nil, errors.New("empty TypesExport data")
	}
	imports := s.symbols.SnapshotTypesPackages()
	fset := token.NewFileSet()
	pkg, err := gcexportdata.Read(bytes.NewReader(entry.Data), fset, imports, entry.ImportPath)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}

// buildExportsFromFunctionTable constructs the exports map for one package.
//
// Uses the bundle's shared CompiledFileSet and the per-package FunctionTable carried in
// the module.TypesExportEntry. Filters out unexported names and synthetic dispatch keys
// (those containing a "."), mirroring collectPackageExports's rules for source-compiled
// packages.
//
// Takes cfs (*CompiledFileSet) which is the reconstructed file set.
// Takes functionTable (map[string]uint16) which maps each exported name to its index in
// program.ExportFunctions(cfs.Root()).
//
// Returns map[string]reflect.Value which is the exports map keyed by name.
func buildExportsFromFunctionTable(cfs *program.CompiledFileSet, functionTable map[string]uint16) map[string]reflect.Value {
	if cfs == nil || cfs.Root() == nil {
		return nil
	}
	exports := make(map[string]reflect.Value, len(functionTable))
	for name, index := range functionTable {
		if !ast.IsExported(name) || strings.Contains(name, ".") {
			continue
		}
		if int(index) >= len(program.ExportFunctions(cfs.Root())) {
			continue
		}
		closure := engine.NewRuntimeClosure(
			program.ExportFunctions(cfs.Root())[index], cfs.Root())
		exports[name] = reflect.ValueOf(closure)
	}
	return exports
}

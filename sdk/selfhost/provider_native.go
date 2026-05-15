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

//go:build !js || !wasm

package selfhost

import (
	"errors"
	"go/token"
	"go/types"
	"log/slog"
	"maps"
	"sync"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/sdk/stdlib/typeinfo"
)

var (
	// sharedTypesOnce guards the one decode of the export data.
	sharedTypesOnce sync.Once

	// sharedTypesPackages holds the decoded packages keyed by import path.
	sharedTypesPackages map[string]*types.Package

	// sharedTypesError records a decode failure.
	sharedTypesError error
)

// TypesPackages returns the pre-loaded *types.Package objects of the generated packages.
//
// Returns the map keyed by import path.
func (*Provider) TypesPackages() map[string]*types.Package {
	sharedTypesOnce.Do(loadSharedTypesPackages)
	return sharedTypesPackages
}

// TypesPackagesLoadError reports whether decoding the export data failed.
//
// Returns error which is nil after a clean decode.
func (*Provider) TypesPackagesLoadError() error {
	sharedTypesOnce.Do(loadSharedTypesPackages)
	return sharedTypesError
}

// loadSharedTypesPackages decodes every export blob through one loader whose import map
// starts with the system provider's packages.
func loadSharedTypesPackages() {
	imports := make(map[string]*types.Package, len(typesExportManifest))
	maps.Copy(imports, typeinfo.TypesPackages())
	entries := make(map[string]typesExportEntry, len(typesExportManifest))
	for _, entry := range typesExportManifest {
		entries[entry.Path] = entry
	}
	loader := typesExportLoader{fset: token.NewFileSet(), imports: imports, entries: entries, visited: make(map[string]bool, len(entries)), errs: nil}
	for _, entry := range typesExportManifest {
		loader.load(entry.Path)
	}
	sharedTypesPackages = make(map[string]*types.Package, len(typesExportManifest))
	for _, entry := range typesExportManifest {
		if pkg, ok := imports[entry.Path]; ok {
			sharedTypesPackages[entry.Path] = pkg
		}
	}
	if len(loader.errs) > 0 {
		sharedTypesError = errors.Join(loader.errs...)
		slog.Warn("self-host types export data failed to decode", logging.ErrAttr(sharedTypesError))
	}
}

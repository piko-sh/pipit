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
	"context"
	"go/types"
)

// contextImporter adapts a function to go/types.Importer.
type contextImporter func(importPath string) (*types.Package, error)

// Import resolves importPath through the function.
//
// Takes importPath (string) which is the import path to resolve.
//
// Returns *types.Package which is the package.
// Returns error when the package cannot be resolved.
func (importer contextImporter) Import(importPath string) (*types.Package, error) {
	return importer(importPath)
}

// Importer returns a go/types importer for one type check, which synthesises packages
// under ctx so their warnings reach the check's logger.
//
// Returns types.Importer which resolves through Import.
func (r *SymbolRegistry) Importer(ctx context.Context) types.Importer {
	return contextImporter(func(importPath string) (*types.Package, error) {
		return r.Import(ctx, importPath)
	})
}

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

import "pipit.sh/pipit/internal/policy"

// ApplyRestrictedSurface filters the registry against the policy.
//
// For each package the policy mentions, FullyDenied packages have every symbol removed
// and Allowed packages keep only symbols listed in the policy. Packages not mentioned in
// either map are left untouched, which makes the policy additive: hosts opt in to
// restriction on a per-package basis without having to list every safe stdlib package.
//
// Takes registry (*SymbolRegistry) which is the registry to filter in place.
// Takes surface (policy.RestrictedPackageSet) which describes the allowed and denied
// surface.
//
// Returns int which is the number of symbols removed.
//
// Concurrency: acquires the registry write lock for the duration of the call.
func ApplyRestrictedSurface(registry *SymbolRegistry, surface policy.RestrictedPackageSet) int {
	if registry == nil {
		return 0
	}
	removed := 0
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for packagePath := range surface.FullyDenied {
		if pkg, ok := registry.symbols[packagePath]; ok {
			removed += len(pkg)
			delete(registry.symbols, packagePath)
		}
	}
	for packagePath, allowed := range surface.Allowed {
		pkg, ok := registry.symbols[packagePath]
		if !ok {
			continue
		}
		for name := range pkg {
			if _, keep := allowed[name]; !keep {
				delete(pkg, name)
				removed++
			}
		}
	}
	return removed
}

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

package modloader

import (
	"fmt"
	"strings"
	"time"

	"github.com/dolmen-go/modfs"
)

const (
	// DefaultGoproxyURL is the public Go module proxy. Pipit consults it when no operator
	// override is configured.
	DefaultGoproxyURL = "https://proxy.golang.org"

	// goproxyFetchTimeout is the per-module wall-clock budget for a GOPROXY round-trip.
	// Short enough that hung proxies do not wedge pipit; generous enough for the cold-cache
	// case where the proxy fronts a slow upstream.
	goproxyFetchTimeout = 30 * time.Second
)

// resolveModuleVersion picks the module version named by requested, falling back to the
// proxy's "latest" when requested is empty.
//
// Takes sourceModule (*modfs.Module) which is the opened proxy module.
// Takes requested (string) which is the wanted version, empty for latest.
//
// Returns *Version which is the resolved module version.
// Returns error when the version cannot be resolved.
func resolveModuleVersion(sourceModule *modfs.Module, requested string) (*modfs.Version, error) {
	if requested == "" {
		latest, err := sourceModule.VersionLatest()
		if err != nil {
			return nil, fmt.Errorf("modloader: resolving latest version: %w", err)
		}
		return latest, nil
	}
	version, err := sourceModule.Version(requested)
	if err != nil {
		return nil, fmt.Errorf("modloader: resolving version %q: %w", requested, err)
	}
	return version, nil
}

// isBundledSource reports whether a file in a module zip belongs in the bundle.
//
// Takes path (string) which is the file being visited.
//
// Returns bool which is true for non-test .go files.
func isBundledSource(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

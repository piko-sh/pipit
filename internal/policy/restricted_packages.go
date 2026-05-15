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

package policy

import (
	"fmt"
	"strings"
)

// RestrictedPackageSet describes selective changes to an existing stdlib surface.
// Packages absent from both maps are left unchanged.
type RestrictedPackageSet struct {
	// Allowed maps package paths to the set of symbol names permitted from that package. An
	// empty inner set means "every symbol the package exports", equivalent to the
	// pre-restriction default.
	Allowed map[string]map[string]struct{}

	// FullyDenied lists package paths that are removed from the registry entirely.
	// Equivalent to ProtectPackage() on the registry side but expressed declaratively here
	// so security reviewers can audit the policy at a glance.
	FullyDenied map[string]struct{}
}

// DefaultRestrictedSurface returns a selective stdlib restriction set for safe execution
// environments.
//
// Returns RestrictedPackageSet which describes the selective restrictions.
func DefaultRestrictedSurface() RestrictedPackageSet {
	return RestrictedPackageSet{
		Allowed: map[string]map[string]struct{}{
			"reflect": {
				"TypeOf":    {},
				"DeepEqual": {},
				"Indirect":  {},
				"Kind":      {},
				"Type":      {},
				"Value":     {},
				"Zero":      {},
				"PtrTo":     {},
			},
		},
		FullyDenied: map[string]struct{}{
			"unsafe":        {},
			"runtime/debug": {},
			"runtime/pprof": {},
			"runtime/trace": {},
			"net/rpc":       {},
		},
	}
}

// DescribeRestrictedSurface returns a human-readable summary of the policy.
//
// Takes surface (RestrictedPackageSet) which is the active policy.
//
// Returns string which is a multi-line summary suitable for direct printing.
func DescribeRestrictedSurface(surface RestrictedPackageSet) string {
	var builder strings.Builder
	builder.WriteString("Restricted package surface:\n")
	if len(surface.FullyDenied) > 0 {
		builder.WriteString("  Fully denied packages:\n")
		for path := range surface.FullyDenied {
			builder.WriteString("    - ")
			builder.WriteString(path)
			builder.WriteString("\n")
		}
	}
	if len(surface.Allowed) > 0 {
		builder.WriteString("  Symbol-allowlisted packages:\n")
		for path, allowed := range surface.Allowed {
			fmt.Fprintf(&builder, "    - %s (%d symbols)\n", path, len(allowed))
		}
	}
	return builder.String()
}

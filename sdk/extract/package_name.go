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

package extract

import (
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"piko.sh/goastutil"
)

// PackageNameSymbol is the generated symbol-table key for the declared package name,
// matching pipit.PackageNameSymbol.
const PackageNameSymbol = "__pipit_package_name"

// buildPackageNameEntries returns the entry that records the package's declared name, or
// nothing when the interpreter would assume that name from the import path anyway, so
// existing tables stay unchanged.
//
// Takes extractedPackage (ExtractedPackage) which supplies the import path and the
// declared name.
//
// Returns []symbolEntry which holds at most the one PackageNameSymbol entry.
func buildPackageNameEntries(extractedPackage ExtractedPackage) []symbolEntry {
	name := extractedPackage.Name
	if name == "" || name == assumedPackageName(extractedPackage.ImportPath) {
		return nil
	}
	return []symbolEntry{{
		name:       PackageNameSymbol,
		expression: goastutil.CallExpr(goastutil.SelectorExpr(reflectPackage, "ValueOf"), goastutil.StrLit(name)),
	}}
}

// assumedPackageName returns the package name goimports, and so the interpreter, assumes
// for an import path. It mirrors the interpreter's own rule, which this module cannot
// import; a shared table of cases keeps the two in step.
//
// Takes importPath (string) which is the import path to name.
//
// Returns string which is the assumed package name.
func assumedPackageName(importPath string) string {
	base := path.Base(importPath)
	if strings.HasPrefix(base, "v") {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			if parent := path.Dir(importPath); parent != "." {
				base = path.Base(parent)
			}
		}
	}
	base = strings.TrimPrefix(base, "go-")
	if index := strings.IndexFunc(base, isNotIdentifierRune); index >= 0 {
		base = base[:index]
	}
	return base
}

// isNotIdentifierRune reports whether r cannot appear in a Go identifier.
//
// Takes r (rune) which is the character to classify.
//
// Returns bool which is true for characters outside letters, digits and underscore.
func isNotIdentifierRune(r rune) bool {
	if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_' {
		return false
	}
	return r < utf8.RuneSelf || !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

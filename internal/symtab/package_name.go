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
	"go/token"
	"path"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PackageNameSymbol is the reserved symbol-table key for a package's declared name as a
// string reflect.Value. It is needed when the name differs from AssumedPackageName.
const PackageNameSymbol = "__pipit_package_name"

// AssumedPackageName returns the package name goimports infers from an import path when
// source is unavailable.
//
// Takes importPath (string) which is the import path to name.
//
// Returns string which is the assumed name; it can be empty for a degenerate path.
func AssumedPackageName(importPath string) string {
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

// declaredPackageName reads the PackageNameSymbol entry of a package's symbol table.
//
// Takes symbols (map[string]reflect.Value) which is the table; nil is allowed.
//
// Returns string which is the recorded name.
// Returns bool which is false when the entry is absent or is not a valid identifier.
func declaredPackageName(symbols map[string]reflect.Value) (string, bool) {
	value, ok := symbols[PackageNameSymbol]
	if !ok || !value.IsValid() || value.Kind() != reflect.String {
		return "", false
	}
	name := value.String()
	if !isValidPackageName(name) {
		return "", false
	}
	return name, true
}

// isValidPackageName reports whether name can be declared by a package clause.
//
// Takes name (string) which is the candidate name.
//
// Returns bool which is true for a Go identifier other than the blank identifier.
func isValidPackageName(name string) bool {
	return name != "_" && token.IsIdentifier(name)
}

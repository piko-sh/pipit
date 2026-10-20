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

//go:build crosslang

package harness

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

var (
	// packageLineRegex matches a package clause.
	packageLineRegex = regexp.MustCompile(`(?m)^package\s+\w+\s*\n`)

	// importBlockRegex matches a parenthesised import block.
	importBlockRegex = regexp.MustCompile(`(?ms)^import\s*\(\s*[^)]*?\s*\)\s*\n?`)

	// importBlockInnerRegex captures the inside of a parenthesised import block.
	importBlockInnerRegex = regexp.MustCompile(`(?ms)^import\s*\(\s*([^)]*?)\s*\)`)

	// singleImportRegex matches a one-line import.
	singleImportRegex = regexp.MustCompile(`(?m)^import\s+(?:[A-Za-z_]\w*\s+)?"[^"]+"\s*\n?`)
	// singleImportInnerRegex captures the path, and any name, of a one-line import.
	singleImportInnerRegex = regexp.MustCompile(`(?m)^import\s+((?:[A-Za-z_]\w*\s+)?"[^"]+")`)
)

// singleFileSource joins Go source bodies into one `package main` file whose import block
// is the given set, for interpreters that take a single source string.
//
// Takes imports (map[string]struct{}) which holds the import lines, quoted paths with any
// name.
// Takes bodies (...string) which are the declarations, joined by newlines.
//
// Returns string which is the merged file.
func singleFileSource(imports map[string]struct{}, bodies ...string) string {
	var builder strings.Builder
	builder.WriteString("package main\n\n")
	if len(imports) > 0 {
		builder.WriteString("import (\n")
		for _, line := range slices.Sorted(maps.Keys(imports)) {
			builder.WriteString("\t" + line + "\n")
		}
		builder.WriteString(")\n")
	}
	builder.WriteString(strings.Join(bodies, "\n"))
	return builder.String()
}

// stripPackageAndImports removes `package main` plus any import statement (single or
// block) from content, recording each import string into the provided set. Returns the
// residual body.
func stripPackageAndImports(content string, imports map[string]struct{}) string {
	content = packageLineRegex.ReplaceAllString(content, "")
	content = importBlockRegex.ReplaceAllStringFunc(content, func(match string) string {
		inner := importBlockInnerRegex.FindStringSubmatch(match)
		if len(inner) >= 2 {
			for line := range strings.SplitSeq(inner[1], "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "//") {
					continue
				}
				imports[line] = struct{}{}
			}
		}
		return ""
	})
	content = singleImportRegex.ReplaceAllStringFunc(content, func(match string) string {
		body := singleImportInnerRegex.FindStringSubmatch(match)
		if len(body) >= 2 {
			imports[body[1]] = struct{}{}
		}
		return ""
	})
	return content
}

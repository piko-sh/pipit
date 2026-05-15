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
	"path/filepath"
	"slices"
	"strings"
)

// goModBlockDirectives lists the directives that accept a parenthesised block.
var goModBlockDirectives = []string{"require", "replace"}

// goModFile is the subset of a go.mod file pipit reads: the module path, the require pins
// and the replace directives that point at a directory.
type goModFile struct {
	// Require maps each required module path to its pinned version.
	Require map[string]string

	// Replace maps a replaced module path to the directory that stands in for it, as written
	// in the file (relative paths are relative to the go.mod directory).
	Replace map[string]string

	// Module is the module path declared by the module directive, empty when absent.
	Module string
}

// record applies one directive entry to the file.
//
// Takes directive (string) which is module, require or replace; others are ignored.
// Takes entry (string) which is the directive's argument text.
func (file *goModFile) record(directive, entry string) {
	switch directive {
	case "module":
		file.Module = strings.Trim(entry, "\"")
	case "require":
		modulePath, version, ok := splitModuleVersion(entry)
		if !ok {
			return
		}
		if file.Require == nil {
			file.Require = make(map[string]string)
		}
		file.Require[modulePath] = version
	case "replace":
		modulePath, directory, ok := splitDirectoryReplace(entry)
		if !ok {
			return
		}
		if file.Replace == nil {
			file.Replace = make(map[string]string)
		}
		file.Replace[modulePath] = directory
	}
}

// parseGoMod extracts the module path, require pins and directory replacements from
// go.mod bytes. Malformed lines are skipped rather than reported, matching the lenient
// behaviour of the version-only reader this generalises.
//
// Takes data ([]byte) which contains live or captured go.mod bytes.
//
// Returns goModFile which holds the parsed directives; maps are nil when empty.
func parseGoMod(data []byte) goModFile {
	var file goModFile
	block := ""
	for raw := range strings.SplitSeq(string(data), "\n") {
		line, ok := stripGoModComment(raw)
		if !ok {
			continue
		}
		if block != "" {
			if line == ")" {
				block = ""
				continue
			}
			file.record(block, line)
			continue
		}
		directive, rest, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "(" && slices.Contains(goModBlockDirectives, directive) {
			block = directive
			continue
		}
		file.record(directive, rest)
	}
	return file
}

// splitDirectoryReplace parses `old [v] => dir` and reports the directory replacement it
// describes. Replacements whose target is another module path with a version are not
// directory replacements and yield false.
//
// Takes entry (string) which is one replace-directive entry.
//
// Returns string which is the replaced module path.
// Returns string which is the replacement directory as written.
// Returns bool which is false when the entry is malformed or not a directory replacement.
func splitDirectoryReplace(entry string) (modulePath, directory string, ok bool) {
	left, right, found := strings.Cut(entry, "=>")
	if !found {
		return "", "", false
	}
	modulePath = strings.Fields(left)[0]
	target := strings.Fields(right)
	if modulePath == "" || len(target) != 1 || !isDirectoryReplacement(target[0]) {
		return "", "", false
	}
	return modulePath, target[0], true
}

// isDirectoryReplacement reports whether a replace target names a directory: Go treats
// absolute paths and paths beginning with ./ or ../ as directories, everything else as a
// module path.
//
// Takes target (string) which is the text after =>.
//
// Returns bool which is true for a directory target.
func isDirectoryReplacement(target string) bool {
	return filepath.IsAbs(target) || target == "." || target == ".." ||
		strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../")
}

// stripGoModComment trims a go.mod line and removes any trailing line comment.
//
// Takes raw (string) which is the unprocessed go.mod line.
//
// Returns string which is the trimmed, comment-free line.
// Returns bool which is false when nothing but whitespace or a comment remains.
func stripGoModComment(raw string) (string, bool) {
	line := strings.TrimSpace(raw)
	if comment := strings.Index(line, "//"); comment >= 0 {
		line = strings.TrimSpace(line[:comment])
	}
	return line, line != ""
}

// splitModuleVersion parses `module/path v1.2.3` into its two fields.
//
// Takes line (string) which is one require-directive entry to split.
//
// Returns string which is the module path.
// Returns string which is the version.
// Returns bool which is false when the input is malformed.
func splitModuleVersion(line string) (modulePath, version string, ok bool) {
	space := strings.IndexAny(line, " \t")
	if space < 0 {
		return "", "", false
	}
	modulePath = strings.TrimSpace(line[:space])
	version = strings.TrimSpace(line[space+1:])
	if modulePath == "" || version == "" {
		return "", "", false
	}
	return modulePath, version, true
}

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

package sandboxbroker

import (
	"strings"
	"unicode/utf8"
)

const (
	// maximumPathBytes is the upper bound on total relative path length.
	maximumPathBytes = 4096

	// maximumPathDepth is the upper bound on slash-separated components.
	maximumPathDepth = 64

	// maximumNameBytes is the upper bound on a single path component.
	maximumNameBytes = 255

	// maximumRootNameBytes is the upper bound on an opaque root identifier.
	maximumRootNameBytes = 64
)

// validRelativePath accepts one portable slash-separated relative name without
// normalising it. Backends must still resolve beneath pinned roots.
//
// Takes name (string) which is the relative path supplied by the worker.
// Takes allowRoot (bool) which permits "." for directory listing.
//
// Returns true only for a bounded, unambiguous portable relative path.
func validRelativePath(name string, allowRoot bool) bool {
	if name == "." {
		return allowRoot
	}
	if name == "" || len(name) > maximumPathBytes || !utf8.ValidString(name) {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) > maximumPathDepth {
		return false
	}
	for _, part := range parts {
		if !validPathComponent(part) {
			return false
		}
	}
	return true
}

// validPathComponent excludes separators, device aliases, streams and ambiguous names.
//
// Takes name (string) containing exactly one proposed path component.
//
// Returns true only for a portable component without aliasing or control bytes.
func validPathComponent(name string) bool {
	if strings.HasPrefix(strings.ToLower(name), stagingPrefix) {
		return false
	}
	if name == "" || name == "." || name == ".." || len(name) > maximumNameBytes ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, character := range name {
		if character < ' ' || character == '\x7f' || strings.ContainsRune("\\:<>\"|?*", character) {
			return false
		}
	}
	base, _, _ := strings.Cut(name, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	suffix, device := strings.CutPrefix(base, "COM")
	if !device {
		suffix, device = strings.CutPrefix(base, "LPT")
	}
	if device {
		switch suffix {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
			return false
		}
	}
	return true
}

// validRootName requires a short, opaque grant identifier rather than a host pathname.
//
// Takes name (string) selected by the host or referenced by a worker request.
//
// Returns true for a lower-case ASCII identifier starting with a letter.
func validRootName(name string) bool {
	if name == "" || len(name) > maximumRootNameBytes || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

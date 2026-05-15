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
	"testing"
)

func TestRelativePath(t *testing.T) {
	valid := []string{"file", "directory/file.go", ".hidden", "résumé.txt", "COM12", "LPT10", strings.Repeat("a", maximumNameBytes), strings.Repeat("a/", maximumPathDepth-1) + "a"}
	for _, name := range valid {
		if !validRelativePath(name, false) {
			t.Errorf("rejected %q", name)
		}
	}
	invalid := []string{"", ".", "..", "/etc/passwd", "a/../b", "a/./b", "a//b", "a/", "\\server\\share", "C:file", "a:b", "a\x00b", "a\x1fb", "a\x7fb", "a*", "a?", "a|b", "a<b", "a>b", "a\"b", "a.", "a ", "NUL", "nul.txt", "CON .txt", "conin$", "CONOUT$.txt", "com1", "LPT9.txt", "COM¹", "lpt².log", "COM³", string([]byte{0xff}), strings.Repeat("a", maximumNameBytes+1), strings.Repeat("a/", maximumPathDepth) + "a", strings.Repeat("abcd/", maximumPathBytes/5+1)}
	for _, name := range invalid {
		if validRelativePath(name, false) {
			t.Errorf("accepted %q", name)
		}
	}
	if !validRelativePath(".", true) || validRelativePath("../", true) {
		t.Fatal("root listing exception escaped its scope")
	}
}

func TestRootName(t *testing.T) {
	for _, name := range []string{"data", "a0-_", strings.Repeat("a", maximumRootNameBytes)} {
		if !validRootName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for _, name := range []string{"", "A", "0a", "_a", "-a", "../data", "/data", "é", "a b", strings.Repeat("a", maximumRootNameBytes+1)} {
		if validRootName(name) {
			t.Errorf("accepted %q", name)
		}
	}
}

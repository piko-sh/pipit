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
	"errors"
	"strings"
	"testing"
)

func TestStagingIdentity(t *testing.T) {
	namespace, err := newStagingNamespace()
	if err != nil {
		t.Fatal(err)
	}
	other, err := newStagingNamespace()
	if err != nil || namespace == other {
		t.Fatal("namespace generation failed:", err)
	}
	first, err := StagingName(namespace, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := StagingName(namespace, 2)
	if err != nil || first == second {
		t.Fatal("operation identities collided:", err)
	}
	again, err := StagingName(namespace, 1)
	if err != nil || first != again {
		t.Fatal("staging identity not reproducible:", err)
	}
	if len(first) > maximumNameBytes || strings.Contains(first, "/") {
		t.Fatal("unbounded staging basename:", first)
	}
	for _, name := range []string{first, strings.ToUpper(first), "directory/" + first, first + "/file"} {
		if validRelativePath(name, false) {
			t.Fatal("script can address private staging name:", name)
		}
	}
	for _, invalid := range []string{"", namespace[:len(namespace)-1], strings.Repeat("A", 32), strings.Repeat("z", 32), "../" + namespace} {
		if _, err := StagingName(invalid, 1); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatal("invalid namespace accepted:", invalid, err)
		}
	}
	if _, err := StagingName(namespace, 0); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("zero identity accepted:", err)
	}
}

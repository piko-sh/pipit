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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInvocationIdentityCanonicalGates(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("package main; func main() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	axes := []string{"env.read", "filesystem.read", "env.read"}
	original := slices.Clone(axes)
	path, first, err := InvocationIdentity(target, axes, "main", []string{"a", "bc"})
	if err != nil || path != target {
		t.Fatalf("identity failed: path=%q error=%v", path, err)
	}
	_, equivalent, err := InvocationIdentity(target, []string{"filesystem.read", "env.read"}, "main", []string{"a", "bc"})
	if err != nil || first != equivalent || !slices.Equal(axes, original) {
		t.Fatalf("gate order changed identity or input: error=%v", err)
	}
	for _, changed := range []struct {
		axes       []string
		entrypoint string
		arguments  []string
	}{
		{axes: axes, entrypoint: "other", arguments: []string{"a", "bc"}},
		{axes: []string{"env.read"}, entrypoint: "main", arguments: []string{"a", "bc"}},
		{axes: axes, entrypoint: "main", arguments: []string{"ab", "c"}},
		{axes: axes, entrypoint: "main", arguments: []string{"bc", "a"}},
		{axes: axes, entrypoint: "main", arguments: []string{"a", "bc", ""}},
		{axes: axes, entrypoint: "main", arguments: []string{"a\x00bc"}},
	} {
		_, identity, err := InvocationIdentity(target, changed.axes, changed.entrypoint, changed.arguments)
		if err != nil || identity == first {
			t.Fatalf("changed invocation retained approval identity: %+v error=%v", changed, err)
		}
	}
	_, sourceOnly, err := ScriptIdentity(target)
	if err != nil || sourceOnly == first {
		t.Fatalf("legacy source-only approval retained: error=%v", err)
	}
}

func TestInvocationIdentityBoundsBeforeSourceScan(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		axes       []string
		entrypoint string
		arguments  []string
	}{
		{axes: make([]string, maxInvocationArguments+1), entrypoint: "main"},
		{arguments: make([]string, maxInvocationArguments+1), entrypoint: "main"},
		{entrypoint: strings.Repeat("x", maxInvocationBytes+1)},
		{axes: []string{strings.Repeat("x", maxInvocationBytes)}, entrypoint: "main"},
		{arguments: []string{strings.Repeat("x", maxInvocationBytes)}, entrypoint: "main"},
	} {
		_, _, err := InvocationIdentity("missing source", test.axes, test.entrypoint, test.arguments)
		if err == nil || !strings.Contains(err.Error(), "approval invocation exceeds") {
			t.Fatalf("unbounded invocation reached source scan: %v", err)
		}
	}
}

func TestInvocationIdentityBindsWorkingDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(target, []byte("package main; func main() {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstDirectory := t.TempDir()
	t.Chdir(firstDirectory)
	_, first, err := InvocationIdentity(target, nil, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureInvocation(target, nil, "main", nil)
	if err != nil || snapshot.Identity() != first {
		t.Fatalf("snapshot identity differs: %v", err)
	}
	t.Chdir(t.TempDir())
	_, second, err := InvocationIdentity(target, nil, "main", nil)
	if err != nil || second == first {
		t.Fatalf("working directory did not change approval: %v", err)
	}
	t.Chdir(firstDirectory)
	_, restored, err := InvocationIdentity(target, nil, "main", nil)
	if err != nil || restored != first {
		t.Fatalf("same working directory lost stable identity: %v", err)
	}
}

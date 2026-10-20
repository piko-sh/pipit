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

	"github.com/stretchr/testify/require"
)

func TestInvocationIdentityCanonicalGates(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(target, []byte("package main; func main() {}"), 0600))
	axes := []string{"env.read", "filesystem.read", "env.read"}
	original := slices.Clone(axes)
	path, first, err := InvocationIdentity(target, axes, "main", []string{"a", "bc"})
	require.NoError(t, err)
	require.Equal(t, target, path)
	_, equivalent, err := InvocationIdentity(target, []string{"filesystem.read", "env.read"}, "main", []string{"a", "bc"})
	require.NoError(t, err)
	require.Equal(t, first, equivalent, "gate order does not change the identity")
	require.Equal(t, original, axes, "the caller's axes are left alone")
	_, sourceOnly, err := ScriptIdentity(target)
	require.NoError(t, err)
	require.NotEqual(t, first, sourceOnly, "a legacy source-only approval is dropped")
	tests := []struct {
		name       string
		axes       []string
		entrypoint string
		arguments  []string
	}{
		{name: "another entrypoint", axes: axes, entrypoint: "other", arguments: []string{"a", "bc"}},
		{name: "fewer axes", axes: []string{"env.read"}, entrypoint: "main", arguments: []string{"a", "bc"}},
		{name: "argument boundary moved", axes: axes, entrypoint: "main", arguments: []string{"ab", "c"}},
		{name: "arguments reordered", axes: axes, entrypoint: "main", arguments: []string{"bc", "a"}},
		{name: "empty argument added", axes: axes, entrypoint: "main", arguments: []string{"a", "bc", ""}},
		{name: "arguments joined by a NUL", axes: axes, entrypoint: "main", arguments: []string{"a\x00bc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, identity, err := InvocationIdentity(target, tt.axes, tt.entrypoint, tt.arguments)
			require.NoError(t, err)
			require.NotEqual(t, first, identity, "a changed invocation loses the approval identity")
		})
	}
}

func TestInvocationIdentityBoundsBeforeSourceScan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		axes       []string
		entrypoint string
		arguments  []string
	}{
		{name: "too many axes", axes: make([]string, maxInvocationArguments+1), entrypoint: "main"},
		{name: "too many arguments", arguments: make([]string, maxInvocationArguments+1), entrypoint: "main"},
		{name: "oversized entrypoint", entrypoint: strings.Repeat("x", maxInvocationBytes+1)},
		{name: "oversized axis", axes: []string{strings.Repeat("x", maxInvocationBytes)}, entrypoint: "main"},
		{name: "oversized argument", arguments: []string{strings.Repeat("x", maxInvocationBytes)}, entrypoint: "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := InvocationIdentity("missing source", tt.axes, tt.entrypoint, tt.arguments)
			require.ErrorContains(t, err, "approval invocation exceeds", "an unbounded invocation is refused before the source scan")
		})
	}
}

func TestInvocationIdentityBindsWorkingDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(target, []byte("package main; func main() {}"), 0o600))
	firstDirectory := t.TempDir()
	t.Chdir(firstDirectory)
	_, first, err := InvocationIdentity(target, nil, "main", nil)
	require.NoError(t, err)
	snapshot, err := CaptureInvocation(target, nil, "main", nil)
	require.NoError(t, err, "the snapshot identity matches")
	require.Equal(t, first, snapshot.Identity())
	t.Chdir(t.TempDir())
	_, second, err := InvocationIdentity(target, nil, "main", nil)
	require.NoError(t, err, "the working directory is part of the approval")
	require.NotEqual(t, first, second)
	t.Chdir(firstDirectory)
	_, restored, err := InvocationIdentity(target, nil, "main", nil)
	require.NoError(t, err, "the same working directory keeps a stable identity")
	require.Equal(t, first, restored)
}

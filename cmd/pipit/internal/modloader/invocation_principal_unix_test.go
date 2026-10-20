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

//go:build unix

package modloader

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvocationIdentityBindsReplacedDirectory(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "current")
	target := filepath.Join(parent, "main.go")
	require.NoError(t, os.Mkdir(directory, 0o700))
	require.NoError(t, os.WriteFile(target, []byte("package main; func main() {}"), 0o600))
	t.Chdir(directory)
	_, first, err := InvocationIdentity(target, nil, "main", nil)
	require.NoError(t, err)
	require.NoError(t, os.Rename(directory, filepath.Join(parent, "previous")))
	require.NoError(t, os.Mkdir(directory, 0o700))
	t.Chdir(directory)
	_, replaced, err := InvocationIdentity(target, nil, "main", nil)
	require.NoError(t, err, "a replacement directory loses the approval")
	require.NotEqual(t, first, replaced)
}

func TestInvocationPrincipalIdentity(t *testing.T) {
	t.Parallel()
	groups := []int{20, 10, 20}
	original := slices.Clone(groups)
	identity := unixPrincipalIdentity(1, 2, 3, 4, groups, "5", "6")
	canonical := unixPrincipalIdentity(1, 2, 3, 4, []int{10, 20}, "5", "6")
	require.Equal(t, canonical, identity, "group order does not change the identity")
	require.Equal(t, original, groups, "the caller's groups are left alone")
	baseline := invocationHash("source", nil, "main", nil, identity)
	for _, changed := range [][]string{
		unixPrincipalIdentity(9, 2, 3, 4, groups, "5", "6"),
		unixPrincipalIdentity(1, 9, 3, 4, groups, "5", "6"),
		unixPrincipalIdentity(1, 2, 9, 4, groups, "5", "6"),
		unixPrincipalIdentity(1, 2, 3, 9, groups, "5", "6"),
		unixPrincipalIdentity(1, 2, 3, 4, []int{10, 21}, "5", "6"),
		unixPrincipalIdentity(1, 2, 3, 4, groups, "9", "6"),
		unixPrincipalIdentity(1, 2, 3, 4, groups, "5", "9"),
	} {
		require.NotEqual(t, baseline, invocationHash("source", nil, "main", nil, changed), "changed credentials or directory identity drop the approval")
	}
}

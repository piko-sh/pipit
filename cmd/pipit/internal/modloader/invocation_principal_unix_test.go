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
)

func TestInvocationIdentityBindsReplacedDirectory(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "current")
	target := filepath.Join(parent, "main.go")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package main; func main() {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	_, first, err := InvocationIdentity(target, nil, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, filepath.Join(parent, "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	_, replaced, err := InvocationIdentity(target, nil, "main", nil)
	if err != nil || replaced == first {
		t.Fatalf("replacement directory retained approval: %v", err)
	}
}

func TestInvocationPrincipalIdentity(t *testing.T) {
	t.Parallel()
	groups := []int{20, 10, 20}
	original := slices.Clone(groups)
	identity := unixPrincipalIdentity(1, 2, 3, 4, groups, "5", "6")
	canonical := unixPrincipalIdentity(1, 2, 3, 4, []int{10, 20}, "5", "6")
	if !slices.Equal(identity, canonical) || !slices.Equal(groups, original) {
		t.Fatal("group canonicalisation changed identity or caller data")
	}
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
		if invocationHash("source", nil, "main", nil, changed) == baseline {
			t.Fatal("changed credentials or directory identity retained approval")
		}
	}
}

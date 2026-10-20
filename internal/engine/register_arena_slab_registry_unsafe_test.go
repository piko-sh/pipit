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

//go:build !safe && !(js && wasm)

package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func stringAddress(s string) uintptr {
	return uintptr(unsafe.Pointer(unsafe.StringData(s)))
}

func TestArenaSlabRegistryTracksEveryByteSlab(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	current := arenaString(arena, "in-the-current-slab")
	require.True(t, inArenaSlab(stringAddress(current)), "a new arena records its byte slab")
	require.False(t, inArenaSlab(stringAddress(strings.Clone("host"))), "host memory is in no arena")

	arena.GrowByteSlab(len(arena.byteSlab) + 1)
	grown := arenaString(arena, "in-the-grown-slab")
	require.True(t, inArenaSlab(stringAddress(current)), "a retired slab stays recorded while its strings may be read")
	require.True(t, inArenaSlab(stringAddress(grown)), "a grown slab is recorded")

	arena.dropRetainedOldSlabs()
	require.False(t, inArenaSlab(stringAddress(current)), "a dropped slab is forgotten")

	arena.abandonEscapedSlabs()
	require.False(t, inArenaSlab(stringAddress(grown)), "an abandoned slab is no arena's to reuse")
	require.True(t, inArenaSlab(stringAddress(arenaString(arena, "fresh"))), "its replacement is recorded")
}

func TestArenaSlabRegistryForgetsACollectedArena(t *testing.T) {
	t.Parallel()
	owner := func() uint64 {
		arena := NewRegisterArena()
		arena.GrowByteSlab(len(arena.byteSlab) + 1)
		return arena.slabOwner
	}()
	recorded := func() bool {
		return slices.ContainsFunc(loadArenaSlabRanges(), func(entry arenaSlabRange) bool { return entry.owner == owner })
	}

	require.Eventually(t, func() bool {
		runtime.GC()
		return !recorded()
	}, 10*time.Second, 10*time.Millisecond, "a collected arena's slabs leave the registry")
}

func inArenaSlab(pointer uintptr) bool {
	_, _, found := arenaSlabContaining(pointer)
	return found
}

func TestForeignStringBarrierClonesOnlyArenaStrings(t *testing.T) {
	t.Parallel()
	parent := GetRegisterArena()
	t.Cleanup(func() { PutRegisterArena(parent) })
	abandoning := NewRegisterArena()
	abandoned := arenaString(abandoning, "left-to-an-escaped-value")
	abandoning.abandonEscapedSlabs()

	tests := []struct {
		name      string
		value     string
		wantClone bool
	}{
		{name: "a host string", value: strings.Clone("host-string"), wantClone: false},
		{name: "a constant", value: "constant-string", wantClone: false},
		{name: "a string in another arena", value: arenaString(parent, "parent-string"), wantClone: true},
		{name: "a string in an abandoned slab", value: abandoned, wantClone: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callback := GetRegisterArena()
			defer PutRegisterArena(callback)
			callback.cloneUnownedStrings = true

			stored := materialiseStringForSliceStore(callback, unsafe.Pointer(unsafe.SliceData(callback.AllocStringBacking(1))), tt.value)

			if tt.wantClone {
				requireClone(t, tt.value, stored, "a string another arena may reuse is cloned")
			} else {
				requireSameBytes(t, tt.value, stored, "a string no arena will reuse is stored as it is")
			}
		})
	}
}

func TestInAnArenaSlabRemembersTheLastForeignSlab(t *testing.T) {
	t.Parallel()
	parent := GetRegisterArena()
	defer PutRegisterArena(parent)
	callback := GetRegisterArena()
	defer PutRegisterArena(callback)
	callback.cloneUnownedStrings = true
	foreign := arenaString(parent, "parent-string")

	require.True(t, callback.inAnArenaSlab(foreign))
	start, length := callback.foreignSlabStart, callback.foreignSlabLength
	require.Positive(t, length, "the slab the lookup found is remembered")
	require.False(t, callback.inAnArenaSlab(strings.Clone("host-string")), "a host string misses the remembered slab and the registry")
	require.Equal(t, start, callback.foreignSlabStart, "a miss keeps the remembered slab")
	require.True(t, callback.inAnArenaSlab(arenaString(parent, "another")), "a second string in the same slab hits it")

	callback.Reset()
	require.Zero(t, callback.foreignSlabLength, "Reset forgets the remembered slab")
}

func TestArenaSlabRegistryIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			for range 50 {
				arena := NewRegisterArena()
				arena.GrowByteSlab(len(arena.byteSlab) + 1)
				arena.dropRetainedOldSlabs()
				arena.abandonEscapedSlabs()
			}
		})
		group.Go(func() {
			probe := strings.Clone("probe")
			for range 2000 {
				inArenaSlab(stringAddress(probe))
			}
		})
	}
	group.Wait()
}

func TestOnlyReplaceByteSlabAssignsTheByteSlab(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	var offenders []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, readErr := os.ReadFile(name)
		require.NoError(t, readErr)
		file, parseErr := parser.ParseFile(fileSet, name, source, 0)
		require.NoError(t, parseErr)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Name.Name == "replaceByteSlab" {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						if selector, isSelector := target.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "byteSlab" {
							offenders = append(offenders, fileSet.Position(target.Pos()).String())
						}
					}
				case *ast.KeyValueExpr:
					if key, isIdent := typed.Key.(*ast.Ident); isIdent && key.Name == "byteSlab" {
						offenders = append(offenders, fileSet.Position(typed.Pos()).String())
					}
				}
				return true
			})
		}
	}
	require.Empty(t, offenders, "install a byte slab through replaceByteSlab so the slab registry records it")
}

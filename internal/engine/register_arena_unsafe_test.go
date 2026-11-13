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
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var reviewedStringConstructors = map[string]string{
	"ArenaConcatenateString":     "AllocStringBytes; the in-place extension is at the string byte slab's tail",
	"arenaConcatenateStringRune": "AllocStringBytes; the in-place extension is at the string byte slab's tail",
	"arenaRuneToString":          "AllocStringBytes",
	"arenaBytesToString":         "copies b into AllocStringBytes",
	"arenaItoaString":            "AllocStringBytes",
	"arenaFormatIntString":       "AllocStringBytes",
	"arenaToUpper":               "AllocStringBytes",
	"arenaToLower":               "AllocStringBytes",
	"arenaRepeat":                "AllocStringBytes",
	"arenaReplaceAll":            "AllocStringBytes",
	"arenaJoin":                  "AllocStringBytes",
	"auditUnbarrieredStore":      "the reuse audit's own leak probe, AllocStringBytes",
}

func TestStringConstructorsNeverBuildOverMutableBytes(t *testing.T) {
	arena := newArena(t)
	backing := arena.AllocByteBacking(16)
	copy(backing, "mutable-bytes-01")
	require.True(t, arena.ownsMutBytes(bytesStart(backing)))
	built := map[string]string{}
	built["string(b)"] = arenaBytesToString(arena, backing)
	built["string(b[i:j])"] = arenaBytesToString(arena, backing[3:9])
	head := arenaBytesToString(arena, backing)
	built["concat at tail"] = ArenaConcatenateString(arena, head, "tail")
	built["concat"] = ArenaConcatenateString(arena, "x", arenaBytesToString(arena, backing))
	built["rune concat"] = arenaConcatenateStringRune(arena, arenaBytesToString(arena, backing), 'z')
	built["rune"] = arenaRuneToString(arena, rune(backing[0]))
	built["itoa"] = arenaItoaString(arena, int64(len(backing)))
	built["format int"] = arenaFormatIntString(arena, 12345, 16)
	built["upper"] = arenaToUpper(arena, arenaBytesToString(arena, backing))
	built["lower"] = arenaToLower(arena, "ABC"+arenaBytesToString(arena, backing))
	built["repeat"] = arenaRepeat(arena, arenaBytesToString(arena, backing[:2]), 4)
	built["replace all"] = arenaReplaceAll(arena, arenaBytesToString(arena, backing), "-", "+")
	built["join"] = arenaJoin(arena, reflect.ValueOf([]string{arenaBytesToString(arena, backing), "y"}), ",")
	for name, s := range built {
		require.Falsef(t, arena.stringOverMutBytes(s), "%s built a string over the mutable byte slab", name)
	}
	for i := range backing {
		backing[i] = 'X'
	}
	require.Equal(t, "mutable-bytes-01", built["string(b)"], "writing the []byte leaves the strings built from it")
	require.Equal(t, "mutable-bytes-01tail", built["concat at tail"])
	require.Equal(t, "MUTABLE-BYTES-01", built["upper"])
	arena.Reset()
}

func TestNoStringConstructorReadsMutableBytes(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	found := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		require.NoError(t, err)
		file, err := parser.ParseFile(fileSet, name, source, 0)
		require.NoError(t, err)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			constructs, allocatesBacking := false, false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if base, ok := selector.X.(*ast.Ident); ok && ((base.Name == "unsafe" && selector.Sel.Name == "String") || (base.Name == "mem" && selector.Sel.Name == "String")) {
					constructs = true
				}
				if selector.Sel.Name == "AllocByteBacking" || selector.Sel.Name == "allocMutByteBacking" {
					allocatesBacking = true
				}
				return true
			})
			if !constructs {
				continue
			}
			found[function.Name.Name] = true
			_, listed := reviewedStringConstructors[function.Name.Name]
			require.Truef(t, listed, "%s: %s builds a string over raw bytes and is not a reviewed constructor; "+
				"no string may be built over the mutable byte slab, whose bytes the guest can still write", name, function.Name.Name)
			require.Falsef(t, allocatesBacking, "%s: %s builds a string and allocates a []byte backing", name, function.Name.Name)
		}
	}
	var missing []string
	for name := range reviewedStringConstructors {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	require.Empty(t, missing, "reviewed constructors no longer in the sources: update the list")
}

func TestStringOfMutableBytesIsACopy(t *testing.T) {
	arena := newArena(t)
	backing := arena.AllocByteBacking(5)
	copy(backing, "bytes")
	text := arenaBytesToString(arena, backing)
	backing[0] = 'X'
	require.Equal(t, "bytes", text)
	require.False(t, arena.stringOverMutBytes(text))
	arena.Reset()
}

func TestResetShrinksAnIdleOvergrownMutableByteSlab(t *testing.T) {
	arena := newArena(t)
	setArenaBudget(t, arena, 1<<30)
	grown := initialMutByteSlabSize * maxArenaMultiplier * 4
	arena.allocMutByteBacking(grown)
	require.GreaterOrEqual(t, len(arena.mutByteSlab), grown, "test harness: one run grew the mutable byte slab")

	arena.Reset()
	for range backingSlabIdleResetsBeforeShrink {
		if arena.mutByteSlab == nil {
			break
		}
		require.Greater(t, len(arena.mutByteSlab), initialMutByteSlabSize*maxArenaMultiplier, "test harness: the slab survives the reset that ends its run")
		arena.Reset()
	}

	require.LessOrEqual(t, len(arena.mutByteSlab), initialMutByteSlabSize,
		"a pooled arena whose runs no longer use the grown slab gives it back")
}

func TestResetKeepsAMutableByteSlabEachRunUses(t *testing.T) {
	arena := newArena(t)
	setArenaBudget(t, arena, 1<<30)
	grown := initialMutByteSlabSize * maxArenaMultiplier * 4
	for range 2 * backingSlabIdleResetsBeforeShrink {
		arena.allocMutByteBacking(grown)
		arena.Reset()
	}
	require.GreaterOrEqual(t, len(arena.mutByteSlab), grown, "a slab every run fills is kept")
}

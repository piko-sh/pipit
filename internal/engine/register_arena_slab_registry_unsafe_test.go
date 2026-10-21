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
	require.False(t, inOwnersSlab(arena.slabOwner, stringAddress(current)), "a dropped slab is forgotten")

	arena.abandonEscapedSlabsLazily()
	require.False(t, inOwnersSlab(arena.slabOwner, stringAddress(grown)), "an abandoned slab is no longer the arena's to reuse")
	require.True(t, inArenaSlab(stringAddress(arenaString(arena, "fresh"))), "its replacement is recorded")
}

func inOwnersSlab(owner uint64, pointer uintptr) bool {
	return slices.ContainsFunc(loadArenaSlabRanges(), func(entry arenaSlabRange) bool {
		return entry.owner == owner && entry.start <= pointer && pointer < entry.end
	})
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
	abandoning.abandonEscapedSlabsLazily()

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
	require.False(t, callback.inAnArenaSlab(hostStringOutsideTheRegistry(t)), "a host string misses the remembered slab and the registry")
	require.Equal(t, start, callback.foreignSlabStart, "a miss keeps the remembered slab")
	require.True(t, callback.inAnArenaSlab(arenaString(parent, "another")), "a second string in the same slab hits it")

	callback.Reset()
	require.Zero(t, callback.foreignSlabLength, "Reset forgets the remembered slab")
}

func hostStringOutsideTheRegistry(t *testing.T) string {
	t.Helper()
	held := make([]string, 0, 64)
	for attempt := range 64 {
		if attempt > 0 && attempt%16 == 0 {
			runtime.GC()
		}
		host := strings.Repeat("h", 11<<(attempt%12))
		if _, _, found := arenaSlabContaining(stringAddress(host)); !found {
			runtime.KeepAlive(held)
			return host
		}
		held = append(held, host)
	}
	require.FailNow(t, "64 host strings in a row lay in recorded slab ranges")
	return ""
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
				arena.abandonEscapedSlabsLazily()
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

func TestReuseHazardForeignStringBarrierSeesAReusedByteSlab(t *testing.T) {
	observations := &hazardObservations{}
	const text = "foreign bytes past the end of a dead arena's shorter slab"
	buffer := make([]byte, InitialByteSlabSize)
	base := slabBase(buffer)
	half := len(buffer) / 2

	dead := NewRegisterArena()
	dead.replaceByteSlab(buffer[:half:half], false)
	if !inOwnersSlab(dead.slabOwner, base) {
		assertNoHazard(t, observations, "the first arena's half-length slab is not recorded")
		return
	}

	owner := NewRegisterArena()
	owner.replaceByteSlab(buffer, false)
	owner.attachLicencePolicy(&VMLimits{})
	arenaString(owner, strings.Repeat("f", half))
	lent := arenaString(owner, text)
	address := stringAddress(lent)
	if slabBase(owner.byteSlab) != base || address < base+uintptr(half) {
		assertNoHazard(t, observations, "the lent string is not in the second half of the reused slab")
		return
	}
	observations.note("the first arena's entry is still recorded=%v", inOwnersSlab(dead.slabOwner, base))
	if start, holder, found := arenaSlabHolder(address); !found || start != base || holder != owner.slabOwner {
		observations.fire("slab-misattributed", "the byte registry names start %#x holder %d (found=%v) for the lent string, want %#x holder %d",
			start, holder, found, base, owner.slabOwner)
	}

	callback := newArena(t)
	callback.cloneUnownedStrings = true
	stored := materialiseString(callback, lent)
	cloned := stringAddress(stored) != address
	observations.note("the callback's store into heap memory cloned the foreign string=%v", cloned)

	owner.Reset()
	observations.note("owner's release recycled the slab=%v", slabBase(owner.byteSlab) == base)
	relicence(owner)
	overwriteStrings(owner, half+len(text))
	observations.compareString("foreign string a callback stored into heap memory", text, stored)
	runtime.KeepAlive(lent)
	runtime.KeepAlive(buffer)
	runtime.KeepAlive(dead)
	callback.Reset()
	owner.Reset()
	assertNoHazard(t, observations, "")
}

func ownerHasRegistryEntries(owner uint64) bool {
	if slices.ContainsFunc(loadArenaSlabRanges(), func(entry arenaSlabRange) bool { return entry.owner == owner }) {
		return true
	}
	return slices.ContainsFunc(licenceRegistryRanges(), func(entry licenceSlabRange) bool { return entry.owner == owner })
}

func TestAnArenaThePoolRefusesLeavesNeitherRegistryAtOnce(t *testing.T) {
	bound := stablepoolRetentionBound(registerArenaPool.Cap(), registerArenaPool.MaxCap())
	arenas := burst(2*bound, func() *RegisterArena {
		arena := GetRegisterArena()
		arena.attachLicencePolicy(&VMLimits{})
		arenaString(arena, "published before release")
		return arena
	}, PutRegisterArena)
	kept := registerArenaPool.Drain()
	for _, arena := range kept {
		registerArenaPool.Put(arena)
	}
	var refused []*RegisterArena
	for _, arena := range arenas {
		if !slices.Contains(kept, arena) {
			refused = append(refused, arena)
		}
	}
	require.NotEmpty(t, refused, "a burst of %d arenas overflows a pool bounded at %d", len(arenas), bound)
	for _, arena := range refused {
		require.NotZero(t, arena.slabOwner, "test harness: the arena recorded its slabs")
		require.False(t, ownerHasRegistryEntries(arena.slabOwner),
			"an arena the pool refused is torn down at once: none of its slabs stays registered until its cleanup runs")
	}
	for _, arena := range kept {
		if arena.slabOwner != 0 && slices.Contains(arenas, arena) {
			require.True(t, ownerHasRegistryEntries(arena.slabOwner), "a pooled arena keeps its slabs registered")
		}
	}
}

func collectedArenaSlab() ([]byte, uint64) {
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	arenaString(arena, "in a slab whose arena is collected")
	return arena.byteSlab, arena.slabOwner
}

func TestACollectedArenasSlabsAreNoArenasStorageBeforeItsCleanupRuns(t *testing.T) {
	slab, owner := collectedArenaSlab()
	start := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))

	arenaSlabRangesMu.Lock()
	runtime.GC()
	runtime.GC()
	recorded := ownerHasRegistryEntries(owner)
	_, _, inByteRegistry := arenaSlabContaining(start)
	_, holder, heldByteSlab := arenaSlabHolder(start)
	licenceEntry, inLicenceRegistry := licenceSlabContaining(start)
	publishes := licenceHolderPublishes(licenceForeignKey{start: start, owner: owner})
	arenaSlabRangesMu.Unlock()
	runtime.KeepAlive(slab)

	require.True(t, recorded, "test harness: the arena's cleanup, waiting for the registry's lock, has not run yet")
	require.False(t, inByteRegistry, "the byte slab of a collected arena is not arena storage, whether or not its cleanup has run")
	require.False(t, heldByteSlab, "no holder is named for it (holder %d)", holder)
	require.False(t, inLicenceRegistry, "the licence registry names no holder for it (owner %d)", licenceEntry.owner)
	require.False(t, publishes, "a foreign revocation against a collected arena is not recorded")
}

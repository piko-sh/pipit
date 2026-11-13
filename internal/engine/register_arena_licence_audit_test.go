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

//go:build pipit_reuse_audit && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

type licenceDeepNode struct {
	next *licenceDeepNode
	data *[16]byte
}

func licenceDeepChain(hops int, data *[16]byte) *licenceDeepNode {
	head := &licenceDeepNode{data: data}
	for range hops - 1 {
		head = &licenceDeepNode{next: head}
	}
	return head
}

func runDeepRootCollection(t *testing.T, hops int) []string {
	t.Helper()
	AuditReuse(AuditOptions{IncludeActiveRuns: true})
	arena := newAuditArena(t)
	arena.attachLicencePolicy(&VMLimits{})
	retireChunks(t, arena)
	retired := arena.oldGenericByteSlabs[len(arena.oldGenericByteSlabs)-1]
	data := (*[16]byte)(unsafe.Pointer(&retired[0]))
	data[0] = 0x5A
	chain := licenceDeepChain(hops, data)

	vm := newGCTestVM()
	vm.Arena = arena
	vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{reflect.ValueOf(chain)}}}}
	vm.FramePointer = 0
	setArenaOwnerVM(t, arena, vm)
	vm.checkpointFlags |= checkpointFlagGCPending
	previous := AuditSetMidRunEvery(1)
	vm.runPendingCheckpoints()
	AuditSetMidRunEvery(previous)

	var fired []string
	if data[0] != 0x5A {
		fired = append(fired, "value-cleared-by-recycling")
	}
	for _, hit := range auditHitsForOwner(AuditReuse(AuditOptions{IncludeActiveRuns: true}), arena.slabOwner) {
		if hit.SlabKind == slabGenericBytes && hit.ArenaHeaderRefs == 0 {
			fired = append(fired, "audit-"+hit.Class.String())
		}
	}
	keepAliveChain(chain)
	return fired
}

func keepAliveChain(chain *licenceDeepNode) {
	if chain == nil {
		panic("unreachable")
	}
}

func TestLicenceMarkKeepsAChunkAVeryLongChainReaches(t *testing.T) {
	require.Empty(t, runDeepRootCollection(t, 1<<16), "the licensed mark must keep a chunk a chain of 65,536 hops reaches")
}

func TestLicenceMarkTracesAChainPastTheUnlicensedBound(t *testing.T) {
	require.Empty(t, runDeepRootCollection(t, 64*gcTraceMaxDepth), "the licensed mark must keep a chunk a long chain reaches")
}

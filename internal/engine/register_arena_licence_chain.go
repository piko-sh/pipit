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

package engine

import (
	"sync"
	"sync/atomic"
)

const (
	// fibonacciHashMultiplier is 2^64 divided by the golden ratio; multiplying by it spreads
	// a pointer's bits across the word's high bits.
	fibonacciHashMultiplier = 0x9e3779b97f4a7c15

	// servedChainTopInitialSize is the initial capacity of a shard's map of serving VMs.
	servedChainTopInitialSize = 16

	// servedChainShardShift selects the token bits that pick a served-chain shard.
	servedChainShardShift = 9

	// servedChainShardMixShift folds higher token bits into the shard index.
	servedChainShardMixShift = 15
)

const (
	// chainFastSlots is the table's size; a power of two.
	chainFastSlots = 4096

	// chainFastProbes bounds the probe sequence.
	chainFastProbes = 8
)

// chainFastSlot is one goroutine identity's innermost registered VM.
type chainFastSlot struct {
	// top holds the innermost VM of the served chain.
	top atomic.Pointer[VM]

	// key identifies the goroutine whose served chain occupies this slot.
	key atomic.Uintptr
}

const (
	// servedChainMax bounds the served chain a blind hand-off marks; a longer chain falls
	// back.
	servedChainMax = 8

	// servedChainRegistryShards is the number of independently locked shards of the
	// registry.
	servedChainRegistryShards = 64
)

// chainFast is the slot table.
var chainFast [chainFastSlots]chainFastSlot

var (
	// servedChainEngaged is set at the first licensed arena acquired in a build that reads
	// goroutine identities; from then on every VM run registers.
	servedChainEngaged atomic.Bool

	// servedChainRegistry maps a goroutine's identity to the innermost VM running on it,
	// each VM linking the one below (servedChainRunState.below), for goroutines without a
	// slot in chainFast.
	servedChainRegistry [servedChainRegistryShards]servedChainRegistryShard

	// servedChainCounters counts callback blind hand-offs counted on their served chain and
	// those that fell back to the process-wide word, while the licence counters are on
	// (SetLicenceCountingForTest).
	servedChainCounters struct {
		// chainCounted counts callback blind hand-offs counted on their served chain.
		chainCounted atomic.Int64
		// globalFallbacks counts those that fell back to the process-wide word.
		globalFallbacks atomic.Int64
	}

	// exportMarkSerials hands out the identities of the runs markGuestCodeExported marks
	// (servedChainRunState.exportSerial). Zero is never handed out.
	exportMarkSerials atomic.Uint64
)

// servedChainRegistryShard is one shard of servedChainRegistry, padded to its own cache
// lines.
type servedChainRegistryShard struct {
	// top maps goroutine tokens to the innermost VM of each served chain.
	top map[uintptr]*VM

	// mu serialises access to the served-chain registry.
	mu sync.Mutex

	_ [40]byte
}

// servedChainRunState is a VM's place on its goroutine's registry stack and its served
// chain.
type servedChainRunState struct {
	// parkedServed holds the served arena on which this run's callback was counted for
	// parked-caller walks (register_arena_licence_callbacks.go).
	parkedServed *RegisterArena

	// parkedMade holds the maker arena on which this run's callback was counted for
	// parked-caller walks.
	parkedMade *RegisterArena

	// below is the VM that was innermost on the goroutine when this VM's run began.
	below *VM

	// chain holds the served chain's arenas, innermost first, once computed.
	chain [servedChainMax]*RegisterArena

	// token is the goroutine the VM runs on while registered, and 0 otherwise. Other
	// goroutines may read it (an abort marked from elsewhere), so it is atomic.
	token atomic.Uintptr

	// exportSerial identifies the run once markGuestCodeExported has marked it, and is zero
	// before that and while the VM is not registered. Only the VM's goroutine touches it.
	exportSerial uint64

	// depth counts the VM's nested dispatch entries on the goroutine.
	depth int32

	// chainLen is the number of arenas in chain.
	chainLen uint8

	// chainKnown is true once the run computed its chain.
	chainKnown bool

	// chainOK is true when the run's computed chain was found.
	chainOK bool
}

// ServedChainStats counts where callback blind hand-offs were counted since the process
// started.
type ServedChainStats struct {
	// ChainCounted counts callback blind hand-offs counted on their served chain.
	ChainCounted int64

	// GlobalFallbacks counts callback blind hand-offs counted process-wide instead.
	GlobalFallbacks int64
}

// chainFastSlotOf returns token's slot, claiming a free one when claim is set.
//
// Takes token (uintptr) which is the goroutine identity, non-zero.
// Takes claim (bool) which is true when a free slot may be taken.
//
// Returns *chainFastSlot which is nil when the token has no slot.
func chainFastSlotOf(token uintptr, claim bool) *chainFastSlot {
	hash := uint64(token>>pointerAlignShift) * fibonacciHashMultiplier
	start := int(hash >> 52)
	for probe := range chainFastProbes {
		slot := &chainFast[(start+probe)&(chainFastSlots-1)]
		key := slot.key.Load()
		if key == token {
			return slot
		}
		if key != 0 {
			continue
		}
		if !claim {
			return nil
		}
		if slot.key.CompareAndSwap(0, token) || slot.key.Load() == token {
			return slot
		}
	}
	return nil
}

// pushServedChain makes vm the innermost VM registered on token.
//
// Takes token (uintptr) which is the calling goroutine's identity.
// Takes vm (*VM) which starts its run.
//
// Returns *VM which was innermost before.
//
// Concurrency: Protects the fallback served-chain registry with its mutex; each goroutine
// maintains its own chain order.
func pushServedChain(token uintptr, vm *VM) *VM {
	if slot := chainFastSlotOf(token, true); slot != nil {
		return slot.top.Swap(vm)
	}
	shard := servedChainShardOf(token)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if shard.top == nil {
		shard.top = make(map[uintptr]*VM, servedChainTopInitialSize)
	}
	below := shard.top[token]
	shard.top[token] = vm
	return below
}

// popServedChain takes vm off token's registry.
//
// Takes token (uintptr) which is the identity vm registered on.
// Takes vm (*VM) which ends its run.
// Takes below (*VM) which was innermost before it.
//
// Concurrency: Protects the fallback served-chain registry with its mutex; the caller
// must balance the current goroutine's pushes and pops.
func popServedChain(token uintptr, vm, below *VM) {
	if slot := chainFastSlotOf(token, false); slot != nil {
		if !slot.top.CompareAndSwap(vm, below) {
			slot.top.Store(nil)
		}
		return
	}
	shard := servedChainShardOf(token)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if shard.top[token] == vm {
		shard.top[token] = below
	} else {
		shard.top[token] = nil
	}
}

// servedChainTop returns the innermost VM registered on token.
//
// Takes token (uintptr) which is the identity.
//
// Returns *VM which is nil when none is.
//
// Concurrency: Reads the fallback served-chain registry under its mutex; cached chain
// slots are read atomically.
func servedChainTop(token uintptr) *VM {
	if slot := chainFastSlotOf(token, false); slot != nil {
		return slot.top.Load()
	}
	shard := servedChainShardOf(token)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	return shard.top[token]
}

// servedChainStatistics returns the served-chain counters, which move only while the
// licence counters are on (SetLicenceCountingForTest).
//
// Returns ServedChainStats which is a snapshot.
func servedChainStatistics() ServedChainStats {
	return ServedChainStats{
		ChainCounted:    servedChainCounters.chainCounted.Load(),
		GlobalFallbacks: servedChainCounters.globalFallbacks.Load(),
	}
}

// engageServedChain starts registration at the first licensed arena, in a build that
// reads goroutine identities. The served chain, parked-caller walks and served stores all
// read the registry.
func engageServedChain() {
	if goroutineTokens && !servedChainEngaged.Load() {
		servedChainEngaged.Store(true)
	}
}

// servedChainShardOf returns the registry shard of a goroutine identity.
//
// Takes token (uintptr) which is the identity.
//
// Returns *servedChainRegistryShard which is the shard.
func servedChainShardOf(token uintptr) *servedChainRegistryShard {
	return &servedChainRegistry[(token>>servedChainShardShift^token>>servedChainShardMixShift)%servedChainRegistryShards]
}

// enterServedChain registers vm as the innermost VM on the calling goroutine for the run
// that starts, or counts a nested entry of a VM already registered.
//
// Returns bool which is true when the caller must call leaveServedChain when the run
// ends.
func (vm *VM) enterServedChain() bool {
	if !servedChainEngaged.Load() {
		return false
	}
	state := &vm.servedChainRun
	if state.depth > 0 {
		state.depth++
		return true
	}
	token := currentGoroutineToken()
	if token == 0 {
		return false
	}
	state.below = pushServedChain(token, vm)
	state.depth = 1
	state.chainKnown, state.chainOK, state.chainLen = false, false, 0
	state.exportSerial = 0
	state.token.Store(token)
	if vm.reentrantInterpreterVM {
		vm.registerParkedCaller(state.below)
	}
	return true
}

// leaveServedChain ends a run enterServedChain registered.
func (vm *VM) leaveServedChain() {
	state := &vm.servedChainRun
	if state.depth <= 0 {
		return
	}
	if state.depth--; state.depth > 0 {
		return
	}
	token := state.token.Load()
	popServedChain(token, vm, state.below)
	vm.unregisterParkedCaller()
	state.token.Store(0)
	state.below = nil
	state.exportSerial = 0
	vm.exportChainGates = nil
	clear(state.chain[:])
	state.depth, state.chainLen, state.chainKnown, state.chainOK = 0, 0, false, false
}

// servedChain returns the served chain of the registry, which served stores
// (register_arena_licence_callbacks.go) also read to find the arena a callback serves.
//
// Returns []*RegisterArena which is the chain, innermost first.
// Returns bool which is false when the registry does not establish one.
func (a *RegisterArena) servedChain() ([]*RegisterArena, bool) {
	vm := a.licenceVM
	if vm == nil {
		vm = a.ownerVM
	}
	if !servedChainEngaged.Load() || vm == nil || vm.Arena != a && vm.Arena != nil {
		return nil, false
	}
	state := &vm.servedChainRun
	if token := state.token.Load(); token == 0 || currentGoroutineToken() != token {
		return nil, false
	}
	if !state.chainKnown {
		state.chainKnown = true
		state.chainOK = vm.computeServedChain()
	}
	if !state.chainOK {
		return nil, false
	}
	return state.chain[:state.chainLen], true
}

// computeServedChain fills vm's served chain from the registry.
//
// Returns bool which is true when a chain was found.
func (vm *VM) computeServedChain() bool {
	state := &vm.servedChainRun
	token := state.token.Load()
	if servedChainTop(token) != vm {
		return false
	}
	start := state.below
	count := 0
	for served := start; served != nil; served = served.servedChainRun.below {
		if served.servedChainRun.token.Load() != token || count == servedChainMax || served.Arena == nil || !served.Arena.licensed {
			return false
		}
		state.chain[count] = served.Arena
		count++
		if !served.reentrantInterpreterVM {
			state.chainLen = uint8(count)
			return true
		}
	}
	return false
}

// servedChainStale reports whether a blind hand-off would move any chain arena's counter.
//
// Takes chain ([]*RegisterArena) which is the served chain.
// Takes bytes (bool) which selects the byte-only counters.
//
// Returns bool which is true when some counter has been read since it last moved.
func servedChainStale(chain []*RegisterArena, bytes bool) bool {
	for _, served := range chain {
		counters := &served.licenceState.chainBlind
		if bytes && counters.bytes.stale() || !bytes && counters.all.stale() {
			return true
		}
	}
	return false
}

// bumpServedChain counts a blind hand-off on every arena of the served chain. The caller
// holds licenceSealMu shared, as for every blind increment.
//
// Takes chain ([]*RegisterArena) which is the served chain.
// Takes bytes (bool) which selects the byte-only counters.
func bumpServedChain(chain []*RegisterArena, bytes bool) {
	for _, served := range chain {
		counters := &served.licenceState.chainBlind
		if bytes {
			counters.bytes.bump()
		} else {
			counters.all.bump()
		}
	}
	if licenceCounting {
		servedChainCounters.chainCounted.Add(1)
	}
}

// countChainFallback counts a callback blind hand-off counted process-wide while the
// served chain is engaged.
func countChainFallback() {
	if licenceCounting && servedChainEngaged.Load() {
		servedChainCounters.globalFallbacks.Add(1)
	}
}

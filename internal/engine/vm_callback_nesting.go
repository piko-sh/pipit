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
)

const (
	// callbackNestingSharedFactor widens the nesting limit where goroutines cannot be told
	// apart (currentGoroutineToken returns 0), since the count then covers every goroutine
	// running a callback, not one goroutine's nested callbacks.
	callbackNestingSharedFactor = 16

	// callbackRunFrames is the frames a nested callback run adds for itself to the live
	// frames of the run it nests on, namely the run's own base frame.
	callbackRunFrames = 1

	// callbackSharedRunFrames is the frames each callback run counts as where goroutines
	// cannot be told apart (currentGoroutineToken returns 0).
	callbackSharedRunFrames = 8
)

var (
	// callbackNesting tracks the callback runs in progress on each goroutine.
	callbackNesting = &callbackNestingTable{
		mu:     sync.Mutex{},
		top:    nil,
		shared: 0,
	}
)

// callbackNestingTable tracks nested callback runs per goroutine.
//
// Each callback run is placed on top of the run below it on the same goroutine, and its
// frames count from where that run's frames end (VM.callbackDepthBase). Where goroutines
// cannot be told apart (currentGoroutineToken returns 0), each run counts as
// callbackRunFrames frames against a shared count instead.
type callbackNestingTable struct {
	// top maps a goroutine's token to its innermost callback run. The mu guards it.
	top map[uintptr]*VM

	// mu guards top and shared.
	mu sync.Mutex

	// shared counts the callback runs in progress where goroutines cannot be told apart. The
	// mu guards it.
	shared int
}

// callbackRunPlace is where enterCallbackRun placed a callback run, for leaveCallbackRun.
type callbackRunPlace struct {
	// below is the run below on the goroutine, or nil.
	below *VM

	// token is the goroutine's token, or 0 where goroutines cannot be told apart.
	token uintptr

	// savedBase is the VM's callbackDepthBase before the run.
	savedBase int
}

// enterCallbackRun places a callback run of vm on top of the runs in progress on the
// current goroutine, so its frames count from where theirs end. The caller ends the place
// with leaveCallbackRun.
//
// Returns callbackRunPlace which leaveCallbackRun takes.
//
// Panics with the stack overflow error when the runs below already reach the call depth
// limit, as a frame past the limit does (guardCallDepth).
func (vm *VM) enterCallbackRun() callbackRunPlace {
	limit := vm.Limits.MaxCallDepth
	if limit <= 0 {
		limit = maxCallDepth
	}
	token := currentGoroutineToken()
	callbackNesting.mu.Lock()
	defer callbackNesting.mu.Unlock()
	if token == 0 {
		if callbackNesting.shared*callbackSharedRunFrames >= limit*callbackNestingSharedFactor {
			panic(stackOverflowAt(callbackNesting.shared*callbackSharedRunFrames/callbackNestingSharedFactor, limit))
		}
		callbackNesting.shared++
		return callbackRunPlace{below: nil, token: 0, savedBase: vm.callbackDepthBase}
	}
	below := callbackNesting.top[token]
	base := 0
	if below != nil {
		base = below.callbackDepthBase + below.FramePointer + callbackRunFrames
	}
	if base >= limit {
		panic(stackOverflowAt(base, limit))
	}
	if callbackNesting.top == nil {
		callbackNesting.top = make(map[uintptr]*VM)
	}
	callbackNesting.top[token] = vm
	place := callbackRunPlace{below: below, token: token, savedBase: vm.callbackDepthBase}
	vm.callbackDepthBase = base
	return place
}

// leaveCallbackRun ends the place enterCallbackRun gave a callback run of vm.
//
// Takes place (callbackRunPlace) which enterCallbackRun returned.
//
// Concurrent access is guarded by a mutex. The goroutine-state mutex guards removal of
// the run.
func (vm *VM) leaveCallbackRun(place callbackRunPlace) {
	callbackNesting.mu.Lock()
	defer callbackNesting.mu.Unlock()
	vm.callbackDepthBase = place.savedBase
	switch {
	case place.token == 0:
		callbackNesting.shared--
	case place.below == nil:
		delete(callbackNesting.top, place.token)
	default:
		callbackNesting.top[place.token] = place.below
	}
}

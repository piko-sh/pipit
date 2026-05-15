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

package dap

import (
	"reflect"
	"sync"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
)

// containerKind selects which variant of [variableContainer] applies.
type containerKind uint8

const (
	// containerKindScope expands one variable scope of one frame through
	// pipit.Debugger.Variables.
	containerKindScope containerKind = iota + 1

	// containerKindReflectValue expands the fields / entries of a struct / map / slice /
	// array already read out of the interpreter.
	containerKindReflectValue
)

// frameRef addresses one frame of one paused thread.
type frameRef struct {
	// threadID is the debugger thread id.
	threadID uint64

	// frameIndex counts from the innermost frame, 0 being the paused one.
	frameIndex int
}

// variableContainer is one node in the lazily-expanded variable tree. It identifies what
// the IDE should see when it asks for the children of a given variablesReference.
type variableContainer struct {
	// value is the reflect.Value being expanded for [containerKindReflectValue]. Pointers
	// and interfaces are already unwrapped.
	value reflect.Value

	// frame addresses the frame for [containerKindScope].
	frame frameRef

	// scope selects the variable group for [containerKindScope].
	scope pipit.ScopeKind

	// kind selects the variant.
	kind containerKind
}

// stopState is the per-stop container for DAP IDs the client may reference (frame IDs,
// variablesReferences). Allocated fresh on every `stopped` event so IDs stay short and
// deterministic and so the server can release the underlying reflect values for GC.
type stopState struct {
	// frames maps a DAP `frameId` to the thread and frame it addresses. Frame IDs are issued
	// lazily by the stackTrace handler; the scopes, variables and evaluate handlers look
	// them up here.
	frames map[int]frameRef

	// containers maps a `variablesReference` (or a frame ID, since both come out of the same
	// allocator namespace per DAP) to the variable-tree node it expands. Index 0 is reserved
	// because DAP treats `variablesReference: 0` as "no children".
	containers []variableContainer

	// event is the pause that produced this stop.
	event pipit.DebugEvent

	// mu guards containers and frames.
	mu sync.Mutex
}

// newStopState returns an empty stopState for a pause.
//
// Takes event (pipit.DebugEvent) which is the pause.
//
// Returns *stopState which starts with only the reserved slot 0 filled.
func newStopState(event pipit.DebugEvent) *stopState {
	return &stopState{
		frames:     make(map[int]frameRef),
		containers: []variableContainer{{value: reflect.Value{}, frame: frameRef{threadID: 0, frameIndex: 0}, scope: 0, kind: 0}},
		event:      event,
		mu:         sync.Mutex{}}
}

// allocateContainer registers c and returns the ID the client passes back to expand it.
// IDs are 1-based per the DAP variablesReference convention.
//
// Takes c (variableContainer) which is the node to register for expansion.
//
// Returns int which is the 1-based variablesReference for c.
//
// Concurrency: safe for concurrent use; the stop mutex guards the append.
func (p *stopState) allocateContainer(c variableContainer) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := len(p.containers)
	p.containers = append(p.containers, c)
	return id
}

// lookupContainer returns the container for id, or the zero value when the ID is unknown
// or stale (for example, left over from a prior stop).
//
// Takes id (int) which is the variablesReference to resolve.
//
// Returns variableContainer which is the registered node, or the zero value.
// Returns bool which is true when the ID was found.
//
// Concurrency: safe for concurrent use; the stop mutex guards the read.
func (p *stopState) lookupContainer(id int) (variableContainer, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id <= 0 || id >= len(p.containers) {
		return variableContainer{}, false
	}
	return p.containers[id], true
}

// allocateFrame returns the DAP frameId for a frame of a thread, allocating a fresh ID on
// first call and reusing it thereafter.
//
// Takes frame (frameRef) which addresses the frame.
//
// Returns int which is the DAP frameId for that frame.
//
// Concurrency: safe for concurrent use; the stop mutex guards the map.
func (p *stopState) allocateFrame(frame frameRef) int {
	p.mu.Lock()
	for id, known := range p.frames {
		if known == frame {
			p.mu.Unlock()
			return id
		}
	}
	p.mu.Unlock()
	id := p.allocateContainer(variableContainer{kind: containerKindScope, frame: frame, scope: debug.ScopeLocals, value: reflect.Value{}})
	p.mu.Lock()
	p.frames[id] = frame
	p.mu.Unlock()
	return id
}

// frameFor returns the thread and frame a DAP frameId addresses.
//
// Takes frameID (int) which is the DAP frameId to resolve.
//
// Returns frameRef which addresses the frame.
// Returns bool which is true when the frameId was found.
//
// Concurrency: safe for concurrent use; the stop mutex guards the read.
func (p *stopState) frameFor(frameID int) (frameRef, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	frame, ok := p.frames[frameID]
	return frame, ok
}

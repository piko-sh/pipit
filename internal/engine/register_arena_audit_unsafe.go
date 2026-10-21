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

// auditReleaseKRef records every KRef chunk the next owner would reuse, clears it so it
// cannot keep other recorded slabs reachable, and drops it, so the next owner makes fresh
// chunks.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseKRef(recorder *auditRecorder) {
	for _, chunk := range a.kref.sorted {
		elementBytes := int(chunk.slab.Type().Elem().Size())
		auditRecordRange(recorder, chunk.slab.UnsafePointer(), chunk.bytes(), chunk.used*elementBytes, slabKRef, false)
		chunk.slab.Clear()
	}
	a.krefDropAll()
}

// keptPageRunAt reports whether a release kept a run starting at start as a slab, and
// returns its end.
//
// Takes start (uintptr) which is a slab's start.
//
// Returns uintptr which is the run's end.
// Returns bool which is true for a kept run.
func (a *RegisterArena) keptPageRunAt(start uintptr) (uintptr, bool) {
	for _, run := range a.licenceState.keptRuns {
		if run.start == start {
			return run.end, true
		}
	}
	return 0, false
}

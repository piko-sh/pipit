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

package program

import "piko.sh/safeconv"

// RecordObservedCallDepth remembers the deepest frame stack a run of this root reached,
// keeping the maximum across runs.
//
// Takes depth (int) which is the number of frames the run occupied at its peak.
func (f *CompiledFunction) RecordObservedCallDepth(depth int) {
	if f == nil || depth <= 0 {
		return
	}
	clamped := safeconv.IntToInt32(depth)
	for {
		current := f.observedCallDepth.Load()
		if clamped <= current {
			return
		}
		if f.observedCallDepth.CompareAndSwap(current, clamped) {
			return
		}
	}
}

// ObservedCallDepth returns the deepest frame stack any run of this root has reached, or
// 0 when the root has not run yet.
//
// Returns int which is the recorded peak frame count.
func (f *CompiledFunction) ObservedCallDepth() int {
	if f == nil {
		return 0
	}
	return int(f.observedCallDepth.Load())
}

// NoteOptimisticMakePlaced records that a run of this root placed a heap-flagged make in
// the arena.
func (f *CompiledFunction) NoteOptimisticMakePlaced() {
	if f != nil && !f.optimisticMakePlaced.Load() {
		f.optimisticMakePlaced.Store(true)
	}
}

// NoteCallbackForeignClone records that a callback VM running this root cloned a string
// of another arena; once a run of the root has placed a heap-flagged make in the arena,
// this vetoes such placements for later runs.
func (f *CompiledFunction) NoteCallbackForeignClone() {
	if f != nil && f.optimisticMakePlaced.Load() && !f.optimisticMakeVetoed.Load() {
		f.optimisticMakeVetoed.Store(true)
	}
}

// OptimisticMakeVetoed reports whether NoteCallbackForeignClone vetoed this root's
// placements.
//
// Returns bool which is true once vetoed.
func (f *CompiledFunction) OptimisticMakeVetoed() bool {
	return f != nil && f.optimisticMakeVetoed.Load()
}

// DeclareHostTopLevelCalls records the host's declaration that function values received
// from this root's program are called only at top level.
func (f *CompiledFunction) DeclareHostTopLevelCalls() {
	if f != nil && !f.hostTopLevelCalls.Load() {
		f.hostTopLevelCalls.Store(true)
	}
}

// HostCallsTopLevel reports whether DeclareHostTopLevelCalls was called on this root.
//
// Returns bool which is true once declared.
func (f *CompiledFunction) HostCallsTopLevel() bool {
	return f != nil && f.hostTopLevelCalls.Load()
}

// MarkLicenceRejected records that the licence checker rejected the compiled program for
// a defect. The mark lives and dies with the function.
func (f *CompiledFunction) MarkLicenceRejected() {
	if f != nil {
		f.licenceRejected.Store(true)
	}
}

// LicenceRejected reports whether MarkLicenceRejected marked the compiled function.
//
// Returns bool which is true once marked.
func (f *CompiledFunction) LicenceRejected() bool {
	return f != nil && f.licenceRejected.Load()
}

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

// frameStackPeak returns the number of frame slots the current run has occupied. It scans
// from the top down because some paths push frames without incrementing framesUsed, so
// the highest slot with a non-nil Function is the true peak.
//
// Returns int which is the peak frame count, at least framesUsed.
func (a *RegisterArena) frameStackPeak() int {
	for i := len(a.frameSlab) - 1; i >= a.framesUsed; i-- {
		if a.frameSlab[i].Function != nil {
			return i + 1
		}
	}
	return a.framesUsed
}

// shrinkFrameStackWithHysteresis applies the backing-slab shrink policy to the frame
// stack, releasing capacity after enough idle resets.
func (a *RegisterArena) shrinkFrameStackWithHysteresis() {
	shrunk := shrinkSlabWithHysteresis(a.frameSlab, initialFrameSlabs, a.frameStackPeakLastReset, &a.frameStackIdleResets)
	if len(shrunk) == len(a.frameSlab) {
		return
	}
	a.frameSlab = shrunk
	a.callInfoBasesSlab = make([]uintptr, len(shrunk))
	a.dispatchSavesSlab = make([]asmDispatchSave, len(shrunk))
}

// frameStackCapacity returns how many frames the stack can hold before it grows.
//
// Returns int which is the frame slab length.
func (a *RegisterArena) frameStackCapacity() int {
	return len(a.frameSlab)
}

// FrameStackGrowths returns how many times the frame stack has grown since the arena was
// created.
//
// Returns int which is the growth count.
func (a *RegisterArena) FrameStackGrowths() int {
	return a.frameStackGrowths
}

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

// frameStackPeak returns the number of frame slots the current run has occupied.
//
// Returns int which is the peak frame count, at least framesUsed.
//
// Panics in the paranoid build when occupied frame slots contain a gap.
func (a *RegisterArena) frameStackPeak() int {
	peak := a.framesUsed
	for peak < len(a.frameSlab) && a.frameSlab[peak].Function != nil {
		peak++
	}
	if arenaParanoidEnabled {
		top := a.framesUsed
		for i := len(a.frameSlab) - 1; i >= a.framesUsed; i-- {
			if a.frameSlab[i].Function != nil {
				top = i + 1
				break
			}
		}
		if top != peak {
			panic(newInvariantError("frame stack occupied with a gap: peak %d, top %d", peak, top))
		}
	}
	return peak
}

// frameBankPeak returns each register bank's peak use in the current run as far as its
// frames recorded it: the arena's allocation indices now, raised to the save point each
// occupied frame slot holds (the indices on the frame's entry, before its own registers).
//
// Takes frames (int) which is the run's frame-stack peak (frameStackPeak).
//
// Returns arenaIndices which is the peak per bank.
func (a *RegisterArena) frameBankPeak(frames int) arenaIndices {
	peak := a.arenaIndices
	for index := range frames {
		peak.raise(&a.frameSlab[index].arenaSave.arenaIndices)
	}
	return peak
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

// raise lifts each index to other's where other's is higher.
//
// Takes other (*arenaIndices) which is the indices to compare with.
func (indices *arenaIndices) raise(other *arenaIndices) {
	indices.IntIndex = max(indices.IntIndex, other.IntIndex)
	indices.FloatIndex = max(indices.FloatIndex, other.FloatIndex)
	indices.StringIndex = max(indices.StringIndex, other.StringIndex)
	indices.GeneralIndex = max(indices.GeneralIndex, other.GeneralIndex)
	indices.BoolIndex = max(indices.BoolIndex, other.BoolIndex)
	indices.UintIndex = max(indices.UintIndex, other.UintIndex)
	indices.ComplexIndex = max(indices.ComplexIndex, other.ComplexIndex)
	indices.SlicesIntIndex = max(indices.SlicesIntIndex, other.SlicesIntIndex)
	indices.SlicesFloatIndex = max(indices.SlicesFloatIndex, other.SlicesFloatIndex)
	indices.SlicesStringIndex = max(indices.SlicesStringIndex, other.SlicesStringIndex)
	indices.SlicesBoolIndex = max(indices.SlicesBoolIndex, other.SlicesBoolIndex)
	indices.SlicesUintIndex = max(indices.SlicesUintIndex, other.SlicesUintIndex)
	indices.SlicesByteIndex = max(indices.SlicesByteIndex, other.SlicesByteIndex)
	indices.UpvalueCellIndex = max(indices.UpvalueCellIndex, other.UpvalueCellIndex)
	indices.UpvalueReferenceIndex = max(indices.UpvalueReferenceIndex, other.UpvalueReferenceIndex)
}

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

//go:build pipit_reuse_audit

package engine

// auditReleaseClosureChunks records every closure chunk the next owner would reuse and
// drops it, so the reuse audit sees a chunk a host-held value still reaches (a release
// hit).
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseClosureChunks(recorder *auditRecorder) {
	slabs := &a.closureChunks
	if a.closureChunkCount() == 0 {
		return
	}
	auditRecordList(recorder, slabs.cells, slabClosureChunkCells, true)
	auditRecordList(recorder, slabs.freeCells, slabClosureChunkCells, true)
	auditRecordList(recorder, slabs.closures, slabClosureChunks, true)
	auditRecordList(recorder, slabs.freeClosures, slabClosureChunks, true)
	a.closureChunks = closureChunkSlabs{
		sites: nil, cells: nil, freeCells: nil, closures: nil, freeClosures: nil,
		cellIndex: 0, closureIndex: 0, chunk: slabs.chunk, madeChunks: 0,
	}
}

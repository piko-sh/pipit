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

//go:build safe || (js && wasm)

package engine

// closeOverRevoked has nothing to close over in this build, whose probes cannot name the
// storage a revoked slab references; a hand-off there revokes the whole epoch instead.
func (*RegisterArena) closeOverRevoked() {}

// dropRevokedSlabs abandons every data slab, as a revoked epoch's release does
// (detachRevoked). This build's hand-offs revoke the whole epoch, so it keeps no release
// that replaces revoked slabs one by one.
//
// Takes tally (*licenceReleaseTally) which counts the outcome.
func (a *RegisterArena) dropRevokedSlabs(tally *licenceReleaseTally) {
	tally.abandoned += int64(a.countDataSlabs())
	a.abandonEscapedSlabsLazily()
}

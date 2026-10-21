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

// revokeByteSlabsFrom is revokeByteSlabs, leaving the current slab alone when current is
// false.
//
// Takes current (bool) which is false when the current slab need not be revoked.
func (a *RegisterArena) revokeByteSlabsFrom(current bool) {
	revoke := func(slab []byte) {
		if cap(slab) != 0 {
			a.revokeSlab(licenceSlabAddress(slab), noHandOffCause)
		}
	}
	if current {
		revoke(a.byteSlab)
	}
	for _, slab := range a.oldByteSlabs {
		revoke(slab)
	}
}

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

//go:build pipit_reuse_audit && (safe || (js && wasm))

package engine

// auditReleaseKRef has no KRef chunk to record in this build, which makes none.
func (a *RegisterArena) auditReleaseKRef(_ *auditRecorder) {
	a.krefDropAll()
}

// keptPageRunAt finds no kept run in this build, which revokes no slab page by page.
//
// Returns uintptr which is zero.
// Returns bool which is false.
func (*RegisterArena) keptPageRunAt(_ uintptr) (uintptr, bool) {
	return 0, false
}

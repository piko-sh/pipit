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

import "reflect"

// licenceWalkMemo is an arena's walk memo. This build has no walk, so the memo records
// nothing.
type licenceWalkMemo struct {
	// hint is the size the table would be made at its first growth; never read in this
	// build.
	hint int
}

// reset is the epoch boundary; the empty memo has nothing to age.
func (*licenceWalkMemo) reset() {}

// release is the arena's return to the pool; the empty memo drops no table.
//
// Returns int which is zero.
func (*licenceWalkMemo) release() int { return 0 }

// seedFreshObject records nothing in this build, which has no walk memo to seed.
func (*VM) seedFreshObject(_ reflect.Value) {}

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

// finaliserUseSets is the bit of finaliserUse recording a program that calls
// runtime.SetFinalizer. The bits above it hold the scanned table size plus one.
const finaliserUseSets uint64 = 1

// RecordFinaliserUse records on a root whether its program calls runtime.SetFinalizer, as
// found by scanning a function table of the given size. A record that the program does is
// never replaced, so a concurrent scan that found no call cannot undo it.
//
// Takes sets (bool) which is true when it does.
// Takes functions (int) which is the number of functions in the table scanned.
func (f *CompiledFunction) RecordFinaliserUse(sets bool, functions int) {
	if f == nil || functions < 0 {
		return
	}
	use := (safeconv.IntToUint64(functions) + 1) << 1
	if sets {
		use |= finaliserUseSets
	}
	for {
		current := f.finaliserUse.Load()
		if current&finaliserUseSets != 0 || f.finaliserUse.CompareAndSwap(current, use) {
			return
		}
	}
}

// FinaliserUse returns what RecordFinaliserUse recorded on this root. A record that the
// program sets no finaliser holds only while the function table keeps the size it was
// scanned at, since a session can add functions to the root.
//
// Takes functions (int) which is the current number of functions in the table.
//
// Returns sets (bool) which is true when the program calls runtime.SetFinalizer.
// Returns known (bool) which is false when the table must be scanned.
func (f *CompiledFunction) FinaliserUse(functions int) (sets, known bool) {
	sets, scanned := f.FinaliserScan()
	if sets {
		return true, true
	}
	return false, scanned >= 0 && scanned == functions
}

// FinaliserScan returns what RecordFinaliserUse recorded on this root, so that a scan
// after the function table grew can start where the last one stopped.
//
// Returns sets (bool) which is true when the program calls runtime.SetFinalizer.
// Returns scanned (int) which is the table size the last scan covered, or -1 when no scan
// was recorded.
func (f *CompiledFunction) FinaliserScan() (sets bool, scanned int) {
	if f == nil {
		return false, -1
	}
	use := f.finaliserUse.Load()
	if use == 0 {
		return false, -1
	}
	return use&finaliserUseSets != 0, safeconv.Uint64ToInt(use>>1) - 1
}

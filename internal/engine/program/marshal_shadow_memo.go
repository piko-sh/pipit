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

// boundedMemoLimit is the most entries MarshalShadowMemo holds. Past it, values are
// computed again rather than stored.
const boundedMemoLimit = 4096

// StoreMarshalShadow sets the value for key in MarshalShadowMemo, unless key is new and
// the memo is full.
//
// Takes key (any) which is a comparable key.
// Takes value (any) which is the value.
//
// The memo synchronises stores and keeps the count within the limit.
func (f *CompiledFunction) StoreMarshalShadow(key, value any) {
	if _, loaded := f.MarshalShadowMemo.LoadOrStore(key, value); loaded {
		f.MarshalShadowMemo.Store(key, value)
		return
	}
	if f.marshalShadowCount.Add(1) > boundedMemoLimit {
		f.MarshalShadowMemo.Delete(key)
		f.marshalShadowCount.Add(-1)
	}
}

// MarshalShadowLen returns the number of entries StoreMarshalShadow stored.
//
// Returns int which is the count.
func (f *CompiledFunction) MarshalShadowLen() int {
	return int(f.marshalShadowCount.Load())
}

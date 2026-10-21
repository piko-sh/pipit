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

//go:build js && wasm

package engine

// sharedFlag stores GlobalStore.shared without atomic access on js/wasm.
type sharedFlag struct {
	// set is the flag's value.
	set bool
}

// Load reports the flag's value.
//
// Returns bool which is the value last stored.
func (flag *sharedFlag) Load() bool {
	return flag.set
}

// Store sets the flag's value.
//
// Takes value (bool) which is the value to store.
func (flag *sharedFlag) Store(value bool) {
	flag.set = value
}

// frontPointer is a pointer slot in a js/wasm front cache.
//
// js/wasm runs every goroutine on one thread, and a goroutine is never switched out
// during a load or store.
type frontPointer[T any] struct {
	// value is the slot's pointer.
	value *T
}

// Load returns the slot's pointer.
//
// Returns *T which is the pointer last stored, or nil.
func (slot *frontPointer[T]) Load() *T {
	return slot.value
}

// Store sets the slot's pointer.
//
// Takes value (*T) which is the pointer to store.
func (slot *frontPointer[T]) Store(value *T) {
	slot.value = value
}

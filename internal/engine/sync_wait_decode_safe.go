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

//go:build !go1.27 || safe || (js && wasm)

package engine

import (
	"reflect"
	"sync"
)

// decodeSyncWait names the blocking sync method a call makes.
//
// Without unsafe the method value cannot be read, so only the receiver register is used,
// and only Mutex.Lock and Cond.Wait are recognised. An RWMutex call cannot be told from
// its receiver, and a WaitGroup's counter cannot be read.
//
// Takes receiver (reflect.Value) which is the call's receiver register, or invalid.
//
// Returns syncWaitTarget which is the decoded call, kind syncWaitNone otherwise.
func decodeSyncWait(_ reflect.Value, receiver reflect.Value) syncWaitTarget {
	return syncWaitFromReceiver(receiver)
}

// waitGroupIdle is never reached in these builds.
//
// decodeSyncWait does not recognise WaitGroup.Wait.
//
// Takes group (*sync.WaitGroup) which is the group.
//
// Returns bool which is always false.
func waitGroupIdle(_ *sync.WaitGroup) bool { return false }

// syncMethodDecoderAvailable reports whether the method-value decoder is built.
//
// It always returns false in these builds.
//
// Returns bool which is false.
func syncMethodDecoderAvailable() bool { return false }

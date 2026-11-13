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

import (
	"reflect"
	"strings"
)

// placeStringToBytes builds []byte(s) on the heap in this build, which places no []byte
// backing in the arena.
//
// Takes s (string) which is converted.
//
// Returns reflect.Value which is the []byte.
func placeStringToBytes(_ *RegisterArena, _ *CallFrame, s string) reflect.Value {
	return reflect.ValueOf([]byte(s))
}

// placeSplit builds strings.Split(s, sep) on the heap in this build, which places no
// split result in the arena.
//
// Takes s (string) which is split.
// Takes sep (string) which is the separator.
//
// Returns reflect.Value which is the []string.
func placeSplit(_ *RegisterArena, _ *CallFrame, s, sep string) reflect.Value {
	return reflect.ValueOf(strings.Split(s, sep))
}

// copiesNativeString declines every string in this build, whose probes cannot name the
// byte slab a string lies in; the argument takes the revoking path.
//
// Returns bool which is false.
func (*RegisterArena) copiesNativeString(_ string) bool {
	return false
}

// probeStringInRevokedEpoch settles a string handed off in a revoked epoch as
// revokeWithoutWalk does, since this build's probes cannot name the storage the string's
// bytes lie in.
//
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) probeStringInRevokedEpoch(_ string, cause HandOffCause) {
	a.revokeWithoutWalk(cause)
}

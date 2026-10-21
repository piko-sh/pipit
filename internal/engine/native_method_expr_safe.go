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

	"pipit.sh/pipit/internal/engine/program"
)

// nativeMethodShape is the receiver-free cache of one native method call site; in this
// build no method value has a direct path, so the shape is always empty.
type nativeMethodShape struct {
	// expression is the method expression's word, never set in this build.
	expression nativeMethodWord
}

// nativeMethodWord stands for a method expression's or receiver's word, which this build
// never reads.
type nativeMethodWord struct{}

// receiverWord never matches in this build.
//
// Without the reflect.Value layout every native method call binds its method value for
// that call only.
//
// Returns nativeMethodWord which is empty.
// Returns bool which is false.
func (*nativeMethodShape) receiverWord(_ reflect.Value) (nativeMethodWord, bool) {
	return nativeMethodWord{}, false
}

// dispatchNativeMethodTagged is never reached in this build, where receiverWord and
// buildNativeMethodShape never match.
//
// Returns any which is nil.
func dispatchNativeMethodTagged(_ *VM, _ nativeFastPathTag, _, _ nativeMethodWord, _ *program.CallSite, _ *Registers) any {
	return nil
}

// buildNativeMethodShape finds no shape in this build.
//
// Returns nativeMethodShape which is empty.
// Returns bool which is false.
func buildNativeMethodShape(_ reflect.Value, _ nativeFastPathTag) (nativeMethodShape, bool) {
	return nativeMethodShape{}, false
}

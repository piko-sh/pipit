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

// markFunctionValue traces a function value for the licensed mark in the unsafe build;
// the safe build's licensed mark fails closed on every function value.
//
// Returns bool which is always false.
func markFunctionValue(_ *RegisterArena, _ *gcMarkState, _ reflect.Value, _ int) bool {
	return false
}

// markClosureOrCellPointer leaves closures and cells to the reflective trace in this
// build, which cannot read a read-only pointer's referent by its fields' meaning.
//
// Returns bool which is always false.
func markClosureOrCellPointer(_ *RegisterArena, _ *gcMarkState, _ reflect.Value, _ int) bool {
	return false
}

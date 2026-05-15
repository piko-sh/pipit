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

import "errors"

var (
	// ErrCompileJumpRange is returned when a relative jump offset overflows the signed
	// 16-bit encoding.
	ErrCompileJumpRange = errors.New("compiled function jump offset exceeds int16 range")

	// errConstantPoolExhausted is returned when a per-function constant pool (int, float,
	// string, bool, uint, complex, general, type or call-site table) would exceed the
	// configured maximum capacity, defending the host against pathological input that drives
	// unbounded pool growth.
	errConstantPoolExhausted = errors.New("constant pool exhausted")

	// errSpecialisationLimitReached is returned when registering a new generic-function
	// specialisation would exceed the configured maximum. Callers should fall back to the
	// generic reflect path.
	errSpecialisationLimitReached = errors.New("specialisation limit reached")

	// errMethodTableExhausted is returned when registering a new entry in
	// CompiledFunction.methodTable would exceed the configured maximum. Defends against
	// pathological method declarations.
	errMethodTableExhausted = errors.New("method table exhausted")
)

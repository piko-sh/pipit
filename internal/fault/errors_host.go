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

package fault

import "errors"

var (
	// ErrParse is returned when go/parser rejects the source code.
	ErrParse = errors.New("parse failed")

	// ErrTypeCheck is returned when go/types rejects the source code.
	ErrTypeCheck = errors.New("type check failed")

	// ErrCyclicImport is returned when the import graph contains a cycle, which is illegal
	// in Go.
	ErrCyclicImport = errors.New("cyclic import detected")

	// ErrNoBytecodeStore is returned when bytecode save/load is attempted without a
	// configured BytecodeStorePort.
	ErrNoBytecodeStore = errors.New("no bytecode store configured")

	// ErrSourceSizeLimit is returned when the total source code size exceeds the configured
	// maximum set via WithMaxSourceSize.
	ErrSourceSizeLimit = errors.New("source size limit exceeded")

	// ErrCompilePanic is returned when type-checking or compilation panics. A panic here is
	// an internal interpreter defect, never valid user input; it is recovered and surfaced
	// as an ordinary compile error so a single bad package cannot crash the host.
	ErrCompilePanic = errors.New("internal Compiler panic")

	// ErrBytecodeVerification is returned when the bytecode verifier rejects a compiled or
	// loaded program; the wrapped error lists every violation.
	ErrBytecodeVerification = errors.New("bytecode verification failed")

	// ErrSlogOperandResult is returned when a script method an operand adapter for log/slog
	// calls gives a result of the wrong type.
	ErrSlogOperandResult = errors.New("script method returned an unexpected result for log/slog")

	// ErrUnsupportedGenericArgument is the panic raised when a generic host function such as
	// unique.Make or weak.Make is called with a type the bridge has no instantiation for.
	ErrUnsupportedGenericArgument = errors.New("no instantiation of this generic function for the type")

	// ErrTypeArgument is the panic value's wrapped error when a linked sibling receives a
	// type argument its generic's constraint rules out, which the type checker prevents.
	ErrTypeArgument = errors.New("type argument outside the generic's constraint")

	// ErrCleanupArgumentIsPointer mirrors runtime.AddCleanup's refusal of an argument equal
	// to the pointer, whose cleanup could never run.
	ErrCleanupArgumentIsPointer = errors.New("runtime.AddCleanup: ptr is equal to arg, cleanup will never run")

	// ErrCleanupPointer is the panic value's wrapped error for a runtime.AddCleanup pointer
	// that is not a non-nil pointer.
	ErrCleanupPointer = errors.New("runtime.AddCleanup: ptr is not a non-nil pointer")
)

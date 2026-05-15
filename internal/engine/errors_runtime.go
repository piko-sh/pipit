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

package engine

import "errors"

var (
	// errNilPointerIndex is returned when an index operation targets a nil pointer (e.g. a
	// nil *[N]T).
	errNilPointerIndex = errors.New("index of nil pointer")

	// errInvalidOpcode is returned when the VM meets an unrecognised opcode during
	// execution.
	errInvalidOpcode = errors.New("invalid opcode")

	// errStructLiteralTypeNotStruct reports an ALLOC_STRUCT_LITERAL whose type-table entry
	// is not a struct type.
	errStructLiteralTypeNotStruct = errors.New("struct literal allocation: type is not a struct")

	// errUnsafeBounds is returned in runtime safe mode when unsafe pointer arithmetic would
	// read or write outside the origin allocation.
	errUnsafeBounds = errors.New("unsafe pointer arithmetic out of bounds")

	// errStringLimit is returned when a string concatenation would exceed the maximum set
	// via WithMaxStringSize.
	errStringLimit = errors.New("string size limit exceeded")

	// errLinkedSiblingPanic wraps any panic recovered while invoking a //piko:link-routed
	// sibling. The usual cause is a stale generated symbol file whose descriptors no longer
	// match the sibling's signature.
	errLinkedSiblingPanic = errors.New("linked sibling invocation panicked")

	// errLinkedSiblingShapeMismatch is returned when argument arity or kind checks on a
	// //piko:link sibling fail before the call is attempted, so the VM reports a structured
	// error instead of delegating to reflect.Call's panic.
	errLinkedSiblingShapeMismatch = errors.New("linked sibling signature does not match call site")

	// errNativeCallPanic wraps any panic recovered while invoking a registered native
	// function via reflect.Call.
	errNativeCallPanic = errors.New("native call panicked")

	// errBlockingOpUnblocked is surfaced when a blocking channel or select operation woke
	// through its context-cancellation or goroutine-panic wake arm but neither cause could
	// be recovered. Guards against silently continuing with an undefined value.
	errBlockingOpUnblocked = errors.New("blocking operation unblocked without a cause")

	// errDeferStackExhausted is returned when the per-VM defer stack would exceed
	// maxDeferStackSize, defending the host against runaway `for { defer ... }` loops.
	errDeferStackExhausted = errors.New("defer stack exhausted")
)

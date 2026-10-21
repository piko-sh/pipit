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

// Package engine implements a bytecode-compiling Go interpreter.
//
// The interpreter uses [go/parser] and [go/types] for parsing and type checking, then
// compiles the typed AST to bytecode which runs on a register-based virtual machine with
// typed register banks (int64, float64, string, reflect.Value).
//
//	Go source -> go/parser -> go/types.Check -> Compiler -> Program -> VM -> result
//
// Handlers layer fast paths over general paths. A primitive that may decline returns (T,
// bool). A handler-level fast path returns (OpResult, handled bool). New fast paths use
// one of these two shapes.
//
// # Licensed reuse
//
// Licensed reuse determines which storage Pipit can safely reuse. Whenever a value may
// leave the arena, Pipit revokes the licence of the storage that value references. At
// release, Pipit clears and reuses only storage whose licence is still clean. It detaches
// revoked storage without clearing it and leaves it to Go's garbage collector, which
// frees it once nothing references it. Pipit cannot see host roots, so it never restores
// a revoked licence within an execution.
//
// Here, copying follows Go's value semantics. Pipit can copy immutable string bytes,
// by-value structs and arrays, and scalar boxes without changing the program's meaning.
// It never copies storage whose identity the program can observe, such as a pointer's
// pointee, a slice backing or a map. Storage retained by identity is revoked.
//
// # Concurrency
//
// Revoked storage may be reachable from outside the arena. The same is true of storage
// covered by a pending foreign revocation or blind hand-off. A store into any of this
// storage is therefore itself a hand-off (licenceCoveredAt).
//
// A walk reads only memory that no other goroutine can write. While the owning VM shares
// memory with another goroutine (licenceShared), the walk stops if it meets an
// addressable value or map it cannot read safely. Unless the walk memo already settles
// that container, it revokes the epoch and records a blind hand-off.
//
// Collection in a licensed arena runs only at dispatch safepoints, where no handler's Go
// locals hold an arena value (register_arena_abort.go).
//
// # Licence checks
//
// The checker validates the escape pass's arena grants and the inferred map grants. Each
// granted store must write only into a fresh allocation that the function still holds
// alone. The checker also confirms that every reachable instruction declares its
// hand-offs. It cannot be switched off.
//
// Bytecode loaded from a file is verified (VerifyLoadedFileSet) but does not run the
// checker, so it has no local-container grants and every map store is handed off.
//
// Licensed reuse depends on several engine properties. Every store that can publish a
// reference must pass through a barrier or hand-off, and no code may write arena
// references through unsafe or reflect outside those barriers. Compiler grants must be
// correct, every VM root set must be registered with the mark, walks must handle every
// kind, and closures must hold only their cells and receiver.
//
// # Build variants
//
// Most files whose name ends in _unsafe have a twin ending in _safe that does the same
// work through reflect. The safe build tag selects the reflect twin of every pair.
package engine

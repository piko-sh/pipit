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

// Package pipit is a register-based bytecode interpreter for Go source code.
//
// It compiles Go programs to bytecode and runs them in-process on a register-based
// virtual machine with resource limits and capability gating. Which packages interpreted
// code may import is the host's choice, and host packages register the same way as the
// standard library.
//
// Two entry points cover most needs:
//
//   - [NewInterpreter] returns an [Interpreter] for CLI usage, one-shot evaluation and
//     embedding. [Interpreter.Clone] shares the symbol registry but not execution state,
//     which is how a service evaluates on many goroutines at once.
//
//   - [Interpreter.NewSession] returns a [Session], an incremental REPL-style context
//     that keeps declarations across submissions.
//
// Behaviour is configured through the With* options ([WithMaxExecutionTime],
// [WithCostBudget], [WithCapabilityHook], [WithImportAllowlist] and the rest). Run
// outcomes are reported through the Err* sentinels ([ErrParse], [ErrTypeCheck],
// [ErrCostBudgetExceeded], [ErrExecutionCancelled], ...) and tested with errors.Is.
//
// # Getting an interpreter
//
// [NewInterpreter] registers no packages by default. Most hosts want the standard library
// from pipit.sh/pipit/sdk/stdlib:
//
//	interpreter := pipit.NewInterpreter(
//		stdlib.WithStandardLibrary(),
//		pipit.WithMaxExecutionTime(5*time.Second),
//	)
//	result, err := interpreter.Eval(ctx, "1 + 1")
//
// The standard library is the largest thing a pipit binary links (~17 MiB). A host that
// wants less takes individual bundles (core.WithCore, codec.WithCodec, etc.) instead. A
// bundle left out is absent, not filtered.
//
// [WithSymbolProvider] registers host packages alongside the standard library.
// [NewInterpreterWithSymbols] is the shorthand for a plain export table.
//
// # Compiled bytecode
//
// A program can be compiled once and executed many times, or persisted and reloaded.
// [PackCompiledFileSetToBytes] and [LoadCompiledFromBytes] serialise a compiled program
// to a schema-versioned wire format. A payload written by an incompatible schema is
// rejected rather than misread.
package pipit

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
// The pipeline is:
//
//	Go source -> go/parser -> go/types.Check -> Compiler -> Program -> VM -> result
//
// Handlers layer fast paths over general paths. A primitive that may decline returns (T,
// bool); a handler-level fast path returns (OpResult, handled bool). New fast paths use
// one of these two shapes.
package engine

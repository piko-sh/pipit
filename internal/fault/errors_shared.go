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
	// ErrCompilation is returned when the source code fails to compile. The underlying error
	// provides specific parsing or type-checking details.
	ErrCompilation = errors.New("compilation failed")

	// ErrEntrypointNotFound is returned when the requested entrypoint function does not
	// exist in the compiled file set.
	ErrEntrypointNotFound = errors.New("entrypoint not found")

	// ErrCallArgumentCount is returned when a function is called from the host with a number
	// of arguments that does not match its parameter list.
	ErrCallArgumentCount = errors.New("wrong number of arguments for call")

	// ErrExecutionCancelled is returned when the execution context is cancelled or its
	// deadline is exceeded during evaluation.
	ErrExecutionCancelled = errors.New("execution cancelled")

	// ErrFeatureNotAllowed is returned when compiled code uses a language feature that has
	// been disabled via WithFeatures.
	ErrFeatureNotAllowed = errors.New("language feature not allowed")

	// ErrMainReturned is the cancellation cause applied to an execution's context once its
	// main function has returned, telling spawned goroutines to wind down. Children that
	// stop for this reason are not reported as goroutine failures.
	ErrMainReturned = errors.New("main function returned")

	// ErrPackageNotInRegistry is returned when the go/types Importer cannot locate an
	// interpreted package in the symbol registry. Usually means no provider registered it:
	// an interpreter registers nothing until a host asks.
	ErrPackageNotInRegistry = errors.New("package not registered with interpreter")

	// ErrNoSymbolProvider is returned when a tier that reviews imports is built with no
	// symbol provider at all, so there is nothing for it to review.
	ErrNoSymbolProvider = errors.New("no symbol provider registered")

	// ErrCompileDepthLimit is returned when a recursive compile path (expressions,
	// statements, import topological sort) exceeds the configured maximum depth, defending
	// the host against stack exhaustion from pathological user input.
	ErrCompileDepthLimit = errors.New("compile recursion depth exceeded")
)

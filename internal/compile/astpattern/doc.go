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

// Package astpattern holds the for-statement recognisers that replace a recognised loop
// with specialised bytecode: the float64 SIMD kernels (dot product, sum, element-wise
// add, scale) and the small constant-trip-count loop unroller.
//
// Each recogniser implements patterns.Recogniser: Match validates the fingerprinted shape
// against go/types information and returns a token, and Emit writes the specialised
// bytecode through patterns.Emitter, falling back to the compiler's scalar path when an
// operand turns out not to fit. Nothing here imports the compiler; the compiler holds a
// patterns.Registry (DefaultRegistry by default) and asks it once per for statement.
package astpattern

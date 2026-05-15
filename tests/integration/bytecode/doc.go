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

//go:build integration

// Package bytecode holds integration tests that span the bytecode pipeline: the ISA, the
// emitter, the optimiser passes, the verifier and the handlers that execute the result.
//
// These tests live here rather than alongside any single pipeline package because they
// exercise cross-package interactions that would introduce import cycles if placed in a
// leaf package like internal/isa.
package bytecode

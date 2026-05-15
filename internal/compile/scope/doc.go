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

// Package scope tracks lexical variable bindings and allocates registers.
//
// A scope stack maps each in-scope name to the register holding it, and the register
// allocator hands out and recycles slots within a bank. The two are one package because
// leaving a scope is also what makes its registers reusable.
//
// The allocator's watermark is what the compiler records as a function's register count,
// which the arena then uses to size a frame.
package scope

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

// Package isa defines the interpreter's instruction set.
//
// An instruction is four bytes: an opcode and three operand bytes. Operations are grouped
// into tiers by how many operands they take, with the drill opcodes using their leading
// operand bytes as sub-opcodes; that is how the set stays inside one byte while covering
// several hundred operations. The tier is a consequence of arity, not a ranking: dispatch
// is flat, so an operation at tier 3 costs no more to execute than one at tier 0.
package isa

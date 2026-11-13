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
// An instruction has an opcode and three operand bytes. Drill opcodes use leading operand
// bytes as sub-opcodes to select a tier.
//
// # Naming grammar
//
// Printed names start with a verb, followed by register banks and modifiers such as
// CONSTANT, IMMEDIATE, WIDE and UNCHECKED. A fused operation joins two names with _THEN_,
// as in LESS_THAN_INT64_THEN_JUMP_IF_FALSE. The disassembler prints the tier as a prefix,
// as in 2:RETURN. Names are unique across tiers.
//
// Words are spelt out, as in INCREMENT, SUBTRACT and CAPTURED_VARIABLE. Go type names,
// function names such as MATH_SQRT and STRCONV_ITOA, and the terms IIFE, SIMD and OK keep
// their usual spelling.
//
// Register banks are named by the Go types they hold. INT64 holds signed integers, UINT64
// unsigned integers and FLOAT64 also holds float32 values. GENERAL holds other reflected
// values. A sized type in an operation name describes its arithmetic width. ADD_INT32
// wraps at 32 bits, and ROUND_TO_FLOAT32 rounds to float32 precision.
package isa

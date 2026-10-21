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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

// handlerSetFieldExit is the ASM exit stub for isa.OpSetField.
//
//go:noescape
func handlerSetFieldExit()

// handlerGetFieldExit is the ASM exit stub for isa.OpGetField.
//
//go:noescape
func handlerGetFieldExit()

// handlerGetMapExit is the ASM exit stub for isa.OpGetMap.
//
//go:noescape
func handlerGetMapExit()

// handlerAppendExit is the ASM exit stub for isa.OpAppend.
//
//go:noescape
func handlerAppendExit()

// handlerAppendByteExit is the ASM exit stub for isa.OpAppendByte.
//
//go:noescape
func handlerAppendByteExit()

// handlerSetSliceStringElementExit is the ASM exit stub for the tier-1 []string element
// store. It is installed into the tier-1 jump table by initJumpTable (see
// buildStaticJumpTableEntries) rather than by installPerOpDirectExits, because tier-1
// slots are filled at code-generation time.
//
//go:noescape
func handlerSetSliceStringElementExit()

// handlerAppendStructExit is the ASM exit stub for isa.OpAppendStruct.
//
//go:noescape
func handlerAppendStructExit()

// handlerAppendInt64Exit is the ASM exit stub for isa.OpAppendInt64.
//
//go:noescape
func handlerAppendInt64Exit()

// handlerAppendFloat64Exit is the ASM exit stub for isa.OpAppendFloat64.
//
//go:noescape
func handlerAppendFloat64Exit()

// handlerAppendStringExit is the ASM exit stub for isa.OpAppendString.
//
//go:noescape
func handlerAppendStringExit()

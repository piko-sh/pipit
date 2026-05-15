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

const (
	// flatJumpTableSize is the number of slots in flatJumpTable. Four tiers of 256
	// sub-opcode slots each.
	flatJumpTableSize = 1024
)

var (
	// flatJumpTable is the unified dispatch table for the TZCNT-based flat-dispatch model.
	// Populated once at init by installFlatJumpTableASM from the four per-tier source
	// tables.
	flatJumpTable [flatJumpTableSize]uintptr
)

// installFlatJumpTableASM copies handler addresses into flatJumpTable. Must be called
// after the per-tier source tables are fully populated.
//
//go:noescape
func installFlatJumpTableASM()

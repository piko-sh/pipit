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

package isa

const (
	// BytecodeVersionMajor identifies the major bytecode format version. Bytecode emitted at
	// a different major version is incompatible and rejected on load.
	BytecodeVersionMajor uint16 = 12

	// bytecodeVersionMinor identifies an additive bytecode revision. Bumped when new opcodes
	// are appended without disturbing existing iota assignments.
	bytecodeVersionMinor uint16 = 0
)

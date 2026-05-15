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

//go:build !safe && !(js && wasm)

package engine

import (
	// Imported for the //go:linkname directive below that aliases runtime.writeBarrier.
	_ "unsafe"
)

var (
	// runtimeWriteBarrier aliases the runtime's write-barrier control word via linkname,
	// mirroring the runtime.writeBarrier struct layout so ASM handlers can gate on the
	// enabled byte and fall back to Go while the GC is marking.
	//
	//go:linkname runtimeWriteBarrier runtime.writeBarrier
	runtimeWriteBarrier struct {
		enabled bool
		pad     [3]byte
		cgo     bool
		alignme uint64
	}
)

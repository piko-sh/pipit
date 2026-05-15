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

package program

// heapMutationClassification labels a function by its observable effect on the heap. The
// zero value is "unknown", treated as "may mutate" so the absence of classification is
// safe.
type heapMutationClassification uint8

const (
	// HeapPureCallee marks a function whose body provably mutates no heap-resident state (no
	// opSet*, isa.OpMapSet, isa.OpIndexSet, isa.OpAddr, no SET sub-ops) and whose every
	// transitively-reachable callee is also HeapPureCallee.
	HeapPureCallee heapMutationClassification = iota + 1

	// HeapMutatingCallee marks a function whose body contains any heap mutator or which
	// transitively calls another mutating function.
	HeapMutatingCallee
)

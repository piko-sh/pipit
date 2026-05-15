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

package escape

import (
	"context"
	"fmt"

	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/compile/passes"
)

const (

	// maxHeapPurityFixpointIter caps the SCC fixpoint loop as a safety net for pathological
	// call graphs.
	maxHeapPurityFixpointIter = 16
)

// RunHeapPurityAnalysis classifies every reachable function by its heap-mutation
// behaviour and sets CompiledFunction.HeapMutationClass to either HeapPureCallee or
// HeapMutatingCallee.
//
// Takes root (*CompiledFunction) which is the program's top-level compiled function whose
// nested functions are walked.
//
// Returns error when the context is cancelled before completion.
func RunHeapPurityAnalysis(ctx context.Context, root *program.CompiledFunction) error {
	if root == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("runHeapPurityAnalysis cancelled: %w", err)
	}
	all := inline.CollectReachableFunctions(root)
	if len(all) == 0 {
		return nil
	}
	for _, compiledFunction := range all {
		compiledFunction.HeapMutationClass = program.HeapPureCallee
	}
	for range maxHeapPurityFixpointIter {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("runHeapPurityAnalysis cancelled: %w", err)
		}
		if !demoteMutatingFunctions(all) {
			return nil
		}
	}
	demoteAllToMutating(all)
	return nil
}

// demoteAllToMutating forces every function to HeapMutatingCallee, used when the purity
// fixpoint does not converge.
//
// Takes functions ([]*CompiledFunction) whose classification is forced to
// HeapMutatingCallee.
func demoteAllToMutating(functions []*program.CompiledFunction) {
	for _, compiledFunction := range functions {
		compiledFunction.HeapMutationClass = program.HeapMutatingCallee
	}
}

// demoteMutatingFunctions performs a single fixpoint pass. Each function whose body
// contains any heap-mutator instruction, or any call to an already-classified mutating
// callee, is demoted from HeapPureCallee to HeapMutatingCallee.
//
// Takes functions ([]*CompiledFunction) being classified.
//
// Returns true when at least one function flipped in this pass.
func demoteMutatingFunctions(functions []*program.CompiledFunction) bool {
	changed := false
	for _, compiledFunction := range functions {
		if compiledFunction.HeapMutationClass == program.HeapMutatingCallee {
			continue
		}
		if functionHasHeapMutator(compiledFunction) {
			compiledFunction.HeapMutationClass = program.HeapMutatingCallee
			changed = true
		}
	}
	return changed
}

// functionHasHeapMutator reports whether compiledFunction's body either directly mutates
// the heap (struct-field write, map set, etc.) or transitively does so via a
// statically-resolvable call to a HeapMutatingCallee or an unknown callee.
//
// Unknown callees include isa.SubOpCallNative/isa.SubOpCallMethod with non-static
// receiver types, closures, and method dispatch through interface types. All are
// conservatively treated as mutating.
//
// Takes compiledFunction (*CompiledFunction) being classified.
//
// Returns true when compiledFunction is impure.
func functionHasHeapMutator(compiledFunction *program.CompiledFunction) bool {
	for _, inst := range compiledFunction.Body {
		if passes.InstructionDirectlyMutatesHeap(inst) {
			return true
		}
		if !passes.IsCallInstruction(inst) {
			continue
		}
		if passes.CallInvalidatesPurity(compiledFunction, inst) {
			return true
		}
	}
	return false
}

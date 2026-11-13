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

package inline

import (
	"context"
	"fmt"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// callWord says what one word of a body means to a walk of the calls a program names.
type callWord uint8

const (
	// callWordPlain is a word the walk has no interest in.
	callWordPlain callWord = iota

	// callWordFollowed is a call the walk follows into its callee.
	callWordFollowed

	// callWordOpaque is a word that settles the question for the whole function, such as a
	// call whose target is not known when the program is compiled.
	callWordOpaque
)

// classifyCallWord reads one word of a function for a namedCallGraph.
type classifyCallWord func(function *program.CompiledFunction, instruction isa.Instruction) (*program.CompiledFunction, callWord)

// namedCallGraph is the graph of the calls a walk follows, over every function that can
// be reached through them, with the functions an opaque word marks.
type namedCallGraph struct {
	// index gives each function its position in the slices below.
	index map[*program.CompiledFunction]int

	// callees lists, per function, the functions its followed calls name.
	callees [][]int

	// marked is true, per function, when one of its own words is opaque.
	marked []bool
}

// reachesMarked finds the functions from which a chain of followed calls arrives at a
// marked function, the marked ones included.
//
// Returns []bool which holds the answer for each function.
func (graph *namedCallGraph) reachesMarked() []bool {
	callers := graph.callers()
	reaches := make([]bool, len(graph.callees))
	pending := make([]int, 0, len(graph.callees))
	for node, marked := range graph.marked {
		if marked {
			reaches[node] = true
			pending = append(pending, node)
		}
	}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, caller := range callers[node] {
			if !reaches[caller] {
				reaches[caller] = true
				pending = append(pending, caller)
			}
		}
	}
	return reaches
}

// components numbers the graph's strongly connected components, so that two functions
// share a number exactly when each can reach the other.
//
// Returns []int which holds the component of each function.
func (graph *namedCallGraph) components() []int {
	callers := graph.callers()
	component := make([]int, len(graph.callees))
	for node := range component {
		component[node] = -1
	}
	next := 0
	for _, root := range slices.Backward(graph.finishOrder()) {
		if component[root] >= 0 {
			continue
		}
		component[root] = next
		pending := []int{root}
		for len(pending) > 0 {
			node := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			for _, caller := range callers[node] {
				if component[caller] < 0 {
					component[caller] = next
					pending = append(pending, caller)
				}
			}
		}
		next++
	}
	return component
}

// callers reverses the graph.
//
// Returns [][]int which lists, per function, the functions that call it.
func (graph *namedCallGraph) callers() [][]int {
	callers := make([][]int, len(graph.callees))
	for caller, callees := range graph.callees {
		for _, callee := range callees {
			callers[callee] = append(callers[callee], caller)
		}
	}
	return callers
}

// finishOrder lists the functions in the order a depth-first walk of the graph leaves
// them, each after everything it calls that the walk had not already left.
//
// Returns []int which holds every function once.
func (graph *namedCallGraph) finishOrder() []int {
	type frame struct {
		node int
		next int
	}
	order := make([]int, 0, len(graph.callees))
	entered := make([]bool, len(graph.callees))
	var stack []frame
	for start := range graph.callees {
		if entered[start] {
			continue
		}
		entered[start] = true
		stack = append(stack, frame{node: start, next: 0})
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next == len(graph.callees[top.node]) {
				order = append(order, top.node)
				stack = stack[:len(stack)-1]
				continue
			}
			callee := graph.callees[top.node][top.next]
			top.next++
			if !entered[callee] {
				entered[callee] = true
				stack = append(stack, frame{node: callee, next: 0})
			}
		}
	}
	return order
}

// callGraphReader builds a namedCallGraph.
type callGraphReader struct {
	// graph is the graph being built.
	graph *namedCallGraph

	// found lists the functions entered so far, by position.
	found []*program.CompiledFunction
}

// enter gives a function its position in the graph, the first time it is met.
//
// Takes function (*program.CompiledFunction) which is the function.
//
// Returns int which is the function's position.
func (reader *callGraphReader) enter(function *program.CompiledFunction) int {
	if position, known := reader.graph.index[function]; known {
		return position
	}
	position := len(reader.found)
	reader.graph.index[function] = position
	reader.found = append(reader.found, function)
	reader.graph.callees = append(reader.graph.callees, nil)
	reader.graph.marked = append(reader.graph.marked, false)
	return position
}

// read records the calls one function's body follows, entering each callee, and stops at
// the first opaque word, which marks the function.
//
// Takes position (int) which is the function's position.
// Takes classify (classifyCallWord) which says what each word means to the walk.
func (reader *callGraphReader) read(position int, classify classifyCallWord) {
	function := reader.found[position]
	for _, instruction := range function.Body {
		callee, word := classify(function, instruction)
		if word == callWordOpaque {
			reader.graph.marked[position] = true
			return
		}
		if word == callWordFollowed {
			reader.graph.callees[position] = append(reader.graph.callees[position], reader.enter(callee))
		}
	}
}

// newNamedCallGraph reads the bodies of functions, and of every function a followed call
// leads to, once each.
//
// Takes ctx (context.Context) which cancels the walk between functions.
// Takes functions ([]*program.CompiledFunction) which are where the walk starts. A nil
// entry is skipped.
// Takes classify (classifyCallWord) which says what each word means to the walk.
//
// Returns *namedCallGraph which holds the calls followed and the functions marked.
// Returns error when the context is cancelled.
func newNamedCallGraph(ctx context.Context, functions []*program.CompiledFunction, classify classifyCallWord) (*namedCallGraph, error) {
	reader := callGraphReader{
		graph: &namedCallGraph{index: make(map[*program.CompiledFunction]int, len(functions)), callees: nil, marked: nil},
		found: nil,
	}
	for _, function := range functions {
		if function != nil {
			reader.enter(function)
		}
	}
	for position := 0; position < len(reader.found); position++ {
		if position&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("reading the program's calls cancelled: %w", err)
			}
		}
		reader.read(position, classify)
	}
	return reader.graph, nil
}

// namesItsCallee reports whether an instruction is one of the calls that name their
// callee when the program is compiled.
//
// Takes instruction (isa.Instruction) which is the instruction.
//
// Returns bool which is true for a call, a scalar call and a tail call.
func namesItsCallee(instruction isa.Instruction) bool {
	return isa.InstrIsTier1SubOp(instruction, isa.SubOpCall) || isa.InstrIsTier1SubOp(instruction, isa.SubOpCallScalar) ||
		isa.InstrIsTier1SubOp(instruction, isa.SubOpTailCall)
}

// calleeNamedAt returns the callee of a call that names it.
//
// Takes function (*program.CompiledFunction) which holds the call.
// Takes instruction (isa.Instruction) which is the call.
//
// Returns *program.CallSite which is the call's site, or nil when its index is outside
// the function's sites or the site records no callee.
func calleeNamedAt(function *program.CompiledFunction, instruction isa.Instruction) *program.CallSite {
	siteIndex := int(instruction.WideIndex())
	if siteIndex >= len(function.CallSites) || function.CallSites[siteIndex].CachedCallee == nil {
		return nil
	}
	return &function.CallSites[siteIndex]
}

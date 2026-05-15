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

import (
	"fmt"
	"slices"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/policy"
)

// maxCallGraphRecursionDepth caps DFS recursion depth in Tarjan SCC, cycle detection, and
// call-depth analysis. Deeper chains are conservatively treated as recursive.
const maxCallGraphRecursionDepth = 4096

// tarjanState holds the mutable state for a single Tarjan SCC pass. Keeping it in a
// struct avoids the nested-closure shape that inflates cognitive complexity in
// classifyLocalEscape's caller.
type tarjanState struct {
	// adjacency is the call-graph adjacency list under analysis.
	adjacency [][]uint16

	// inSCC marks each node true when it belongs to a non-trivial SCC.
	inSCC []bool

	// indices stores each node's DFS discovery index, -1 when unvisited.
	indices []int

	// lowLinks stores each node's Tarjan low-link value.
	lowLinks []int

	// onStack marks each node true while it sits on the DFS stack.
	onStack []bool

	// stack holds the node indices currently in the active SCC frame.
	stack []int

	// index is the next DFS discovery counter to assign.
	index int
}

// newTarjanState initialises a fresh Tarjan SCC state for the given adjacency list. All
// node indices start as -1 (unvisited).
//
// Takes adjacency ([][]uint16) which is the call-graph edges to analyse.
//
// Returns the initialised tarjanState ready for strongconnect.
func newTarjanState(adjacency [][]uint16) *tarjanState {
	nodeCount := len(adjacency)
	state := &tarjanState{adjacency: adjacency,
		inSCC:    make([]bool, nodeCount),
		indices:  make([]int, nodeCount),
		lowLinks: make([]int, nodeCount),
		onStack:  make([]bool, nodeCount),
		stack:    make([]int, 0, nodeCount), index: 0}
	for i := range state.indices {
		state.indices[i] = -1
	}
	return state
}

// strongconnect performs a single Tarjan DFS step starting from node, updating low-link
// values and emitting any SCC rooted at it.
//
// Takes node (int) which is the DFS entry-point index into adjacency.
// Takes depth (int) which tracks the current DFS recursion depth.
func (state *tarjanState) strongconnect(node int, depth int) {
	state.indices[node] = state.index
	state.lowLinks[node] = state.index
	state.index++
	state.stack = append(state.stack, node)
	state.onStack[node] = true
	state.exploreNeighbours(node, depth)
	if state.lowLinks[node] == state.indices[node] {
		state.emitSCCRootedAt(node)
	}
}

// exploreNeighbours walks every neighbour of node, recursing into unvisited ones and
// lowering node's low-link via back-edges to nodes currently on the stack.
//
// Takes node (int) which is the DFS frame whose neighbours are walked.
// Takes depth (int) which tracks the current DFS recursion depth.
func (state *tarjanState) exploreNeighbours(node int, depth int) {
	nodeCount := len(state.adjacency)
	for _, adjacent := range state.adjacency[node] {
		neighbour := int(adjacent)
		if neighbour < 0 || neighbour >= nodeCount {
			continue
		}
		if state.indices[neighbour] == -1 {
			state.exploreUnvisited(node, neighbour, depth)
		} else if state.onStack[neighbour] {
			state.lowerLowLink(node, state.indices[neighbour])
		}
	}
}

// exploreUnvisited recurses into an unvisited neighbour and lowers node's low-link from
// the neighbour's, or marks the neighbour as cyclic when the recursion budget is spent so
// a pathological graph cannot overflow the stack.
//
// Takes node (int) which is the DFS frame being explored.
// Takes neighbour (int) which is the unvisited adjacent node.
// Takes depth (int) which tracks the current DFS recursion depth.
func (state *tarjanState) exploreUnvisited(node int, neighbour int, depth int) {
	if depth >= maxCallGraphRecursionDepth {
		state.inSCC[neighbour] = true
		return
	}
	state.strongconnect(neighbour, depth+1)
	state.lowerLowLink(node, state.lowLinks[neighbour])
}

// lowerLowLink lowers node's low-link to value when value is smaller.
//
// Takes node (int) which is the DFS frame whose low-link may drop.
// Takes value (int) which is the candidate low-link.
func (state *tarjanState) lowerLowLink(node int, value int) {
	if value < state.lowLinks[node] {
		state.lowLinks[node] = value
	}
}

// emitSCCRootedAt pops the stack down to node, marking each member as in-SCC iff the
// component is non-trivial (size > 1 or self-edge).
//
// Takes node (int) which is the SCC root index sitting on the DFS stack.
func (state *tarjanState) emitSCCRootedAt(node int) {
	startIndex := state.findStackIndex(node)
	memberCount := len(state.stack) - startIndex
	hasSelfLoop := memberCount == 1 && state.hasSelfEdge(node)
	if memberCount > 1 || hasSelfLoop {
		for stackIndex := startIndex; stackIndex < len(state.stack); stackIndex++ {
			state.inSCC[state.stack[stackIndex]] = true
		}
	}
	for stackIndex := startIndex; stackIndex < len(state.stack); stackIndex++ {
		state.onStack[state.stack[stackIndex]] = false
	}
	state.stack = state.stack[:startIndex]
}

// findStackIndex returns the stack index of node. The node is guaranteed to be on the
// stack because strongconnect only emits an SCC when its root sits at the bottom of an
// unpopped frame.
//
// Takes node (int) which is the SCC root being located.
//
// Returns the stack position of node, or len(state.stack) when absent.
func (state *tarjanState) findStackIndex(node int) int {
	for stackIndex, stackedNode := range slices.Backward(state.stack) {
		if stackedNode == node {
			return stackIndex
		}
	}
	return len(state.stack)
}

// hasSelfEdge reports whether node has any edge back to itself in the adjacency list.
// Used to classify size-1 SCCs as either trivial (no self-edge) or non-trivial (direct
// recursion).
//
// Takes node (int) which is the candidate for a self-loop edge.
//
// Returns true when adjacency[node] contains node itself.
func (state *tarjanState) hasSelfEdge(node int) bool {
	for _, adjacent := range state.adjacency[node] {
		if int(adjacent) == node {
			return true
		}
	}
	return false
}

// DetectRecursion walks the call graph of a compiled function set and returns an error if
// any cycle (direct or mutual recursion) is found.
//
// Takes root (*CompiledFunction) which is the root function containing all compiled
// functions.
//
// Returns error which wraps fault.ErrFeatureNotAllowed if a cycle is detected, or nil if
// the call graph is acyclic.
func DetectRecursion(root *CompiledFunction) error {
	functions := root.Functions
	if len(functions) == 0 {
		return nil
	}

	adjacency := BuildCallAdjacency(functions)

	if callGraphHasCycle(adjacency) {
		return fmt.Errorf("%w: %s", fault.ErrFeatureNotAllowed, policy.InterpFeatureRecursion)
	}

	return nil
}

// BuildCallAdjacency constructs an adjacency list for the call graph, excluding closure
// and method calls.
//
// Takes functions ([]*CompiledFunction) which are the compiled functions to analyse.
//
// Returns [][]uint16 where each entry lists the function indices called by that function.
func BuildCallAdjacency(functions []*CompiledFunction) [][]uint16 {
	adjacency := make([][]uint16, len(functions))
	for i := range functions {
		seen := make(map[uint16]bool)
		for j := range functions[i].CallSites {
			cs := &functions[i].CallSites[j]
			if cs.IsClosure || cs.IsMethod {
				continue
			}
			index := cs.FunctionIndex
			if int(index) < len(functions) && !seen[index] {
				adjacency[i] = append(adjacency[i], index)
				seen[index] = true
			}
		}
	}
	return adjacency
}

// FindCallGraphSCCs runs Tarjan's algorithm on the call-graph adjacency list and returns
// a per-node flag indicating membership in a non-trivial SCC.
//
// Takes adjacency ([][]uint16) which is the call graph adjacency list.
//
// Returns []bool where entry i is true when function i participates in direct or mutual
// recursion.
func FindCallGraphSCCs(adjacency [][]uint16) []bool {
	nodeCount := len(adjacency)
	if nodeCount == 0 {
		return nil
	}
	state := newTarjanState(adjacency)
	for i := range adjacency {
		if state.indices[i] == -1 {
			state.strongconnect(i, 0)
		}
	}
	return state.inSCC
}

// callGraphHasCycle performs a DFS-based cycle detection on a directed adjacency list.
//
// Takes adjacency ([][]uint16) which is the call graph adjacency list.
//
// Returns true if any cycle exists, false otherwise.
func callGraphHasCycle(adjacency [][]uint16) bool {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make([]int, len(adjacency))

	var dfs func(u int, depth int) bool
	dfs = func(u int, depth int) bool {
		if depth >= maxCallGraphRecursionDepth {
			return true
		}
		colour[u] = grey
		for _, v := range adjacency[u] {
			switch colour[v] {
			case grey:
				return true
			case white:
				if dfs(int(v), depth+1) {
					return true
				}
			}
		}
		colour[u] = black
		return false
	}

	for i := range adjacency {
		if colour[i] == white && dfs(i, 0) {
			return true
		}
	}

	return false
}

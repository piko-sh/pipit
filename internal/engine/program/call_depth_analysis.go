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

const (
	// recursiveSCCFanout charges this many frames per member of a strongly-connected
	// component in the static call graph. Sized so direct/mutual recursion in typical
	// workloads (parseExpression, fib, JSON parser) lands inside the pre-sized frame stack
	// rather than tripping GrowCallStack.
	recursiveSCCFanout = 8

	// recursiveSCCMinimumDepth is the fewest frames a recursive component is charged,
	// however small it is. A three-function parser SCC charged only
	// sccSize*recursiveSCCFanout frames climbs the frame stack through several doublings on
	// a cold run; this floor costs a few hundred kilobytes of pre-sized frames and removes
	// those first climbs.
	recursiveSCCMinimumDepth = 256
)

// EstimateMaxCallDepth returns the deepest static call chain rooted at any function in
// the file set, expressed in frames. The result feeds arena frame-stack pre-sizing.
//
// Takes root (*CompiledFunction) which carries the program's functions slice plus its own
// bytecode.
//
// Returns the deepest path length in frames, or 1 when the file set is empty.
func EstimateMaxCallDepth(root *CompiledFunction) int {
	if root == nil {
		return 1
	}

	if cached := root.cachedMaxCallDepth.Load(); cached > 0 {
		return int(cached - 1)
	}
	functions := root.Functions
	if len(functions) == 0 {
		root.cachedMaxCallDepth.Store(2)
		return 1
	}
	adjacency := BuildCallAdjacency(functions)
	inSCC := FindCallGraphSCCs(adjacency)
	sccSizes := computeSCCSizes(adjacency, inSCC)
	memo := make([]int, len(functions))
	for i := range memo {
		memo[i] = -1
	}
	maxDepth := 1
	for i := range functions {
		depth := depthFromNode(adjacency, inSCC, sccSizes, memo, i, 0)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	root.cachedMaxCallDepth.Store(int32(maxDepth + 1))
	return maxDepth
}

// depthFromNode returns the longest call chain reachable from node.
//
// Counts the node itself plus the deepest sub-chain. Results are memoised so each node is
// visited at most once. Inside an SCC the helper returns sccSize * recursiveSCCFanout
// without descending into peers, ensuring termination and a bounded charge for recursive
// call patterns.
//
// Takes adjacency ([][]uint16) which is the static call-graph adjacency list.
// Takes inSCC ([]bool) which marks SCC participants.
// Takes sccSizes ([]int) which provides per-node SCC size (0 when not in an SCC).
// Takes memo ([]int) which caches per-node results; -1 means not yet computed.
// Takes node (int) which is the current node index.
// Takes recursionDepth (int) which guards against stack overflow in pathological graphs.
//
// Returns the deepest chain length from node, in frames.
func depthFromNode(adjacency [][]uint16, inSCC []bool, sccSizes []int, memo []int, node int, recursionDepth int) int {
	if memo[node] >= 0 {
		return memo[node]
	}
	if inSCC[node] {
		depth := max(sccSizes[node]*recursiveSCCFanout, recursiveSCCMinimumDepth)
		memo[node] = depth
		return depth
	}
	if recursionDepth >= maxCallGraphRecursionDepth {
		memo[node] = 1
		return 1
	}
	best := 0
	for _, callee := range adjacency[node] {
		calleeIndex := int(callee)
		if calleeIndex < 0 || calleeIndex >= len(adjacency) {
			continue
		}
		childDepth := depthFromNode(adjacency, inSCC, sccSizes, memo, calleeIndex, recursionDepth+1)
		if childDepth > best {
			best = childDepth
		}
	}
	result := best + 1
	memo[node] = result
	return result
}

// computeSCCSizes assigns each SCC-member node its SCC size via a fresh DFS; non-SCC
// nodes receive 0.
//
// Takes adjacency ([][]uint16) which is the static call-graph adjacency list.
// Takes inSCC ([]bool) which marks SCC participants.
//
// Returns []int indexed identically to adjacency; entries for SCC-member nodes hold the
// size of their SCC and entries for non-SCC nodes hold 0.
func computeSCCSizes(adjacency [][]uint16, inSCC []bool) []int {
	sizes := make([]int, len(adjacency))
	visited := make([]bool, len(adjacency))
	for start := range adjacency {
		if visited[start] || !inSCC[start] {
			continue
		}
		members := collectSCCMembers(adjacency, inSCC, visited, start)
		for _, member := range members {
			sizes[member] = len(members)
		}
	}
	return sizes
}

// collectSCCMembers performs a DFS rooted at start that visits every SCC member reachable
// through edges where both endpoints are SCC-flagged. Returns the visited node indices.
//
// This is correct because FindCallGraphSCCs guarantees that every SCC-member node is
// reachable from any other member through edges among SCC participants (the SCC is, by
// definition, strongly connected via those edges). A pure forward DFS therefore
// enumerates the entire component without needing reverse-edge traversal.
//
// Takes adjacency ([][]uint16) which is the static call graph.
// Takes inSCC ([]bool) which restricts traversal to SCC members.
// Takes visited ([]bool) which records nodes already collected by an earlier SCC
// traversal; mutated as nodes are reached.
// Takes start (int) which is the entry node.
//
// Returns the slice of node indices belonging to the SCC.
func collectSCCMembers(adjacency [][]uint16, inSCC []bool, visited []bool, start int) []int {
	const initialMembersCapacity = 4
	members := make([]int, 0, initialMembersCapacity)
	stack := []int{start}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[node] || !inSCC[node] {
			continue
		}
		visited[node] = true
		members = append(members, node)
		for _, callee := range adjacency[node] {
			calleeIndex := int(callee)
			if calleeIndex < 0 || calleeIndex >= len(adjacency) {
				continue
			}
			if inSCC[calleeIndex] && !visited[calleeIndex] {
				stack = append(stack, calleeIndex)
			}
		}
	}
	return members
}

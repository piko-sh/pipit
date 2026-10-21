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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func callGraphOf(count int, edges [][2]int, marked ...int) []*program.CompiledFunction {
	functions := make([]*program.CompiledFunction, count)
	for index := range functions {
		functions[index] = &program.CompiledFunction{}
	}
	for _, edge := range edges {
		caller := functions[edge[0]]
		site := program.CallSite{}
		if edge[1] >= 0 {
			site.CachedCallee = functions[edge[1]]
		}
		caller.Body = append(caller.Body, isa.NewTier1Instruction(isa.SubOpCall, uint8(len(caller.CallSites)), 0))
		caller.CallSites = append(caller.CallSites, site)
	}
	for _, index := range marked {
		functions[index].Body = append(functions[index].Body, isa.NewInstruction(isa.OpDefer, 0, 0, 0))
	}
	return functions
}

func TestNamedCallGraphFindsEveryFunctionThatReachesAMarkedOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		edges  [][2]int
		marked []int
		want   []bool
		count  int
	}{
		{name: "nothing marked", count: 3, edges: [][2]int{{0, 1}, {1, 2}}, want: []bool{false, false, false}},
		{name: "the end of a chain", count: 3, edges: [][2]int{{0, 1}, {1, 2}}, marked: []int{2}, want: []bool{true, true, true}},
		{name: "the start of a chain", count: 3, edges: [][2]int{{0, 1}, {1, 2}}, marked: []int{0}, want: []bool{true, false, false}},
		{
			name: "one arm of a diamond", count: 4, edges: [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}}, marked: []int{1},
			want: []bool{true, true, false, false},
		},
		{
			name: "a cycle with a marked function hanging off it", count: 4, edges: [][2]int{{0, 1}, {1, 2}, {2, 0}, {2, 3}}, marked: []int{3},
			want: []bool{true, true, true, true},
		},
		{name: "a cycle that reaches nothing marked", count: 3, edges: [][2]int{{0, 1}, {1, 0}}, marked: []int{2}, want: []bool{false, false, true}},
		{name: "a call that names no callee", count: 2, edges: [][2]int{{0, 1}, {1, -1}}, want: []bool{true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			functions := callGraphOf(tt.count, tt.edges, tt.marked...)

			graph, err := newNamedCallGraph(t.Context(), functions, tailCallWord)
			require.NoError(t, err)

			reaches := graph.reachesMarked()
			for index, function := range functions {
				require.Equalf(t, tt.want[index], reaches[graph.index[function]], "function %d", index)
			}
		})
	}
}

func TestNamedCallGraphGroupsFunctionsThatCanReachEachOther(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		edges [][2]int
		same  [][2]int
		apart [][2]int
		count int
	}{
		{name: "a chain", count: 3, edges: [][2]int{{0, 1}, {1, 2}}, apart: [][2]int{{0, 1}, {1, 2}, {0, 2}}},
		{name: "a function that calls itself", count: 2, edges: [][2]int{{0, 0}, {0, 1}}, apart: [][2]int{{0, 1}}},
		{name: "two functions that call each other", count: 3, edges: [][2]int{{0, 1}, {1, 0}, {1, 2}}, same: [][2]int{{0, 1}}, apart: [][2]int{{0, 2}}},
		{
			name: "two cycles joined one way", count: 4, edges: [][2]int{{0, 1}, {1, 0}, {1, 2}, {2, 3}, {3, 2}},
			same: [][2]int{{0, 1}, {2, 3}}, apart: [][2]int{{0, 2}, {1, 3}},
		},
		{
			name: "a cycle entered from outside it", count: 4, edges: [][2]int{{3, 0}, {0, 1}, {1, 2}, {2, 0}},
			same: [][2]int{{0, 1}, {1, 2}}, apart: [][2]int{{3, 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			functions := callGraphOf(tt.count, tt.edges)
			graph, err := newNamedCallGraph(t.Context(), functions, tailCallWord)
			require.NoError(t, err)

			component := graph.components()

			of := func(index int) int { return component[graph.index[functions[index]]] }
			for _, pair := range tt.same {
				require.Equalf(t, of(pair[0]), of(pair[1]), "functions %d and %d reach each other", pair[0], pair[1])
			}
			for _, pair := range tt.apart {
				require.NotEqualf(t, of(pair[0]), of(pair[1]), "functions %d and %d do not both reach each other", pair[0], pair[1])
			}
		})
	}
}

func TestNamedCallGraphWalksAChainFarDeeperThanAnyCallStackShouldBe(t *testing.T) {
	t.Parallel()

	const length = 200_000
	edges := make([][2]int, 0, length)
	for index := range length - 1 {
		edges = append(edges, [2]int{index, index + 1})
	}
	functions := callGraphOf(length, edges, length-1)

	graph, err := newNamedCallGraph(t.Context(), functions[:1], tailCallWord)
	require.NoError(t, err)

	require.Len(t, graph.callees, length, "every function is found from the first")
	require.True(t, graph.reachesMarked()[graph.index[functions[0]]], "the mark at the far end is seen from the start")
	component := graph.components()
	require.NotEqual(t, component[graph.index[functions[0]]], component[graph.index[functions[length-1]]])
}

func TestNamedCallGraphSkipsNilFunctions(t *testing.T) {
	t.Parallel()

	functions := callGraphOf(2, [][2]int{{0, 1}})

	graph, err := newNamedCallGraph(t.Context(), []*program.CompiledFunction{nil, functions[0], nil}, tailCallWord)
	require.NoError(t, err)

	require.Len(t, graph.callees, 2)
}

func TestNamedCallGraphStopsReadingWhenItsContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	graph, err := newNamedCallGraph(ctx, []*program.CompiledFunction{program.NewNamedFunction("only")}, tailCallWord)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, graph)
}

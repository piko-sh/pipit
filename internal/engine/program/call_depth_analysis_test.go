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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecursiveSCCChargesMinimumDepth(t *testing.T) {
	t.Parallel()
	recursive := &CompiledFunction{Name: "rec", CallSites: []CallSite{{FunctionIndex: 0}}}
	root := &CompiledFunction{Name: "root", Functions: []*CompiledFunction{recursive}}
	require.GreaterOrEqual(t, EstimateMaxCallDepth(root), recursiveSCCMinimumDepth)

	leaf := &CompiledFunction{Name: "leaf"}
	middle := &CompiledFunction{Name: "middle", CallSites: []CallSite{{FunctionIndex: 1}}}
	chainRoot := &CompiledFunction{Name: "root", Functions: []*CompiledFunction{middle, leaf}}
	require.Less(t, EstimateMaxCallDepth(chainRoot), recursiveSCCMinimumDepth)
}

func TestRecordObservedCallDepthKeepsTheMaximum(t *testing.T) {
	t.Parallel()
	root := &CompiledFunction{Name: "root"}
	require.Zero(t, root.ObservedCallDepth())
	root.RecordObservedCallDepth(120)
	root.RecordObservedCallDepth(40)
	root.RecordObservedCallDepth(0)
	root.RecordObservedCallDepth(-3)
	require.Equal(t, 120, root.ObservedCallDepth())
	root.RecordObservedCallDepth(500)
	require.Equal(t, 500, root.ObservedCallDepth())

	var missing *CompiledFunction
	missing.RecordObservedCallDepth(9)
	require.Zero(t, missing.ObservedCallDepth())
}

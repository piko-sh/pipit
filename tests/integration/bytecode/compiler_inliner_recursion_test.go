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

//go:build integration

package bytecode_test

import (
	"testing"

	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestCanInlineSelfRecursiveRefusesAlreadyUnrolled(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{mk(isa.OpAddInt, 0, 0, 0)},
	}
	site := &program.CallSite{RecursionUnrolled: true}
	require.Equal(t, program.InlineRefusalAlreadyUnrolled,
		inline.CanInlineSelfRecursive(site, callee, false))
}

func TestCanInlineSelfRecursiveRefusesInLoop(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{mk(isa.OpAddInt, 0, 0, 0)},
	}
	site := &program.CallSite{}
	require.Equal(t, program.InlineRefusalSelfInLoop,
		inline.CanInlineSelfRecursive(site, callee, true))
}

func TestCanInlineSelfRecursiveRefusesHairyCallee(t *testing.T) {
	t.Parallel()
	body := make([]isa.Instruction, inline.SelfUnrollBudget+5)
	for i := range body {
		body[i] = mk(isa.OpAddInt, 0, 0, 0)
	}
	callee := &program.CompiledFunction{Body: body}
	site := &program.CallSite{}
	require.Equal(t, program.InlineRefusalSelfHairy,
		inline.CanInlineSelfRecursive(site, callee, false))
}

func TestCanInlineSelfRecursiveRefusesTailCallInCallee(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAddInt, 0, 0, 0),
			isa.NewTier1Instruction(isa.SubOpTailCall, 0, 0),
		},
	}
	site := &program.CallSite{}
	got := inline.CanInlineSelfRecursive(site, callee, false)
	require.Equal(t, program.InlineRefusalTailCall, got,
		"callee with opTailCall is refused by the shared scanner")
}

func TestCanInlineSelfRecursiveAcceptsEligibleCallee(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAddInt, 0, 0, 0),
			mk(isa.OpMulInt, 1, 0, 0),
		},
	}
	site := &program.CallSite{}
	require.Equal(t, program.InlineEligible,
		inline.CanInlineSelfRecursive(site, callee, false))
}

func TestRecursionUnrolledFlagPersistsOnCallSite(t *testing.T) {
	t.Parallel()
	site := program.CallSite{}
	require.False(t, site.RecursionUnrolled, "default flag is false")
	site.RecursionUnrolled = true
	require.True(t, site.RecursionUnrolled, "flag stores through copies")
}

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
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func tailCaller(callee *program.CompiledFunction, results ...isa.RegisterKind) *program.CompiledFunction {
	return &program.CompiledFunction{
		Name: "caller",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt64, 1, 0, 0),
			isa.NewTier1Instruction(isa.SubOpTailCall, 0, 0),
		},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 2, isa.RegisterString: 1},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt64},
		ResultKinds:    results,
		CallSites: []program.CallSite{{
			CachedCallee: callee,
			Arguments:    []program.VarLocation{intSlot(1)},
		}},
		EmittedInlineBlocker: program.InlineRefusalTailCall,
		CachedInlineRefusal:  program.InlineRefusalTailCall,
	}
}

func TestATailCallThatCannotComeBackBecomesACallAndAReturn(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	caller := tailCaller(callee, isa.RegisterInt64)

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee}))

	require.Len(t, caller.Body, 4)
	target, jumps := program.JumpTargetAt(caller.Body, 1)
	require.True(t, jumps, "the tail call's word now jumps to the appended call")
	require.Equal(t, 2, target)
	require.True(t, isa.InstrIsTier1SubOp(caller.Body[2], isa.SubOpCallScalar), "a callee that deals only in scalars takes the lean call")
	require.Equal(t, uint16(0), caller.Body[2].WideIndex(), "the call keeps the tail call's site")
	require.Equal(t, isa.NewTier2Instruction(isa.SubOpTier2Return, 1), caller.Body[3])
	require.Equal(t, []program.VarLocation{intSlot(0)}, caller.CallSites[0].Returns,
		"the callee's result lands in the caller's own result slot, where the return reads it")
	require.Equal(t, program.InlineRefusalUnknown, caller.EmittedInlineBlocker, "the function no longer makes a tail call")
	require.Equal(t, program.InlineRefusalUnknown, caller.CachedInlineRefusal)
}

func TestADemotedTailCallDeliversEachResultToItsOwnSlot(t *testing.T) {
	t.Parallel()

	results := []isa.RegisterKind{isa.RegisterInt64, isa.RegisterString, isa.RegisterInt64}
	callee := doublingCallee()
	callee.ResultKinds = results
	caller := tailCaller(callee, results...)

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee}))

	require.Equal(t, []program.VarLocation{
		intSlot(0),
		{Kind: isa.RegisterString, Register: 0},
		intSlot(1),
	}, caller.CallSites[0].Returns, "results are numbered per bank, in the order they are declared")
	require.Equal(t, isa.NewTier2Instruction(isa.SubOpTier2Return, 3), caller.Body[3])
}

func TestADemotedTailCallReservesTheCallersResultSlots(t *testing.T) {
	t.Parallel()

	results := []isa.RegisterKind{isa.RegisterInt64, isa.RegisterString, isa.RegisterInt64}
	callee := doublingCallee()
	callee.ResultKinds = results
	caller := tailCaller(callee, results...)
	caller.NumRegisters = [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1}
	caller.PrecomputedAllocCountsValid = true

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee}))

	require.Equal(t, uint32(2), caller.NumRegisters[isa.RegisterInt64],
		"a function that only tail-called needed no register for a result; now the call writes two there")
	require.Equal(t, uint32(1), caller.NumRegisters[isa.RegisterString])
	require.Equal(t, int(caller.NumRegisters[isa.RegisterInt64]), caller.PrecomputedAllocCounts.Ints,
		"the frame's allocation counts are worked out again for its new size")

	roomy := tailCaller(callee, results...)
	roomy.NumRegisters = [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 9, isa.RegisterString: 4}
	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{roomy, callee}))
	require.Equal(t, uint32(9), roomy.NumRegisters[isa.RegisterInt64], "a bank that is already large enough is left alone")
}

func TestADemotedTailCallOfAFunctionWithoutResultsReturnsNothing(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	callee.ResultKinds = nil
	caller := tailCaller(callee)

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee}))

	require.Equal(t, isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid), caller.Body[3])
	require.Empty(t, caller.CallSites[0].Returns)
}

func TestATailCallIsKeptWhereverItMaySaveStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		arrange func(caller, callee *program.CompiledFunction)
	}{
		{name: "the callee calls the caller back", arrange: func(caller, callee *program.CompiledFunction) {
			callee.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, callee.Body...)
			callee.CallSites = []program.CallSite{{CachedCallee: caller}}
		}},
		{name: "the callee reaches the caller through another function", arrange: func(caller, callee *program.CompiledFunction) {
			middle := doublingCallee()
			middle.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallScalar, 0, 0)}, middle.Body...)
			middle.CallSites = []program.CallSite{{CachedCallee: caller}}
			callee.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, callee.Body...)
			callee.CallSites = []program.CallSite{{CachedCallee: middle}}
		}},
		{name: "the callee calls a function value, which may be anything", arrange: func(_, callee *program.CompiledFunction) {
			callee.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, callee.Body...)
			callee.CallSites = []program.CallSite{{IsClosure: true, CachedCallee: doublingCallee()}}
		}},
		{name: "the callee calls a method chosen at run time", arrange: func(_, callee *program.CompiledFunction) {
			callee.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 0)}, callee.Body...)
			callee.CallSites = []program.CallSite{{IsMethod: true}}
		}},
		{name: "the callee makes a native call, which may call a function back", arrange: func(_, callee *program.CompiledFunction) {
			callee.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0)}, callee.Body...)
			callee.CallSites = []program.CallSite{{IsNative: true}}
		}},
		{name: "the callee defers a call", arrange: func(_, callee *program.CompiledFunction) {
			callee.Body = append([]isa.Instruction{isa.NewInstruction(isa.OpDefer, 0, 0, 0)}, callee.Body...)
		}},
		{name: "the function calls itself", arrange: func(caller, _ *program.CompiledFunction) {
			caller.CallSites[0].CachedCallee = caller
		}},
		{name: "the site reuses the frame in place", arrange: func(caller, _ *program.CompiledFunction) {
			caller.CallSites[0].TailReuseFrameInPlace = true
		}},
		{name: "the callee is not known", arrange: func(caller, _ *program.CompiledFunction) {
			caller.CallSites[0].CachedCallee = nil
		}},
		{name: "the caller has named results, which are not plain result slots", arrange: func(caller, _ *program.CompiledFunction) {
			caller.NamedResultLocations = []program.VarLocation{intSlot(1)}
		}},
		{name: "the two functions return different numbers of results", arrange: func(_, callee *program.CompiledFunction) {
			callee.ResultKinds = nil
		}},
		{name: "the caller returns more results than a return can count", arrange: func(caller, callee *program.CompiledFunction) {
			caller.ResultKinds = make([]isa.RegisterKind, math.MaxUint8+1)
			callee.ResultKinds = caller.ResultKinds
		}},
		{name: "the tail call is through a method value and the callee calls the caller by name", arrange: func(caller, callee *program.CompiledFunction) {
			caller.CallSites[0].IsMethod = true
			callee.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallScalar, 0, 0)}, callee.Body...)
			callee.CallSites = []program.CallSite{{CachedCallee: caller}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := doublingCallee()
			caller := tailCaller(callee, isa.RegisterInt64)
			tt.arrange(caller, callee)
			before := slices.Clone(caller.Body)

			require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee}))

			require.Equal(t, before, caller.Body)
			require.Equal(t, program.InlineRefusalTailCall, caller.EmittedInlineBlocker)
		})
	}
}

func TestATailCallIsDemotedWhenTheCalleeOnlyCallsFunctionsThatCannotComeBack(t *testing.T) {
	t.Parallel()

	leaf := doublingCallee()
	callee := doublingCallee()
	callee.Body = append([]isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		isa.NewTier1Instruction(isa.SubOpCallScalar, 1, 0),
	}, callee.Body...)
	callee.CallSites = []program.CallSite{{CachedCallee: leaf}, {CachedCallee: callee}}
	caller := tailCaller(callee, isa.RegisterInt64)

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee, leaf}))

	require.Len(t, caller.Body, 4, "the callee calls a leaf and itself, neither of which can arrive back at the caller")
}

func TestADemotedTailCallKeepsTheSourceMapInStep(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	caller := tailCaller(callee, isa.RegisterInt64)
	caller.DebugSourceMap = &program.SourceMap{Positions: []program.SourcePosition{{Line: 3}, {Line: 4}}}

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), []*program.CompiledFunction{caller, callee}))

	require.Len(t, caller.DebugSourceMap.Positions, len(caller.Body))
	require.Equal(t, int32(4), int32(caller.DebugSourceMap.Positions[2].Line), "the appended words carry the tail call's line")
	require.Equal(t, int32(4), int32(caller.DebugSourceMap.Positions[3].Line))
}

func TestEveryTailCallOfALongChainIsDemoted(t *testing.T) {
	t.Parallel()

	const length = 4000
	chain := make([]*program.CompiledFunction, length)
	chain[length-1] = doublingCallee()
	for index := length - 2; index >= 0; index-- {
		chain[index] = tailCaller(chain[index+1], isa.RegisterInt64)
	}

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), chain))

	for index, function := range chain[:length-1] {
		require.Lenf(t, function.Body, 4, "function %d tail-calls the next, which never comes back", index)
	}
}

func TestNoTailCallOfALongCycleIsDemoted(t *testing.T) {
	t.Parallel()

	const length = 4000
	ring := make([]*program.CompiledFunction, length)
	for index := range ring {
		ring[index] = tailCaller(nil, isa.RegisterInt64)
	}
	for index, function := range ring {
		function.CallSites[0].CachedCallee = ring[(index+1)%length]
	}

	require.NoError(t, demoteTailCallsThatSaveNoStack(t.Context(), ring))

	for index, function := range ring {
		require.Lenf(t, function.Body, 2, "function %d is on a cycle of tail calls, where the frame is what keeps the stack flat", index)
	}
}

func TestTailCallDemotionStopsWhenItsContextIsCancelled(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	caller := tailCaller(callee, isa.RegisterInt64)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := demoteTailCallsThatSaveNoStack(ctx, []*program.CompiledFunction{caller, callee})

	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, caller.Body, 2, "nothing is rewritten once the work is cancelled")
}

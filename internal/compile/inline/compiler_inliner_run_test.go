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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func intSlot(register uint8) program.VarLocation {
	return program.VarLocation{Kind: isa.RegisterInt64, Register: register}
}

func doublingCallee() *program.CompiledFunction {
	return &program.CompiledFunction{
		Name: "double",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt64, 0, 1, 1),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 2},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterInt64},
		ParameterRegisters: []uint8{1},
		ResultKinds:        []isa.RegisterKind{isa.RegisterInt64},
	}
}

func callerCalling(callee *program.CompiledFunction, site program.CallSite) *program.CompiledFunction {
	site.CachedCallee = callee
	return &program.CompiledFunction{
		Name: "root",
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 4},
		ResultKinds:  []isa.RegisterKind{isa.RegisterInt64},
		CallSites:    []program.CallSite{site},
		Functions:    []*program.CompiledFunction{callee},
	}
}

func callsRemaining(compiledFunction *program.CompiledFunction) int {
	count := 0
	for _, instruction := range compiledFunction.Body {
		if isa.InstrIsTier1SubOp(instruction, isa.SubOpCall) {
			count++
		}
	}
	return count
}

func TestInliningSplicesASmallCalleeIntoItsCaller(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	caller := callerCalling(callee, program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(0)},
	})

	require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))

	require.Zero(t, callsRemaining(caller),
		"the call word is what the splice replaces, so a spliced site leaves none behind")
	require.True(t, slices.ContainsFunc(caller.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpAddInt64 }),
		"the callee's own instructions have to end up in the caller's body")
}

func TestInliningRefusesACalleeItCannotSpliceSafely(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		callee func() *program.CompiledFunction
	}{
		{
			name: "a callee with no body",
			callee: func() *program.CompiledFunction {
				callee := doublingCallee()
				callee.Body = nil
				return callee
			},
		},
		{
			name: "a callee that captures upvalues",
			callee: func() *program.CompiledFunction {
				callee := doublingCallee()
				callee.UpvalueDescriptors = []program.UpvalueDescriptor{{}}
				return callee
			},
		},
		{
			name: "a callee that recovers from panics",
			callee: func() *program.CompiledFunction {
				callee := doublingCallee()
				callee.HasRecover = true
				return callee
			},
		},
		{
			name: "a callee that defers",
			callee: func() *program.CompiledFunction {
				callee := doublingCallee()
				callee.Body = append([]isa.Instruction{isa.NewInstruction(isa.OpDefer, 0, 0, 0)}, callee.Body...)
				return callee
			},
		},
		{

			name: "a callee using an instruction the splice driver does not reproduce",
			callee: func() *program.CompiledFunction {
				callee := doublingCallee()
				callee.Body = append([]isa.Instruction{isa.NewInstruction(isa.OpAllocateIndirect, 1, 0, 0)}, callee.Body...)
				return callee
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			caller := callerCalling(tt.callee(), program.CallSite{
				Arguments: []program.VarLocation{intSlot(2)},
				Returns:   []program.VarLocation{intSlot(0)},
			})
			before := len(caller.Body)

			require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))

			require.Equal(t, 1, callsRemaining(caller),
				"a callee the splice cannot reproduce keeps its real call")
			require.Len(t, caller.Body, before,
				"a refused site must leave the caller's body exactly as it was")
		})
	}
}

func TestInliningRefusesACallSiteShapeItCannotHandle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site program.CallSite
	}{
		{name: "a call through a host function", site: program.CallSite{IsNative: true}},
		{name: "a call through a closure value", site: program.CallSite{IsClosure: true}},
		{name: "a call through a method", site: program.CallSite{IsMethod: true}},
		{name: "a call spreading a slice into a variadic", site: program.CallSite{IsEllipsisSpread: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			site := tt.site
			site.Arguments = []program.VarLocation{intSlot(2)}
			site.Returns = []program.VarLocation{intSlot(0)}
			caller := callerCalling(doublingCallee(), site)

			require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))

			require.Equal(t, 1, callsRemaining(caller),
				"the callee is only known at run time for these shapes, so there is nothing to splice")
		})
	}
}

func TestInliningACallSiteWithNoResolvedCalleeIsARefusal(t *testing.T) {
	t.Parallel()

	caller := callerCalling(doublingCallee(), program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(0)},
	})
	caller.CallSites[0].CachedCallee = nil

	require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))

	require.Equal(t, 1, callsRemaining(caller),
		"a site whose callee was never resolved has no body to copy in")
}

func TestInliningAFunctionWithNoCallSitesChangesNothing(t *testing.T) {
	t.Parallel()

	root := &program.CompiledFunction{
		Name: "root",
		Body: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2Return, 0)},
	}
	before := append([]isa.Instruction(nil), root.Body...)

	require.NoError(t, RunBytecodeInliner(t.Context(), root, passes.DefaultOptions()))

	require.Equal(t, before, root.Body)
}

func TestInliningNothingAtAllIsHarmless(t *testing.T) {
	t.Parallel()

	require.NoError(t, RunBytecodeInliner(t.Context(), nil, passes.DefaultOptions()),
		"a compilation that produced no root has nothing to inline, and refusing it would fail a valid build")
}

func TestInliningStopsWhenTheCallerCancels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	caller := callerCalling(doublingCallee(), program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(0)},
	})

	require.Error(t, RunBytecodeInliner(ctx, caller, passes.DefaultOptions()),
		"inlining runs inside the caller's compile, so a cancelled compile must not keep rewriting bodies")
}

func TestCollectingReachableFunctionsWalksTheWholeNestingTree(t *testing.T) {
	t.Parallel()

	grandchild := &program.CompiledFunction{Name: "grandchild"}
	child := &program.CompiledFunction{Name: "child", Functions: []*program.CompiledFunction{grandchild}}
	root := &program.CompiledFunction{Name: "root", Functions: []*program.CompiledFunction{child}}

	reachable := CollectReachableFunctions(root)

	require.Len(t, reachable, 3,
		"every nested function is compiled with its parent, so every one is a candidate caller")
	require.Contains(t, reachable, root)
	require.Contains(t, reachable, child)
	require.Contains(t, reachable, grandchild)
}

func TestCollectingReachableFunctionsVisitsEachFunctionOnce(t *testing.T) {
	t.Parallel()

	shared := &program.CompiledFunction{Name: "shared"}
	root := &program.CompiledFunction{Name: "root", Functions: []*program.CompiledFunction{shared, shared}}

	require.Len(t, CollectReachableFunctions(root), 2,
		"a function reached twice is still one function, and visiting it twice would splice into it twice")
}

func TestInliningSplicesAScalarCallOutsideADebugBuild(t *testing.T) {
	t.Parallel()

	scalarCaller := func(debugging bool) *program.CompiledFunction {
		caller := callerCalling(doublingCallee(), program.CallSite{
			Arguments: []program.VarLocation{intSlot(2)},
			Returns:   []program.VarLocation{intSlot(0)},
		})
		caller.Body[0] = isa.NewTier1Instruction(isa.SubOpCallScalar, 0, 0)
		if debugging {
			caller.DebugVarTable = &program.DebugVarTable{Entries: nil}
		}
		return caller
	}
	scalarCalls := func(compiledFunction *program.CompiledFunction) int {
		count := 0
		for _, instruction := range compiledFunction.Body {
			if isa.InstrIsTier1SubOp(instruction, isa.SubOpCallScalar) {
				count++
			}
		}
		return count
	}

	inlined := scalarCaller(false)
	require.NoError(t, RunBytecodeInliner(t.Context(), inlined, passes.DefaultOptions()))
	require.Zero(t, scalarCalls(inlined), "a scalar call is spliced as an ordinary one is")

	debugged := scalarCaller(true)
	require.NoError(t, RunBytecodeInliner(t.Context(), debugged, passes.DefaultOptions()))
	require.Equal(t, 1, scalarCalls(debugged), "a debug build keeps the scalar call's frame")
}

func TestInliningKeepsAScalarCallWhoseCalleeLoops(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	callee.Body = []isa.Instruction{
		isa.NewInstruction(isa.OpAddInt64, 0, 1, 1),
		isa.NewTier1Instruction(isa.SubOpJump, 0, 0),
		isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
	}
	require.True(t, program.SetJumpTarget(callee.Body, 1, 0))
	caller := callerCalling(callee, program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(0)},
	})
	caller.Body[0] = isa.NewTier1Instruction(isa.SubOpCallScalar, 0, 0)

	require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))
	require.True(t, isa.InstrIsTier1SubOp(caller.Body[0], isa.SubOpCallScalar), "the call costs little next to the loop it would carry")
}

func TestInliningSplicesCalleesThatConvertBetweenBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
		want []isa.SubOpcode
	}{
		{name: "a negation", want: []isa.SubOpcode{isa.SubOpNotInt64}, body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpNotInt64, 0, 1),
		}},
		{name: "an integer made a bool and back", want: []isa.SubOpcode{isa.SubOpConvertInt64ToBool, isa.SubOpConvertBoolToInt64}, body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpConvertInt64ToBool, 0, 1),
			isa.NewTier1Instruction(isa.SubOpConvertBoolToInt64, 0, 0),
		}},
		{name: "an integer made unsigned and back", want: []isa.SubOpcode{isa.SubOpConvertInt64ToUint64, isa.SubOpConvertUint64ToInt64}, body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpConvertInt64ToUint64, 0, 1),
			isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, 0, 0),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := doublingCallee()
			callee.Body = append(slices.Clone(tt.body), isa.NewTier2Instruction(isa.SubOpTier2Return, 1))
			callee.NumRegisters[isa.RegisterBool], callee.NumRegisters[isa.RegisterUint64] = 1, 1
			caller := callerCalling(callee, program.CallSite{
				Arguments: []program.VarLocation{intSlot(2)},
				Returns:   []program.VarLocation{intSlot(0)},
			})
			caller.NumRegisters[isa.RegisterBool], caller.NumRegisters[isa.RegisterUint64] = 3, 3

			require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))

			require.Zero(t, callsRemaining(caller), "a callee holding only conversions between banks is spliced")
			for _, subOpcode := range tt.want {
				spliced := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool { return isa.InstrIsTier1SubOp(instruction, subOpcode) })
				require.GreaterOrEqual(t, spliced, 0, "the conversion is carried into the caller")
				word := caller.Body[spliced]
				if subOpcode == isa.SubOpNotInt64 || subOpcode == isa.SubOpConvertInt64ToBool || subOpcode == isa.SubOpConvertInt64ToUint64 {
					require.Equal(t, uint8(2), word.C, "the callee's parameter is read from the caller's argument register")
				}
				if subOpcode == isa.SubOpConvertInt64ToBool || subOpcode == isa.SubOpConvertInt64ToUint64 {
					require.GreaterOrEqual(t, word.B, uint8(3), "the callee's temporary in the other bank takes a fresh caller register")
				}
				if subOpcode == isa.SubOpConvertBoolToInt64 || subOpcode == isa.SubOpConvertUint64ToInt64 {
					require.GreaterOrEqual(t, word.C, uint8(3), "the callee's temporary in the other bank takes a fresh caller register")
				}
			}
		})
	}
}

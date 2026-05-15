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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestScanCalleeForRefusal_EmptyBodyRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: nil,
	}
	require.Equalf(t, program.InlineRefusalNoBody, scanCalleeForRefusal(compiledFunction), "empty body must refuse with noBody")
}

func TestScanCalleeForRefusal_UpvaluesRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body:               []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		UpvalueDescriptors: []program.UpvalueDescriptor{{}},
	}
	require.Equalf(t, program.InlineRefusalUpvalues, scanCalleeForRefusal(compiledFunction), "upvalues must refuse")
}

func TestScanCalleeForRefusal_DeferRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
			isa.NewInstruction(isa.OpDefer, 0, 0, 0),
		},
	}
	require.Equalf(t, program.InlineRefusalDefer, scanCalleeForRefusal(compiledFunction), "opDefer must refuse")
}

func TestScanCalleeForRefusal_GoRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{isa.NewInstruction(isa.OpGo, 0, 0, 0)},
	}
	require.Equalf(t, program.InlineRefusalGo, scanCalleeForRefusal(compiledFunction), "opGo must refuse")
}

func TestScanCalleeForRefusal_MethodCallRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 0)},
	}
	require.Equalf(t, program.InlineRefusalMethodCall, scanCalleeForRefusal(compiledFunction), "opCallMethod must refuse")
}

func TestScanCalleeForRefusal_NativeCallRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0)},
	}
	require.Equalf(t, program.InlineRefusalNativeCall, scanCalleeForRefusal(compiledFunction), "opCallNative must refuse")
}

func TestScanCalleeForRefusal_TailCallRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpTailCall, 0, 0)},
	}
	require.Equalf(t, program.InlineRefusalTailCall, scanCalleeForRefusal(compiledFunction), "opTailCall must refuse")
}

func TestScanCalleeForRefusal_ClosureOpsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		instr isa.Instruction
	}{
		{name: "makeClosure", instr: isa.NewInstruction(isa.OpMakeClosure, 0, 0, 0)},
		{name: "getUpvalue", instr: isa.NewInstruction(isa.OpGetUpvalue, 0, 0, 0)},
		{name: "setUpvalue", instr: isa.NewInstruction(isa.OpSetUpvalue, 0, 0, 0)},
		{name: "syncClosureUpvalues", instr: isa.NewTier2Instruction(isa.SubOpTier2SyncClosureUpvalues, 0)},
		{name: "syncIIFEUpvalues", instr: isa.NewTier3Instruction(isa.SubOpTier3SyncIIFEUpvalues)},
		{name: "callIIFE", instr: isa.NewTier1Instruction(isa.SubOpCallIIFE, 0, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body: []isa.Instruction{tc.instr},
			}
			require.Equalf(t, program.InlineRefusalClosureOps, scanCalleeForRefusal(compiledFunction), "%s must refuse with closureOps", tc.name)
		})
	}
}

func TestScanCalleeForRefusal_ChannelOpsRefused(t *testing.T) {
	t.Parallel()
	cases := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpSelect, 0, 0),
		isa.NewInstruction(isa.OpChannelSend, 0, 0, 0),
	}
	for _, instr := range cases {
		t.Run(isa.InstructionDisplayName(instr), func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body: []isa.Instruction{instr},
			}
			require.Equalf(t, program.InlineRefusalChannelOps, scanCalleeForRefusal(compiledFunction), "%v must refuse with channelOps", isa.InstructionDisplayName(instr))
		})
	}
}

func TestScanCalleeForRefusal_VariadicAcceptedAtCalleeLevel(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body:       []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		IsVariadic: true,
	}
	require.Equalf(t, program.InlineEligible, scanCalleeForRefusal(compiledFunction), "variadic alone must be eligible at callee level")
}

func TestScanCalleeForRefusal_GenericPlaceholderRefused(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body:                 []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		IsGenericFunction:    true,
		SpecialisationOrigin: nil,
	}
	require.Equalf(t, program.InlineRefusalGenericPlaceholder, scanCalleeForRefusal(compiledFunction), "generic placeholder must refuse")
}

func TestScanCalleeForRefusal_GenericSpecialisationEligible(t *testing.T) {
	t.Parallel()
	origin := &program.CompiledFunction{IsGenericFunction: true}
	compiledFunction := &program.CompiledFunction{
		Body:                 []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		IsGenericFunction:    true,
		SpecialisationOrigin: origin,
	}
	require.Equalf(t, program.InlineEligible, scanCalleeForRefusal(compiledFunction), "generic specialisation must be eligible")
}

func TestScanCalleeForRefusalAcceptsSmallBody(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt, 0, 1, 2),
			isa.NewInstruction(isa.OpAddInt, 1, 2, 3),
		},
	}
	require.Equalf(t, program.InlineEligible, scanCalleeForRefusal(compiledFunction), "simple body must be eligible")
}

func TestCalleeInlineRefusal_Caches(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{isa.NewInstruction(isa.OpDefer, 0, 0, 0)},
	}
	require.Equalf(t, program.InlineRefusalUnknown, compiledFunction.CachedInlineRefusal, "pre-probe cache must be Unknown")
	first := calleeInlineRefusal(compiledFunction)
	require.Equalf(t, program.InlineRefusalDefer, first, "first probe must return defer")
	require.Equalf(t, program.InlineRefusalDefer, compiledFunction.CachedInlineRefusal, "cache must be populated after first probe")

	compiledFunction.Body = []isa.Instruction{isa.NewInstruction(isa.OpAddInt, 0, 0, 0)}
	second := calleeInlineRefusal(compiledFunction)
	require.Equalf(t, program.InlineRefusalDefer, second, "cached probe must remain Defer despite body change")
}

func TestCanInline_RefusesNativeSite(t *testing.T) {
	t.Parallel()
	site := &program.CallSite{IsNative: true}
	require.Equalf(t, program.InlineRefusalSiteIndirect, canInline(nil, site, 0, false), "native site must refuse")
}

func TestCanInline_RefusesClosureSite(t *testing.T) {
	t.Parallel()
	site := &program.CallSite{IsClosure: true}
	require.Equalf(t, program.InlineRefusalSiteIndirect, canInline(nil, site, 0, false), "closure site must refuse")
}

func TestCanInline_RefusesMethodSite(t *testing.T) {
	t.Parallel()
	site := &program.CallSite{IsMethod: true}
	require.Equalf(t, program.InlineRefusalSiteIndirect, canInline(nil, site, 0, false), "method site must refuse")
}

func TestCanInline_RefusesNilSite(t *testing.T) {
	t.Parallel()
	require.Equalf(t, program.InlineRefusalUnknown, canInline(nil, nil, 0, false), "nil site must refuse with Unknown")
}

func TestCanInline_RefusesNoCachedCallee(t *testing.T) {
	t.Parallel()
	site := &program.CallSite{CachedCallee: nil}
	require.Equalf(t, program.InlineRefusalNoBody, canInline(nil, site, 0, false), "missing cachedCallee must refuse with NoBody")
}

func TestCanInline_RespectsBudget(t *testing.T) {
	t.Parallel()

	body := make([]isa.Instruction, defaultInlineBudget+1)
	for i := range body {
		body[i] = isa.NewInstruction(isa.OpAddInt, 0, 0, 0)
	}
	callee := &program.CompiledFunction{Body: body}
	caller := &program.CompiledFunction{Body: []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)}}
	site := &program.CallSite{CachedCallee: callee}
	require.Equalf(t, program.InlineRefusalOversize, canInline(caller, site, 0, false), "oversize callee must refuse")
	require.NotEqualf(t, program.InlineRefusalOversize, canInline(caller, site, 0, true), "loop-site budget should allow %d-instr callee", len(body))
}

func TestCalleeHairyness_OpcodeWeights(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body []isa.Instruction
		want int
	}{
		{
			name: "ten_simple_ops",
			body: func() []isa.Instruction {
				b := make([]isa.Instruction, 10)
				for i := range b {
					b[i] = isa.NewInstruction(isa.OpAddInt, 0, 0, 0)
				}
				return b
			}(),
			want: 10,
		},
		{
			name: "one_make_slice",
			body: []isa.Instruction{isa.NewInstruction(isa.OpMakeSlice, 0, 0, 0)},
			want: 2,
		},
		{
			name: "two_map_index",
			body: []isa.Instruction{
				isa.NewInstruction(isa.OpMapIndex, 0, 0, 0),
				isa.NewInstruction(isa.OpMapIndex, 0, 0, 0),
			},
			want: 2,
		},
		{
			name: "five_nops",
			body: []isa.Instruction{
				isa.NewInstruction(isa.OpNop, 0, 0, 0),
				isa.NewInstruction(isa.OpNop, 0, 0, 0),
				isa.NewInstruction(isa.OpNop, 0, 0, 0),
				isa.NewInstruction(isa.OpNop, 0, 0, 0),
				isa.NewInstruction(isa.OpNop, 0, 0, 0),
			},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tc.body}
			require.Equalf(t, tc.want, calleeHairyness(compiledFunction), "hairyness mismatch")
		})
	}
}

func TestCanInline_HappyPath(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt, 0, 1, 2),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt: 3},
	}
	caller := &program.CompiledFunction{
		Body:         []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 5},
	}
	site := &program.CallSite{CachedCallee: callee}
	require.Equalf(t, program.InlineEligible, canInline(caller, site, 0, false), "happy path must be eligible")
}

func TestComputeInLoopMaskMarksLoopBody(t *testing.T) {
	t.Parallel()

	offsetBack := -3
	jumpInstr := isa.NewInstruction(
		isa.OpDrillTier1,
		byte(isa.SubOpJump),
		byte(uint16(int16(offsetBack))),
		byte(uint16(int16(offsetBack))>>8),
	)
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt, 0, 0, 0),
			isa.NewInstruction(isa.OpAddInt, 0, 0, 0),
			jumpInstr,
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
	}
	mask := computeInLoopMask(caller)
	require.Lenf(t, mask, 4, "mask length")
	for pc := 0; pc <= 2; pc++ {
		assert.Truef(t, mask[pc], "pc %d should be in-loop", pc)
	}
	assert.Falsef(t, mask[3], "pc 3 should not be in-loop")
}

func TestRunBytecodeInliner_SingleFunctionNoOp(t *testing.T) {
	t.Parallel()
	root := &program.CompiledFunction{Body: []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)}}
	origLen := len(root.Body)
	require.NoError(t, RunBytecodeInliner(context.Background(), root, passes.DefaultOptions()), "runBytecodeInliner unexpected error")
	require.Equalf(t, origLen, len(root.Body), "body length must not change")
}

func TestRunBytecodeInliner_NilRoot(t *testing.T) {
	t.Parallel()
	require.NoError(t, RunBytecodeInliner(context.Background(), nil, passes.DefaultOptions()), "nil root must return nil")
}

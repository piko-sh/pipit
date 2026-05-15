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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type inlineBinop struct {
	Left  any
	Right any
}

func inlineOperandLayout(t *testing.T, fieldIndex int) program.StructFieldLayout {
	t.Helper()

	field := reflect.TypeFor[inlineBinop]().Field(fieldIndex)
	layout := program.StructFieldLayout{Offset: uint32(field.Offset), PathLength: 1, Kind: uint8(field.Type.Kind())}
	layout.Path[0] = uint8(fieldIndex)
	return layout
}

func TestInlineDescriptorCacheServesTheSecondCall(t *testing.T) {
	t.Parallel()

	t.Run("a classified receiver type is cached and found again", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		callee := binopUintCandidate(isa.OpAddUint)
		receiverType := reflect.TypeFor[inlineBinop]()

		updateMethodIC(site, receiverType, 0, callee, false)
		require.Nil(t, lookupInlineDescriptor(site, receiverType), "nothing is classified before the first dispatch")

		classifyAndCacheFromMethodIC(site, receiverType)

		descriptor := lookupInlineDescriptor(site, receiverType)
		require.NotNil(t, descriptor)
		require.Equal(t, program.InlineShapeBinopUint, descriptor.Shape)
		require.Equal(t, isa.OpAddUint, descriptor.BinopOpcode)
		require.Same(t, callee, descriptor.Callee)
	})

	t.Run("an unclassifiable callee is still cached, as a refusal", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		callee := program.NewNamedFunction("plain")
		callee.Body = []isa.Instruction{tier2Return(1)}
		receiverType := reflect.TypeFor[inlineBinop]()

		updateMethodIC(site, receiverType, 0, callee, false)
		classifyAndCacheFromMethodIC(site, receiverType)

		descriptor := lookupInlineDescriptor(site, receiverType)
		require.NotNil(t, descriptor, "the refusal is cached so the classifier runs once per type")
		require.Equal(t, program.InlineShapeNone, descriptor.Shape)
	})

	t.Run("a receiver type the method cache never saw publishes nothing", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}

		classifyAndCacheFromMethodIC(site, reflect.TypeFor[inlineBinop]())

		require.Nil(t, lookupInlineDescriptor(site, reflect.TypeFor[inlineBinop]()))
	})

	t.Run("an unseen receiver type misses the descriptor cache", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		receiverType := reflect.TypeFor[inlineBinop]()

		updateMethodIC(site, receiverType, 0, binopUintCandidate(isa.OpAddUint), false)
		classifyAndCacheFromMethodIC(site, receiverType)

		require.Nil(t, lookupInlineDescriptor(site, reflect.TypeFor[recordHolder]()))
	})
}

func TestInlineDescriptorCarriesTheMatchedMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arithmetic isa.Opcode
	}{
		{name: "addition", arithmetic: isa.OpAddUint},
		{name: "subtraction", arithmetic: isa.OpSubUint},
		{name: "multiplication", arithmetic: isa.OpMulUint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callee := binopUintCandidate(tt.arithmetic)
			descriptor := &program.InlineDescriptor{}

			fillBinopUintDescriptor(callee, descriptor)

			require.Equal(t, tt.arithmetic, descriptor.BinopOpcode)
			require.Equal(t, callee.StructLayoutTable[0], descriptor.LeftLayout)
			require.Equal(t, callee.StructLayoutTable[1], descriptor.RightLayout)
			require.False(t, descriptor.MaskApplies, "an unmasked body records no mask")
		})
	}
}

func TestLookupCalleeForTypeReadsTheMethodCache(t *testing.T) {
	t.Parallel()

	site := &program.CallSite{}
	callee := program.NewNamedFunction("Eval")
	receiverType := reflect.TypeFor[inlineBinop]()

	require.Nil(t, lookupCalleeForType(site, receiverType), "an empty cache has nothing to serve")

	updateMethodIC(site, receiverType, 0, callee, false)

	require.Same(t, callee, lookupCalleeForType(site, receiverType))
	require.Nil(t, lookupCalleeForType(site, reflect.TypeFor[recordHolder]()))
}

func TestInnerCalleeCacheMemoisesPerReceiverType(t *testing.T) {
	t.Parallel()

	descriptor := &program.InlineDescriptor{}
	callee := program.NewNamedFunction("Eval")
	receiverType := reflect.TypeFor[inlineBinop]()

	require.Nil(t, lookupInnerCallee(descriptor, receiverType))

	publishInnerCallee(descriptor, receiverType, callee)

	require.Same(t, callee, lookupInnerCallee(descriptor, receiverType))
	require.Nil(t, lookupInnerCallee(descriptor, reflect.TypeFor[recordHolder]()),
		"the cache is keyed by the concrete receiver type")
}

func TestResolveInlineBinopOperandUnwrapsTheField(t *testing.T) {
	t.Parallel()

	leftLayout := inlineOperandLayout(t, 0)

	tests := []struct {
		name     string
		receiver reflect.Value
		want     bool
	}{
		{
			name:     "a populated interface field resolves to its dynamic value",
			receiver: reflect.ValueOf(&inlineBinop{Left: 4}), want: true,
		},
		{name: "a nil interface field declines", receiver: reflect.ValueOf(&inlineBinop{}), want: false},
		{name: "an invalid receiver declines", receiver: reflect.Value{}, want: false},
		{name: "a receiver that is not a struct declines", receiver: reflect.ValueOf(4), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field, ok := resolveInlineBinopOperand(tt.receiver, leftLayout)
			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, 4, field.Interface())
			}
		})
	}
}

func TestInlineBinopFallsBackWhenTheSiteShapeDisagrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site *program.CallSite
	}{
		{name: "too few arguments", site: &program.CallSite{Returns: []program.VarLocation{{}}}},
		{name: "no return slot", site: &program.CallSite{Arguments: []program.VarLocation{{}, {}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callee := binopUintCandidate(isa.OpAddUint)
			callee.NumRegisters = wideRegCounts(4)
			vm, frame, registers := closureHostVM(t, callee)
			descriptor := &program.InlineDescriptor{Callee: callee}

			got := runInlineBinopUint(vm, frame, registers, tt.site, descriptor)
			require.Equal(t, opFrameChanged, got,
				"a site the inline form cannot read must push the ordinary frame instead")
		})
	}
}

func TestReadInlineEnvAsReflectNamesTheSupportedBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kind      isa.RegisterKind
		load      func(*Registers)
		wantOK    bool
		wantValid bool
	}{
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load: func(r *Registers) { r.General[0] = reflect.ValueOf([]uint32{1}) }, wantOK: true, wantValid: true,
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint,
			load: func(r *Registers) { r.slicesUint[0] = []uint64{1} }, wantOK: true, wantValid: true,
		},
		{
			name: "an empty uint-slice bank", kind: isa.RegisterSliceUint, load: func(*Registers) {}, wantOK: true,
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load: func(r *Registers) { r.SlicesInt[0] = []int64{1} }, wantOK: true, wantValid: true,
		},
		{
			name: "an empty int-slice bank", kind: isa.RegisterSliceInt, load: func(*Registers) {}, wantOK: true,
		},
		{name: "a bank the inline path cannot read", kind: isa.RegisterInt, load: func(*Registers) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)

			value, ok := readInlineEnvAsReflect(&registers, program.VarLocation{Kind: tt.kind})
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantValid, value.IsValid())
		})
	}
}

func TestPlaceInlineEvalEnvWritesTheCalleeSlot(t *testing.T) {
	t.Parallel()

	t.Run("the general bank takes the value as given", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		value := reflect.ValueOf([]uint32{1})

		require.True(t, placeInlineEvalEnv(&registers, isa.RegisterGeneral, value, nil))
		require.Equal(t, value.Interface(), registers.General[1].Interface())
	})

	t.Run("the uint-slice bank takes a matching slice", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()

		require.True(t, placeInlineEvalEnv(&registers, isa.RegisterSliceUint, reflect.ValueOf([]uint64{1, 2}), nil))
		require.Equal(t, []uint64{1, 2}, registers.slicesUint[0])
	})

	t.Run("the uint-slice bank widens a narrow slice", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()

		require.True(t, placeInlineEvalEnv(&registers, isa.RegisterSliceUint, reflect.ValueOf([]uint32{1, 2}), nil))
		require.Equal(t, []uint64{1, 2}, registers.slicesUint[0])
	})

	t.Run("an invalid value clears the slot", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		registers.slicesUint[0] = []uint64{9}

		require.True(t, placeInlineEvalEnv(&registers, isa.RegisterSliceUint, reflect.Value{}, nil))
		require.Nil(t, registers.slicesUint[0])
	})

	t.Run("a value the bank cannot hold is refused", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()

		require.False(t, placeInlineEvalEnv(&registers, isa.RegisterSliceUint, reflect.ValueOf([]string{"s"}), nil))
	})

	t.Run("a bank the inline path cannot write is refused", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()

		require.False(t, placeInlineEvalEnv(&registers, isa.RegisterInt, reflect.ValueOf(1), nil))
	})
}

func TestResolveInlineEvalCalleeNeedsAMethodTable(t *testing.T) {
	t.Parallel()

	t.Run("no VM resolves nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := resolveInlineEvalCallee(nil, reflect.TypeFor[inlineBinop]())
		require.False(t, ok)
	})

	t.Run("a VM with no root function resolves nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := resolveInlineEvalCallee(newTestVM(t), reflect.TypeFor[inlineBinop]())
		require.False(t, ok)
	})

	t.Run("a type the table does not hold resolves nothing", func(t *testing.T) {
		t.Parallel()

		vm := methodTableVM(t, map[string]uint16{"other.Eval": 0})

		_, ok := resolveInlineEvalCallee(vm, reflect.TypeFor[inlineBinop]())
		require.False(t, ok)
	})

	t.Run("a function index past the table resolves nothing", func(t *testing.T) {
		t.Parallel()

		vm := methodTableVM(t, map[string]uint16{"inlineBinop." + inlineEvalMethodName: 9})

		_, ok := resolveInlineEvalCallee(vm, reflect.TypeFor[inlineBinop]())
		require.False(t, ok)
	})
}

func TestCallMethodInlineableRefusesBrokenSites(t *testing.T) {
	t.Parallel()

	t.Run("a call site index past the table", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))

		require.Equal(t, opPanicError, handleCallMethodInlineable(vm, frame, registers, op(0, 99, 0)))
		require.Error(t, vm.evalError)
	})

	t.Run("a site with no receiver argument", func(t *testing.T) {
		t.Parallel()

		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(8)
		siteIndex := builder.AddCallSite(&program.CallSite{})
		builder.Emit(isa.OpDrillTier1, 0, 0, 0)
		builder.body = append(builder.body, op(0, 0, 0))
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

		vm, frame, registers := newFramedVM(t, builder.build())
		frame.ProgramCounter = 1

		require.Equal(t, opPanicError,
			handleCallMethodInlineable(vm, frame, registers, op(0, byte(siteIndex), 0)))
		require.ErrorContains(t, vm.evalError, "no receiver argument")
	})
}

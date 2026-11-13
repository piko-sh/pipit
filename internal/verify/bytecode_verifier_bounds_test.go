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

package verify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestVerifyOperandBoundsAcceptsIIFESyncWithNoGeneralRegisters(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Name: "f",
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 0, 1),
			isa.NewTier1Instruction(isa.SubOpCallIIFE, 0, 0),
			isa.NewTier3Instruction(isa.SubOpTier3SynchroniseIIFECapturedVariables),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		CallSites: []program.CallSite{{}},
	}
	compiledFunction.NumRegisters[isa.RegisterInt64] = 2

	require.NoError(t, VerifyOperandBounds(t.Context(), compiledFunction))
}

func TestVerifyOperandBoundsStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Name: "cancelled",
		Body: []isa.Instruction{isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, VerifyOperandBounds(ctx, compiledFunction), context.Canceled)
}

func TestVerifyOperandBoundsChecksCallSiteIndices(t *testing.T) {
	t.Parallel()

	const declaredSites = 2
	call := func(op isa.SubOpcode, site uint16) isa.Instruction {
		low, high := isa.SplitWide(site)
		return isa.NewTier1Instruction(op, low, high)
	}
	methodExtension := isa.NewInstruction(isa.OpExtension, 0, 0, 0)

	tests := []struct {
		name  string
		body  []isa.Instruction
		valid bool
	}{
		{name: "a call naming the last declared site", body: []isa.Instruction{call(isa.SubOpCall, 1)}, valid: true},
		{name: "a call past the table", body: []isa.Instruction{call(isa.SubOpCall, 2)}, valid: false},
		{name: "a call whose high byte reaches past the table", body: []isa.Instruction{call(isa.SubOpCall, 256)}, valid: false},
		{name: "a scalar call past the table", body: []isa.Instruction{call(isa.SubOpCallScalar, 99)}, valid: false},
		{name: "a native call past the table", body: []isa.Instruction{call(isa.SubOpCallNative, 99)}, valid: false},
		{name: "an immediately invoked literal past the table", body: []isa.Instruction{call(isa.SubOpCallIIFE, 99)}, valid: false},
		{name: "a tail call past the table", body: []isa.Instruction{call(isa.SubOpTailCall, 99)}, valid: false},
		{name: "a method call past the table", body: []isa.Instruction{call(isa.SubOpCallMethod, 99), methodExtension}, valid: false},
		{name: "an inlineable method call past the table", body: []isa.Instruction{call(isa.SubOpCallMethodInlineable, 99), methodExtension}, valid: false},
		{name: "a method call naming a declared site", body: []isa.Instruction{call(isa.SubOpCallMethod, 0), methodExtension}, valid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			compiledFunction := &program.CompiledFunction{
				Name:      "f",
				Body:      tt.body,
				CallSites: make([]program.CallSite, declaredSites),
			}

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrCallSiteOutOfRange)
		})
	}
}

func TestVerifyOperandBoundsChecksClosureSyncRegister(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Name: "f",
		Body: []isa.Instruction{
			isa.NewTier2Instruction(isa.SubOpTier2SynchroniseClosureCapturedVariables, 3),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
	}
	compiledFunction.NumRegisters[isa.RegisterGeneral] = 2

	require.ErrorIs(t, VerifyOperandBounds(t.Context(), compiledFunction), ErrRegisterOperandOutOfRange)
}

func TestVerifyOperandBoundsExtensionWords(t *testing.T) {
	t.Parallel()

	const (
		intCount     = 4
		generalCount = 4
		outOfRange   = 9
	)
	deferWith := func(arguments ...isa.Instruction) []isa.Instruction {
		return append([]isa.Instruction{isa.NewInstruction(isa.OpDefer, 0, uint8(len(arguments)), 0)}, arguments...)
	}
	argument := func(register uint8, kind isa.RegisterKind) isa.Instruction {
		return isa.NewInstruction(isa.OpExtension, 0, register, uint8(kind))
	}
	badInt := isa.NewInstruction(isa.OpAddInt64, outOfRange, 0, 1)
	tests := []struct {
		name  string
		body  []isa.Instruction
		valid bool
	}{
		{
			name:  "a defer with no arguments does not hide the next instruction",
			body:  append(deferWith(), badInt),
			valid: false,
		},
		{
			name:  "a defer skips every argument word",
			body:  append(deferWith(argument(1, isa.RegisterInt64), argument(2, isa.RegisterString)), isa.NewInstruction(isa.OpAddInt64, 3, 0, 1)),
			valid: true,
		},
		{
			name:  "a deferred int argument beyond the frame is rejected",
			body:  deferWith(argument(1, isa.RegisterInt64), argument(outOfRange, isa.RegisterInt64)),
			valid: false,
		},
		{
			name:  "a deferred string argument is not checked against the int count",
			body:  deferWith(argument(outOfRange, isa.RegisterString)),
			valid: true,
		},
		{
			name:  "the instruction after an append is checked",
			body:  []isa.Instruction{isa.NewInstruction(isa.OpAppend, 1, 2, 3), badInt},
			valid: false,
		},
		{
			name:  "an append element beyond the frame is rejected",
			body:  []isa.Instruction{isa.NewInstruction(isa.OpAppend, 1, 2, outOfRange)},
			valid: false,
		},
		{
			name:  "a typed slice read with its int index beyond the frame is rejected",
			body:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetSliceFloat64Element, 0, 0), isa.NewInstruction(isa.OpExtension, outOfRange, 0, 0)},
			valid: false,
		},
		{
			name:  "a struct field layout index is not a register",
			body:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetStructFieldInt64Wide, 0, 1), isa.NewInstruction(isa.OpExtension, outOfRange, outOfRange, 0)},
			valid: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body}
			compiledFunction.NumRegisters[isa.RegisterInt64] = intCount
			compiledFunction.NumRegisters[isa.RegisterGeneral] = generalCount
			compiledFunction.NumRegisters[isa.RegisterString] = 1
			compiledFunction.NumRegisters[isa.RegisterFloat64] = 1
			compiledFunction.NumRegisters[isa.RegisterSliceFloat64] = 1

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrRegisterOperandOutOfRange)
		})
	}
}

func TestVerifyOperandBoundsChecksTheWordsOfAByteStoreRun(t *testing.T) {
	t.Parallel()

	const uintCount = 4
	run := isa.NewTier1Instruction(isa.SubOpSetSliceByteElementRun, 0, 0)
	first := func(stores uint8) isa.Instruction { return isa.NewInstruction(isa.OpExtension, 1, 0, stores) }
	word := func(register uint8) isa.Instruction { return isa.NewInstruction(isa.OpExtension, register, 0, 0) }
	other := isa.NewInstruction(isa.OpAddInt64, 0, 0, 0)
	tests := []struct {
		want error
		name string
		body []isa.Instruction
	}{
		{name: "two stores", body: []isa.Instruction{run, first(2), word(2), other}},
		{name: "four stores", body: []isa.Instruction{run, first(4), word(2), word(3), word(0)}},
		{name: "one store", body: []isa.Instruction{run, first(1), other}, want: ErrRunMalformed},
		{name: "five stores", body: []isa.Instruction{run, first(5), word(1), word(1), word(1), word(1)}, want: ErrRunMalformed},
		{name: "no word at all", body: []isa.Instruction{run}, want: ErrExtensionWordMissing},
		{name: "a counted word past the end of the body", body: []isa.Instruction{run, first(3), word(2)}, want: ErrRunMalformed},
		{name: "a counted word that is an instruction", body: []isa.Instruction{run, first(3), word(2), other}, want: ErrRunMalformed},
		{name: "an extension word past the counted ones", body: []isa.Instruction{run, first(2), word(2), word(1)}, want: ErrRunMalformed},
		{name: "a value register beyond the frame", body: []isa.Instruction{run, first(2), word(uintCount)}, want: ErrRegisterOperandOutOfRange},
		{name: "a first value register beyond the frame", body: []isa.Instruction{run, isa.NewInstruction(isa.OpExtension, 9, 0, 2), word(1)}, want: ErrRegisterOperandOutOfRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 1
			compiledFunction.NumRegisters[isa.RegisterUint64] = uintCount
			compiledFunction.NumRegisters[isa.RegisterSliceByte] = 1

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerifyOperandBoundsChecksTheWordsOfAFieldReadRun(t *testing.T) {
	t.Parallel()

	const uintCount = 4
	run := isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, 0, 1)
	first := func(reads, mode uint8) isa.Instruction {
		return isa.NewInstruction(isa.OpExtension, 1, 0, reads|mode<<isa.FieldRunModeShift)
	}
	word := func(register uint8) isa.Instruction { return isa.NewInstruction(isa.OpExtension, register, 7, 0) }
	other := isa.NewInstruction(isa.OpAddInt64, 0, 0, 0)
	tests := []struct {
		want error
		name string
		body []isa.Instruction
	}{
		{name: "two reads", body: []isa.Instruction{run, first(2, 0), word(2), other}},
		{name: "four reads after a snapshot move", body: []isa.Instruction{run, first(4, 4), word(2), word(3), word(0)}},
		{name: "one read", body: []isa.Instruction{run, first(1, 0), other}, want: ErrRunMalformed},
		{name: "five reads", body: []isa.Instruction{run, first(5, 0), word(1), word(1), word(1), word(1)}, want: ErrRunMalformed},
		{name: "a counted word past the end of the body", body: []isa.Instruction{run, first(3, 0), word(2)}, want: ErrRunMalformed},
		{name: "a counted word that is an instruction", body: []isa.Instruction{run, first(3, 0), word(2), other}, want: ErrRunMalformed},
		{name: "a destination register beyond the frame", body: []isa.Instruction{run, first(2, 0), word(uintCount)}, want: ErrRegisterOperandOutOfRange},
		{name: "a receiver register beyond the frame", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, 9, 1), first(2, 0), word(1)}, want: ErrRegisterOperandOutOfRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 1
			compiledFunction.NumRegisters[isa.RegisterGeneral] = 2
			compiledFunction.NumRegisters[isa.RegisterUint64] = uintCount

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerifyOperandBoundsChecksTheRegistersOfAnElementFieldRun(t *testing.T) {
	t.Parallel()

	const uintCount, generalCount = 4, 2
	run := isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, 0, 0)
	first := isa.NewInstruction(isa.OpExtension, 1, 0, 2)
	second := func(destination, receiver uint8) isa.Instruction {
		return isa.NewInstruction(isa.OpExtension, destination, 7, receiver)
	}
	other := isa.NewInstruction(isa.OpAddInt64, 0, 0, 0)
	tests := []struct {
		want error
		name string
		body []isa.Instruction
	}{
		{name: "registers inside the frame", body: []isa.Instruction{run, first, second(2, 1), other}},
		{name: "a destination register beyond the frame", body: []isa.Instruction{run, first, second(uintCount, 1), other}, want: ErrRegisterOperandOutOfRange},
		{name: "a receiver register beyond the frame", body: []isa.Instruction{run, first, second(2, generalCount), other}, want: ErrRegisterOperandOutOfRange},
		{name: "one read", body: []isa.Instruction{run, isa.NewInstruction(isa.OpExtension, 1, 0, 1), other}, want: ErrRunMalformed},
		{
			name: "a second word that is an instruction is malformed whatever its bytes hold",
			body: []isa.Instruction{run, first, isa.NewInstruction(isa.OpAddInt64, 0, 0, generalCount), other},
			want: ErrRunMalformed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 1
			compiledFunction.NumRegisters[isa.RegisterGeneral] = generalCount
			compiledFunction.NumRegisters[isa.RegisterUint64] = uintCount

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerifyOperandBoundsChecksTheRegistersOfAnElementFieldStore(t *testing.T) {
	t.Parallel()

	const generalCount = 2
	integerStore := isa.NewInstruction(isa.OpSetElementFieldInt64, 0, 1, 0)
	floatStore := isa.NewInstruction(isa.OpSetElementFieldFloat64, 0, 1, 0)
	scratch := func(register uint8) isa.Instruction {
		return isa.NewInstruction(isa.OpExtension, 0, 0, register)
	}
	other := isa.NewInstruction(isa.OpAddInt64, 0, 0, 0)
	tests := []struct {
		want error
		name string
		body []isa.Instruction
	}{
		{name: "an integer store with its registers inside the frame", body: []isa.Instruction{integerStore, scratch(0), other}},
		{name: "a float store with its registers inside the frame", body: []isa.Instruction{floatStore, scratch(1), other}},
		{
			name: "an integer store whose element register is beyond the frame",
			body: []isa.Instruction{integerStore, scratch(generalCount), other},
			want: ErrRegisterOperandOutOfRange,
		},
		{
			name: "a float store whose element register is beyond the frame",
			body: []isa.Instruction{floatStore, scratch(generalCount), other},
			want: ErrRegisterOperandOutOfRange,
		},
		{
			name: "a slice register beyond the frame",
			body: []isa.Instruction{isa.NewInstruction(isa.OpSetElementFieldInt64, 0, generalCount, 0), scratch(0), other},
			want: ErrRegisterOperandOutOfRange,
		},
		{
			name: "a value register beyond the frame",
			body: []isa.Instruction{isa.NewInstruction(isa.OpSetElementFieldFloat64, 1, 1, 0), scratch(0), other},
			want: ErrRegisterOperandOutOfRange,
		},
		{
			name: "an index register beyond the frame",
			body: []isa.Instruction{isa.NewInstruction(isa.OpSetElementFieldFloat64, 0, 1, 1), scratch(0), other},
			want: ErrRegisterOperandOutOfRange,
		},
		{name: "a store followed by an instruction", body: []isa.Instruction{integerStore, other}, want: ErrExtensionWordMissing},
		{name: "a store at the end of the body", body: []isa.Instruction{floatStore}, want: ErrExtensionWordMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 1
			compiledFunction.NumRegisters[isa.RegisterFloat64] = 1
			compiledFunction.NumRegisters[isa.RegisterGeneral] = generalCount

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerifyOperandBoundsRejectsAnInstructionWithoutTheExtensionWordItReads(t *testing.T) {
	t.Parallel()

	multiplyAdd := isa.NewInstruction(isa.OpMultiplyInt64ConstantThenAdd, 0, 1, 0)
	byteRead := isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, 0, 0)
	other := isa.NewInstruction(isa.OpAddInt64, 0, 0, 0)
	noArguments := isa.NewInstruction(isa.OpDefer, 0, 0, 0)
	tests := []struct {
		want error
		name string
		body []isa.Instruction
	}{
		{name: "a fused multiply-add with its word", body: []isa.Instruction{multiplyAdd, isa.NewInstruction(isa.OpExtension, 1, 0, 0), other}},
		{name: "a fused multiply-add at the end of the body", body: []isa.Instruction{multiplyAdd}, want: ErrExtensionWordMissing},
		{name: "a fused multiply-add followed by an instruction", body: []isa.Instruction{multiplyAdd, other}, want: ErrExtensionWordMissing},
		{name: "a byte read followed by an instruction", body: []isa.Instruction{byteRead, other}, want: ErrExtensionWordMissing},
		{name: "a defer with no arguments carries no word", body: []isa.Instruction{noArguments, other}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body, IntConstants: []int64{3}}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 2
			compiledFunction.NumRegisters[isa.RegisterGeneral] = 1
			compiledFunction.NumRegisters[isa.RegisterSliceByte] = 1

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerifyOperandBoundsChecksConstantIndicesAgainstTheirPool(t *testing.T) {
	t.Parallel()

	extension := isa.NewInstruction(isa.OpExtension, 1, 0, 0)
	tests := []struct {
		want  error
		name  string
		body  []isa.Instruction
		ints  []int64
		uints []uint64
	}{
		{name: "an int constant inside the pool", ints: []int64{5}, body: []isa.Instruction{isa.NewInstruction(isa.OpAddInt64Constant, 0, 1, 0)}},
		{name: "an int constant past the pool", ints: []int64{5}, body: []isa.Instruction{isa.NewInstruction(isa.OpAddInt64Constant, 0, 1, 1)},
			want: ErrConstantOutOfRange},
		{name: "a fused multiply-add past the pool", body: []isa.Instruction{isa.NewInstruction(isa.OpMultiplyInt64ConstantThenAdd, 0, 1, 0), extension},
			want: ErrConstantOutOfRange},
		{name: "a fused latch past the pool", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant, 0, 0),
			isa.NewInstruction(isa.OpExtension, 0, 0, 0)}, want: ErrConstantOutOfRange},
		{name: "a wide int load past the pool", ints: []int64{1, 2}, body: []isa.Instruction{isa.NewInstruction(isa.OpLoadInt64Constant, 0, 2, 0)},
			want: ErrConstantOutOfRange},
		{name: "a uint constant inside its own pool", uints: []uint64{7}, body: []isa.Instruction{isa.NewInstruction(isa.OpLoadUint64Constant, 0, 0, 0)}},
		{name: "a uint constant named by an extension word inside the pool", uints: []uint64{7, 9},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 0, 0), extension}},
		{name: "an added uint constant named by an extension word past the pool", uints: []uint64{7},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 0, 0), extension}, want: ErrConstantOutOfRange},
		{name: "a subtracted one", uints: []uint64{7},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSubtractUint64Constant, 0, 0), extension}, want: ErrConstantOutOfRange},
		{name: "a mask", uints: []uint64{7},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, 0, 0), extension}, want: ErrConstantOutOfRange},
		{name: "one added in 32 bits", uints: []uint64{7},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 0, 0), extension}, want: ErrConstantOutOfRange},
		{name: "one subtracted in 32 bits", uints: []uint64{7},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSubtractUint32Constant, 0, 0), extension}, want: ErrConstantOutOfRange},
		{name: "an index whose high byte takes it past the pool", uints: []uint64{7},
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 0, 0), isa.NewInstruction(isa.OpExtension, 0, 1, 0)},
			want: ErrConstantOutOfRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: tt.body, IntConstants: tt.ints, UintConstants: tt.uints}
			compiledFunction.NumRegisters[isa.RegisterInt64] = 2
			compiledFunction.NumRegisters[isa.RegisterUint64] = 1

			err := VerifyOperandBounds(t.Context(), compiledFunction)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

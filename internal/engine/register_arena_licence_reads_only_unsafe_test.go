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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func byteViewFrame(site program.CallSite, prefix ...isa.Instruction) *CallFrame {
	body := append(prefix, isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0))
	return &CallFrame{Function: &program.CompiledFunction{Body: body, CallSites: []program.CallSite{site}}}
}

func byteViewNativeSite() program.CallSite {
	return program.CallSite{
		IsNative:          true,
		NativeRegister:    0,
		Arguments:         []program.VarLocation{{Register: 3, Kind: isa.RegisterGeneral}},
		ByteViewArguments: 1,
	}
}

func readsOnlyArena(t *testing.T, function any) *RegisterArena {
	t.Helper()
	declarations := NewBorrowDeclarations()
	require.NoError(t, declarations.Declare(function, BorrowReadsOnly))
	declarations.Freeze()
	return &RegisterArena{licensed: true, borrowDeclarations: declarations}
}

func TestByteViewSiteFindsTheCallThatTakesTheView(t *testing.T) {
	otherConversion := isa.NewTier1Instruction(isa.SubOpConvertStringToBytes, 4, 0)
	sameConversion := isa.NewTier1Instruction(isa.SubOpConvertStringToBytes, 3, 0)
	cases := []struct {
		frame *CallFrame
		name  string
		found bool
	}{
		{name: "the call next", frame: byteViewFrame(byteViewNativeSite()), found: true},
		{name: "after another conversion", frame: byteViewFrame(byteViewNativeSite(), otherConversion), found: true},
		{name: "after a second conversion into the same register", frame: byteViewFrame(byteViewNativeSite(), sameConversion), found: false},
		{name: "after any other instruction", frame: byteViewFrame(byteViewNativeSite(), isa.NewInstruction(isa.OpMoveGeneral, 1, 2, 0)), found: false},
		{name: "after a tier-1 instruction that is not a conversion", frame: byteViewFrame(byteViewNativeSite(), isa.NewTier1Instruction(isa.SubOpJump, 0, 0)), found: false},
		{name: "past the lookahead", frame: byteViewFrame(byteViewNativeSite(), otherConversion, otherConversion, otherConversion, otherConversion,
			otherConversion, otherConversion, otherConversion, otherConversion), found: false},
		{name: "a site that does not mark the register", frame: byteViewFrame(program.CallSite{IsNative: true}), found: false},
		{name: "no frame", frame: nil, found: false},
		{name: "a frame without a function", frame: &CallFrame{}, found: false},
		{name: "the end of the body", frame: &CallFrame{Function: &program.CompiledFunction{Body: []isa.Instruction{otherConversion}}}, found: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, found := byteViewSite(testCase.frame, 3)
			require.Equal(t, testCase.found, found)
		})
	}
}

func TestByteViewCallSiteNeedsARecordedSite(t *testing.T) {
	function := &program.CompiledFunction{}
	_, found := byteViewCallSite(function, isa.NewTier1Instruction(isa.SubOpCallNative, 2, 0), 3)
	require.False(t, found)
}

func TestByteViewSiteTakesOnlyAPlainGeneralRegister(t *testing.T) {
	mutate := map[string]func(site *program.CallSite){
		"not a native call":     func(site *program.CallSite) { site.IsNative = false },
		"a spread call":         func(site *program.CallSite) { site.IsEllipsisSpread = true },
		"a linked generic":      func(site *program.CallSite) { site.LinkedTypeArgs = []reflect.Type{reflect.TypeFor[int]()} },
		"an unmarked argument":  func(site *program.CallSite) { site.ByteViewArguments = 0 },
		"another register":      func(site *program.CallSite) { site.Arguments[0].Register = 4 },
		"a spilled argument":    func(site *program.CallSite) { site.Arguments[0].IsSpilled = true },
		"an indirect argument":  func(site *program.CallSite) { site.Arguments[0].IsIndirect = true },
		"an upvalue argument":   func(site *program.CallSite) { site.Arguments[0].IsUpvalue = true },
		"another register bank": func(site *program.CallSite) { site.Arguments[0].Kind = isa.RegisterString },
	}
	site := byteViewNativeSite()
	require.True(t, byteViewSiteTakes(&site, 3))
	for name, change := range mutate {
		t.Run(name, func(t *testing.T) {
			site := byteViewNativeSite()
			change(&site)
			require.False(t, byteViewSiteTakes(&site, 3))
		})
	}
}

func TestByteViewForHandsAReadsOnlyNativeTheStringsBytes(t *testing.T) {
	measure := func(data []byte) int { return len(data) }
	vm := newTestVM(t)
	vm.Arena = readsOnlyArena(t, measure)
	registers := &Registers{General: []reflect.Value{reflect.ValueOf(measure)}}
	frame := byteViewFrame(byteViewNativeSite())
	text := "a string read in place"

	view, ok := byteViewFor(vm, frame, registers, 3, text)
	require.True(t, ok)
	if !arenaAuditEnabled {
		bytes, isBytes := reflect.TypeAssert[[]byte](view)
		require.True(t, isBytes)
		require.Equal(t, unsafe.StringData(text), unsafe.SliceData(bytes), "the view is the string's own bytes")
	}

	empty, ok := byteViewFor(vm, frame, registers, 3, "")
	require.True(t, ok)
	require.Zero(t, empty.Len())

	_, ok = byteViewFor(vm, frame, registers, 4, text)
	require.False(t, ok, "the call does not take this register as a view")

	registers.General[0] = reflect.ValueOf(func(data []byte) int { return cap(data) })
	_, ok = byteViewFor(vm, frame, registers, 3, text)
	require.False(t, ok, "an undeclared native gets a copy")
}

func TestAuditReadsOnlyArgumentCopiesForTheFirstCall(t *testing.T) {
	t.Cleanup(ResetReadsOnlyAuditForTest)
	ResetReadsOnlyAuditForTest()
	measure := reflect.ValueOf(func(data []byte) int { return len(data) })
	vm := newTestVM(t)

	copied, audited := auditReadsOnlyArgument(vm, measure, "first")
	require.True(t, audited, "the first call is passed a copy")
	require.Equal(t, []byte("first"), copied.Interface())
	verifyReadsOnlyChecks(takeReadsOnlyChecks(vm))
	require.Empty(t, ReadsOnlyViolations())

	_, audited = auditReadsOnlyArgument(vm, measure, "later")
	require.False(t, audited, "an attested native is passed the bytes and checksummed")
	checks := takeReadsOnlyChecks(vm)
	require.Len(t, checks, 1)
	require.Equal(t, checksumBytes([]byte("later")), checks[0].sum)
}

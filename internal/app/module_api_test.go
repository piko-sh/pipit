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

package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/module"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/verify"
)

func stubPacker(_ *program.CompiledFileSet) ([]byte, error) {
	return []byte("stub-packed-bytecode"), nil
}

func stubUnpacker(_ []byte, _ *symtab.SymbolRegistry) (*program.CompiledFileSet, error) {
	return &program.CompiledFileSet{}, nil
}

func stubBundle() *module.Bundle {
	return &module.Bundle{
		Descriptor: &module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: "example.com/mod", Version: "v1"},
		},
		Bytecode: []byte("payload"),
	}
}

func pinnedRefFor(t *testing.T, bundle *module.Bundle) module.Ref {
	t.Helper()
	fingerprint, err := bundle.Fingerprint()
	require.NoError(t, err)
	return module.Ref{
		Path:    bundle.Descriptor.Ref.Path,
		Version: bundle.Descriptor.Ref.Version,
		Pin:     fingerprint,
	}
}

func TestPackageModuleRequiresPacker(t *testing.T) {
	t.Parallel()
	service := NewService()
	_, err := service.PackageModule(t.Context(),
		module.Descriptor{},
		"example.com/mod",
		nil,
		nil,
	)
	require.Error(t, err, "a nil bytecode packer is refused")
}

func TestPackageModuleReportsAPackerFailure(t *testing.T) {
	t.Parallel()
	packFailure := errors.New("cannot pack")
	service := NewService()
	bundle, err := service.PackageModule(t.Context(),
		module.Descriptor{SchemaVersion: module.DescriptorVersion, Ref: module.Ref{Path: "example.com/lib", Version: "v0.0.0"}},
		"example.com/lib",
		map[string]map[string]string{"": {"lib.go": "package lib\n\nfunc Add(a, b int) int { return a + b }\n"}},
		func(*program.CompiledFileSet) ([]byte, error) { return nil, packFailure },
	)
	require.ErrorIs(t, err, packFailure)
	require.Nil(t, bundle, "no bundle is built from bytecode that failed to pack")
}

func TestPackageModuleRejectsInvalidDescriptor(t *testing.T) {
	t.Parallel()
	service := NewService()
	_, err := service.PackageModule(t.Context(),
		module.Descriptor{},
		"example.com/mod",
		nil,
		stubPacker,
	)
	require.Error(t, err, "an empty descriptor fails validation")
}

func TestLoadModuleVerifiesPin(t *testing.T) {
	t.Parallel()
	service := NewService()
	bundle := &module.Bundle{
		Descriptor: &module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: "example.com/mod", Version: "v1"},
		},
		Bytecode: []byte("payload"),
	}
	wrongRef := module.Ref{
		Path:    bundle.Descriptor.Ref.Path,
		Version: bundle.Descriptor.Ref.Version,
		Pin:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	_, err := service.LoadModule(t.Context(), bundle, wrongRef, nil, stubUnpacker)
	require.ErrorIs(t, err, module.ErrIntegrityMismatch, "a mismatched pin fails the integrity check")
}

func TestLoadModuleAcceptsMatchingPin(t *testing.T) {
	t.Parallel()
	service := NewService()
	bundle := &module.Bundle{
		Descriptor: &module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: "example.com/mod", Version: "v1"},
		},
		Bytecode: []byte("payload"),
	}
	fingerprint, err := bundle.Fingerprint()
	require.NoError(t, err)
	reference := module.Ref{
		Path:    bundle.Descriptor.Ref.Path,
		Version: bundle.Descriptor.Ref.Version,
		Pin:     fingerprint,
	}
	loaded, err := service.LoadModule(t.Context(), bundle, reference, nil, stubUnpacker)
	require.NoError(t, err)
	require.Equal(t, fingerprint, loaded.Fingerprint, "the loaded module keeps the bundle fingerprint")
}

func TestLoadModuleRejectsUnpinnedRefByDefault(t *testing.T) {
	t.Parallel()
	service := NewService()
	bundle := stubBundle()
	reference := module.Ref{Path: bundle.Descriptor.Ref.Path, Version: bundle.Descriptor.Ref.Version}
	_, err := service.LoadModule(t.Context(), bundle, reference, nil, stubUnpacker)
	require.ErrorIs(t, err, module.ErrUnpinnedRef)
	require.ErrorContains(t, err, "example.com/mod@v1")
}

func TestLoadModuleAllowsUnpinnedRefWhenOptedIn(t *testing.T) {
	t.Parallel()
	service := NewService(WithAllowUnpinnedModules(true))
	bundle := stubBundle()
	reference := module.Ref{Path: bundle.Descriptor.Ref.Path, Version: bundle.Descriptor.Ref.Version}
	loaded, err := service.LoadModule(t.Context(), bundle, reference, nil, stubUnpacker)
	require.NoError(t, err)
	require.NotEmpty(t, loaded.Fingerprint)
}

func TestLoadModuleRunsBytecodeVerifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       []isa.Instruction
		wantErr    error
		wantDetail string
	}{
		{
			name:       "a register operand past the frame",
			body:       []isa.Instruction{{Op: isa.OpAddInt, A: 0, B: 1, C: 7}},
			wantErr:    verify.ErrRegisterOperandOutOfRange,
			wantDetail: "slot=7",
		},
		{
			name:       "a call-site index past the table",
			body:       []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 99, 0)},
			wantErr:    verify.ErrCallSiteOutOfRange,
			wantDetail: "site=99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(WithBytecodeVerification(false))
			bundle := stubBundle()
			tamperedUnpacker := func(_ []byte, _ *symtab.SymbolRegistry) (*program.CompiledFileSet, error) {
				root := &program.CompiledFunction{Name: "tampered", Body: tt.body}
				root.NumRegisters[isa.RegisterInt] = 2
				return program.NewCompiledFileSet(root, nil, nil, nil), nil
			}
			_, err := service.LoadModule(t.Context(), bundle, pinnedRefFor(t, bundle), nil, tamperedUnpacker)
			require.ErrorIs(t, err, tt.wantErr)
			require.ErrorContains(t, err, "example.com/mod")
			require.ErrorContains(t, err, tt.wantDetail)
		})
	}
}

func TestVerifyOperandBoundsAcceptsCompiledProgram(t *testing.T) {
	t.Parallel()
	service := NewService()
	cfs, err := service.CompileProgram(t.Context(), "example.com/bounds", map[string]map[string]string{
		"": {"main.go": `package main

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	words := map[string][]string{}
	for i := 0; i < fib(10); i++ {
		key := "k" + string(rune('a'+i%3))
		words[key] = append(words[key], key)
	}
	_ = words
}
`},
	})
	require.NoError(t, err)
	require.NoError(t, verify.VerifyOperandBounds(cfs.Root()))
	require.NoError(t, verify.VerifyOperandBounds(cfs.VariableInitFunction()))
}

func TestLoadModuleConsultsCapabilityHook(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	service := NewService(WithCapabilityHook(hook))
	bundle := &module.Bundle{
		Descriptor: &module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: "example.com/mod", Version: "v1"},
			Capabilities: module.CapabilitySet{
				{Axis: "network"},
				{Axis: "filesystem.read", Scope: "/etc/*"},
			},
		},
		Bytecode: []byte("payload"),
	}
	reference := pinnedRefFor(t, bundle)
	_, err := service.LoadModule(t.Context(), bundle, reference, nil, stubUnpacker)
	require.NoError(t, err)
	calls := hook.snapshot()
	require.GreaterOrEqual(t, len(calls), 2, "the hook is consulted once per capability")
	for _, call := range calls {
		assert.Equal(t, "example.com/mod", call.ModulePath)
	}
}

func TestLoadModuleHookDenialBlocksLoad(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{
		denyFn: func(call recordedHookCall) error {
			return errors.New("module load denied")
		},
	}
	service := NewService(WithCapabilityHook(hook))
	bundle := &module.Bundle{
		Descriptor: &module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: "example.com/mod", Version: "v1"},
			Capabilities:  module.CapabilitySet{{Axis: "network"}},
		},
		Bytecode: []byte("payload"),
	}
	reference := pinnedRefFor(t, bundle)
	_, err := service.LoadModule(t.Context(), bundle, reference, nil, stubUnpacker)
	require.ErrorIs(t, err, module.ErrCapabilityDenied, "an undeclared capability is denied")
}

func TestLoadModuleRequiresUnpacker(t *testing.T) {
	t.Parallel()
	service := NewService()
	_, err := service.LoadModule(t.Context(),
		&module.Bundle{
			Descriptor: &module.Descriptor{
				SchemaVersion: module.DescriptorVersion,
				Ref:           module.Ref{Path: "x"},
			},
			Bytecode: []byte("data"),
		},
		module.Ref{Path: "x"},
		nil,
		nil,
	)
	require.Error(t, err, "a nil unpacker is refused")
}

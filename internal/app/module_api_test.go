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
	"context"
	"errors"
	"testing"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/verify"

	"pipit.sh/pipit/internal/symtab"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/module"
)

func stubPacker(_ *program.CompiledFileSet) []byte {
	return []byte("stub-packed-bytecode")
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
	_, err := service.PackageModule(context.Background(),
		module.Descriptor{},
		"example.com/mod",
		nil,
		nil,
	)
	if err == nil {
		t.Fatalf("expected error when bytecodePacker is nil")
	}
}

func TestPackageModuleRejectsInvalidDescriptor(t *testing.T) {
	t.Parallel()
	service := NewService()
	_, err := service.PackageModule(context.Background(),
		module.Descriptor{},
		"example.com/mod",
		nil,
		stubPacker,
	)
	if err == nil {
		t.Fatalf("expected validation error for empty descriptor")
	}
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
	_, err := service.LoadModule(context.Background(), bundle, wrongRef, nil, stubUnpacker)
	if !errors.Is(err, module.ErrIntegrityMismatch) {
		t.Fatalf("expected ErrIntegrityMismatch, got %v", err)
	}
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
	if err != nil {
		t.Fatalf("Fingerprint error: %v", err)
	}
	reference := module.Ref{
		Path:    bundle.Descriptor.Ref.Path,
		Version: bundle.Descriptor.Ref.Version,
		Pin:     fingerprint,
	}
	loaded, err := service.LoadModule(context.Background(), bundle, reference, nil, stubUnpacker)
	if err != nil {
		t.Fatalf("LoadModule error: %v", err)
	}
	if loaded.Fingerprint != fingerprint {
		t.Fatalf("LoadedModule.Fingerprint mismatch: got %s want %s", loaded.Fingerprint, fingerprint)
	}
}

func TestLoadModuleRejectsUnpinnedRefByDefault(t *testing.T) {
	t.Parallel()
	service := NewService()
	bundle := stubBundle()
	reference := module.Ref{Path: bundle.Descriptor.Ref.Path, Version: bundle.Descriptor.Ref.Version}
	_, err := service.LoadModule(context.Background(), bundle, reference, nil, stubUnpacker)
	require.ErrorIs(t, err, module.ErrUnpinnedRef)
	require.ErrorContains(t, err, "example.com/mod@v1")
}

func TestLoadModuleAllowsUnpinnedRefWhenOptedIn(t *testing.T) {
	t.Parallel()
	service := NewService(WithAllowUnpinnedModules(true))
	bundle := stubBundle()
	reference := module.Ref{Path: bundle.Descriptor.Ref.Path, Version: bundle.Descriptor.Ref.Version}
	loaded, err := service.LoadModule(context.Background(), bundle, reference, nil, stubUnpacker)
	require.NoError(t, err)
	require.NotEmpty(t, loaded.Fingerprint)
}

func TestLoadModuleRunsBytecodeVerifier(t *testing.T) {
	t.Parallel()
	service := NewService(WithBytecodeVerification(false))
	bundle := stubBundle()
	tamperedUnpacker := func(_ []byte, _ *symtab.SymbolRegistry) (*program.CompiledFileSet, error) {
		root := &program.CompiledFunction{
			Name: "tampered",
			Body: []isa.Instruction{{Op: isa.OpAddInt, A: 0, B: 1, C: 7}},
		}
		root.NumRegisters[isa.RegisterInt] = 2
		return program.NewCompiledFileSet(root, nil, nil, nil), nil
	}
	_, err := service.LoadModule(context.Background(), bundle, pinnedRefFor(t, bundle), nil, tamperedUnpacker)
	require.ErrorIs(t, err, verify.ErrRegisterOperandOutOfRange)
	require.ErrorContains(t, err, "example.com/mod")
	require.ErrorContains(t, err, "slot=7")
}

func TestVerifyRegisterOperandBoundsAcceptsCompiledProgram(t *testing.T) {
	t.Parallel()
	service := NewService()
	cfs, err := service.CompileProgram(context.Background(), "example.com/bounds", map[string]map[string]string{
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
	require.NoError(t, verify.VerifyRegisterOperandBounds(cfs.Root()))
	require.NoError(t, verify.VerifyRegisterOperandBounds(cfs.VariableInitFunction()))
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
	if _, err := service.LoadModule(context.Background(), bundle, reference, nil, stubUnpacker); err != nil {
		t.Fatalf("LoadModule error: %v", err)
	}
	calls := hook.snapshot()
	if len(calls) < 2 {
		t.Fatalf("expected hook to be consulted at least twice (once per capability), got %d calls", len(calls))
	}
	for _, call := range calls {
		if call.ModulePath != "example.com/mod" {
			t.Errorf("hook saw modulePath=%q, want example.com/mod", call.ModulePath)
		}
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
	_, err := service.LoadModule(context.Background(), bundle, reference, nil, stubUnpacker)
	if !errors.Is(err, module.ErrCapabilityDenied) {
		t.Fatalf("expected ErrCapabilityDenied, got %v", err)
	}
}

func TestLoadModuleRequiresUnpacker(t *testing.T) {
	t.Parallel()
	service := NewService()
	_, err := service.LoadModule(context.Background(),
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
	if err == nil {
		t.Fatalf("expected error when unpacker is nil")
	}
}

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

//go:build !js || !wasm

package adapters

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/schema"
	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestNewBytecodeStore_ReturnsStore(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	require.NotNil(t, store)
	assert.Equal(t, backing, store.store)
}

func TestNewBytecodeStore_NilStore(t *testing.T) {
	t.Parallel()

	store := NewBytecodeStore(nil)

	require.NotNil(t, store)
	assert.Nil(t, store.store)
}

func TestBytecodeStore_SaveCompiledFileSet_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backing func() rootfs.Store
		key     string
		wantErr string
	}{
		{name: "no backing store", backing: nil, key: "test-key", wantErr: "bytecode store requires a store and key"},
		{name: "empty key", backing: memoryStore(nil), key: "", wantErr: "bytecode store requires a store and key"},
		{
			name: "directory creation fails",
			backing: memoryStore(func(store *rootfs.MemoryStore) {
				store.MkdirAllErr = errors.New("permission denied")
			}),
			key:     "test-key",
			wantErr: "failed to create bytecode directory",
		},
		{
			name: "atomic write fails",
			backing: memoryStore(func(store *rootfs.MemoryStore) {
				store.WriteFileAtomicErr = errors.New("disk full")
			}),
			key:     "test-key",
			wantErr: "failed to write bytecode file atomically",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewBytecodeStore(backingStore(tt.backing))
			err := store.SaveCompiledFileSet(t.Context(), tt.key, codec.NewCompiledFileSetFromData(nil, nil, nil, nil))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestBytecodeStore_SaveCompiledFileSet_RefusesInconsistentGeneralConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		function *program.CompiledFunction
	}{
		{
			name: "more descriptors than constants",
			function: &program.CompiledFunction{
				GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{{Kind: descriptor.GeneralConstantCompositeZero}},
			},
		},
		{
			name: "a composite zero without a value",
			function: &program.CompiledFunction{
				GeneralConstants:           []reflect.Value{{}},
				GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{{Kind: descriptor.GeneralConstantCompositeZero}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backing := rootfs.NewMemoryStore()
			store := NewBytecodeStore(backing)

			err := store.SaveCompiledFileSet(t.Context(), "test-key", codec.NewCompiledFileSetFromData(tt.function, nil, nil, nil))

			require.ErrorContains(t, err, "general constants disagree with their descriptors",
				"a pool that cannot load again must not be written")
			_, readErr := backing.ReadFile("bytecode-test-key.bin")
			require.Error(t, readErr, "nothing is written")
		})
	}
}

func memoryStore(configure func(*rootfs.MemoryStore)) func() rootfs.Store {
	return func() rootfs.Store {
		store := rootfs.NewMemoryStore()
		if configure != nil {
			configure(store)
		}
		return store
	}
}

func backingStore(build func() rootfs.Store) rootfs.Store {
	if build == nil {
		return nil
	}
	return build()
}

func TestBytecodeStore_SaveCompiledFileSet_Success(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, nil, nil)
	err := store.SaveCompiledFileSet(t.Context(), "test-key", emptyFileSet)

	require.NoError(t, err)

	file := backing.GetFile("bytecode-test-key.bin")
	require.NotNil(t, file)
}

func TestBytecodeStore_LoadCompiledFileSet_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backing func() rootfs.Store
		key     string
		wantErr string
	}{
		{name: "no backing store", backing: nil, key: "test-key", wantErr: "bytecode store requires a store and key"},
		{name: "empty key", backing: memoryStore(nil), key: "", wantErr: ""},
		{name: "missing file", backing: memoryStore(nil), key: "nonexistent", wantErr: "cache miss or read error"},
		{
			name: "corrupt data",
			backing: memoryStore(func(store *rootfs.MemoryStore) {
				store.AddFile("bytecode-corrupt.bin", []byte("not valid flatbuffer data"))
			}),
			key:     "corrupt",
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := NewBytecodeStore(backingStore(tt.backing)).LoadCompiledFileSet(t.Context(), tt.key, nil)
			require.Error(t, err)
			require.Nil(t, result)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestBytecodeStore_SaveThenLoad_RoundTrip(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	entrypoints := map[string]uint16{"main": 0}
	initFunctions := []uint16{0}
	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, entrypoints, initFunctions)

	err := store.SaveCompiledFileSet(t.Context(), "roundtrip", emptyFileSet)
	require.NoError(t, err)

	registry := symtab.NewSymbolRegistry(nil)
	loaded, err := store.LoadCompiledFileSet(t.Context(), "roundtrip", registry)

	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, entrypoints, loaded.Entrypoints())
	assert.Equal(t, initFunctions, loaded.InitFunctions())
}

func TestBytecodeStore_InvalidateCache_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backing func() rootfs.Store
		key     string
		wantErr string
	}{
		{name: "no backing store", backing: nil, key: "test-key", wantErr: "bytecode store requires a store and key"},
		{name: "empty key", backing: memoryStore(nil), key: "", wantErr: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := NewBytecodeStore(backingStore(tt.backing)).invalidateCache(t.Context(), tt.key)
			require.Error(t, err)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestBytecodeStore_InvalidateCache_NonExistentFile_NoError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	err := store.invalidateCache(t.Context(), "nonexistent")

	assert.NoError(t, err)
}

func TestBytecodeStore_InvalidateCache_ExistingFile_RemovesIt(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, nil, nil)
	err := store.SaveCompiledFileSet(t.Context(), "to-delete", emptyFileSet)
	require.NoError(t, err)

	file := backing.GetFile("bytecode-to-delete.bin")
	require.NotNil(t, file)

	err = store.invalidateCache(t.Context(), "to-delete")
	require.NoError(t, err)
	require.Nil(t, backing.GetFile("bytecode-to-delete.bin"), "the cache file is removed")
}

func TestBytecodeStore_ImplementsPort(t *testing.T) {
	t.Parallel()

	var _ program.BytecodeStorePort = (*bytecodeStore)(nil)
}

func TestPackCompiledFileSetToBytesPacksWhatALoaderAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fileSet *program.CompiledFileSet
	}{
		{name: "an empty file set", fileSet: codec.NewCompiledFileSetFromData(nil, nil, nil, nil)},
		{
			name:    "a file set with entrypoints",
			fileSet: codec.NewCompiledFileSetFromData(nil, nil, map[string]uint16{"main": 0, "init": 1}, nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := PackCompiledFileSetToBytes(t.Context(), tt.fileSet)

			require.NoError(t, err)
			assert.NotEmpty(t, data)
		})
	}
}

func TestPackCompiledFileSetToBytesStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	root := &program.CompiledFunction{
		Name: "root",
		Body: []isa.Instruction{isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	data, err := PackCompiledFileSetToBytes(ctx, program.NewCompiledFileSet(root, nil, nil, nil))

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, data)
}

func TestPackCompiledFileSetToBytesRefusesWhatALoaderWouldRefuse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileSet  *program.CompiledFileSet
		wantErr  error
		contains string
	}{
		{name: "no file set", fileSet: nil, contains: "requires a compiled file set"},
		{
			name: "general constants that disagree with their descriptors",
			fileSet: codec.NewCompiledFileSetFromData(&program.CompiledFunction{
				GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{{Kind: descriptor.GeneralConstantCompositeZero}},
			}, nil, nil, nil),
			contains: "general constants disagree with their descriptors",
		},
		{
			name: "a payload over the loader's limit",
			fileSet: codec.NewCompiledFileSetFromData(nil, nil,
				map[string]uint16{strings.Repeat("x", schema.MaximumBytecodePayloadBytes): 0}, nil),
			wantErr: schema.ErrBytecodeTooLarge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := PackCompiledFileSetToBytes(t.Context(), tt.fileSet)

			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.contains != "" {
				require.ErrorContains(t, err, tt.contains)
			}
			require.Nil(t, data, "no bytes are returned with the error")
		})
	}
}

func TestEmbeddedFieldsAndBackReferencesSurvivePacking(t *testing.T) {
	t.Parallel()

	original := descriptor.TypeDescriptorData{
		Kind: uint8(descriptor.KindNamed), PackagePath: "example.com/p", Name: "L",
		Elem: &descriptor.TypeDescriptorData{
			Kind: uint8(descriptor.KindSlice),
			Elem: &descriptor.TypeDescriptorData{
				Kind: uint8(descriptor.KindStruct),
				Fields: []descriptor.TypeDescriptorFieldData{
					{Name: "Base", Embedded: true, Typ: descriptor.TypeDescriptorData{Kind: uint8(descriptor.KindNamed), PackagePath: "example.com/p", Name: "L", BackReference: true}},
					{Name: "Plain", Typ: descriptor.TypeDescriptorData{Kind: uint8(descriptor.KindBasic), BasicKind: uint8(reflect.Int)}},
				},
			},
		},
	}
	builder := flatbuffers.NewBuilder(256)
	builder.Finish(packTypeDescriptor(builder, original))
	payload := builder.FinishedBytes()

	decoded, err := unpackTypeDescriptor(schemagen.GetRootAsTypeDescriptor(payload, 0), 0, len(payload), newDescriptorDecodeCache())
	require.NoError(t, err)
	fields := decoded.Elem.Elem.Fields
	require.True(t, fields[0].Embedded)
	require.True(t, fields[0].Typ.BackReference)
	require.False(t, fields[1].Embedded)
	require.False(t, fields[1].Typ.BackReference)
}

func TestCallSiteArgumentTypeNamesSurvivePacking(t *testing.T) {
	t.Parallel()

	builder := flatbuffers.NewBuilder(256)
	site := codec.CallSiteData{
		IsNative:                  true,
		ArgumentStaticTypeNames:   []string{"Label", ""},
		ArgumentStaticTypeStrings: []string{"main.Label", "int"},
	}
	builder.Finish(packCallSite(builder, site))
	payload := builder.FinishedBytes()
	packed := schemagen.GetRootAsCallSite(payload, 0)

	names, err := unpackStrings(packed.ArgumentStaticTypeNamesLength(), packed.ArgumentStaticTypeNames, len(payload))
	require.NoError(t, err)
	require.Equal(t, site.ArgumentStaticTypeNames, names)
	typeStrings, err := unpackStrings(packed.ArgumentStaticTypeStringsLength(), packed.ArgumentStaticTypeStrings, len(payload))
	require.NoError(t, err)
	require.Equal(t, site.ArgumentStaticTypeStrings, typeStrings)
	empty, err := unpackStrings(0, packed.ArgumentStaticTypeNames, len(payload))
	require.NoError(t, err)
	require.Nil(t, empty)
}

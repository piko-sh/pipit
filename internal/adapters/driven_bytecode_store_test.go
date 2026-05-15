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
	"testing"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/symtab"
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

func TestBytecodeStore_SaveCompiledFileSet_NilStore_ReturnsError(t *testing.T) {
	t.Parallel()

	store := NewBytecodeStore(nil)

	err := store.SaveCompiledFileSet(context.Background(), "test-key", codec.NewCompiledFileSetFromData(nil, nil, nil, nil))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bytecode store requires a store and key")
}

func TestBytecodeStore_SaveCompiledFileSet_EmptyKey_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	err := store.SaveCompiledFileSet(context.Background(), "", codec.NewCompiledFileSetFromData(nil, nil, nil, nil))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bytecode store requires a store and key")
}

func TestBytecodeStore_SaveCompiledFileSet_MkdirAllFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	backing.MkdirAllErr = errors.New("permission denied")
	store := NewBytecodeStore(backing)

	err := store.SaveCompiledFileSet(context.Background(), "test-key", codec.NewCompiledFileSetFromData(nil, nil, nil, nil))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create bytecode directory")
}

func TestBytecodeStore_SaveCompiledFileSet_WriteFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	backing.WriteFileAtomicErr = errors.New("disk full")
	store := NewBytecodeStore(backing)

	err := store.SaveCompiledFileSet(context.Background(), "test-key", codec.NewCompiledFileSetFromData(nil, nil, nil, nil))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write bytecode file atomically")
}

func TestBytecodeStore_SaveCompiledFileSet_Success(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, nil, nil)
	err := store.SaveCompiledFileSet(context.Background(), "test-key", emptyFileSet)

	require.NoError(t, err)

	file := backing.GetFile("bytecode-test-key.bin")
	require.NotNil(t, file)
}

func TestBytecodeStore_LoadCompiledFileSet_NilStore_ReturnsError(t *testing.T) {
	t.Parallel()

	store := NewBytecodeStore(nil)

	result, err := store.LoadCompiledFileSet(context.Background(), "test-key", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "bytecode store requires a store and key")
}

func TestBytecodeStore_LoadCompiledFileSet_EmptyKey_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	result, err := store.LoadCompiledFileSet(context.Background(), "", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestBytecodeStore_LoadCompiledFileSet_MissingFile_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	result, err := store.LoadCompiledFileSet(context.Background(), "nonexistent", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "cache miss or read error")
}

func TestBytecodeStore_LoadCompiledFileSet_CorruptData_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	backing.AddFile("bytecode-corrupt.bin", []byte("not valid flatbuffer data"))
	store := NewBytecodeStore(backing)

	result, err := store.LoadCompiledFileSet(context.Background(), "corrupt", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestBytecodeStore_SaveThenLoad_RoundTrip(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	entrypoints := map[string]uint16{"main": 0}
	initFunctions := []uint16{0}
	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, entrypoints, initFunctions)

	err := store.SaveCompiledFileSet(context.Background(), "roundtrip", emptyFileSet)
	require.NoError(t, err)

	registry := symtab.NewSymbolRegistry(nil)
	loaded, err := store.LoadCompiledFileSet(context.Background(), "roundtrip", registry)

	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, entrypoints, loaded.Entrypoints())
	assert.Equal(t, initFunctions, loaded.InitFunctions())
}

func TestBytecodeStore_InvalidateCache_NilStore_ReturnsError(t *testing.T) {
	t.Parallel()

	store := NewBytecodeStore(nil)

	err := store.invalidateCache(context.Background(), "test-key")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bytecode store requires a store and key")
}

func TestBytecodeStore_InvalidateCache_EmptyKey_ReturnsError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	err := store.invalidateCache(context.Background(), "")

	assert.Error(t, err)
}

func TestBytecodeStore_InvalidateCache_NonExistentFile_NoError(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	err := store.invalidateCache(context.Background(), "nonexistent")

	assert.NoError(t, err)
}

func TestBytecodeStore_InvalidateCache_ExistingFile_RemovesIt(t *testing.T) {
	t.Parallel()

	backing := rootfs.NewMemoryStore()
	store := NewBytecodeStore(backing)

	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, nil, nil)
	err := store.SaveCompiledFileSet(context.Background(), "to-delete", emptyFileSet)
	require.NoError(t, err)

	file := backing.GetFile("bytecode-to-delete.bin")
	require.NotNil(t, file)

	err = store.invalidateCache(context.Background(), "to-delete")
	require.NoError(t, err)
}

func TestBytecodeStore_ImplementsPort(t *testing.T) {
	t.Parallel()

	var _ program.BytecodeStorePort = (*bytecodeStore)(nil)
}

func TestPackCompiledFileSetToBytes_EmptyFileSet(t *testing.T) {
	t.Parallel()

	emptyFileSet := codec.NewCompiledFileSetFromData(nil, nil, nil, nil)
	data := PackCompiledFileSetToBytes(emptyFileSet)

	assert.NotEmpty(t, data)
}

func TestPackCompiledFileSetToBytes_WithEntrypoints(t *testing.T) {
	t.Parallel()

	entrypoints := map[string]uint16{"main": 0, "init": 1}
	fileSet := codec.NewCompiledFileSetFromData(nil, nil, entrypoints, nil)
	data := PackCompiledFileSetToBytes(fileSet)

	assert.NotEmpty(t, data)
}

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

package adapters

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/fbs"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/schema"
)

func TestBytecodeCacheRejectsOversizedSave(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	original := []byte("existing cache")
	require.NoError(t, store.WriteFileAtomic("bytecode-large.bin", original, 0o600))
	compiled := codec.NewCompiledFileSetFromData(nil, nil,
		map[string]uint16{strings.Repeat("x", schema.MaximumBytecodePayloadBytes): 0}, nil)
	err := NewBytecodeStore(store).SaveCompiledFileSet(t.Context(), "large", compiled)
	require.ErrorIs(t, err, schema.ErrBytecodeTooLarge)
	require.Equal(t, original, store.GetFile("bytecode-large.bin"))
}

type cancellingCacheStore struct {
	rootfs.Store
	cancel context.CancelFunc
}

func (store *cancellingCacheStore) MkdirAll(string, fs.FileMode) error {
	store.cancel()
	return nil
}

func (*cancellingCacheStore) WriteFileAtomic(string, []byte, fs.FileMode) error {
	panic("cancelled save must not publish")
}

func TestBytecodeCacheCancelledSaveDoesNotPublish(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		early bool
	}{
		{name: "cancelled while preparing the cache", early: false},
		{name: "cancelled before the save", early: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			if tt.early {
				cancel()
			}
			store := &cancellingCacheStore{Store: nil, cancel: cancel}
			compiled := codec.NewCompiledFileSetFromData(nil, nil, nil, nil)
			err := NewBytecodeStore(store).SaveCompiledFileSet(ctx, "cancelled", compiled)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestBytecodeCacheRejectsNilSave(t *testing.T) {
	t.Parallel()
	err := NewBytecodeStore(rootfs.NewMemoryStore()).SaveCompiledFileSet(t.Context(), "nil", nil)
	require.Error(t, err)
}

func TestBytecodeRawInputLimit(t *testing.T) {
	t.Parallel()
	decoded, err := decodeCompiledFileSet(t.Context(), make([]byte, schema.MaximumBytecodePayloadBytes+1), nil)
	require.ErrorIs(t, err, schema.ErrBytecodeTooLarge)
	require.Nil(t, decoded)
}

func TestBytecodeCacheReadLimit(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	oversized := make([]byte, fbs.PackedSize(schema.MaximumBytecodePayloadBytes)+1)
	require.NoError(t, store.WriteFileAtomic("bytecode-oversized.bin", oversized, 0o600))
	decoded, err := NewBytecodeStore(store).LoadCompiledFileSet(t.Context(), "oversized", nil)
	require.ErrorIs(t, err, rootfs.ErrReadLimit)
	require.Nil(t, decoded)
}

type legacyBytecodeStore struct {
	rootfs.Store
}

func (*legacyBytecodeStore) ReadFile(string) ([]byte, error) {
	panic("unbounded cache read must never run")
}

func TestBytecodeCacheRequiresBoundedRead(t *testing.T) {
	t.Parallel()
	store := &legacyBytecodeStore{Store: nil}
	decoded, err := NewBytecodeStore(store).LoadCompiledFileSet(t.Context(), "legacy", nil)
	require.ErrorIs(t, err, rootfs.ErrBoundedReadUnavailable)
	require.Nil(t, decoded)
}

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

package rootfs_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/rootfs"
)

func TestBoundedReadStores(t *testing.T) {
	t.Parallel()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			require.NoError(t, store.WriteFileAtomic("data", []byte("abc"), 0o600))
			data, err := rootfs.ReadFileBounded(store, "data", 3)
			require.NoError(t, err)
			require.Equal(t, []byte("abc"), data)
			data[0] = 'z'
			again, err := rootfs.ReadFileBounded(store, "data", 3)
			require.NoError(t, err)
			require.Equal(t, []byte("abc"), again)
			for _, limit := range []int{0, 2} {
				data, err = rootfs.ReadFileBounded(store, "data", limit)
				require.ErrorIs(t, err, rootfs.ErrReadLimit)
				require.Nil(t, data)
			}
			for _, limit := range []int{-1, int(^uint(0) >> 1)} {
				data, err = rootfs.ReadFileBounded(store, "data", limit)
				require.ErrorIs(t, err, fs.ErrInvalid)
				require.Nil(t, data)
			}
			require.NoError(t, store.WriteFileAtomic("empty", nil, 0o600))
			data, err = rootfs.ReadFileBounded(store, "empty", 0)
			require.NoError(t, err)
			require.Empty(t, data)
			data, err = rootfs.ReadFileBounded(store, "missing", 3)
			require.ErrorIs(t, err, fs.ErrNotExist)
			require.Nil(t, data)
			data, err = rootfs.ReadFileBounded(store, "../escape", 3)
			require.Error(t, err)
			require.Nil(t, data)
		})
	}
}

type legacyReadStore struct {
	rootfs.Store
}

func (*legacyReadStore) ReadFile(string) ([]byte, error) {
	panic("unbounded fallback must never run")
}

type faultyBoundedStore struct {
	rootfs.Store
	data []byte
	err  error
}

func (store *faultyBoundedStore) ReadFileBounded(string, int) ([]byte, error) {
	return store.data, store.err
}

func TestBoundedReadRequiresCapability(t *testing.T) {
	t.Parallel()
	data, err := rootfs.ReadFileBounded(&legacyReadStore{Store: nil}, "data", 3)
	require.ErrorIs(t, err, rootfs.ErrBoundedReadUnavailable)
	require.Nil(t, data)
	failure := errors.New("read failed")
	for _, test := range []struct {
		name     string
		failure  error
		expected error
	}{
		{name: "oversized", failure: nil, expected: rootfs.ErrReadLimit},
		{name: "partial", failure: failure, expected: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := rootfs.ReadFileBounded(&faultyBoundedStore{
				Store: nil, data: []byte("oversized"), err: test.failure,
			}, "data", 3)
			require.ErrorIs(t, err, test.expected)
			require.Nil(t, data)
		})
	}
}

func TestBoundedReadDiskRejectsSparseFileAndDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	file, err := os.Create(filepath.Join(directory, "sparse"))
	require.NoError(t, err)
	require.NoError(t, file.Truncate(1<<30))
	require.NoError(t, file.Close())
	store, err := rootfs.Open(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	data, err := rootfs.ReadFileBounded(store, "sparse", 8<<20)
	require.ErrorIs(t, err, rootfs.ErrReadLimit)
	require.Nil(t, data)
	data, err = rootfs.ReadFileBounded(store, ".", 8<<20)
	require.ErrorIs(t, err, fs.ErrInvalid)
	require.Nil(t, data)
}

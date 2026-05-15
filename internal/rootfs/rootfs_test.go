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

type storeFactory struct {
	name               string
	open               func(t *testing.T) rootfs.Store
	removeMissingFails bool
}

func storeFactories() []storeFactory {
	return []storeFactory{
		{
			name:               "memory",
			open:               func(t *testing.T) rootfs.Store { t.Helper(); return rootfs.NewMemoryStore() },
			removeMissingFails: false,
		},
		{
			name: "disk",
			open: func(t *testing.T) rootfs.Store {
				t.Helper()
				store, err := rootfs.Open(t.TempDir())
				require.NoError(t, err)
				return store
			},
			removeMissingFails: true,
		},
	}
}

func TestStoreWriteReadRemoveRoundTrip(t *testing.T) {
	t.Parallel()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)

			require.NoError(t, store.MkdirAll("a/b", 0o755))
			require.NoError(t, store.WriteFileAtomic("a/b/f.bin", []byte("first"), 0o600))

			got, err := store.ReadFile("a/b/f.bin")
			require.NoError(t, err)
			require.Equal(t, []byte("first"), got)

			require.NoError(t, store.WriteFileAtomic("a/b/f.bin", []byte("second, longer"), 0o600))
			got, err = store.ReadFile("a/b/f.bin")
			require.NoError(t, err)
			require.Equal(t, []byte("second, longer"), got)

			require.NoError(t, store.WriteFileAtomic("a/b/f.bin", nil, 0o600))
			got, err = store.ReadFile("a/b/f.bin")
			require.NoError(t, err)
			require.Empty(t, got)

			require.NoError(t, store.Remove("a/b/f.bin"))
			_, err = store.ReadFile("a/b/f.bin")
			require.ErrorIs(t, err, fs.ErrNotExist)
		})
	}
}

func TestStoreReadMissingFileIsNotExist(t *testing.T) {
	t.Parallel()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)
			_, err := store.ReadFile("never/written")
			require.ErrorIs(t, err, fs.ErrNotExist)
		})
	}
}

func TestStoreRemoveMissingFile(t *testing.T) {
	t.Parallel()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)
			err := store.Remove("absent")
			if factory.removeMissingFails {
				require.ErrorIs(t, err, fs.ErrNotExist)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStoreMkdirAllIsIdempotent(t *testing.T) {
	t.Parallel()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)
			require.NoError(t, store.MkdirAll("x/y/z", 0o755))
			require.NoError(t, store.MkdirAll("x/y/z", 0o755))
			require.NoError(t, store.MkdirAll("x", 0o755))
			require.NoError(t, store.MkdirAll(".", 0o755))
		})
	}
}

func TestStoreRejectsNamesEscapingTheRoot(t *testing.T) {
	t.Parallel()
	escaping := []string{"../escape", "a/../../escape", "../", "a/b/../../../escape"}
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)
			for _, name := range escaping {
				require.Error(t, store.MkdirAll(name, 0o755), "MkdirAll(%q)", name)
				require.Error(t, store.WriteFileAtomic(name, []byte("x"), 0o600), "WriteFileAtomic(%q)", name)
				_, err := store.ReadFile(name)
				require.Error(t, err, "ReadFile(%q)", name)
				require.Error(t, store.Remove(name), "Remove(%q)", name)
			}
		})
	}
}

func TestStoreDotSegmentsInsideTheRootAreFine(t *testing.T) {
	t.Parallel()
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			t.Parallel()
			store := factory.open(t)
			require.NoError(t, store.MkdirAll("dir/sub", 0o755))
			require.NoError(t, store.WriteFileAtomic("dir/sub/../inside", []byte("ok"), 0o600))
			got, err := store.ReadFile("dir/./inside")
			require.NoError(t, err)
			require.Equal(t, []byte("ok"), got)
		})
	}
}

func TestMemoryStoreRootNameIsNotAFile(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	for _, name := range []string{".", "/", "", "./"} {
		require.NoError(t, store.MkdirAll(name, 0o755), "MkdirAll(%q) asks for the root to exist", name)
		err := store.WriteFileAtomic(name, []byte("x"), 0o600)
		require.Error(t, err, "WriteFileAtomic(%q)", name)
		require.ErrorContains(t, err, "names the root")
		_, err = store.ReadFile(name)
		require.ErrorContains(t, err, "names the root", "ReadFile(%q)", name)
		require.ErrorContains(t, store.Remove(name), "names the root", "Remove(%q)", name)
		require.Nil(t, store.GetFile(name))
	}
}

func TestMemoryStoreEscapeErrorMentionsTheRoot(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	err := store.WriteFileAtomic("../x", []byte("x"), 0o600)
	require.ErrorContains(t, err, "escapes the store root")
	require.ErrorContains(t, err, `"../x"`)
	require.Nil(t, store.GetFile("../x"))
}

func TestMemoryStoreNormalisesNames(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	require.NoError(t, store.WriteFileAtomic("a/./b//c", []byte("v"), 0o600))
	for _, alias := range []string{"a/b/c", "/a/b/c", "./a/b/c", "a//b/./c"} {
		got, err := store.ReadFile(alias)
		require.NoError(t, err, alias)
		require.Equal(t, []byte("v"), got, alias)
		require.Equal(t, []byte("v"), store.GetFile(alias), alias)
	}
}

func TestMemoryStoreCopiesOnWriteAndRead(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	buffer := []byte("original")
	require.NoError(t, store.WriteFileAtomic("f", buffer, 0o600))
	buffer[0] = 'X'

	got, err := store.ReadFile("f")
	require.NoError(t, err)
	require.Equal(t, []byte("original"), got, "the store must not alias the caller's buffer")

	got[0] = 'Y'
	again, err := store.ReadFile("f")
	require.NoError(t, err)
	require.Equal(t, []byte("original"), again, "ReadFile must hand out a copy")

	require.Equal(t, []byte("original"), store.GetFile("f"))
}

func TestMemoryStoreAddFileAndGetFile(t *testing.T) {
	t.Parallel()
	store := rootfs.NewMemoryStore()
	require.Nil(t, store.GetFile("seeded"))
	store.AddFile("seeded", []byte{0xde, 0xad})
	require.Equal(t, []byte{0xde, 0xad}, store.GetFile("seeded"))
	got, err := store.ReadFile("seeded")
	require.NoError(t, err)
	require.Equal(t, []byte{0xde, 0xad}, got)

	store.AddFile("../ignored", []byte("x"))
	require.Nil(t, store.GetFile("../ignored"))
	require.Nil(t, store.GetFile("ignored"))
}

func TestMemoryStoreInjectedErrors(t *testing.T) {
	t.Parallel()
	errInjected := errors.New("injected")
	cases := []struct {
		name  string
		setup func(*rootfs.MemoryStore)
		call  func(*rootfs.MemoryStore) error
	}{
		{
			name:  "MkdirAll",
			setup: func(m *rootfs.MemoryStore) { m.MkdirAllErr = errInjected },
			call:  func(m *rootfs.MemoryStore) error { return m.MkdirAll("d", 0o755) },
		},
		{
			name:  "WriteFileAtomic",
			setup: func(m *rootfs.MemoryStore) { m.WriteFileAtomicErr = errInjected },
			call:  func(m *rootfs.MemoryStore) error { return m.WriteFileAtomic("f", []byte("x"), 0o600) },
		},
		{
			name:  "ReadFile",
			setup: func(m *rootfs.MemoryStore) { m.AddFile("f", []byte("x")); m.ReadFileErr = errInjected },
			call:  func(m *rootfs.MemoryStore) error { _, err := m.ReadFile("f"); return err },
		},
		{
			name:  "Remove",
			setup: func(m *rootfs.MemoryStore) { m.AddFile("f", []byte("x")); m.RemoveErr = errInjected },
			call:  func(m *rootfs.MemoryStore) error { return m.Remove("f") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := rootfs.NewMemoryStore()
			tc.setup(store)
			require.ErrorIs(t, tc.call(store), errInjected)
		})
	}
}

func TestOpenRejectsUnusableDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plain-file")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	cases := []struct {
		name string
		dir  string
	}{
		{name: "missing directory", dir: filepath.Join(dir, "does-not-exist")},
		{name: "regular file", dir: filePath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, err := rootfs.Open(tc.dir)
			require.Error(t, err)
			require.ErrorContains(t, err, "rootfs: opening")
			require.Nil(t, store)
		})
	}
}

func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDiskWriteFileAtomicLeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := rootfs.Open(dir)
	require.NoError(t, err)

	require.NoError(t, store.WriteFileAtomic("f", []byte("one"), 0o600))
	require.NoError(t, store.WriteFileAtomic("f", []byte("two"), 0o600))
	require.Equal(t, []string{"f"}, entryNames(t, dir))

	require.NoError(t, store.MkdirAll("nested/deep", 0o755))
	require.NoError(t, store.WriteFileAtomic("nested/deep/g", []byte("three"), 0o600))
	require.Equal(t, []string{"g"}, entryNames(t, filepath.Join(dir, "nested", "deep")))

	got, err := os.ReadFile(filepath.Join(dir, "f"))
	require.NoError(t, err)
	require.Equal(t, []byte("two"), got)

	info, err := os.Stat(filepath.Join(dir, "f"))
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}

func TestDiskWriteFileAtomicRenameFailureRemovesTemporary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := rootfs.Open(dir)
	require.NoError(t, err)

	require.NoError(t, store.MkdirAll("target", 0o755))
	require.NoError(t, store.WriteFileAtomic("target/child", []byte("x"), 0o600))

	err = store.WriteFileAtomic("target", []byte("cannot replace a directory"), 0o600)
	require.Error(t, err)
	require.ErrorContains(t, err, "renaming")
	require.Equal(t, []string{"target"}, entryNames(t, dir), "the temporary must be removed after a failed rename")
}

func TestDiskWriteFileAtomicMissingParentFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := rootfs.Open(dir)
	require.NoError(t, err)

	err = store.WriteFileAtomic("missing/f", []byte("x"), 0o600)
	require.Error(t, err)
	require.ErrorContains(t, err, "writing")
	require.Empty(t, entryNames(t, dir))
}

func TestDiskSymlinkCannotEscapeTheRoot(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600))

	dir := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link")))
	store, err := rootfs.Open(dir)
	require.NoError(t, err)

	_, err = store.ReadFile("link/secret")
	require.Error(t, err)
	require.Error(t, store.WriteFileAtomic("link/planted", []byte("x"), 0o600))
	_, statErr := os.Stat(filepath.Join(outside, "planted"))
	require.ErrorIs(t, statErr, fs.ErrNotExist, "nothing may be written outside the root")
	require.Error(t, store.Remove("link/secret"))
	_, statErr = os.Stat(filepath.Join(outside, "secret"))
	require.NoError(t, statErr, "the outside file must survive")
}

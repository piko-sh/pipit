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

package modloader

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytecodeCachePairReplacement(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	require.NoError(t, cache.Put("example/lib", "v1", BytecodeCacheEntry{Bytecode: []byte("old"), TypesExport: []byte("old types")}))
	require.NoError(t, cache.Put("example/lib", "v1", BytecodeCacheEntry{Bytecode: []byte("new")}))
	entry, hit, err := cache.Get("example/lib", "v1")
	require.NoError(t, err, "a replacement drops the old type export")
	require.True(t, hit)
	require.Equal(t, "new", string(entry.Bytecode))
	require.Empty(t, entry.TypesExport)
}

func TestBytecodeCacheConcurrentPairs(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	require.NoError(t, cache.Put("module", "version", BytecodeCacheEntry{Bytecode: []byte("initial"), TypesExport: []byte("initial")}))
	failures := make([]error, 8)
	var workers sync.WaitGroup
	for index := range failures {
		workers.Go(func() { failures[index] = exerciseCachePairs(cache, index) })
	}
	workers.Wait()
	for index, err := range failures {
		require.NoError(t, err, "worker %d", index)
	}
}

func exerciseCachePairs(cache *BytecodeCache, index int) error {
	marker := bytes.Repeat([]byte{byte(index + 1)}, 100+index)
	for range 20 {
		if err := cache.Put("module", "version", BytecodeCacheEntry{Bytecode: marker, TypesExport: marker}); err != nil {
			return err
		}
		entry, hit, err := cache.Get("module", "version")
		if err != nil {
			return err
		}
		if !hit {
			return errors.New("the entry is not cached")
		}
		if !bytes.Equal(entry.Bytecode, entry.TypesExport) {
			return errors.New("a reader saw parts of two publications")
		}
	}
	return nil
}

func TestBytecodeCachePathsCannotEscapeRoot(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	for _, name := range []string{"../../outside", "/absolute", "..\\outside", "name\x00suffix"} {
		path := cache.path(name, "../../version")
		relative, err := filepath.Rel(cache.root, path)
		require.NoError(t, err, "an untrusted name stays inside the cache root: path=%q", path)
		require.True(t, filepath.IsLocal(relative))
		require.Equal(t, "v2", filepath.Dir(relative))
		require.NoError(t, cache.Put(name, "../../version", BytecodeCacheEntry{Bytecode: []byte("data")}))
	}
	require.NotEqual(t, cache.path("ab", "c"), cache.path("a", "bc"), "key framing is unambiguous")
}

func TestBytecodeCacheRejectsMalformedEntries(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	require.NoError(t, cache.Put("module", "version", BytecodeCacheEntry{Bytecode: []byte("code"), TypesExport: []byte("types")}))
	path := cache.path("module", "version")
	valid, err := os.ReadFile(path)
	require.NoError(t, err)
	overflow := bytes.Clone(valid)
	binary.BigEndian.PutUint64(overflow[8:16], ^uint64(0))
	for _, data := range [][]byte{nil, valid[:8], valid[:len(valid)-1], append(bytes.Clone(valid), 0), overflow} {
		require.NoError(t, os.WriteFile(path, data, 0600))
		entry, hit, err := cache.Get("module", "version")
		require.Error(t, err, "invalid cache data is rejected")
		require.False(t, hit)
		require.Nil(t, entry)
	}
	require.NoError(t, os.Truncate(path, maximumCacheEntry+1))
	_, hit, err := cache.Get("module", "version")
	require.Error(t, err, "an oversized cache file is rejected")
	require.False(t, hit)
	require.Error(t, cache.Put("module", "version", BytecodeCacheEntry{Bytecode: make([]byte, maximumCachedPart+1)}), "an oversized bytecode publication is refused")
}

func FuzzDecodeCacheEntry(f *testing.F) {
	valid := make([]byte, bytecodeCacheHeader+1)
	copy(valid, bytecodeCacheMagic)
	binary.BigEndian.PutUint64(valid[8:16], 1)
	f.Add(valid)
	f.Add([]byte("legacy"))
	f.Fuzz(func(t *testing.T, data []byte) {
		entry, err := decodeCacheEntry(data)
		if err == nil {
			require.NotEmpty(t, entry.Bytecode, "a decoded entry has bytecode")
			require.LessOrEqual(t, len(entry.Bytecode), maximumCachedPart, "the decoder bounds the bytecode")
			require.LessOrEqual(t, len(entry.TypesExport), maximumCachedPart, "the decoder bounds the types export")
		}
	})
}

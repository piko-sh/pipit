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
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestBytecodeCachePairReplacement(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	if err := cache.Put("example/lib", "v1", BytecodeCacheEntry{Bytecode: []byte("old"), TypesExport: []byte("old types")}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put("example/lib", "v1", BytecodeCacheEntry{Bytecode: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	entry, hit, err := cache.Get("example/lib", "v1")
	if err != nil || !hit || string(entry.Bytecode) != "new" || len(entry.TypesExport) != 0 {
		t.Fatalf("old type export survived replacement: entry=%+v hit=%v error=%v", entry, hit, err)
	}
}

func TestBytecodeCacheConcurrentPairs(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	if err := cache.Put("module", "version", BytecodeCacheEntry{Bytecode: []byte("initial"), TypesExport: []byte("initial")}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for index := range 8 {
		workers.Go(func() {
			marker := bytes.Repeat([]byte{byte(index + 1)}, 100+index)
			for range 20 {
				if err := cache.Put("module", "version", BytecodeCacheEntry{Bytecode: marker, TypesExport: marker}); err != nil {
					t.Error(err)
					return
				}
				entry, hit, err := cache.Get("module", "version")
				if err != nil || !hit {
					t.Errorf("read failed: hit=%v error=%v", hit, err)
					return
				}
				if !bytes.Equal(entry.Bytecode, entry.TypesExport) {
					t.Error("reader observed payloads from different publications")
					return
				}
			}
		})
	}
	workers.Wait()
}

func TestBytecodeCachePathsCannotEscapeRoot(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	for _, name := range []string{"../../outside", "/absolute", "..\\outside", "name\x00suffix"} {
		path := cache.path(name, "../../version")
		relative, err := filepath.Rel(cache.root, path)
		if err != nil || !filepath.IsLocal(relative) || filepath.Dir(relative) != "v2" {
			t.Fatalf("untrusted name escaped cache root: path=%q error=%v", path, err)
		}
		if err := cache.Put(name, "../../version", BytecodeCacheEntry{Bytecode: []byte("data")}); err != nil {
			t.Fatal(err)
		}
	}
	if cache.path("ab", "c") == cache.path("a", "bc") {
		t.Fatal("ambiguous key framing")
	}
}

func TestBytecodeCacheRejectsMalformedEntries(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	if err := cache.Put("module", "version", BytecodeCacheEntry{Bytecode: []byte("code"), TypesExport: []byte("types")}); err != nil {
		t.Fatal(err)
	}
	path := cache.path("module", "version")
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	overflow := bytes.Clone(valid)
	binary.BigEndian.PutUint64(overflow[8:16], ^uint64(0))
	for _, data := range [][]byte{nil, valid[:8], valid[:len(valid)-1], append(bytes.Clone(valid), 0), overflow} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if entry, hit, err := cache.Get("module", "version"); err == nil || hit || entry != nil {
			t.Fatalf("invalid cache data accepted: hit=%v error=%v", hit, err)
		}
	}
	if err := os.Truncate(path, maximumCacheEntry+1); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := cache.Get("module", "version"); err == nil || hit {
		t.Fatal("oversized cache file accepted")
	}
	if err := cache.Put("module", "version", BytecodeCacheEntry{Bytecode: make([]byte, maximumCachedPart+1)}); err == nil {
		t.Fatal("oversized bytecode publication accepted")
	}
}

func FuzzDecodeCacheEntry(f *testing.F) {
	valid := make([]byte, bytecodeCacheHeader+1)
	copy(valid, bytecodeCacheMagic)
	binary.BigEndian.PutUint64(valid[8:16], 1)
	f.Add(valid)
	f.Add([]byte("legacy"))
	f.Fuzz(func(t *testing.T, data []byte) {
		entry, err := decodeCacheEntry(data)
		if err == nil && (len(entry.Bytecode) == 0 || len(entry.Bytecode) > maximumCachedPart || len(entry.TypesExport) > maximumCachedPart) {
			t.Fatal("decoder returned an unbounded payload")
		}
	})
}

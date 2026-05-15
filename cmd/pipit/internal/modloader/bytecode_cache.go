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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// bytecodeCacheMagic is the file header that identifies a bytecode cache entry on disk.
	bytecodeCacheMagic = "PIPITBC2"

	// bytecodeCacheHeader is the byte length of the fixed header preceding the two payloads.
	bytecodeCacheHeader = 24

	// maximumCachedPart is the ceiling on each individual payload (bytecode or type export)
	// in bytes.
	maximumCachedPart = 8 << 20

	// maximumCacheEntry is the ceiling on a complete entry including header and both
	// payloads.
	maximumCacheEntry = bytecodeCacheHeader + 2*maximumCachedPart
)

// BytecodeCacheEntry keeps bytecode and its corresponding type export together.
type BytecodeCacheEntry struct {
	// Bytecode holds the compiled bytecode payload.
	Bytecode []byte

	// TypesExport holds the serialised type export that accompanies the bytecode.
	TypesExport []byte
}

// BytecodeCache atomically stores complete entries beneath a host-controlled root. Hashed
// names do not authenticate entries against malicious cache writers.
type BytecodeCache struct {
	// root is the on-disk cache directory.
	root string

	// identity binds cache keys to a particular acquired dependency set.
	identity string
}

// NewBytecodeCache constructs an optional host-selected cache.
//
// Takes root (string) which disables caching when empty.
//
// Returns *BytecodeCache using versioned, hashed entry names.
func NewBytecodeCache(root string) *BytecodeCache { return &BytecodeCache{root: root, identity: ""} }

// WithIdentity adds an input binding to an independent cache view. It does not mutate
// another invocation's cache view or authenticate cache writers.
//
// Takes identity (string) which binds the complete acquired dependency set.
//
// Returns *BytecodeCache which shares storage but not keys with different identities.
func (cache *BytecodeCache) WithIdentity(identity string) *BytecodeCache {
	if cache == nil {
		return nil
	}
	digest := sha256.New()
	fmt.Fprintf(digest, "pipit-cache-context-v1:%d:%s%d:%s", len(cache.identity), cache.identity, len(identity), identity)
	return &BytecodeCache{root: cache.root, identity: fmt.Sprintf("%x", digest.Sum(nil))}
}

// Get reads one bounded entry, never separately published bytecode and types.
//
// Takes modulePath (string) which identifies the compiled package.
// Takes version (string) which contains the caller's version and source identity.
//
// Returns *BytecodeCacheEntry which is the corresponding payload on a hit.
// Returns bool which reports whether a complete entry exists.
// Returns error which reports malformed entries or filesystem failures.
func (cache *BytecodeCache) Get(modulePath, version string) (*BytecodeCacheEntry, bool, error) {
	if cache == nil || cache.root == "" {
		return nil, false, nil
	}
	file, err := os.OpenFile(cache.path(modulePath, version), identityOpenFlags, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("modloader: opening bytecode cache: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() < bytecodeCacheHeader || info.Size() > maximumCacheEntry {
		return nil, false, errors.New("modloader: invalid bytecode cache file size or type")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumCacheEntry+1))
	if err != nil {
		return nil, false, err
	}
	entry, err := decodeCacheEntry(data)
	return entry, err == nil, err
}

// Put publishes both payloads through one atomic rename of a private file. Concurrent
// readers see a complete old or new pair, including empty type exports.
//
// Takes modulePath (string) which identifies the compiled package.
// Takes version (string) which contains the caller's version and source identity.
// Takes entry (BytecodeCacheEntry) which supplies bounded compiled payloads.
//
// Returns error for oversized data or failed atomic publication.
func (cache *BytecodeCache) Put(modulePath, version string, entry BytecodeCacheEntry) error {
	if cache == nil || cache.root == "" {
		return nil
	}
	if len(entry.Bytecode) == 0 || len(entry.Bytecode) > maximumCachedPart || len(entry.TypesExport) > maximumCachedPart {
		return errors.New("modloader: invalid bytecode cache payload size")
	}
	data := make([]byte, bytecodeCacheHeader+len(entry.Bytecode)+len(entry.TypesExport))
	copy(data, bytecodeCacheMagic)
	binary.BigEndian.PutUint64(data[8:16], uint64(len(entry.Bytecode)))
	binary.BigEndian.PutUint64(data[16:24], uint64(len(entry.TypesExport)))
	copy(data[bytecodeCacheHeader:], entry.Bytecode)
	copy(data[bytecodeCacheHeader+len(entry.Bytecode):], entry.TypesExport)
	path := cache.path(modulePath, version)
	if err := os.MkdirAll(filepath.Dir(path), cacheDirMode); err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

// path keeps module names and versions out of filesystem path components.
//
// Takes modulePath (string) which identifies the package.
// Takes version (string) which identifies compilation inputs.
//
// Returns string beneath the configured root's versioned directory.
func (cache *BytecodeCache) path(modulePath, version string) string {
	digest := sha256.New()
	fmt.Fprintf(digest, "pipit-bytecode-cache-v2:%d:%s%d:%s", len(modulePath), modulePath, len(version), version)
	fmt.Fprintf(digest, "%d:%s", len(cache.identity), cache.identity)
	return filepath.Join(cache.root, "v2", fmt.Sprintf("%x.entry", digest.Sum(nil)))
}

// decodeCacheEntry validates framing before slicing or returning payloads.
//
// Takes data ([]byte) which contains one bounded entry.
//
// Returns *BytecodeCacheEntry containing matching bytecode and type export.
// Returns error for unknown formats, excessive lengths or incomplete/trailing data.
func decodeCacheEntry(data []byte) (*BytecodeCacheEntry, error) {
	if len(data) < bytecodeCacheHeader || len(data) > maximumCacheEntry || string(data[:8]) != bytecodeCacheMagic {
		return nil, errors.New("modloader: invalid bytecode cache header")
	}
	bytecodeSize := binary.BigEndian.Uint64(data[8:16])
	typesSize := binary.BigEndian.Uint64(data[16:24])
	if bytecodeSize == 0 || bytecodeSize > maximumCachedPart || typesSize > maximumCachedPart ||
		uint64(len(data)) != bytecodeCacheHeader+bytecodeSize+typesSize {
		return nil, errors.New("modloader: invalid bytecode cache lengths")
	}
	boundary := bytecodeCacheHeader + int(bytecodeSize)
	return &BytecodeCacheEntry{Bytecode: data[bytecodeCacheHeader:boundary], TypesExport: data[boundary:]}, nil
}

// atomicWriteFile syncs and publishes one private cache file.
//
// Takes dest (string) which is the final cache path.
// Takes data ([]byte) which contains the complete encoded entry.
//
// Returns error when writing, syncing, closing or renaming fails.
func atomicWriteFile(dest string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(dest), ".pipit-bytecode-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), dest)
}

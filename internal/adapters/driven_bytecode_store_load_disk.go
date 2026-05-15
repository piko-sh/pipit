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
	"fmt"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/fbs"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/schema"
)

// LoadCompiledFileSet reads and deserialises a previously saved compiled file set,
// reconstructing runtime types and values via the provided SymbolRegistry.
//
// Takes key (string) which identifies the cached bytecode to load.
// Takes registry (*symtab.SymbolRegistry) which provides symbol and type lookups for
// runtime reconstruction.
//
// Returns *program.CompiledFileSet which is the reconstructed compiled file set.
// Returns error when the store is nil, the key is empty, the cache is missing, the schema
// version has changed, or reconstruction fails.
func (bytecodeStore *bytecodeStore) LoadCompiledFileSet(ctx context.Context, key string, registry *symtab.SymbolRegistry) (*program.CompiledFileSet, error) {
	if bytecodeStore.store == nil || key == "" {
		return nil, errors.New("bytecode store requires a store and key")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("bytecode load cancelled before read: %w", err)
	}

	fileName := fmt.Sprintf("bytecode-%s.bin", key)
	data, err := rootfs.ReadFileBounded(bytecodeStore.store, fileName, fbs.PackedSize(schema.MaximumBytecodePayloadBytes))
	if err != nil {
		return nil, fmt.Errorf("bytecode cache miss or read error for key %s: %w", key, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("bytecode load cancelled after read: %w", err)
	}

	payload, err := schema.Unpack(data)
	if err != nil {
		removeError := bytecodeStore.store.Remove(fileName)
		if errors.Is(err, fbs.ErrSchemaVersionMismatch) {
			return nil, errors.Join(fmt.Errorf("bytecode schema version mismatch for key %s, invalidated", key), removeError)
		}
		return nil, errors.Join(fmt.Errorf("failed to unpack versioned bytecode for key %s: %w", key, err), removeError)
	}

	fileSet, err := decodeCompiledFileSet(ctx, payload, registry)
	if err != nil {
		removeError := bytecodeStore.store.Remove(fileName)
		return nil, errors.Join(fmt.Errorf("failed to reconstruct bytecode for key %s: %w", key, err), removeError)
	}
	return fileSet, nil
}

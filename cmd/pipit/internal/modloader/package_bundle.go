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
	"context"
	"fmt"
	"sync"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/module"
)

var (
	// stdlibPackageSetCache holds the lazily built stdlib import-path set shared by
	// StdlibPackageSet.
	stdlibPackageSetCache map[string]struct{}

	// stdlibPackageSetOnce guards the one-time build of stdlibPackageSetCache.
	stdlibPackageSetOnce sync.Once
)

// StdlibPackageSet returns the import paths pipit recognises as standard library, so an
// import is classified rather than fetched as a module. It reads the name index, not the
// symbol tables: getting this set wrong sends the resolver looking for "strings" on a
// module proxy.
//
// Returns map[string]struct{} which holds the stdlib import paths. Callers must not
// modify it.
func StdlibPackageSet() map[string]struct{} {
	stdlibPackageSetOnce.Do(func() {
		paths := pipit.StandardLibraryPaths()
		stdlibPackageSetCache = make(map[string]struct{}, len(paths))
		for _, importPath := range paths {
			stdlibPackageSetCache[importPath] = struct{}{}
		}
	})
	return stdlibPackageSetCache
}

// LoadOrCompilePackage compiles a sub-package into a bundle ready for LoadModule,
// consulting the bytecode cache first.
//
// Each sub-package of a multi-package module becomes its own bundle so the pipit symbol
// registry is populated incrementally, in topo order. pipit's bytecode wire format
// references intra-module sibling symbols via the registry, which must be populated
// before the unpacker runs. bundle.Descriptor.Ref.Pin is always populated with the bundle
// fingerprint, on both the cache-hit and the compile path, so callers can pass it as the
// integrity pin to LoadModule, which rejects unpinned refs by default.
//
// Takes interpreter (*pipit.Interpreter) which compiles the package.
// Takes cache (*BytecodeCache) which stores compiled bytecode.
// Takes importPath (string) which is the full registered import path.
// Takes version (string) which is the module version.
// Takes sources (map[string]string) which holds the sub-package files.
// Takes warn (func(error)) which reports a failed cache write; the write is best effort,
// so it does not fail the load.
//
// Returns *module.Bundle which wraps the compiled or cached package.
// Returns bool which is true when the bundle was served from cache.
// Returns error when the cache read, compilation, or fingerprint fails.
func LoadOrCompilePackage(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	cache *BytecodeCache,
	importPath, version string,
	sources map[string]string,
	warn func(error),
) (*module.Bundle, bool, error) {
	cacheVersion := PackageCacheVersion(version, sources)
	if entry, hit, err := cache.Get(importPath, cacheVersion); err != nil {
		return nil, false, err
	} else if hit {
		bundle := &module.Bundle{
			Descriptor: &module.Descriptor{
				SchemaVersion: module.DescriptorVersion,
				Ref:           module.Ref{Path: importPath, Version: version, Pin: ""},
				Entrypoints:   nil, Annotations: nil, StdlibVersion: "", Capabilities: nil, SymbolAllowlist: nil},
			Bytecode:    entry.Bytecode,
			TypesExport: entry.TypesExport,
		}
		if err := EnsureBundlePin(bundle); err != nil {
			return nil, false, err
		}
		return bundle, true, nil
	}

	descriptor := module.Descriptor{
		SchemaVersion: module.DescriptorVersion,
		Ref:           module.Ref{Path: importPath, Version: version, Pin: ""},
		Entrypoints:   nil, Annotations: nil, StdlibVersion: "", Capabilities: nil, SymbolAllowlist: nil}
	packages := map[string]map[string]string{"": sources}
	bundle, err := interpreter.PackageModule(ctx, descriptor, importPath, packages, pipit.PackCompiledFileSetToBytes)
	if err != nil {
		return nil, false, fmt.Errorf("compile package: %w", err)
	}
	if err := EnsureBundlePin(bundle); err != nil {
		return nil, false, err
	}
	if err := cache.Put(importPath, cacheVersion, BytecodeCacheEntry{
		Bytecode:    bundle.Bytecode,
		TypesExport: bundle.TypesExport,
	}); err != nil {
		warn(fmt.Errorf("bytecode cache write failed for %s@%s: %w", importPath, version, err))
	}
	return bundle, false, nil
}

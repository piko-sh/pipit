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
	"fmt"
	"io"
	"maps"
	"slices"
)

// PackageCacheVersion binds an entry to exact acquired package sources. Interpreter
// policy and helper ABI need additional identity binding.
//
// Takes version (string) which identifies the resolved module version.
// Takes sources (map[string]string) which contains acquired package files.
//
// Returns string containing the version and deterministic source digest.
func PackageCacheVersion(version string, sources map[string]string) string {
	digest := sha256.New()
	fmt.Fprintf(digest, "pipit-package-source-v1:%d:", len(sources))
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		source := sources[name]
		fmt.Fprintf(digest, "%d:%s%d:", len(name), name, len(source))
		_, _ = io.WriteString(digest, source)
	}
	return fmt.Sprintf("%s:sha256:%x", version, digest.Sum(nil))
}

// ResolvedCacheIdentity binds every package cache entry to the acquired dependency set.
//
// Takes resolved ([]ResolvedModule) which contains all acquired modules in load order.
//
// Returns string which is a deterministic identity for dependency compilation inputs.
func ResolvedCacheIdentity(resolved []ResolvedModule) string {
	digest := sha256.New()
	fmt.Fprintf(digest, "pipit-resolved-source-v1:%d:", len(resolved))
	for _, module := range resolved {
		fmt.Fprintf(digest, "%d:%s%d:%s%d:", len(module.Path), module.Path, len(module.Version), module.Version, len(module.PackageOrder))
		for _, path := range module.PackageOrder {
			fmt.Fprintf(digest, "%d:%s", len(path), path)
		}
		fmt.Fprintf(digest, "%d:", len(module.Packages))
		for _, path := range slices.Sorted(maps.Keys(module.Packages)) {
			identity := PackageCacheVersion(module.Version, module.Packages[path])
			fmt.Fprintf(digest, "%d:%s%d:%s", len(path), path, len(identity), identity)
		}
	}
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

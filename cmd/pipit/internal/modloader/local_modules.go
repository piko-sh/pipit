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
	"errors"
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxGoModAncestors bounds the upward search for a go.mod file so a script deep in an
// unrelated tree does not scan to the filesystem root.
const maxGoModAncestors = 32

// errLocalPackageEmpty reports a local import that names a directory without Go files.
var errLocalPackageEmpty = errors.New("modloader: local package has no Go files for this platform")

// localModule is a module whose sources come from a directory rather than GOPROXY: the
// main module a go.mod declares, or the target of a directory replace directive.
type localModule struct {
	// Path is the module path imports are matched against.
	Path string

	// Dir is the absolute directory holding the module's packages.
	Dir string
}

// localModuleSet holds the local modules visible to a script, longest path first so a
// nested replace (`a/b` replaced separately from `a`) wins the prefix match.
type localModuleSet struct {
	// modules is sorted by descending path length.
	modules []localModule
}

// match finds the local module an import path belongs to.
//
// Takes importPath (string) which is the import to classify.
//
// Returns localModule which owns the import.
// Returns string which is the package path relative to the module root, empty for the
// root package.
// Returns bool which is false when no local module covers the import.
func (set *localModuleSet) match(importPath string) (localModule, string, bool) {
	if set == nil {
		return localModule{}, "", false
	}
	for _, module := range set.modules {
		if importPath == module.Path {
			return module, "", true
		}
		if rest, ok := strings.CutPrefix(importPath, module.Path+"/"); ok {
			return module, rest, true
		}
	}
	return localModule{}, "", false
}

// packageReader reads the compilable Go files of one package directory.
type packageReader interface {
	// readPackage returns the package's sources by file name.
	//
	// Takes dir (string) which is the absolute package directory.
	//
	// Returns map[string]string which maps file name to contents.
	// Returns error when the directory cannot be read.
	readPackage(dir string) (map[string]string, error)
}

// newPackageReader selects the reader for the invocation.
//
// Takes snapshot (*SourceSnapshot) which is nil for the trusted direct-read path.
//
// Returns packageReader which reads live or captured sources.
func newPackageReader(snapshot *SourceSnapshot) packageReader {
	if snapshot != nil {
		return snapshotPackageReader{snapshot: snapshot}
	}
	return livePackageReader{}
}

// livePackageReader reads packages from the filesystem, confined to the package directory
// so a symlink cannot lead the read elsewhere.
type livePackageReader struct{}

// readPackage implements [packageReader] over os.Root.
//
// Takes dir (string) which is the absolute package directory.
//
// Returns map[string]string which maps file name to contents.
// Returns error when the directory or a file cannot be read.
func (livePackageReader) readPackage(dir string) (map[string]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	sources := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isBundledSource(name) {
			continue
		}
		data, err := root.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if compilableForHost(dir, name, string(data)) {
			sources[name] = string(data)
		}
	}
	return sources, nil
}

// snapshotPackageReader serves packages from a gated invocation's captured sources.
type snapshotPackageReader struct {
	// snapshot holds the approved bytes.
	snapshot *SourceSnapshot
}

// readPackage implements [packageReader] over the snapshot.
//
// Takes dir (string) which is the absolute package directory.
//
// Returns map[string]string which maps file name to contents.
// Returns error when the directory lies outside the approved source root.
func (reader snapshotPackageReader) readPackage(dir string) (map[string]string, error) {
	sources, err := reader.snapshot.PackageSources(dir)
	if err != nil {
		return nil, fmt.Errorf("%s is outside the approved source snapshot: %w", dir, err)
	}
	return sources, nil
}

// localResolution accumulates the packages reached from a program's local imports.
type localResolution struct {
	// packages maps module path to relPath to file sources.
	packages map[string]map[string]map[string]string

	// deps maps a local module path to the other local modules it imports.
	deps map[string]map[string]struct{}

	// remote collects hostname-shaped imports found inside local packages.
	remote map[string]struct{}

	// visited records import paths already read.
	visited map[string]struct{}

	// stdlib names host-provided packages.
	stdlib map[string]struct{}

	// locals names the directory-backed modules.
	locals *localModuleSet

	// reader serves package sources.
	reader packageReader
}

// visit reads one local package, records it and classifies its imports.
//
// Takes importPath (string) which is a local import.
//
// Returns []string which lists further local imports to visit.
// Returns error when the package cannot be read.
func (state *localResolution) visit(importPath string) ([]string, error) {
	if _, seen := state.visited[importPath]; seen {
		return nil, nil
	}
	state.visited[importPath] = struct{}{}
	module, relPath, ok := state.locals.match(importPath)
	if !ok {
		return nil, fmt.Errorf("modloader: %s is not in a local module", importPath)
	}
	sources, err := state.reader.readPackage(filepath.Join(module.Dir, filepath.FromSlash(relPath)))
	if err != nil {
		return nil, fmt.Errorf("modloader: reading local package %s: %w", importPath, err)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: %s", errLocalPackageEmpty, importPath)
	}
	if state.packages[module.Path] == nil {
		state.packages[module.Path] = make(map[string]map[string]string)
		state.deps[module.Path] = make(map[string]struct{})
	}
	state.packages[module.Path][relPath] = sources

	var discovered []string
	for _, imported := range collectImportPathsFromFiles(sources) {
		if _, ok := state.stdlib[imported]; ok {
			continue
		}
		if other, _, ok := state.locals.match(imported); ok {
			if other.Path != module.Path {
				state.deps[module.Path][other.Path] = struct{}{}
			}
			discovered = append(discovered, imported)
			continue
		}
		if looksLikeRemoteImport(imported) {
			state.remote[imported] = struct{}{}
		}
	}
	return discovered, nil
}

// ordered turns the accumulated packages into dependency-ordered resolved modules.
//
// Returns []ResolvedModule which lists local modules, dependencies first.
// Returns error when the intra-module packages or the modules form a cycle.
func (state *localResolution) ordered() ([]ResolvedModule, error) {
	accumulator := newModuleAccumulator()
	for _, modulePath := range sortedKeys(state.packages) {
		packages := state.packages[modulePath]
		order, err := topoSortIntraModulePackages(modulePath, packages)
		if err != nil {
			return nil, fmt.Errorf("modloader: topo-sorting local module %s: %w", modulePath, err)
		}
		accumulator.add(modulePath, "", packages, order, sortedKeys(state.deps[modulePath]))
	}
	ordered, _, err := accumulator.orderedForModules(sortedKeys(state.packages))
	return ordered, err
}

// ImportsNeedResolution reports whether any import must go through the module resolver: a
// package of a local module named by the nearest go.mod, or a hostname-shaped path
// fetched from GOPROXY. Host-provided packages never do.
//
// Takes scriptDir (string) which is the directory holding the script or package.
// Takes imports ([]string) which is the union of the program's imports.
// Takes stdlibPackages (map[string]struct{}) which names host-provided packages.
// Takes snapshot (*SourceSnapshot) which is nil for the trusted direct-read path.
//
// Returns bool which is true when the resolver must run before compilation.
func ImportsNeedResolution(scriptDir string, imports []string, stdlibPackages map[string]struct{}, snapshot *SourceSnapshot) bool {
	local, remote := classifyImports(imports, stdlibPackages, discoverLocalModules(scriptDir, snapshot))
	return len(local) > 0 || len(remote) > 0
}

// nearestGoMod locates the go.mod that governs a script directory.
//
// Takes scriptDir (string) which is the directory holding the script or package.
// Takes snapshot (*SourceSnapshot) which is nil for the trusted direct-read path.
//
// Returns string which is the directory containing the go.mod.
// Returns goModFile which is the parsed file.
// Returns bool which is false when no go.mod was found.
func nearestGoMod(scriptDir string, snapshot *SourceSnapshot) (string, goModFile, bool) {
	if snapshot != nil {
		data, err := snapshot.ReadFile(filepath.Join(scriptDir, "go.mod"))
		if err != nil {
			return "", goModFile{}, false
		}
		return scriptDir, parseGoMod(data), true
	}
	dir := scriptDir
	if absolute, err := filepath.Abs(scriptDir); err == nil {
		dir = absolute
	}
	for range maxGoModAncestors {
		data, err := osReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir, parseGoMod(data), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", goModFile{}, false
}

// discoverLocalModules builds the local module set for a script: the main module named by
// the nearest go.mod plus every directory replacement it declares.
//
// Takes scriptDir (string) which is the directory holding the script or package.
// Takes snapshot (*SourceSnapshot) which is nil for the trusted direct-read path.
//
// Returns *localModuleSet which may be empty when there is no go.mod or it declares no
// module path.
func discoverLocalModules(scriptDir string, snapshot *SourceSnapshot) *localModuleSet {
	modDir, file, ok := nearestGoMod(scriptDir, snapshot)
	if !ok {
		return &localModuleSet{modules: nil}
	}
	var modules []localModule
	if file.Module != "" {
		modules = append(modules, localModule{Path: file.Module, Dir: modDir})
	}
	for modulePath, directory := range file.Replace {
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(modDir, directory)
		}
		modules = append(modules, localModule{Path: modulePath, Dir: filepath.Clean(directory)})
	}
	slices.SortFunc(modules, func(a, b localModule) int {
		if diff := len(b.Path) - len(a.Path); diff != 0 {
			return diff
		}
		return strings.Compare(a.Path, b.Path)
	})
	return &localModuleSet{modules: modules}
}

// classifyImports splits a package's imports into the ones served by local modules and
// the ones fetched from GOPROXY. Host-provided packages and paths that are neither local
// nor hostname-shaped are left to the compiler, which reports them as unknown.
//
// Takes imports ([]string) which is the raw import list.
// Takes stdlibPackages (map[string]struct{}) which names host-provided packages.
// Takes locals (*localModuleSet) which names the directory-backed modules.
//
// Returns []string which is the sorted set of local imports.
// Returns []string which is the sorted set of remote imports.
func classifyImports(imports []string, stdlibPackages map[string]struct{}, locals *localModuleSet) (local, remote []string) {
	var others []string
	for _, importPath := range imports {
		if _, ok := stdlibPackages[importPath]; ok {
			continue
		}
		if _, _, ok := locals.match(importPath); ok {
			local = append(local, importPath)
			continue
		}
		others = append(others, importPath)
	}
	slices.Sort(local)
	return slices.Compact(local), filterExternalImports(others, stdlibPackages)
}

// compilableForHost applies Go's build constraints (file name suffixes and //go:build
// lines) for the host platform, so a package with platform-specific files compiles the
// same set the go command would.
//
// Takes dir (string) which is the package directory, used only to form the path.
// Takes name (string) which is the file name.
// Takes content (string) which is the file's source.
//
// Returns bool which is true when the file takes part in the build.
func compilableForHost(dir, name, content string) bool {
	ctxt := build.Default
	ctxt.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	ok, err := ctxt.MatchFile(dir, name)
	return err == nil && ok
}

// resolveLocalModules reads every local package reachable from seeds and orders the local
// modules so each one's local dependencies load first. Only reachable packages are
// compiled: an unrelated `cmd/` tree in the same module, with its own imports, never
// costs a fetch.
//
// Takes locals (*localModuleSet) which names the directory-backed modules.
// Takes seeds ([]string) which are the program's local imports.
// Takes stdlibPackages (map[string]struct{}) which names host-provided packages.
// Takes snapshot (*SourceSnapshot) which is nil for the trusted direct-read path.
//
// Returns []ResolvedModule which lists the local modules in dependency order.
// Returns []string which is the sorted set of remote imports the local packages need.
// Returns error when a package cannot be read or the modules form a cycle.
func resolveLocalModules(
	ctx context.Context,
	locals *localModuleSet,
	seeds []string,
	stdlibPackages map[string]struct{},
	snapshot *SourceSnapshot,
) ([]ResolvedModule, []string, error) {
	state := &localResolution{
		packages: make(map[string]map[string]map[string]string),
		deps:     make(map[string]map[string]struct{}),
		remote:   make(map[string]struct{}),
		visited:  make(map[string]struct{}),
		stdlib:   stdlibPackages,
		locals:   locals,
		reader:   newPackageReader(snapshot),
	}
	queue := append([]string(nil), seeds...)
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		next := queue[0]
		queue = queue[1:]
		discovered, err := state.visit(next)
		if err != nil {
			return nil, nil, err
		}
		queue = append(queue, discovered...)
	}
	ordered, err := state.ordered()
	if err != nil {
		return nil, nil, err
	}
	return ordered, sortedKeys(state.remote), nil
}

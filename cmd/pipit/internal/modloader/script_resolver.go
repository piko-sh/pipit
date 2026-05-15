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
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/dolmen-go/modfs"
	"github.com/dolmen-go/modfs/httpfs"
)

const (
	// MainPackageLabel is the synthetic module path under which the script's own files are
	// compiled. It only needs to be unique relative to every fetched module; "main" is the
	// obvious choice and pipit treats it like any other label.
	MainPackageLabel = "main"

	// moduleRootSegments is the number of leading import-path segments treated as the module
	// root in the common "<host>/<owner>/<repo>" layout.
	moduleRootSegments = 3
)

// ErrNetworkNotAllowed is returned by ResolveScript when a non-stdlib import is
// encountered but the caller has not enabled --allow-network. Hosts errors.Is on it.
var ErrNetworkNotAllowed = errors.New("modloader: non-stdlib import requires --allow-network")

// osReadFile is split out so tests can inject their own reader. Always os.ReadFile in
// production.
var osReadFile = func(path string) ([]byte, error) {
	return osReadFileReal(path)
}

// ScriptResolution carries the script's main package plus a topologically sorted slice of
// external modules ready to feed into pipit's CompileProgram one module at a time.
type ScriptResolution struct {
	// MainSources holds the script's .go files by basename. Fed to CompileProgram as
	// `map[string]map[string]string{"": MainSources}`.
	MainSources map[string]string

	// MainPackage is the modulePath the script's own files are compiled under, which is
	// always MainPackageLabel.
	MainPackage string

	// CacheRoot is the absolute path of the on-disk cache that served this resolve; empty
	// when caching is off.
	CacheRoot string

	// AppliedCacheMode is the cache mode that applied after fallback resolution, which may
	// differ from the requested mode.
	AppliedCacheMode CacheMode

	// Modules is the list of Go modules pulled in by the script, in dependency order
	// (modules with no fetched deps first, remote modules before the local modules that
	// import them). Each entry's Packages map keys are paths relative to the module root.
	Modules []ResolvedModule

	// LocalModules lists the module paths served from directories rather than GOPROXY: the
	// main module the nearest go.mod declares and its directory replacements. Their entries
	// in Modules carry an empty Version; the bytecode cache keys them by content.
	LocalModules []string

	// Fetched lists the module paths pipit fetched from GOPROXY during this resolve (cache
	// misses that hit the network). The run command uses it to surface what was downloaded.
	Fetched []string

	// CacheHits lists the module@version keys served directly from the on-disk cache during
	// this resolve. Empty when no cache is in use.
	CacheHits []string
}

// ResolvedModule pairs a module's import path with the packages pipit fetched for it.
// Each Packages entry is a relPath -> filename -> source map; relPath "" is the module's
// root package.
type ResolvedModule struct {
	// Path is the module's import path (e.g. "github.com/google/uuid").
	Path string

	// Version is the resolved module version the proxy returned.
	Version string

	// Packages is the per-relPath file map ready to feed directly into CompileProgram.
	Packages map[string]map[string]string

	// PackageOrder is the topological ordering of relPaths in Packages: loading sub-packages
	// in this order guarantees each one's intra-module dependencies are already registered
	// by the time it is loaded.
	PackageOrder []string
}

// ScriptResolverOptions configures ResolveScript.
type ScriptResolverOptions struct {
	// Snapshot pins local source and adjacent go.mod for a gated invocation.
	Snapshot *SourceSnapshot

	// HTTPClient overrides the proxy HTTP client. nil selects a default with a sensible
	// timeout.
	HTTPClient *http.Client

	// StdlibPackages is the set of import paths the host already provides as native symbols.
	StdlibPackages map[string]struct{}

	// RequiredVersions pins remote modules to specific versions keyed by module path.
	RequiredVersions map[string]string

	// GoproxyURL overrides the proxy base. Empty selects proxy.golang.org.
	GoproxyURL string

	// CacheRoot is the absolute root of the on-disk artefact cache. Empty disables caching.
	CacheRoot string

	// AppliedCacheMode is the mode that applied when resolving CacheRoot.
	AppliedCacheMode CacheMode

	// AllowNetwork toggles whether the resolver may reach GOPROXY.
	AllowNetwork bool
}

// moduleAccumulator collects fetched modules and their inter-module dependencies for
// ordering at the end.
type moduleAccumulator struct {
	// byPath maps module import path -> ResolvedModule.
	byPath map[string]*ResolvedModule

	// deps maps module path -> set of other module paths it imports.
	deps map[string]map[string]struct{}
}

// newModuleAccumulator constructs an empty accumulator.
//
// Returns *moduleAccumulator which is ready to record modules.
func newModuleAccumulator() *moduleAccumulator {
	return &moduleAccumulator{
		byPath: make(map[string]*ResolvedModule),
		deps:   make(map[string]map[string]struct{}),
	}
}

// has reports whether modulePath has already been fetched.
//
// Takes modulePath (string) which is the module import path to check.
//
// Returns bool which is true when the module is already recorded.
func (m *moduleAccumulator) has(modulePath string) bool {
	_, ok := m.byPath[modulePath]
	return ok
}

// add records a fetched module, its resolved version, and its inter-module dependencies,
// but does not yet order them.
//
// Takes modulePath (string) which is the module import path.
// Takes version (string) which is the resolved module version.
// Takes packages (map[string]map[string]string) which holds the source files.
// Takes packageOrder ([]string) which is the intra-module load order.
// Takes deps ([]string) which lists other module paths it imports.
func (m *moduleAccumulator) add(modulePath, version string, packages map[string]map[string]string, packageOrder []string, deps []string) {
	m.byPath[modulePath] = &ResolvedModule{
		Path:         modulePath,
		Version:      version,
		Packages:     packages,
		PackageOrder: packageOrder,
	}
	if _, ok := m.deps[modulePath]; !ok {
		m.deps[modulePath] = make(map[string]struct{}, len(deps))
	}
	for _, dep := range deps {
		m.deps[modulePath][dep] = struct{}{}
	}
}

// orderedFor sorts the modules so each module's dependencies come first, alongside the
// flat sorted list of fetched paths.
//
// Takes roots ([]string) which is the set of directly imported modules.
//
// Returns []ResolvedModule which is the dependency-ordered module list.
// Returns []string which is the flat sorted list of fetched module paths.
// Returns error when a dependency cycle is detected.
func (m *moduleAccumulator) orderedFor(roots []string) ([]ResolvedModule, []string, error) {
	rootSet := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		modulePath, _ := splitModuleFromPackage(root)
		rootSet[modulePath] = struct{}{}
	}
	return m.orderedForModules(sortedKeys(rootSet))
}

// orderedForModules returns every accumulated module in dependency order, visiting the
// given module paths first so the roots' own order is stable.
//
// Takes moduleRoots ([]string) which lists module paths to visit first.
//
// Returns []ResolvedModule which is the dependency-ordered module list.
// Returns []string which is the sorted list of every module path visited.
// Returns error when the modules form a cycle.
func (m *moduleAccumulator) orderedForModules(moduleRoots []string) ([]ResolvedModule, []string, error) {
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(m.byPath))
	var ordered []ResolvedModule

	var visit func(string) error
	visit = func(modulePath string) error {
		switch state[modulePath] {
		case visiting:
			return fmt.Errorf("modloader: cyclic module dependency at %s", modulePath)
		case visited:
			return nil
		}
		state[modulePath] = visiting
		if err := m.visitDeps(modulePath, visit); err != nil {
			return err
		}
		state[modulePath] = visited
		ordered = append(ordered, *m.byPath[modulePath])
		return nil
	}

	visitAll := func(paths []string) error {
		for _, path := range paths {
			if err := visit(path); err != nil {
				return err
			}
		}
		return nil
	}

	if err := visitAll(moduleRoots); err != nil {
		return nil, nil, err
	}
	if err := visitAll(sortedKeys(m.byPath)); err != nil {
		return nil, nil, err
	}

	fetched := make([]string, 0, len(m.byPath))
	for _, mod := range ordered {
		fetched = append(fetched, mod.Path)
	}
	slices.Sort(fetched)
	return ordered, fetched, nil
}

// visitDeps invokes visit for every known dependency of modulePath, in sorted order.
//
// Takes modulePath (string) which is the module whose dependencies are visited.
// Takes visit (func(string) error) which is the traversal step to run per dependency.
//
// Returns error which is the first error visit returned.
func (m *moduleAccumulator) visitDeps(modulePath string, visit func(string) error) error {
	for _, dep := range sortedKeys(m.deps[modulePath]) {
		if _, ok := m.byPath[dep]; !ok {
			continue
		}
		if err := visit(dep); err != nil {
			return err
		}
	}
	return nil
}

// goproxyHandle wraps the modfs client.
type goproxyHandle struct {
	// modfs is the module-proxy filesystem client.
	modfs *modfs.ModFS

	// cache holds fetched module sources keyed by module@version.
	cache map[string]map[string]map[string]string

	// versions maps a module@version key to the resolved version.
	versions map[string]string

	// recorder collects cache hit and miss events for the run summary.
	recorder *MemoryRecorder

	// mu guards cache and versions.
	mu sync.Mutex
}

// newGoproxyHandle constructs a handle bound to the configured proxy.
//
// When options.CacheRoot is set the HTTP transport is wrapped in [cachingFS] so on-disk
// hits short-circuit the network.
//
// Takes options (ScriptResolverOptions) which configures proxy and cache.
//
// Returns *goproxyHandle which is bound to the proxy transport.
// Returns error when the GOPROXY URL is invalid.
func newGoproxyHandle(options ScriptResolverOptions) (*goproxyHandle, error) {
	baseURL := options.GoproxyURL
	if baseURL == "" {
		baseURL = DefaultGoproxyURL
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: goproxyFetchTimeout}
	}
	httpFS, err := httpfs.NewHTTPFS(client, baseURL)
	if err != nil {
		return nil, fmt.Errorf("modloader: GOPROXY %q: %w", baseURL, err)
	}
	recorder := NewMemoryRecorder()
	var transport fs.FS = httpFS
	if options.CacheRoot != "" {
		transport = newCachingFS(httpFS, options.CacheRoot, options.AllowNetwork, recorder)
	}
	return &goproxyHandle{
		modfs:    modfs.New(transport),
		cache:    make(map[string]map[string]map[string]string),
		versions: make(map[string]string),
		recorder: recorder,
		mu:       sync.Mutex{}}, nil
}

// fetchModuleSources returns the .go sources for every package in a module, keyed by
// module-relative directory.
//
// Takes modulePath (string) which is the module import path to fetch.
// Takes requestedVersion (string) which pins a version, empty for latest.
//
// Returns map[string]map[string]string which holds sources per package.
// Returns string which is the resolved module version.
// Returns error when the module cannot be opened or read.
//
// Concurrency: safe for concurrent use; the mutex guards the cache maps.
func (g *goproxyHandle) fetchModuleSources(ctx context.Context, modulePath, requestedVersion string) (map[string]map[string]string, string, error) {
	cacheKey := modulePath + "@" + requestedVersion
	g.mu.Lock()
	cached, ok := g.cache[cacheKey]
	cachedVer := g.versions[cacheKey]
	g.mu.Unlock()
	if ok {
		return cached, cachedVer, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	module, err := g.modfs.OpenModule(modulePath)
	if err != nil {
		return nil, "", fmt.Errorf("opening module: %w", err)
	}
	version, err := resolveModuleVersion(module, requestedVersion)
	if err != nil {
		return nil, "", err
	}
	resolvedVersion := version.Version
	zip, err := version.OpenFS()
	if err != nil {
		return nil, "", fmt.Errorf("opening zip: %w", err)
	}

	out, walkErr := collectModuleZipSources(zip)
	if walkErr != nil {
		return nil, "", walkErr
	}

	g.mu.Lock()
	g.cache[cacheKey] = out
	g.versions[cacheKey] = resolvedVersion
	g.mu.Unlock()
	return out, resolvedVersion, nil
}

// ResolveScript reads the script at path, walks its import graph, reads every package of
// a local module from disk, fetches every remote module via GOPROXY, and returns the
// modules in dependency order alongside the script's own sources.
//
// Takes scriptPath (string) which is the path of the script to resolve.
// Takes options (ScriptResolverOptions) which configures the resolve.
//
// Returns *ScriptResolution which holds the ordered modules and sources.
// Returns error when reading, parsing, or fetching a module fails.
func ResolveScript(ctx context.Context, scriptPath string, options ScriptResolverOptions) (*ScriptResolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	readSource := readScript
	if options.Snapshot != nil {
		readSource = options.Snapshot.ReadFile
	}
	scriptBytes, err := readSource(scriptPath)
	if err != nil {
		return nil, err
	}
	scriptImports, err := extractImports(scriptPath, scriptBytes)
	if err != nil {
		return nil, fmt.Errorf("modloader: parsing %s imports: %w", scriptPath, err)
	}
	name := scriptPath
	if absolute, err := filepath.Abs(scriptPath); err == nil {
		name = absolute
	}
	mainSources := map[string]string{name: string(scriptBytes)}
	return resolveProgram(ctx, filepath.Dir(scriptPath), mainSources, scriptImports, options)
}

// ResolveDirectory resolves the imports of a package whose sources were already read, as
// ResolveScript does for a single file: local modules come from the nearest go.mod,
// remote modules from GOPROXY.
//
// Takes directory (string) which is the package directory the sources came from.
// Takes sources (map[string]string) which maps file name to source.
// Takes options (ScriptResolverOptions) which configures the resolve.
//
// Returns *ScriptResolution which holds the ordered modules and sources.
// Returns error when parsing or fetching a module fails.
func ResolveDirectory(ctx context.Context, directory string, sources map[string]string, options ScriptResolverOptions) (*ScriptResolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	imports := collectImportPathsFromFiles(sources)
	for name, source := range sources {
		if _, err := extractImports(name, []byte(source)); err != nil {
			return nil, fmt.Errorf("modloader: parsing %s imports: %w", name, err)
		}
	}
	return resolveProgram(ctx, directory, sources, imports, options)
}

// ExtractImports parses a single Go source and returns its import paths.
//
// Exported so the pipit run dispatcher can pre-flight a script's imports without invoking
// the full resolver.
//
// Takes name (string) which names the source for parser diagnostics.
// Takes source ([]byte) which is the Go source to scan.
//
// Returns []string which is the list of imported paths.
// Returns error when the source cannot be parsed.
func ExtractImports(name string, source []byte) ([]string, error) {
	return extractImports(name, source)
}

// resolveProgram is the shared body of the two entry points: local modules first, since
// their packages may import remote modules, then the remote graph, then the ordering.
//
// Takes scriptDir (string) which locates the nearest go.mod.
// Takes mainSources (map[string]string) which holds the program's own files.
// Takes imports ([]string) which is the union of the program's imports.
// Takes options (ScriptResolverOptions) which configures the resolve.
//
// Returns *ScriptResolution which holds the ordered modules and sources.
// Returns error when reading, parsing, or fetching a module fails.
func resolveProgram(ctx context.Context, scriptDir string, mainSources map[string]string, imports []string, options ScriptResolverOptions) (*ScriptResolution, error) {
	options.RequiredVersions = mergeAdjacentVersions(options.RequiredVersions, invocationVersions(scriptDir, options.Snapshot))

	resolution := &ScriptResolution{
		MainPackage: MainPackageLabel,
		MainSources: mainSources,
		CacheRoot:   "", AppliedCacheMode: "", Modules: nil, LocalModules: nil, Fetched: nil, CacheHits: nil}

	locals := discoverLocalModules(scriptDir, options.Snapshot)
	localSeeds, external := classifyImports(imports, options.StdlibPackages, locals)
	if len(localSeeds) == 0 && len(external) == 0 {
		return resolution, nil
	}

	localModules, remoteFromLocal, err := resolveLocalModules(ctx, locals, localSeeds, options.StdlibPackages, options.Snapshot)
	if err != nil {
		return nil, err
	}
	external = slices.Compact(slices.Sorted(slices.Values(append(external, remoteFromLocal...))))
	for _, module := range localModules {
		resolution.LocalModules = append(resolution.LocalModules, module.Path)
	}

	if len(external) > 0 {
		if err := resolveRemoteModules(ctx, resolution, external, options); err != nil {
			return nil, err
		}
	}
	resolution.Modules = append(resolution.Modules, localModules...)
	return resolution, nil
}

// resolveRemoteModules fetches the remote module graph rooted at external and records it
// on the resolution.
//
// Takes resolution (*ScriptResolution) which receives the modules and cache summary.
// Takes external ([]string) which is the sorted set of remote imports.
// Takes options (ScriptResolverOptions) which configures the resolve.
//
// Returns error when the network is not allowed or a fetch fails.
func resolveRemoteModules(ctx context.Context, resolution *ScriptResolution, external []string, options ScriptResolverOptions) error {
	if !options.AllowNetwork && options.CacheRoot == "" {
		return fmt.Errorf("%w: script imports %v", ErrNetworkNotAllowed, external)
	}

	provider, err := newGoproxyHandle(options)
	if err != nil {
		return err
	}

	modules := newModuleAccumulator()
	if err := resolveTransitive(ctx, provider, options, modules, external); err != nil {
		return err
	}

	ordered, allModules, err := modules.orderedFor(external)
	if err != nil {
		return err
	}
	resolution.Modules = ordered
	resolution.CacheRoot = options.CacheRoot
	resolution.AppliedCacheMode = options.AppliedCacheMode

	resolution.CacheHits, resolution.Fetched = splitCacheHits(provider.recorder.Hits(), allModules)
	return nil
}

// mergeAdjacentVersions folds versions discovered in an adjacent go.mod into the
// caller-supplied pins, leaving explicit caller pins untouched.
//
// Takes required (map[string]string) which holds the caller's pins, possibly nil.
// Takes discovered (map[string]string) which holds versions read from go.mod.
//
// Returns map[string]string which is the merged pin set.
func mergeAdjacentVersions(required, discovered map[string]string) map[string]string {
	if len(discovered) == 0 {
		return required
	}
	if required == nil {
		required = make(map[string]string, len(discovered))
	}
	for modulePath, version := range discovered {
		if _, ok := required[modulePath]; !ok {
			required[modulePath] = version
		}
	}
	return required
}

// splitCacheHits partitions the resolved modules into cache hits and network fetches.
//
// Takes hits ([]string) which are the module@version keys served from the cache.
// Takes allModules ([]string) which are every module path the resolve produced.
//
// Returns []string which lists the modules served from the cache, sorted.
// Returns []string which lists the modules fetched over the network, sorted.
func splitCacheHits(hits, allModules []string) (cached, fetched []string) {
	hitModules := make(map[string]struct{}, len(hits))
	for _, key := range hits {
		modulePath, _, _ := strings.Cut(key, "@")
		hitModules[modulePath] = struct{}{}
	}
	for _, modulePath := range allModules {
		if _, ok := hitModules[modulePath]; ok {
			cached = append(cached, modulePath)
		} else {
			fetched = append(fetched, modulePath)
		}
	}
	slices.Sort(cached)
	slices.Sort(fetched)
	return cached, fetched
}

// invocationVersions reads the require pins of the go.mod that governs a script
// directory: the nearest one up the tree on the trusted path, the captured adjacent one
// for a gated invocation.
//
// Takes scriptDir (string) which is the directory holding the script or package.
// Takes snapshot (*SourceSnapshot) which is nil for the trusted direct-read path.
//
// Returns map[string]string which holds the acquired module requirements, nil when none.
func invocationVersions(scriptDir string, snapshot *SourceSnapshot) map[string]string {
	_, file, ok := nearestGoMod(scriptDir, snapshot)
	if !ok {
		return nil
	}
	return file.Require
}

// parseAdjacentVersions extracts version requirements from acquired module metadata.
//
// Takes data ([]byte) which contains live or captured go.mod bytes.
//
// Returns map[string]string containing required module versions, nil when none.
func parseAdjacentVersions(data []byte) map[string]string {
	return parseGoMod(data).Require
}

// readScript is a tiny os.ReadFile wrapper that surfaces a pipit-friendly error message
// on failure.
//
// Takes path (string) which is the filesystem path to read.
//
// Returns []byte which is the file content.
// Returns error when the file cannot be read.
func readScript(path string) ([]byte, error) {
	data, err := osReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("modloader: reading %s: %w", path, err)
	}
	return data, nil
}

// filterExternalImports drops imports whose paths are in the stdlib set.
//
// Takes imports ([]string) which is the raw import list to filter.
// Takes stdlibPackages (map[string]struct{}) which lists host-provided paths.
//
// Returns []string which is the sorted set of external imports.
func filterExternalImports(imports []string, stdlibPackages map[string]struct{}) []string {
	if len(imports) == 0 {
		return nil
	}
	out := make([]string, 0, len(imports))
	seen := make(map[string]struct{}, len(imports))
	for _, importPath := range imports {
		if _, ok := stdlibPackages[importPath]; ok {
			continue
		}
		if !looksLikeRemoteImport(importPath) {
			continue
		}
		if _, ok := seen[importPath]; ok {
			continue
		}
		seen[importPath] = struct{}{}
		out = append(out, importPath)
	}
	slices.Sort(out)
	return out
}

// looksLikeRemoteImport reports whether path resembles a module-proxy-fetchable import.
//
// Stdlib-shaped paths such as "strings" or "encoding/json" are excluded. The check is
// heuristic but safe: any path with a dot before the first slash is treated as
// hostname-shaped.
//
// Takes path (string) which is the import path to classify.
//
// Returns bool which is true when the path looks remotely fetchable.
func looksLikeRemoteImport(path string) bool {
	before, _, ok := strings.Cut(path, "/")
	if !ok {
		return false
	}
	first := before
	return strings.ContainsRune(first, '.')
}

// extractImports parses a single Go source and returns its import paths.
//
// Takes name (string) which names the source for parser diagnostics.
// Takes source ([]byte) which is the Go source to scan.
//
// Returns []string which is the list of imported paths.
// Returns error when the source cannot be parsed.
func extractImports(name string, source []byte) ([]string, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, name, source, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parsed.Imports))
	for _, importSpec := range parsed.Imports {
		raw := importSpec.Path.Value
		if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
			raw = raw[1 : len(raw)-1]
		}
		out = append(out, raw)
	}
	return out, nil
}

// sortedKeys returns a map's keys in ascending order, so traversal is deterministic.
//
// Takes set (map[string]V) which is the map to read keys from.
//
// Returns []string which holds the sorted keys.
func sortedKeys[V any](set map[string]V) []string {
	return slices.Sorted(maps.Keys(set))
}

// resolveTransitive walks the queue breadth-first, fetching each module and recording its
// sources.
//
// A visited set prevents re-fetching modules that appear in multiple dependency chains.
//
// Takes provider (*goproxyHandle) which fetches module sources.
// Takes options (ScriptResolverOptions) which configures the resolve.
// Takes modules (*moduleAccumulator) which accumulates fetched modules.
// Takes initial ([]string) which is the seed set of import paths.
//
// Returns error when a module fetch or topological sort fails.
func resolveTransitive(
	ctx context.Context,
	provider *goproxyHandle,
	options ScriptResolverOptions,
	modules *moduleAccumulator,
	initial []string,
) error {
	queue := append([]string(nil), initial...)

	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		modulePath, packagePath := splitModuleFromPackage(next)
		if modules.has(modulePath) {
			continue
		}

		moduleSources, resolvedVersion, err := provider.fetchModuleSources(ctx, modulePath, options.RequiredVersions[modulePath])
		if err != nil {
			return fmt.Errorf("modloader: fetching %s: %w", modulePath, err)
		}

		interModuleDeps, discovered := externalModuleDeps(modulePath, moduleSources, options.StdlibPackages)
		queue = append(queue, discovered...)

		deps := sortedKeys(interModuleDeps)
		packageOrder, err := topoSortIntraModulePackages(modulePath, moduleSources)
		if err != nil {
			return fmt.Errorf("modloader: topo-sorting %s sub-packages: %w", modulePath, err)
		}
		modules.add(modulePath, resolvedVersion, moduleSources, packageOrder, deps)

		if packagePath != "" {
			if _, ok := moduleSources[packagePath]; !ok {
				return fmt.Errorf("modloader: %s did not contain package %s", modulePath, packagePath)
			}
		}
	}
	return nil
}

// externalModuleDeps scans a fetched module's sources for imports that belong to other
// remote modules.
//
// Takes modulePath (string) which is the module being scanned, excluded from the result.
// Takes moduleSources (map[string]map[string]string) which holds the module's packages.
// Takes stdlibPackages (map[string]struct{}) which names host-provided packages to skip.
//
// Returns map[string]struct{} which is the set of other module paths depended on.
// Returns []string which lists the import paths to enqueue for fetching.
func externalModuleDeps(
	modulePath string,
	moduleSources map[string]map[string]string,
	stdlibPackages map[string]struct{},
) (map[string]struct{}, []string) {
	deps := make(map[string]struct{})
	var queue []string
	for _, files := range moduleSources {
		for _, importPath := range collectImportPathsFromFiles(files) {
			if _, ok := stdlibPackages[importPath]; ok {
				continue
			}
			if !looksLikeRemoteImport(importPath) {
				continue
			}
			depModulePath, _ := splitModuleFromPackage(importPath)
			if depModulePath == modulePath {
				continue
			}
			deps[depModulePath] = struct{}{}
			queue = append(queue, importPath)
		}
	}
	return deps, queue
}

// collectModuleZipSources reads every compilable .go file out of a module zip, keyed by
// package directory then filename.
//
// Takes zip (fs.FS) which is the opened module zip.
//
// Returns map[string]map[string]string which maps package directory to file sources.
// Returns error when a file cannot be read.
func collectModuleZipSources(zip fs.FS) (map[string]map[string]string, error) {
	out := make(map[string]map[string]string)
	err := fs.WalkDir(zip, ".", func(path string, dirEntry fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return walkErr
		case dirEntry.IsDir():
			return skipNonCompilableDir(path)
		case !isBundledSource(path):
			return nil
		}
		file, err := zip.Open(path)
		if err != nil {
			return err
		}
		buffer := new(bytes.Buffer)
		_, copyErr := io.Copy(buffer, file)
		_ = file.Close()
		if copyErr != nil {
			return copyErr
		}
		dir, name := splitDirFile(path)
		content := buffer.String()
		if !compilableForHost(dir, name, content) {
			return nil
		}
		bucket, ok := out[dir]
		if !ok {
			bucket = make(map[string]string)
			out[dir] = bucket
		}
		bucket[name] = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// skipNonCompilableDir reports whether a directory in a module zip should be walked.
//
// Takes path (string) which is the directory being visited.
//
// Returns error which is fs.SkipDir for vendor and testdata trees.
func skipNonCompilableDir(path string) error {
	switch {
	case path == "vendor" || strings.HasPrefix(path, "vendor/"):
		return fs.SkipDir
	case path == "testdata" || strings.HasPrefix(path, "testdata/"):
		return fs.SkipDir
	}
	return nil
}

// collectImportPathsFromFiles parses every file in a package for its imports and returns
// the union, sorted.
//
// Takes files (map[string]string) which maps filename to source text.
//
// Returns []string which is the sorted union of imported paths.
func collectImportPathsFromFiles(files map[string]string) []string {
	seen := make(map[string]struct{})
	for name, content := range files {
		imports, err := extractImports(name, []byte(content))
		if err != nil {
			continue
		}
		for _, importPath := range imports {
			seen[importPath] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for importPath := range seen {
		out = append(out, importPath)
	}
	slices.Sort(out)
	return out
}

// splitModuleFromPackage separates an import path into module path and package sub-path.
//
// Takes importPath (string) which is the import path to split.
//
// Returns string which is the module path.
// Returns string which is the package sub-path, empty for the root.
func splitModuleFromPackage(importPath string) (modulePath, subPath string) {
	parts := strings.Split(importPath, "/")
	if len(parts) <= moduleRootSegments {
		return importPath, ""
	}
	root := moduleRootSegments
	if isMajorVersionSegment(parts[root]) {
		root++
	}
	return strings.Join(parts[:root], "/"), strings.Join(parts[root:], "/")
}

// isMajorVersionSegment reports whether a path segment is a semantic-import major version
// suffix (v2, v3, ...), which belongs to the module path.
//
// Takes segment (string) which is one import path segment.
//
// Returns bool which is true for "v" followed by an integer of at least 2.
func isMajorVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return segment != "v0" && segment != "v1"
}

// topoSortIntraModulePackages orders a module's relPaths so each sub-package's
// intra-module dependencies precede it.
//
// Takes modulePath (string) which is the module import path.
// Takes packages (map[string]map[string]string) which holds package sources.
//
// Returns []string which is the dependency-ordered list of relPaths.
// Returns error when an intra-module dependency cycle is detected.
func topoSortIntraModulePackages(modulePath string, packages map[string]map[string]string) ([]string, error) {
	if len(packages) <= 1 {
		return sortedKeys(packages), nil
	}

	intraDeps := intraModuleDeps(modulePath, packages)

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(packages))
	var ordered []string
	var visit func(string) error
	visit = func(relPath string) error {
		switch state[relPath] {
		case visiting:
			return fmt.Errorf("cyclic intra-module dependency at %s/%s", modulePath, relPath)
		case visited:
			return nil
		}
		state[relPath] = visiting
		for _, dep := range sortedKeys(intraDeps[relPath]) {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[relPath] = visited
		ordered = append(ordered, relPath)
		return nil
	}
	for _, relPath := range sortedKeys(packages) {
		if err := visit(relPath); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// intraModuleDeps maps each sub-package to the sibling sub-packages it imports.
//
// Takes modulePath (string) which is the module the packages belong to.
// Takes packages (map[string]map[string]string) which maps relPath to its file sources.
//
// Returns map[string]map[string]struct{} which maps relPath to its sibling dependencies.
func intraModuleDeps(modulePath string, packages map[string]map[string]string) map[string]map[string]struct{} {
	deps := make(map[string]map[string]struct{}, len(packages))
	for relPath, files := range packages {
		deps[relPath] = make(map[string]struct{})
		for _, importPath := range collectImportPathsFromFiles(files) {
			if !strings.HasPrefix(importPath, modulePath) {
				continue
			}
			rest := strings.TrimPrefix(strings.TrimPrefix(importPath, modulePath), "/")
			if rest == relPath {
				continue
			}
			if _, ok := packages[rest]; !ok {
				continue
			}
			deps[relPath][rest] = struct{}{}
		}
	}
	return deps
}

// splitDirFile splits a path into directory and filename.
//
// Takes path (string) which is the slash-separated path to split.
//
// Returns string which is the directory, empty for a bare filename.
// Returns string which is the filename.
func splitDirFile(path string) (dir, file string) {
	index := strings.LastIndexByte(path, '/')
	if index < 0 {
		return "", path
	}
	return path[:index], path[index+1:]
}

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

package extract

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	// maxSourceFileSizeBytes caps the per-file byte count read during discovery. A source
	// file is typically well under 100 KiB; this cap guards against accidental or
	// adversarial oversized inputs exhausting memory during the initial scan.
	maxSourceFileSizeBytes = 4 * 1024 * 1024

	// maxDiscoveredImports caps the deduplicated import set collected from scanned source
	// files. The real-world limit is far lower; this is a defence-in-depth bound that still
	// leaves generous headroom.
	maxDiscoveredImports = 10_000

	// errDiscoverWrap is the format string used to wrap errors surfaced from the discovery
	// pipeline so callers see a consistent prefix.
	errDiscoverWrap = "discover: %w"

	// cgoPseudoPackage is the import path of cgo's pseudo-package, which is not a package
	// and never needs a symbol table.
	cgoPseudoPackage = "C"

	// goModFileName is the name of the module file at the project root.
	goModFileName = "go.mod"

	// maxGoModSizeBytes caps how much of go.mod discovery reads to find the module path.
	maxGoModSizeBytes = 1024 * 1024
)

var (
	// errTooManyImports is returned when the aggregate import set collected from scanned
	// files exceeds maxDiscoveredImports. Real projects never come close; hitting this
	// indicates an abusive or malformed input.
	errTooManyImports = errors.New("discovery exceeded import limit")

	// errSourceFileTooLarge is returned when a scanned file exceeds maxSourceFileSizeBytes
	// during discovery.
	errSourceFileTooLarge = errors.New("source file exceeds size limit")

	// errNotRegularFile is returned for a source path that names a device, pipe or other
	// non-regular file, which discovery never reads.
	errNotRegularFile = errors.New("not a regular file")

	// errNotProjectDirectory is returned when an import path inside the project module does
	// not name a directory below the project root.
	errNotProjectDirectory = errors.New("not a directory inside the project")

	// errNoModulePath is returned when go.mod declares no module path.
	errNoModulePath = errors.New("go.mod declares no module path")
)

// SourceScanner finds the Go imports in one kind of project source file. Discover walks
// the project and hands every file a scanner matches to that scanner.
type SourceScanner interface {
	// Match reports whether the scanner reads a file with this base name.
	//
	// Takes name (string) which is the file's base name.
	//
	// Returns bool which is true for names this scanner handles.
	Match(name string) bool

	// Imports returns the import paths referenced by one file.
	//
	// Takes path (string) which is the root-relative file path, for error messages.
	// Takes data ([]byte) which is the file content.
	//
	// Returns []string which holds the import paths, in any order.
	// Returns error when the file cannot be parsed; Discover then fails.
	Imports(ctx context.Context, path string, data []byte) ([]string, error)
}

// GoFileScanner is the default SourceScanner. It reads the import declarations of .go
// files.
type GoFileScanner struct{}

// Match reports whether name is a Go source file.
//
// Takes name (string) which is the file's base name.
//
// Returns bool which is true for names ending in .go.
func (GoFileScanner) Match(name string) bool {
	return strings.HasSuffix(name, ".go")
}

// Imports reads a Go file's import declarations, ignoring generated files.
//
// Takes path (string) which names the file in error messages.
// Takes data ([]byte) which is the file content.
//
// Returns []string which holds the import paths, none for a generated file.
// Returns error when the import block cannot be parsed.
func (GoFileScanner) Imports(_ context.Context, path string, data []byte) ([]string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, data, parser.ImportsOnly|parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing Go imports in %s: %w", path, err)
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}
	return importPaths(file), nil
}

// ParseGoImports parses the import declarations of Go source text. Scanners for formats
// that embed Go source use it once they have extracted that source.
//
// Takes path (string) which names the source in error messages.
// Takes source ([]byte) which is the Go source text.
//
// Returns []string which holds the import paths, empty entries dropped.
// Returns error when the import block cannot be parsed.
func ParseGoImports(path string, source []byte) ([]string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing Go imports in %s: %w", path, err)
	}
	return importPaths(file), nil
}

// importPaths returns the non-empty import paths of a parsed file.
//
// Takes file (*ast.File) which is the parsed file.
//
// Returns []string which holds the unquoted import paths.
func importPaths(file *ast.File) []string {
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		value := strings.Trim(spec.Path.Value, `"`)
		if value != "" {
			imports = append(imports, value)
		}
	}
	return imports
}

// DiscoverOptions configures a Discover run. Zero values provide sensible defaults where
// possible.
type DiscoverOptions struct {
	// Root is the project root, which must contain a go.mod file and defaults to "." when
	// empty.
	Root string

	// SourceDirs lists directories under Root to scan. When empty, the whole root is
	// scanned.
	SourceDirs []string

	// Scanners read the imports of the files they match. When empty, GoFileScanner is used.
	Scanners []SourceScanner

	// IgnoredPrefixes lists import-path prefixes to exclude, each matching the path itself
	// and every path below it. A host that compiles the project's own module from source
	// passes that module path here.
	IgnoredPrefixes []string

	// AlreadyProvided lists import paths the host already has symbol tables for, so
	// discovery does not report them as missing. Empty means every imported package looks
	// unprovided.
	AlreadyProvided []string

	// ExtraIgnored lists additional import paths to exclude from the result on top of
	// AlreadyProvided and IgnoredPrefixes.
	ExtraIgnored []string

	// FileFilter decides which Go files the interpreter compiles.
	FileFilter FileFilter

	// BuildTags selects matching files during project traversal and downstream package
	// loading.
	BuildTags []string

	// FollowProjectImports discovers dependencies by following imports of the project's own
	// packages.
	FollowProjectImports bool
}

// DiscoverResult captures the output of a Discover run. Values are deterministic: slices
// are sorted and deduplicated.
type DiscoverResult struct {
	// RequiredImports lists the import paths the scanned sources reference that the host
	// does not already provide and that were not ignored. This is the set that belongs in
	// the symbol manifest.
	RequiredImports []string

	// SkippedCgo lists discovered packages that use cgo and therefore cannot be interpreted.
	// These are reported so the user knows not to attempt registering them.
	SkippedCgo []string

	// GenericCandidates lists discovered packages that export generic types. These need a
	// manual generic: block in the manifest and are flagged so the user can address them.
	GenericCandidates []string

	// Unresolved lists imports that could not be loaded and their errors, excluding
	// them from RequiredImports.
	Unresolved []UnresolvedImport

	// Warnings lists non-fatal discovery problems that may leave results incomplete.
	Warnings []string
}

// UnresolvedImport is an import that discovery found but could not load, so it cannot be
// extracted.
type UnresolvedImport struct {
	// ImportPath is the import path the sources reference.
	ImportPath string

	// Reason says why the package could not be loaded.
	Reason string
}

// directInspection is what loading the candidate imports found.
type directInspection struct {
	// resolved lists the candidates that loaded as Go packages.
	resolved []string

	// cgo lists the resolved candidates that use cgo.
	cgo []string

	// generic lists the resolved candidates that export generic types.
	generic []string

	// unresolved lists the candidates that did not load, with the reason.
	unresolved []UnresolvedImport

	// warnings lists problems with the load as a whole.
	warnings []string
}

// discoverRun holds the settings of one Discover run with their defaults applied.
type discoverRun struct {
	// filter decides which Go files count.
	filter FileFilter

	// root is the absolute project root.
	root string

	// sourceDirs are the root-relative directories scanned.
	sourceDirs []string

	// scanners read the files they match.
	scanners []SourceScanner
}

// newDiscoverRun applies the defaults of DiscoverOptions.
//
// Takes opts (DiscoverOptions) which configures the run.
//
// Returns discoverRun which holds the settings.
// Returns error when the root cannot be made absolute.
func newDiscoverRun(opts DiscoverOptions) (discoverRun, error) {
	root := opts.Root
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return discoverRun{}, fmt.Errorf("resolving root %q: %w", opts.Root, err)
	}
	run := discoverRun{filter: opts.FileFilter, root: root, sourceDirs: opts.SourceDirs, scanners: opts.Scanners}
	if len(run.sourceDirs) == 0 {
		run.sourceDirs = []string{"."}
	}
	if len(run.scanners) == 0 {
		run.scanners = []SourceScanner{GoFileScanner{}}
	}
	if run.filter == nil {
		run.filter = DefaultFileFilter(opts.BuildTags)
	}
	return run, nil
}

// follow follows the project's own packages that the scanned sources import, adding their
// imports to sourceImports.
//
// Takes ignored (map[string]struct{}) which holds the provided and ignored packages,
// which are not followed.
// Takes ignoredPrefixes ([]string) which are the prefixes left out of the result.
// Takes sourceImports (map[string]struct{}) which receives the followed imports.
//
// Returns prefixes ([]string) which are the prefixes to leave out, now including the
// project module.
// Returns warnings ([]string) which are the warnings.
// Returns err (error) when following fails.
func (run discoverRun) follow(ctx context.Context, ignored map[string]struct{}, ignoredPrefixes []string, sourceImports map[string]struct{}) (prefixes, warnings []string, err error) {
	modulePath, err := projectModulePath(run.root)
	if err != nil {
		return ignoredPrefixes, []string{fmt.Sprintf("cannot follow project imports: %v", err)}, nil
	}
	warnings, err = followProjectImports(ctx, run.root, modulePath, run.filter, ignored, sourceImports)
	if err != nil {
		return nil, nil, err
	}
	return append(slices.Clone(ignoredPrefixes), modulePath), warnings, nil
}

// Discover walks a project, collects every Go import its scanned sources reference,
// filters out the packages the host already provides, and returns the remaining set as
// DiscoverResult.
//
// Takes opts (DiscoverOptions) which configures the run.
//
// Returns DiscoverResult which holds the deduplicated, sorted lists of discovered
// imports.
// Returns error when the project cannot be walked or loaded.
func Discover(ctx context.Context, opts DiscoverOptions) (DiscoverResult, error) {
	if err := ctx.Err(); err != nil {
		return DiscoverResult{}, fmt.Errorf(errDiscoverWrap, err)
	}
	run, err := newDiscoverRun(opts)
	if err != nil {
		return DiscoverResult{}, err
	}

	sourceImports, err := collectSourceImports(ctx, run.root, run.sourceDirs, run.scanners, run.filter)
	if err != nil {
		return DiscoverResult{}, err
	}

	ignored := buildIgnoreSet(opts.AlreadyProvided, opts.ExtraIgnored)
	ignoredPrefixes := opts.IgnoredPrefixes
	var warnings []string
	if opts.FollowProjectImports {
		ignoredPrefixes, warnings, err = run.follow(ctx, ignored, ignoredPrefixes, sourceImports)
		if err != nil {
			return DiscoverResult{}, err
		}
	}

	candidates := filterImports(sourceImports, ignored, ignoredPrefixes)
	if len(candidates) == 0 {
		return DiscoverResult{RequiredImports: nil, SkippedCgo: nil, GenericCandidates: nil, Unresolved: nil, Warnings: warnings}, nil
	}

	if err := ctx.Err(); err != nil {
		return DiscoverResult{}, fmt.Errorf(errDiscoverWrap, err)
	}

	inspection, err := inspectDirectPackages(ctx, run.root, opts.BuildTags, candidates)
	if err != nil {
		return DiscoverResult{}, err
	}

	return DiscoverResult{
		RequiredImports:   inspection.resolved,
		SkippedCgo:        inspection.cgo,
		GenericCandidates: inspection.generic,
		Unresolved:        inspection.unresolved,
		Warnings:          slices.Concat(warnings, inspection.warnings),
	}, nil
}

// filterImports removes ignored paths, ignored prefixes and cgo's pseudo-package.
// Standard-library packages remain candidates unless the host explicitly provides them.
//
// Takes sourceImports (map[string]struct{}) which is the raw import set.
// Takes ignored (map[string]struct{}) which holds the already-provided paths and
// user-supplied exclusions.
// Takes ignoredPrefixes ([]string) which are prefixes excluded with every path below
// them.
//
// Returns []string of sorted, deduplicated candidate imports.
func filterImports(sourceImports, ignored map[string]struct{}, ignoredPrefixes []string) []string {
	result := make(map[string]struct{}, len(sourceImports))
	for path := range sourceImports {
		if _, skip := ignored[path]; skip || path == cgoPseudoPackage {
			continue
		}
		if hasPathPrefix(path, ignoredPrefixes) {
			continue
		}
		result[path] = struct{}{}
	}
	return sortedKeys(result)
}

// hasPathPrefix reports whether path equals one of prefixes or sits below it, so "crypto"
// covers crypto and crypto/aes but not cryptobyte.
//
// Takes path (string) which is the import path to test.
// Takes prefixes ([]string) which are the ignored prefixes.
//
// Returns bool which is true when path is covered by a prefix.
func hasPathPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && (path == prefix || strings.HasPrefix(path, prefix+"/")) {
			return true
		}
	}
	return false
}

// inspectDirectPackages loads and classifies direct imports without traversing
// dependencies. Packages with load errors are unresolved, and cgo or generic exports
// are flagged for callers.
//
// Takes root (string) which is the absolute project root for package resolution.
// Takes buildTags ([]string) which is forwarded to packages.Load.
// Takes candidates ([]string) which is the filtered candidate list.
//
// Returns directInspection whose slices are sorted. When packages.Load fails, the
// candidates are returned as resolved with a warning.
// Returns error on cancellation.
func inspectDirectPackages(ctx context.Context, root string, buildTags, candidates []string) (directInspection, error) {
	if len(candidates) == 0 {
		return directInspection{}, nil
	}
	if err := ctx.Err(); err != nil {
		return directInspection{}, fmt.Errorf(errDiscoverWrap, err)
	}

	loaded, loadErr := packages.Load(discoverLoadConfig(ctx, root, buildTags), candidates...)
	if loadErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return directInspection{}, fmt.Errorf(errDiscoverWrap, ctxErr)
		}
		return directInspection{
			resolved:   candidates,
			cgo:        nil,
			generic:    nil,
			unresolved: nil,
			warnings:   []string{fmt.Sprintf("could not load the discovered packages, so they are reported unverified: %v", loadErr)},
		}, nil
	}
	if err := ctx.Err(); err != nil {
		return directInspection{}, fmt.Errorf(errDiscoverWrap, err)
	}

	loadedByPath := indexPackagesByPath(loaded)

	resolvedSet := make(map[string]struct{})
	cgoSet := make(map[string]struct{})
	genericSet := make(map[string]struct{})
	var unresolved []UnresolvedImport

	for _, path := range candidates {
		pkg, ok := loadedByPath[path]
		if reason := unresolvedReason(pkg, ok); reason != "" {
			unresolved = append(unresolved, UnresolvedImport{ImportPath: path, Reason: reason})
			continue
		}
		resolvedSet[path] = struct{}{}
		if packageUsesCgo(pkg) {
			cgoSet[path] = struct{}{}
			continue
		}
		if packageExportsGenericType(pkg) {
			genericSet[path] = struct{}{}
		}
	}
	return directInspection{
		resolved:   sortedKeys(resolvedSet),
		cgo:        sortedKeys(cgoSet),
		generic:    sortedKeys(genericSet),
		unresolved: unresolved,
		warnings:   nil,
	}, nil
}

// discoverLoadConfig returns the packages.Load configuration that inspects the candidate
// imports.
//
// Takes root (string) which is the project root the go command runs in.
// Takes buildTags ([]string) which are passed through -tags.
//
// Returns *packages.Config which loads names, files, imports and types.
func discoverLoadConfig(ctx context.Context, root string, buildTags []string) *packages.Config {
	config := &packages.Config{
		Context: ctx,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedImports |
			packages.NeedTypes,
		Dir: root,
	}
	if len(buildTags) > 0 {
		config.BuildFlags = []string{"-tags=" + strings.Join(buildTags, ",")}
	}
	return config
}

// unresolvedReason says why a candidate cannot be extracted.
//
// Takes pkg (*packages.Package) which is the loaded package, nil when not loaded.
// Takes loaded (bool) which reports whether the loader returned the package.
//
// Returns string which is the reason, or empty when the package is usable.
func unresolvedReason(pkg *packages.Package, loaded bool) string {
	switch {
	case !loaded || pkg == nil:
		return "the package loader did not return it"
	case len(pkg.Errors) > 1:
		return fmt.Sprintf("%s (and %d more errors)", pkg.Errors[0].Msg, len(pkg.Errors)-1)
	case len(pkg.Errors) == 1:
		return pkg.Errors[0].Msg
	case !packageHasGoSource(pkg):
		return "it has no Go source files"
	default:
		return ""
	}
}

// indexPackagesByPath returns a lookup table of loaded packages keyed by PkgPath so
// inspectDirectPackages can honour the caller's original ordering and detect missing
// entries deterministically.
//
// Takes loaded ([]*packages.Package) which is the packages.Load output.
//
// Returns a map keyed by PkgPath; nil entries and empty paths are skipped.
func indexPackagesByPath(loaded []*packages.Package) map[string]*packages.Package {
	result := make(map[string]*packages.Package, len(loaded))
	for _, pkg := range loaded {
		if pkg == nil || pkg.PkgPath == "" {
			continue
		}
		result[pkg.PkgPath] = pkg
	}
	return result
}

// packageHasGoSource reports whether a loaded package contains any Go source files.
// Virtual paths such as a docs directory with no Go code surface in packages.Load output
// with an empty GoFiles list; they must be excluded from the registered set.
//
// Takes pkg (*packages.Package) which is the loaded package.
//
// Returns bool which is true when the package has at least one .go file.
func packageHasGoSource(pkg *packages.Package) bool {
	if pkg == nil {
		return false
	}
	return len(pkg.GoFiles) > 0 || len(pkg.CompiledGoFiles) > 0
}

// collectSourceImports walks the source directories through an os.Root opened on the
// project root and returns every import the scanners report.
//
// Takes root (string) which is the absolute project root.
// Takes sourceDirs ([]string) which are root-relative directories to scan.
// Takes scanners ([]SourceScanner) which read the files they match.
// Takes filter (FileFilter) which decides which Go files count.
//
// Returns map[string]struct{} which is a set keyed by import path.
// Returns error when the root cannot be opened, or file I/O or parsing fails.
func collectSourceImports(ctx context.Context, root string, sourceDirs []string, scanners []SourceScanner, filter FileFilter) (map[string]struct{}, error) {
	projectRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening project root %s: %w", root, err)
	}
	defer func() { _ = projectRoot.Close() }()

	imports := make(map[string]struct{})
	for _, directoryName := range sourceDirs {
		if err := walkSourceDirectory(ctx, projectRoot, directoryName, scanners, filter, imports); err != nil {
			return nil, err
		}
	}
	return imports, nil
}

// walkSourceDirectory walks one source directory and adds the imports of every file a
// scanner matches, skipping hidden, underscore-prefixed, vendor and testdata directories.
//
// Takes projectRoot (*os.Root) which confines reads to the project root.
// Takes directoryName (string) which is the root-relative directory to walk.
// Takes scanners ([]SourceScanner) which read the files they match.
// Takes filter (FileFilter) which decides which Go files count.
// Takes imports (map[string]struct{}) which accumulates the results.
//
// Returns error when I/O or parsing fails, or when discovery limits are exceeded.
func walkSourceDirectory(ctx context.Context, projectRoot *os.Root, directoryName string, scanners []SourceScanner, filter FileFilter, imports map[string]struct{}) error {
	info, err := projectRoot.Stat(directoryName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", directoryName, err)
	}
	if !info.IsDir() {
		return nil
	}

	walker := sourceWalker{ctx: ctx, projectRoot: projectRoot, directoryName: directoryName, scanners: scanners, filter: filter, imports: imports}
	walkErr := fs.WalkDir(projectRoot.FS(), directoryName, walker.visit)
	if walkErr != nil {
		return fmt.Errorf("walking %s: %w", directoryName, walkErr)
	}
	return nil
}

// sourceWalker holds the state of one source directory walk.
type sourceWalker struct {
	// ctx carries cancellation.
	ctx context.Context

	// projectRoot confines reads to the project root.
	projectRoot *os.Root

	// imports accumulates the discovered import paths.
	imports map[string]struct{}

	// filter decides which Go files count.
	filter FileFilter

	// directoryName is the root-relative directory being walked.
	directoryName string

	// scanners read the files they match.
	scanners []SourceScanner
}

// visit is the fs.WalkDir callback. It skips tool directories and scans every file a
// scanner matches.
//
// Takes path (string) which is the root-relative path of the entry.
// Takes entry (fs.DirEntry) which describes the entry.
// Takes walkErr (error) which reports a failure to read the entry.
//
// Returns error to stop the walk, or fs.SkipDir to skip a directory.
func (walker *sourceWalker) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if err := walker.ctx.Err(); err != nil {
		return err
	}
	if entry.IsDir() {
		if path != walker.directoryName && isSkippedDirectory(entry.Name()) {
			return fs.SkipDir
		}
		return nil
	}
	scanner := matchingScanner(entry.Name(), walker.scanners)
	if scanner == nil {
		return nil
	}
	if len(walker.imports) >= maxDiscoveredImports {
		return fmt.Errorf("%w: %d", errTooManyImports, len(walker.imports))
	}
	return appendFileImports(walker.ctx, walker.projectRoot, path, scanner, walker.filter, walker.imports)
}

// isSkippedDirectory reports whether the walk should not descend into a directory.
//
// Takes name (string) which is the directory's base name.
//
// Returns bool which is true for hidden, "_"-prefixed, vendor and testdata directories.
func isSkippedDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "testdata"
}

// matchingScanner returns the first scanner that reads files with this name. Files whose
// name starts with "_" are never scanned.
//
// Takes name (string) which is the file's base name.
// Takes scanners ([]SourceScanner) which are tried in order.
//
// Returns SourceScanner, or nil when no scanner matches.
func matchingScanner(name string, scanners []SourceScanner) SourceScanner {
	if strings.HasPrefix(name, "_") {
		return nil
	}
	for _, scanner := range scanners {
		if scanner.Match(name) {
			return scanner
		}
	}
	return nil
}

// appendFileImports reads one file within the size limit and adds the imports its scanner
// reports to the set. A Go file the interpreter does not compile adds nothing.
//
// Takes projectRoot (*os.Root) which confines the read to the project root.
// Takes path (string) which is the root-relative file path.
// Takes scanner (SourceScanner) which reads the file's imports.
// Takes filter (FileFilter) which decides which Go files count.
// Takes imports (map[string]struct{}) which receives the discovered paths.
//
// Returns error when the file cannot be read or scanned, or the import limit is reached.
func appendFileImports(ctx context.Context, projectRoot *os.Root, path string, scanner SourceScanner, filter FileFilter, imports map[string]struct{}) error {
	data, err := readBoundedFile(projectRoot, path, maxSourceFileSizeBytes)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if strings.HasSuffix(path, ".go") && !keepsFile(filter, path, data) {
		return nil
	}
	found, err := scanner.Imports(ctx, path, data)
	if err != nil {
		return err
	}
	for _, value := range found {
		if value == "" {
			continue
		}
		if len(imports) >= maxDiscoveredImports {
			return fmt.Errorf("%w: %d", errTooManyImports, len(imports))
		}
		imports[value] = struct{}{}
	}
	return nil
}

// readBoundedFile reads a regular file within the root up to limit bytes, rejecting
// non-regular files and files that exceed the limit before or during reading.
//
// Takes projectRoot (*os.Root) which confines the read to its root.
// Takes name (string) which is the root-relative path to read.
// Takes limit (int64) which is the maximum acceptable byte count.
//
// Returns []byte which is the file content.
// Returns error which wraps errSourceFileTooLarge when the file exceeds the cap,
// errNotRegularFile for a non-regular file, or any underlying I/O failure.
func readBoundedFile(projectRoot *os.Root, name string, limit int64) (data []byte, err error) {
	info, err := projectRoot.Stat(name)
	if err != nil {
		return nil, err
	}
	if err := checkReadable(name, info, limit); err != nil {
		return nil, err
	}
	file, err := projectRoot.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := checkReadable(name, opened, limit); err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s (>%d bytes)", errSourceFileTooLarge, name, limit)
	}
	return data, nil
}

// checkReadable reports why a file cannot be read within limit.
//
// Takes name (string) which names the file in errors.
// Takes info (fs.FileInfo) which describes it.
// Takes limit (int64) which is the maximum acceptable byte count.
//
// Returns error which wraps errNotRegularFile or errSourceFileTooLarge, or nil.
func checkReadable(name string, info fs.FileInfo, limit int64) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", errNotRegularFile, name)
	}
	if info.Size() > limit {
		return fmt.Errorf("%w: %s (>%d bytes)", errSourceFileTooLarge, name, limit)
	}
	return nil
}

// buildIgnoreSet assembles the complete set of import paths excluded from the discovered
// list.
//
// Both inputs are caller-supplied so that discovery holds no opinion about which symbol
// tables the host ships.
//
// Takes provided ([]string) which are import paths the host already has symbol tables
// for.
// Takes extras ([]string) which are caller-supplied ignores on top of those.
//
// Returns a set keyed by import path.
func buildIgnoreSet(provided []string, extras []string) map[string]struct{} {
	ignored := make(map[string]struct{}, len(provided)+len(extras))
	for _, path := range provided {
		ignored[path] = struct{}{}
	}
	for _, path := range extras {
		ignored[path] = struct{}{}
	}
	return ignored
}

// packageUsesCgo reports whether any file in the loaded package imports the
// pseudo-package "C", which indicates cgo usage and therefore incompatibility with the
// interpreter.
//
// Takes pkg (*packages.Package) which is the loaded package.
//
// Returns bool which is true when cgo is used.
func packageUsesCgo(pkg *packages.Package) bool {
	for _, imp := range pkg.Imports {
		if imp != nil && imp.PkgPath == "C" {
			return true
		}
	}
	for _, goFile := range pkg.GoFiles {
		if strings.HasSuffix(goFile, ".cgo1.go") {
			return true
		}
	}
	return false
}

// packageExportsGenericType reports whether the package has any exported type declaration
// with type parameters. Such packages need manual generic: configuration in the manifest.
//
// Takes pkg (*packages.Package) which is the loaded package.
//
// Returns bool which is true when a generic exported type is present.
func packageExportsGenericType(pkg *packages.Package) bool {
	if pkg.Types == nil {
		return false
	}
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		typeName, isTypeName := object.(*types.TypeName)
		if !isTypeName || !typeName.Exported() {
			continue
		}
		named, isNamed := typeName.Type().(*types.Named)
		if !isNamed {
			continue
		}
		if named.TypeParams() != nil && named.TypeParams().Len() > 0 {
			return true
		}
	}
	return false
}

// sortedKeys returns the keys of a set as a sorted slice so callers receive deterministic
// output.
//
// Takes set (map[string]struct{}) which is the source set.
//
// Returns []string sorted ascending.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

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
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"go/format"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/gcexportdata"
)

const (
	// TypesExportDirectory is the directory, relative to the generated package, that holds
	// one gzip-compressed gcexportdata blob per extracted package.
	TypesExportDirectory = "gen_types_export"

	// TypesExportBlobSuffix is the file suffix of every export blob.
	TypesExportBlobSuffix = ".bin.gz"

	// TypesManifestFileName is the generated Go file that lists every export blob together
	// with its in-set dependencies and the toolchain that produced it.
	TypesManifestFileName = "gen_types_manifest.go"

	// TypesLoaderFileName is the generated Go file that decodes the embedded blobs lazily.
	TypesLoaderFileName = "gen_types_loader.go"

	// TypesLoaderWASMFileName is the js/wasm twin of TypesLoaderFileName, which embeds
	// nothing and exposes an empty package set.
	TypesLoaderWASMFileName = "gen_types_loader_javascript.go"

	// nativeTypesLoaderBuildTag is the build constraint shared by the manifest and the
	// native loader.
	nativeTypesLoaderBuildTag = "//go:build !js || !wasm"

	// typesLoaderBody is the toolchain-free loader emitted after the package clause.
	typesLoaderBody = `import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"io"
	"log/slog"
	"sync"

	"golang.org/x/tools/go/gcexportdata"
)

// typesExportFS holds the gzip-compressed gcexportdata blob of every package in
// typesExportManifest.
//
//go:embed gen_types_export/*
var typesExportFS embed.FS

var (
	// typesPackagesOnce guards the one-time decode of the embedded export data.
	typesPackagesOnce sync.Once

	// typesPackagesLoaded is the decoded package set keyed by import path.
	typesPackagesLoaded = make(map[string]*types.Package)

	// typesPackagesLoadError joins the per-package decode failures, if any.
	typesPackagesLoadError error
)

// TypesPackages returns the pre-built *types.Package objects for every extracted package,
// decoding the embedded export data dependency-first on first call. Packages that fail to
// decode are omitted; TypesPackagesLoadError reports them.
//
// Returns the package set keyed by import path.
func TypesPackages() map[string]*types.Package {
	typesPackagesOnce.Do(loadTypesPackages)
	return typesPackagesLoaded
}

// TypesPackagesLoadError reports why any embedded package failed to decode.
//
// Returns the joined per-package errors, or nil when every package decoded.
func TypesPackagesLoadError() error {
	typesPackagesOnce.Do(loadTypesPackages)
	return typesPackagesLoadError
}

// loadTypesPackages decodes every manifest entry into typesPackagesLoaded, visiting
// in-set dependencies before their dependants and sharing one FileSet and import map so
// cross-package references resolve to the same *types.Package instances.
func loadTypesPackages() {
	fset := token.NewFileSet()
	imports := make(map[string]*types.Package, len(typesExportManifest))
	entries := make(map[string]typesExportEntry, len(typesExportManifest))
	for _, entry := range typesExportManifest {
		entries[entry.Path] = entry
	}
	loader := typesExportLoader{fset: fset, imports: imports, entries: entries, visited: make(map[string]bool, len(entries))}
	for _, entry := range typesExportManifest {
		loader.load(entry.Path)
	}
	if len(loader.errs) == 0 {
		return
	}
	typesPackagesLoadError = errors.Join(loader.errs...)
	slog.Warn("embedded types export data failed to decode; generic type checking is degraded",
		slog.String("generated_with", typesExportGoVersion),
		slog.Any("error", typesPackagesLoadError),
	)
}

// typesExportLoader carries the shared decode state across the dependency walk.
type typesExportLoader struct {
	fset    *token.FileSet
	imports map[string]*types.Package
	entries map[string]typesExportEntry
	visited map[string]bool
	errs    []error
}

// load decodes one entry after its in-set dependencies, at most once per path.
//
// Takes importPath (string) which is the entry to decode.
func (l *typesExportLoader) load(importPath string) {
	if l.visited[importPath] {
		return
	}
	l.visited[importPath] = true
	entry, known := l.entries[importPath]
	if !known {
		return
	}
	for _, dependency := range entry.Dependencies {
		l.load(dependency)
	}
	pkg, err := decodeTypesExportBlob(l.fset, l.imports, entry)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s: %w", importPath, err))
		return
	}
	typesPackagesLoaded[importPath] = pkg
}

// decodeTypesExportBlob decompresses and reads one embedded blob.
//
// Takes fset (*token.FileSet) which receives the decoded positions.
// Takes imports (map[string]*types.Package) which is the shared import cache.
// Takes entry (typesExportEntry) which names the blob and its import path.
//
// Returns the decoded package.
// Returns error when the blob is missing, corrupt, or fails to decode.
func decodeTypesExportBlob(fset *token.FileSet, imports map[string]*types.Package, entry typesExportEntry) (*types.Package, error) {
	compressed, err := typesExportFS.ReadFile(entry.File)
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if err := reader.Close(); err != nil {
		return nil, err
	}
	return gcexportdata.Read(bytes.NewReader(data), fset, imports, entry.Path)
}
`

	// typesLoaderWASMBody is the js/wasm loader emitted after the package clause.
	typesLoaderWASMBody = `import "go/types"

// TypesPackages returns the pre-built *types.Package set. Export data is not embedded in
// the js/wasm build, so the set is empty.
//
// Returns an empty map.
func TypesPackages() map[string]*types.Package {
	return map[string]*types.Package{}
}

// TypesPackagesLoadError reports why any embedded package failed to decode.
//
// Returns nil, as the js/wasm build embeds nothing.
func TypesPackagesLoadError() error {
	return nil
}
`
)

var (
	// errTypesExportBlobCollision reports two import paths escaping to the same blob name.
	errTypesExportBlobCollision = errors.New("types export blob file name collision")

	// errTypesExportMissingPackage reports an extracted package with no loaded types.
	errTypesExportMissingPackage = errors.New("extracted package has no types.Package to export")
)

// TypesExportBlob is one package's serialised go/types data ready to be embedded.
type TypesExportBlob struct {
	// ImportPath is the Go import path the blob describes.
	ImportPath string

	// FileName is the blob's path relative to the generated package directory.
	FileName string

	// Dependencies lists the import paths, within the same generated set, that the package
	// imports directly. The loader decodes them first so cross-package references resolve to
	// complete packages.
	Dependencies []string

	// Data is the gzip-compressed gcexportdata payload.
	Data []byte
}

// TypesExportBlobFileName maps an import path to its blob path relative to the generated
// package directory, using the same slash escaping as OutputFileName.
//
// Takes importPath (string) which is the Go import path.
//
// Returns the relative blob path, for example "gen_types_export/net_http.bin.gz".
func TypesExportBlobFileName(importPath string) string {
	return TypesExportDirectory + "/" + strings.ReplaceAll(importPath, "/", "_") + TypesExportBlobSuffix
}

// GenerateTypesExportBlobs serialises the loaded go/types package of every extracted
// package with gcexportdata so the generated package can rebuild complete type
// information at run time without a Go toolchain. Positions are dropped from the export
// data so the output is independent of the generating machine's GOROOT.
//
// Takes packages ([]ExtractedPackage) which carry the loaded types packages.
//
// Returns []TypesExportBlob which holds the blobs sorted by import path.
// Returns error when a package has no types, encoding fails, or two import paths escape
// to the same blob name.
func GenerateTypesExportBlobs(packages []ExtractedPackage) ([]TypesExportBlob, error) {
	inSet := make(map[string]bool, len(packages))
	for i := range packages {
		inSet[packages[i].ImportPath] = true
	}
	blobs := make([]TypesExportBlob, 0, len(packages))
	seenFiles := make(map[string]string, len(packages))
	for i := range packages {
		blob, err := encodeTypesExportBlob(&packages[i], inSet)
		if err != nil {
			return nil, err
		}
		if previous, clash := seenFiles[blob.FileName]; clash {
			return nil, fmt.Errorf("%w: %s and %s both map to %s", errTypesExportBlobCollision, previous, blob.ImportPath, blob.FileName)
		}
		seenFiles[blob.FileName] = blob.ImportPath
		blobs = append(blobs, blob)
	}
	slices.SortFunc(blobs, func(a, b TypesExportBlob) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	return blobs, nil
}

// encodeTypesExportBlob serialises and compresses one package.
//
// Takes pkg (*ExtractedPackage) which carries the loaded types package.
// Takes inSet (map[string]bool) which marks the import paths in the generated set.
//
// Returns TypesExportBlob which is the blob for the package.
// Returns error when the package has no types or encoding fails.
func encodeTypesExportBlob(pkg *ExtractedPackage, inSet map[string]bool) (TypesExportBlob, error) {
	if pkg.TypesPackage == nil {
		return TypesExportBlob{}, fmt.Errorf("%w: %s", errTypesExportMissingPackage, pkg.ImportPath)
	}
	var raw bytes.Buffer
	if err := gcexportdata.Write(&raw, nil, pkg.TypesPackage); err != nil {
		return TypesExportBlob{}, fmt.Errorf("encoding types export for %s: %w", pkg.ImportPath, err)
	}
	compressed, err := gzipDeterministic(raw.Bytes())
	if err != nil {
		return TypesExportBlob{}, fmt.Errorf("compressing types export for %s: %w", pkg.ImportPath, err)
	}
	return TypesExportBlob{
		ImportPath:   pkg.ImportPath,
		FileName:     TypesExportBlobFileName(pkg.ImportPath),
		Dependencies: inSetDependencies(pkg.TypesPackage, inSet),
		Data:         compressed,
	}, nil
}

// inSetDependencies lists the direct imports of pkg that belong to the generated set.
//
// Takes pkg (*types.Package) which is the loaded package.
// Takes inSet (map[string]bool) which marks the import paths in the generated set.
//
// Returns the sorted, de-duplicated dependency import paths.
func inSetDependencies(pkg *types.Package, inSet map[string]bool) []string {
	dependencies := make([]string, 0, len(pkg.Imports()))
	for _, imported := range pkg.Imports() {
		if imported.Path() != pkg.Path() && inSet[imported.Path()] {
			dependencies = append(dependencies, imported.Path())
		}
	}
	slices.Sort(dependencies)
	return slices.Compact(dependencies)
}

// gzipDeterministic compresses data with a zeroed gzip header so identical input always
// produces identical bytes.
//
// Takes data ([]byte) which is the payload to compress.
//
// Returns []byte which is the compressed output.
// Returns error when compression fails.
func gzipDeterministic(data []byte) ([]byte, error) {
	var out bytes.Buffer
	writer, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// GenerateTypesManifestFile calls Generator.GenerateTypesManifestFile with the default
// tool name.
//
// Takes blobs ([]TypesExportBlob) which are the blobs to list.
// Takes outputPackage (string) which is the Go package name for the generated file.
// Takes goVersion (string) which is the generating runtime.Version().
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func GenerateTypesManifestFile(blobs []TypesExportBlob, outputPackage, goVersion string) ([]byte, error) {
	return defaultGenerator.GenerateTypesManifestFile(blobs, outputPackage, goVersion)
}

// GenerateTypesManifestFile produces the Go source listing every export blob, its in-set
// dependencies and the toolchain version that generated the set.
//
// Takes blobs ([]TypesExportBlob) which are the blobs to list, in the order to emit them.
// Takes outputPackage (string) which is the Go package name for the generated file.
// Takes goVersion (string) which is the generating runtime.Version().
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func (g Generator) GenerateTypesManifestFile(blobs []TypesExportBlob, outputPackage, goVersion string) ([]byte, error) {
	var source strings.Builder
	source.WriteString(g.marker() + "\n\n" + nativeTypesLoaderBuildTag + "\n\n")
	source.WriteString("package " + outputPackage + "\n\n")
	source.WriteString("// typesExportGoVersion is the toolchain that produced the embedded export data.\n")
	source.WriteString("const typesExportGoVersion = " + strconv.Quote(goVersion) + "\n\n")
	source.WriteString("// typesExportEntry describes one embedded gcexportdata blob.\n")
	source.WriteString("type typesExportEntry struct {\n")
	source.WriteString("\t// Path is the Go import path the blob describes.\n\tPath string\n\n")
	source.WriteString("\t// File is the blob's path within the embedded file system.\n\tFile string\n\n")
	source.WriteString("\t// Dependencies lists the import paths in this set that must be decoded first.\n\tDependencies []string\n")
	source.WriteString("}\n\n")
	source.WriteString("// typesExportManifest lists every embedded blob sorted by import path.\n")
	source.WriteString("var typesExportManifest = []typesExportEntry{\n")
	for _, blob := range blobs {
		writeTypesManifestEntry(&source, blob)
	}
	source.WriteString("}\n")
	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", TypesManifestFileName, err)
	}
	return formatted, nil
}

// writeTypesManifestEntry appends one manifest literal to the builder.
//
// Takes source (*strings.Builder) which accumulates the Go source.
// Takes blob (TypesExportBlob) which is the entry to write.
func writeTypesManifestEntry(source *strings.Builder, blob TypesExportBlob) {
	source.WriteString("\t{Path: " + strconv.Quote(blob.ImportPath) + ", File: " + strconv.Quote(blob.FileName))
	if len(blob.Dependencies) > 0 {
		quoted := make([]string, len(blob.Dependencies))
		for i, dependency := range blob.Dependencies {
			quoted[i] = strconv.Quote(dependency)
		}
		source.WriteString(", Dependencies: []string{" + strings.Join(quoted, ", ") + "}")
	}
	source.WriteString("},\n")
}

// GenerateTypesLoaderFile calls Generator.GenerateTypesLoaderFile with the default tool
// name.
//
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func GenerateTypesLoaderFile(outputPackage string) ([]byte, error) {
	return defaultGenerator.GenerateTypesLoaderFile(outputPackage)
}

// GenerateTypesLoaderFile produces the Go source that embeds the export blobs and decodes
// them on first use, dependency-first, into complete *types.Package values. Decoding
// needs no Go toolchain, so deployments without one still type-check generic calls.
//
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func (g Generator) GenerateTypesLoaderFile(outputPackage string) ([]byte, error) {
	source := g.marker() + "\n\n" + nativeTypesLoaderBuildTag + "\n\n" +
		"package " + outputPackage + "\n\n" + typesLoaderBody
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", TypesLoaderFileName, err)
	}
	return formatted, nil
}

// GenerateTypesLoaderWASMFile calls Generator.GenerateTypesLoaderWASMFile with the
// default tool name.
//
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func GenerateTypesLoaderWASMFile(outputPackage string) ([]byte, error) {
	return defaultGenerator.GenerateTypesLoaderWASMFile(outputPackage)
}

// GenerateTypesLoaderWASMFile produces the js/wasm twin of the loader, which embeds no
// export data and exposes an empty package set.
//
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func (g Generator) GenerateTypesLoaderWASMFile(outputPackage string) ([]byte, error) {
	source := g.marker() + "\n\n//go:build js && wasm\n\n" +
		"package " + outputPackage + "\n\n" + typesLoaderWASMBody
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", TypesLoaderWASMFileName, err)
	}
	return formatted, nil
}

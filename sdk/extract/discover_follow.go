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
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

const (
	// interpBuildTag selects files compiled by the interpreter.
	interpBuildTag = "interp"
)

// projectFollower reads the project's own packages while following their imports.
type projectFollower struct {
	// projectRoot confines reads to the project root.
	projectRoot *os.Root

	// imports accumulates every import path found, shared with the scanned sources.
	imports map[string]struct{}

	// visited holds the project packages already read.
	visited map[string]struct{}

	// modulePath is the project's module path.
	modulePath string

	// filter selects the files the interpreter compiles.
	filter FileFilter

	// skip holds the project packages that are provided or ignored, so not read.
	skip map[string]struct{}

	// warnings collects the project packages that could not be read.
	warnings []string
}

// follow reads one project package and records its imports.
//
// Takes importPath (string) which is the project package to read.
//
// Returns []string which holds the project packages it imports that were not seen before.
// Returns error when the context ends or the import limit is reached; a package that
// cannot be read becomes a warning instead.
func (follower *projectFollower) follow(ctx context.Context, importPath string) ([]string, error) {
	imports, err := follower.packageImports(ctx, importPath)
	if err != nil {
		if ctxErr := contextFailure(ctx); ctxErr != nil {
			return nil, fmt.Errorf(errDiscoverWrap, ctxErr)
		}
		follower.warnings = append(follower.warnings, fmt.Sprintf("cannot follow the imports of %s: %v", importPath, err))
		return nil, nil
	}

	var found []string
	for _, imported := range imports {
		if _, seen := follower.imports[imported]; seen {
			continue
		}
		if len(follower.imports) >= maxDiscoveredImports {
			return nil, fmt.Errorf("%w: %d", errTooManyImports, len(follower.imports))
		}
		follower.imports[imported] = struct{}{}
		if follower.followed(imported) {
			found = append(found, imported)
		}
	}
	return found, nil
}

// followed reports whether importPath belongs to the project and is neither host-provided
// nor ignored.
//
// Takes importPath (string) which is the import path to test.
//
// Returns bool which is true when the package is read.
func (follower *projectFollower) followed(importPath string) bool {
	if _, skipped := follower.skip[importPath]; skipped {
		return false
	}
	return hasPathPrefix(importPath, []string{follower.modulePath})
}

// packageImports collects imports from a project package's selected non-test files.
// Unreadable files and packages without selected source produce warnings.
//
// Takes importPath (string) which is a path inside the project module.
//
// Returns []string which holds the import paths, sorted and deduplicated.
// Returns error when the directory cannot be read or the context ends.
func (follower *projectFollower) packageImports(ctx context.Context, importPath string) ([]string, error) {
	directory := strings.TrimPrefix(strings.TrimPrefix(importPath, follower.modulePath), "/")
	if directory == "" {
		directory = "."
	}
	if !fs.ValidPath(directory) {
		return nil, fmt.Errorf("%w: %q", errNotProjectDirectory, directory)
	}

	entries, err := fs.ReadDir(follower.projectRoot.FS(), directory)
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	goFiles, excludedFiles := 0, 0
	for _, entry := range entries {
		if err := contextFailure(ctx); err != nil {
			return nil, err
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		goFiles++
		if follower.addFileImports(directory, name, set) {
			excludedFiles++
		}
	}
	if goFiles > 0 && excludedFiles == goFiles {
		follower.warnings = append(follower.warnings, fmt.Sprintf("%s has no Go files the interpreter compiles, for example because they need cgo", importPath))
	}
	return sortedKeys(set), nil
}

// addFileImports adds the imports of one Go file to set when the interpreter compiles it.
// A file that cannot be read or parsed becomes a warning and is not counted as left out,
// so the package gets one warning for it.
//
// Takes directory (string) which is the package directory, relative to the project root.
// Takes name (string) which is the file's name.
// Takes set (map[string]struct{}) which receives the import paths.
//
// Returns bool which is true when the interpreter leaves the file out.
func (follower *projectFollower) addFileImports(directory, name string, set map[string]struct{}) bool {
	imports, compiled, err := follower.fileImports(directory, name)
	if err != nil {
		follower.warnings = append(follower.warnings, fmt.Sprintf("cannot read the imports of %s: %v", path.Join(directory, name), err))
		return false
	}
	for _, imported := range imports {
		set[imported] = struct{}{}
	}
	return !compiled
}

// fileImports returns the imports of one Go file, or nothing when the interpreter does
// not compile it.
//
// Takes directory (string) which is the root-relative package directory.
// Takes name (string) which is the file's base name.
//
// Returns []string which holds the file's import paths.
// Returns bool which is true when the interpreter compiles the file.
// Returns error when the file cannot be read or parsed.
func (follower *projectFollower) fileImports(directory, name string) ([]string, bool, error) {
	filePath := path.Join(directory, name)
	data, err := readBoundedFile(follower.projectRoot, filePath, maxSourceFileSizeBytes)
	if err != nil {
		return nil, false, err
	}
	if !keepsFile(follower.filter, filePath, data) {
		return nil, false, nil
	}
	imports, err := ParseGoImports(filePath, data)
	if err != nil {
		return nil, false, err
	}
	return imports, true, nil
}

// followProjectImports reads the project's own packages that the scanned sources import
// and adds their imports to sourceImports, repeating for every project package reached.
//
// Takes root (string) which is the absolute project root.
// Takes modulePath (string) which is the project's module path.
// Takes filter (FileFilter) which selects the files the interpreter compiles.
// Takes skip (map[string]struct{}) which holds the packages the host already provides or
// the caller ignores; they are not read, since they are not compiled from source.
// Takes sourceImports (map[string]struct{}) which holds the scanned imports and receives
// the followed ones.
//
// Returns []string which holds a warning for each project package that could not be read.
// Returns error when the root cannot be opened, the context ends, or the import limit is
// reached.
func followProjectImports(ctx context.Context, root, modulePath string, filter FileFilter, skip, sourceImports map[string]struct{}) ([]string, error) {
	projectRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening project root %s: %w", root, err)
	}
	defer func() { _ = projectRoot.Close() }()

	follower := &projectFollower{
		projectRoot: projectRoot,
		imports:     sourceImports,
		visited:     make(map[string]struct{}),
		modulePath:  modulePath,
		filter:      filter,
		skip:        skip,
		warnings:    nil,
	}

	var pending []string
	for importPath := range sourceImports {
		if follower.followed(importPath) {
			pending = append(pending, importPath)
		}
	}
	slices.Sort(pending)

	for len(pending) > 0 {
		if err := contextFailure(ctx); err != nil {
			return nil, fmt.Errorf(errDiscoverWrap, err)
		}
		importPath := pending[0]
		pending = pending[1:]
		if _, seen := follower.visited[importPath]; seen {
			continue
		}
		follower.visited[importPath] = struct{}{}

		found, err := follower.follow(ctx, importPath)
		if err != nil {
			return nil, err
		}
		pending = append(pending, found...)
	}
	return follower.warnings, nil
}

// contextFailure returns why ctx has ended, wrapping both its error and the cause it was
// cancelled with, or nil while it is live.
//
// Returns error which wraps ctx.Err() and context.Cause(ctx), or nil.
func contextFailure(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(err, cause) {
		return fmt.Errorf("%w: %w", err, cause)
	}
	return err
}

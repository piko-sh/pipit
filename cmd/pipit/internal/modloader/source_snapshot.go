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
	"io/fs"
	"path/filepath"
)

// SourceSnapshot owns the bounded local bytes used for a gated invocation's identity. It
// freezes captured bytes, not a point-in-time filesystem or external dependencies.
type SourceSnapshot struct {
	// sources maps relative file paths to their captured contents.
	sources map[string]string

	// path is the canonical absolute target path.
	path string

	// root is the absolute source root directory.
	root string

	// identity is the domain-separated SHA-256 digest.
	identity string

	// directory reports whether the target is a source directory rather than a single file.
	directory bool
}

// Path returns the canonical target used for approval.
//
// Returns string containing the absolute target path.
func (snapshot *SourceSnapshot) Path() string { return snapshot.path }

// Identity returns the source and invocation approval digest.
//
// Returns string containing the domain-separated SHA-256 digest.
func (snapshot *SourceSnapshot) Identity() string { return snapshot.identity }

// IsDirectory reports the target kind captured before execution.
//
// Returns bool which is true for a source directory.
func (snapshot *SourceSnapshot) IsDirectory() bool { return snapshot.directory }

// ReadFile returns captured bytes without consulting the live filesystem.
//
// Takes path (string) which must select a captured path beneath the source root.
//
// Returns []byte which is an independent copy of the captured source.
// Returns error when the requested file was not captured.
func (snapshot *SourceSnapshot) ReadFile(path string) ([]byte, error) {
	relative, err := filepath.Rel(snapshot.root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, fs.ErrNotExist
	}
	source, ok := snapshot.sources[relative]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(source), nil
}

// DirectorySources returns a defensive map of direct, non-test Go source files.
//
// Returns map[string]string containing the captured target package.
func (snapshot *SourceSnapshot) DirectorySources() map[string]string {
	sources, _ := snapshot.PackageSources(snapshot.root)
	return sources
}

// PackageSources returns the captured, non-test Go files directly inside one directory
// beneath the source root, filtered by the host's build constraints, so a local package
// of a gated invocation is served from the approved bytes rather than the live tree.
//
// Takes dir (string) which is the absolute package directory.
//
// Returns map[string]string which maps file name to source.
// Returns error when dir lies outside the captured root.
func (snapshot *SourceSnapshot) PackageSources(dir string) (map[string]string, error) {
	relative, err := filepath.Rel(snapshot.root, dir)
	if err != nil || !filepath.IsLocal(relative) && relative != "." {
		return nil, fs.ErrNotExist
	}
	sources := make(map[string]string)
	for name, source := range snapshot.sources {
		if filepath.Dir(name) != relative || !isBundledSource(name) {
			continue
		}
		base := filepath.Base(name)
		if compilableForHost(dir, base, source) {
			sources[base] = source
		}
	}
	return sources, nil
}

// CaptureInvocation acquires local source once for both approval and execution.
//
// Takes target (string) which selects a source file or directory.
// Takes axes ([]string) which contains the selected capability axes.
// Takes entrypoint (string) which selects execution.
// Takes arguments ([]string) which contains ordered script arguments.
//
// Returns *SourceSnapshot which is the captured source.
// Returns error when bounded acquisition or invocation validation fails.
func CaptureInvocation(target string, axes []string, entrypoint string, arguments []string) (*SourceSnapshot, error) {
	if err := validateInvocation(axes, entrypoint, arguments); err != nil {
		return nil, err
	}
	snapshot, err := captureSource(target, true)
	if err != nil {
		return nil, err
	}
	snapshot.identity, err = invocationIdentityHash(snapshot.identity, axes, entrypoint, arguments)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

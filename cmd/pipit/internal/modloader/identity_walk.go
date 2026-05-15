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
	"cmp"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// walk traverses only a bounded set of entries through the pinned source root.
//
// Takes relative (string) which selects a directory beneath the source root.
// Takes depth (int) which bounds retained recursion frames and directory paths.
//
// Returns error when traversal cannot stay within its entry and depth budgets.
func (scan *scriptIdentityScan) walk(relative string, depth int) error {
	if depth > maxIdentityDepth {
		return errors.New("modloader: source identity exceeds directory depth limit")
	}
	entries, err := scan.readDirectory(relative)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(relative, entry.Name())
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".pipit" {
				continue
			}
			if err := scan.walk(child, depth+1); err != nil {
				return err
			}
		} else if err := scan.visit(filepath.Join(scan.root, child), entry); err != nil {
			return err
		}
	}
	return nil
}

// readDirectory reads bounded batches before sorting for deterministic identity hashing.
// Every entry consumes the scan-wide budget, including files not selected for hashing.
//
// Takes relative (string) which selects a directory beneath the pinned source root.
//
// Returns []fs.DirEntry in lexical order, bounded by the remaining entry budget.
// Returns error for changed directories, symlinks, read failures or exhausted budgets.
func (scan *scriptIdentityScan) readDirectory(relative string) ([]fs.DirEntry, error) {
	expected, err := scan.rootHandle.Lstat(relative)
	if err != nil {
		return nil, err
	}
	if !expected.IsDir() {
		return nil, errors.New("modloader: source identity directory changed or is a symlink")
	}
	directory, err := scan.rootHandle.OpenRoot(relative)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	file, err := directory.Open(".")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(expected, actual) {
		return nil, errors.New("modloader: source identity directory changed during traversal")
	}
	var entries []fs.DirEntry
	for {
		batch, readErr := file.ReadDir(min(identityReadBatch, maxIdentityEntries-scan.entries+1))
		scan.entries += len(batch)
		if scan.entries > maxIdentityEntries {
			return nil, errors.New("modloader: source identity exceeds directory entry limit")
		}
		entries = append(entries, batch...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	slices.SortFunc(entries, func(left, right fs.DirEntry) int { return cmp.Compare(left.Name(), right.Name()) })
	return entries, nil
}

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
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxIdentityBytes is the total byte budget for all source files included in the
	// identity hash.
	maxIdentityBytes = 64 << 20

	// maxIdentityFiles is the ceiling on regular files hashed during a source identity scan.
	maxIdentityFiles = 4096

	// maxIdentityEntries is the ceiling on directory entries traversed during a source
	// identity scan.
	maxIdentityEntries = 8192

	// maxIdentityDepth is the ceiling on directory nesting depth during a source identity
	// scan.
	maxIdentityDepth = 64

	// identityReadBatch is the number of directory entries read per ReadDir call.
	identityReadBatch = 64
)

// scriptIdentityScan bounds the local files included in an approval identity.
type scriptIdentityScan struct {
	// digest accumulates the identity hash.
	digest hash.Hash

	// rootHandle is the pinned source root directory.
	rootHandle *os.Root

	// sources retains captured file contents when the caller requested source retention.
	sources map[string]string

	// root is the absolute path of the source root.
	root string

	// target is the absolute path of the script file or source directory.
	target string

	// remaining tracks the byte budget left for hashing source files.
	remaining int64

	// files tracks the number of regular files hashed.
	files int

	// entries tracks the total directory entries traversed.
	entries int
}

// visit selects local source inputs and rejects symlinked source files.
//
// Takes path (string) which is the current filesystem path.
// Takes entry (fs.DirEntry) which describes the current directory entry.
//
// Returns error when traversal or bounded source hashing fails.
func (scan *scriptIdentityScan) visit(path string, entry fs.DirEntry) error {
	if !scan.includes(path, entry.Name()) {
		return nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("modloader: source identity refuses symlink %s", path)
	}
	relative, err := filepath.Rel(scan.root, path)
	if err != nil {
		return err
	}
	info, err := scan.rootHandle.Lstat(relative)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("modloader: source identity requires regular file %s", path)
	}
	scan.files++
	if scan.files > maxIdentityFiles || info.Size() > scan.remaining {
		return errors.New("modloader: source identity exceeds bounded scan limits")
	}
	return scan.addFile(path, info)
}

// includes selects the target, Go sources, and local module or workspace metadata.
//
// Takes path (string) which is the candidate path.
// Takes name (string) which is the entry's basename.
//
// Returns bool which reports whether the entry affects the local source identity.
func (scan *scriptIdentityScan) includes(path, name string) bool {
	if path == scan.target || strings.HasSuffix(path, ".go") {
		return true
	}
	switch name {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return true
	default:
		return false
	}
}

// addFile hashes one bounded regular source file and its unambiguous path framing.
//
// Takes path (string) which is the source path.
// Takes expected (fs.FileInfo) which pins the observed file identity and metadata.
//
// Returns error when reading fails, exceeds the budget, or observes a size change.
func (scan *scriptIdentityScan) addFile(path string, expected fs.FileInfo) error {
	relative, err := filepath.Rel(scan.root, path)
	if err != nil {
		return err
	}
	file, err := scan.rootHandle.OpenFile(relative, identityOpenFlags, 0)
	if err != nil {
		return err
	}
	actual, err := file.Stat()
	if err != nil || !identityFileUnchanged(expected, actual) {
		return errors.Join(fmt.Errorf("modloader: source replaced before identity read: %s", path), err, file.Close())
	}
	size := expected.Size()
	fmt.Fprintf(scan.digest, "%d:%s:%d:", len(relative), relative, size)
	var captured bytes.Buffer
	var destination io.Writer = scan.digest
	if scan.sources != nil {
		destination = io.MultiWriter(scan.digest, &captured)
	}
	copied, copyErr := io.Copy(destination, io.LimitReader(file, min(size, scan.remaining)+1))
	final, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(copyErr, statErr, closeErr); err != nil {
		return err
	}
	if copied != size || copied > scan.remaining || !identityFileUnchanged(expected, final) {
		return fmt.Errorf("modloader: source changed during identity scan: %s", path)
	}
	scan.remaining -= copied
	if scan.sources != nil {
		scan.sources[relative] = captured.String()
	}
	return nil
}

// ScriptIdentity hashes the canonical target and local Go source tree.
//
// This detects ordinary edits and copied approvals. It is not an immutable input snapshot
// and does not authenticate external dependencies or a worker executable.
//
// Takes target (string) which is the script file or source directory.
//
// Returns string which is the canonical absolute target path.
// Returns string which is the SHA-256 identity of the local source inputs.
// Returns error when an input is unreadable, non-regular, or exceeds scan limits.
func ScriptIdentity(target string) (scriptPath, scriptHash string, err error) {
	snapshot, err := captureSource(target, false)
	if err != nil {
		return "", "", err
	}
	return snapshot.path, snapshot.identity, nil
}

// DefaultApprovalPath locates approvals outside the script-controlled directory.
//
// Takes scriptPath (string) which is the canonical script path.
//
// Returns string which is the per-script approval file in the user configuration.
// Returns error when the user's configuration directory cannot be determined.
func DefaultApprovalPath(scriptPath string) (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	identity := sha256.Sum256([]byte(scriptPath))
	return filepath.Join(config, "pipit", "approvals", fmt.Sprintf("%x.lock", identity)), nil
}

// captureSource optionally retains the exact bounded bytes written into the source hash.
//
// Takes target (string) which selects a source file or directory.
// Takes retain (bool) which enables private immutable source storage.
//
// Returns *SourceSnapshot which is the canonical identity and optional source bytes.
// Returns error when bounded source acquisition fails.
func captureSource(target string, retain bool) (*SourceSnapshot, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	root := absolute
	if !info.IsDir() {
		root = filepath.Dir(absolute)
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootHandle.Close()
	scan := &scriptIdentityScan{
		digest:     sha256.New(),
		rootHandle: rootHandle,
		root:       root,
		target:     absolute,
		remaining:  maxIdentityBytes,
		files:      0,
		entries:    0,
		sources:    nil,
	}
	if retain {
		scan.sources = make(map[string]string)
	}
	fmt.Fprintf(scan.digest, "pipit-local-source-v2\x00%s\x00", absolute)
	if err := scan.walk(".", 0); err != nil {
		return nil, err
	}
	return &SourceSnapshot{path: absolute, root: root, identity: fmt.Sprintf("sha256:%x", scan.digest.Sum(nil)), directory: info.IsDir(), sources: scan.sources}, nil
}

// identityFileUnchanged checks the opened object, not a second path lookup. Metadata
// checks detect ordinary edits but do not establish an immutable snapshot.
//
// Takes expected (fs.FileInfo) which records the original source object.
// Takes actual (fs.FileInfo) which describes the pinned open descriptor.
//
// Returns bool which reports the same regular file and unchanged metadata.
func identityFileUnchanged(expected, actual fs.FileInfo) bool {
	return actual.Mode().IsRegular() && os.SameFile(expected, actual) && expected.Size() == actual.Size() &&
		expected.Mode() == actual.Mode() && expected.ModTime().Equal(actual.ModTime())
}

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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	// LockfileSchema is the on-disk schema version. It is bumped for any incompatible
	// field-shape change.
	LockfileSchema = 2

	// LockfileName is the default basename of the lockfile pipit writes next to a script.
	// Hosts can override via the --lockfile flag.
	LockfileName = "script.lock"

	// maximumApprovalFileBytes is the ceiling on approval file size in bytes, bounding reads
	// and writes.
	maximumApprovalFileBytes = 8 << 20
)

// Lockfile records the resolved-and-approved module set for a pipit script. It is the
// source of truth for deny-unapproved reruns and the persisted store for interactive
// capability approvals.
type Lockfile struct {
	// GeneratedAt is the wall-clock time the lockfile was last written.
	GeneratedAt time.Time `json:"generated_at"`

	// Script is the script the lockfile pins. Stored as the path passed on the command line;
	// resolution to absolute paths happens at load time.
	Script string `json:"script"`

	// ScriptHash is the SHA-256 approval identity captured before execution. A changed
	// identity invalidates every capability approval.
	ScriptHash string `json:"script_hash"`

	// Modules records every module pipit has resolved while running this script. Ordering is
	// by Path for stable diffs.
	Modules []LockedModule `json:"modules"`

	// Schema is the lockfile format version. Always LockfileSchema for files produced by
	// this pipit binary.
	Schema int `json:"schema"`
}

// LockedModule is one entry in the lockfile. Records the resolution outcome and any
// capability approvals.
type LockedModule struct {
	// ApprovedAt is when the approvals were granted. Used for audit trails.
	ApprovedAt time.Time `json:"approved_at,omitzero"`

	// Path is the module's canonical identifier (the ModuleRef Path).
	Path string `json:"path"`

	// Version is the version selector: semver tag, branch, commit hash, or empty for
	// "latest" semantics.
	Version string `json:"version"`

	// BundleDigest is the SHA-256 hash of the resolved .pkbundle bytes in "sha256:<hex>"
	// form. Refused on rerun if the resolved bundle's digest does not match.
	BundleDigest string `json:"bundle_digest"`

	// ApprovedVia records how the approval was obtained: "interactive", "cli-flag", or
	// "non-interactive" (approved during a previous interactive session).
	ApprovedVia string `json:"approved_via,omitempty"`

	// ApprovedCapabilities lists the capabilities the operator has approved for this (Path,
	// Version, BundleDigest) triple. Capabilities outside this list cause an interactive
	// prompt, or a refusal when prompting is disabled (--autodeny or a non-TTY run).
	ApprovedCapabilities []string `json:"approved_capabilities,omitempty"`
}

// Store wraps an on-disk Lockfile with mutation helpers and atomic save semantics.
type Store struct {
	// path is the lockfile location on disk that Load and Save use.
	path string

	// lockfile is the in-memory copy of the on-disk lockfile content.
	lockfile Lockfile

	// mu guards the lockfile and dirty fields against concurrent access.
	mu sync.Mutex

	// dirty reports whether the in-memory lockfile has unsaved changes.
	dirty bool

	// approvalRevision holds the SHA-256 digest of the last loaded or saved approval file
	// bytes.
	approvalRevision [sha256.Size]byte

	// approvalExists tracks whether the approval file existed at the time of the last load.
	approvalExists bool
}

// NewStore constructs an in-memory Store pinned to a particular filesystem path. Does not
// read from disk; call Load to populate from an existing file, or Save to write a
// freshly-constructed lockfile.
//
// Takes path (string) which is the lockfile location on disk.
//
// Returns a *Store.
func NewStore(path string) *Store {
	return &Store{
		path: path,
		lockfile: Lockfile{
			Schema:      LockfileSchema,
			GeneratedAt: time.Time{}, Script: "", ScriptHash: "", Modules: nil},
		mu: sync.Mutex{}, dirty: false, approvalRevision: [sha256.Size]byte{}, approvalExists: false}
}

// Load replaces the store from a bounded lockfile read. A missing file returns nil with a
// fresh empty store.
//
// Returns error when the file cannot be read or the contents cannot be decoded.
//
// Concurrency: safe for concurrent use; the mutex guards the load.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockfile = Lockfile{
		Schema: LockfileSchema, GeneratedAt: time.Time{}, Script: "", ScriptHash: "", Modules: nil,
	}
	s.dirty = false
	s.approvalRevision = [sha256.Size]byte{}
	s.approvalExists = false
	data, err := readApprovalFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("modloader: reading %s: %w", s.path, err)
	}
	decoded, err := decodeApprovalFile(data)
	if err != nil {
		return fmt.Errorf("modloader: parsing %s: %w", s.path, err)
	}
	if decoded.Schema > LockfileSchema {
		return fmt.Errorf("modloader: lockfile schema %d newer than supported %d; upgrade pipit", decoded.Schema, LockfileSchema)
	}
	if decoded.Schema < 1 {
		return fmt.Errorf("modloader: invalid lockfile schema %d", decoded.Schema)
	}
	if decoded.Schema < LockfileSchema {
		for index := range decoded.Modules {
			decoded.Modules[index].ApprovedCapabilities = nil
			decoded.Modules[index].ApprovedAt = time.Time{}
			decoded.Modules[index].ApprovedVia = ""
		}
	}
	s.lockfile = decoded
	s.approvalRevision = sha256.Sum256(data)
	s.approvalExists = true
	return nil
}

// Save atomically writes the lockfile to disk via temp file and rename. It is a no-op
// when nothing has changed since the last Save or Load.
//
// Returns error when the lockfile cannot be marshalled or written.
//
// Concurrency: safe for concurrent use; the mutex guards the write.
func (s *Store) Save() (result error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	data, err := encodeApprovalSnapshot(s.lockfile)
	if err != nil {
		return fmt.Errorf("modloader: marshalling lockfile: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maximumApprovalFileBytes {
		return fmt.Errorf("modloader: approval file exceeds %d bytes", maximumApprovalFileBytes)
	}
	dir := filepath.Dir(s.path)
	root, err := openApprovalDirectory(dir, true)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	name := filepath.Base(s.path)
	guard, err := acquireApprovalGuardRoot(root, name)
	if err != nil {
		return fmt.Errorf("modloader: locking approval file: %w", err)
	}
	defer func() { result = errors.Join(result, guard.Close()) }()
	if err := s.checkApprovalRevision(root, name); err != nil {
		return err
	}
	if err := publishApprovalFile(root, name, data); err != nil {
		return err
	}
	s.dirty = false
	s.approvalRevision = sha256.Sum256(data)
	s.approvalExists = true
	return nil
}

// Lookup returns the LockedModule for the given (path, version) pair.
//
// Takes path (string) which is the module's canonical identifier.
// Takes version (string) which is the version selector to match.
//
// Returns LockedModule which is the stored entry, or the zero value.
// Returns bool which reports whether a matching entry was found.
//
// Concurrency: safe for concurrent use; the mutex guards the read.
func (s *Store) Lookup(path, version string) (LockedModule, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, module := range s.lockfile.Modules {
		if module.Path == path && module.Version == version {
			module.ApprovedCapabilities = slices.Clone(module.ApprovedCapabilities)
			return module, true
		}
	}
	return LockedModule{}, false
}

// Upsert adds or replaces a LockedModule. It marks the store dirty so the next Save
// persists the change.
//
// Takes module (LockedModule) which is the entry to store.
//
// Concurrency: safe for concurrent use; the mutex guards the write.
func (s *Store) Upsert(module LockedModule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	module.ApprovedCapabilities = slices.Clone(module.ApprovedCapabilities)
	for i := range s.lockfile.Modules {
		if s.lockfile.Modules[i].Path == module.Path && s.lockfile.Modules[i].Version == module.Version {
			s.lockfile.Modules[i] = module
			s.dirty = true
			return
		}
	}
	s.lockfile.Modules = append(s.lockfile.Modules, module)
	s.dirty = true
}

// SetScript records the script path and content hash so subsequent loads of the same
// lockfile can detect when the script has changed.
//
// Takes scriptPath (string) which is the script's filesystem path.
// Takes scriptHash (string) which is the "sha256:<hex>" source or invocation identity.
//
// Concurrency: safe for concurrent use; the mutex guards the write.
func (s *Store) SetScript(scriptPath, scriptHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lockfile.Script == scriptPath && s.lockfile.ScriptHash == scriptHash {
		return
	}
	s.lockfile.Script = scriptPath
	s.lockfile.ScriptHash = scriptHash
	for index := range s.lockfile.Modules {
		s.lockfile.Modules[index].ApprovedCapabilities = nil
		s.lockfile.Modules[index].ApprovedAt = time.Time{}
		s.lockfile.Modules[index].ApprovedVia = ""
	}
	s.dirty = true
}

// Snapshot returns a defensive copy of the lockfile content. It is used by CLI verbs such
// as pipit module list that iterate without the store lock.
//
// Returns Lockfile which is a copy the caller may freely mutate.
//
// Concurrency: safe for concurrent use; the mutex guards the copy.
func (s *Store) Snapshot() Lockfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyOf := s.lockfile
	copyOf.Modules = make([]LockedModule, len(s.lockfile.Modules))
	copy(copyOf.Modules, s.lockfile.Modules)
	for index := range copyOf.Modules {
		copyOf.Modules[index].ApprovedCapabilities = slices.Clone(copyOf.Modules[index].ApprovedCapabilities)
	}
	return copyOf
}

// Path returns the filesystem path the store reads from and writes to.
//
// Returns the configured path.
func (s *Store) Path() string {
	return s.path
}

// MarkDirty sets the dirty bit so callers that mutate the lockfile outside Upsert can
// trigger the next Save.
//
// Concurrency: safe for concurrent use; the mutex guards the write.
func (s *Store) MarkDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = true
}

// readApprovalFile uses bounded storage access without a whole-file fallback.
//
// Takes path (string) which is the host-selected approval pathname.
//
// Returns []byte with no partial content on read or close failure.
// Returns error on read, ownership or size failure.
func readApprovalFile(path string) (data []byte, result error) {
	root, err := openApprovalDirectory(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer func() {
		result = errors.Join(result, root.Close())
		if result != nil {
			data = nil
		}
	}()
	return readApprovalRoot(root, filepath.Base(path))
}

// readApprovalRoot reads from the same pinned directory used by a save transaction.
//
// Takes root (*os.Root) which is the owned directory handle.
// Takes name (string) which is the approval basename.
//
// Returns []byte with bounded content.
// Returns error without partial state on failure.
func readApprovalRoot(root *os.Root, name string) (data []byte, result error) {
	file, err := openApprovalFile(root, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		result = errors.Join(result, file.Close())
		if result != nil {
			data = nil
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumApprovalFileBytes {
		return nil, errors.New("invalid approval file type or size")
	}
	if err := validateApprovalFileOwner(file); err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(file, maximumApprovalFileBytes+1))
	if err != nil || len(data) > maximumApprovalFileBytes {
		return nil, errors.Join(err, errors.New("approval file read failed or exceeded limit"))
	}
	return data, nil
}

// encodeApprovalSnapshot serialises a detached, canonically ordered approval snapshot.
//
// Takes snapshot (Lockfile) which is a copy of the store state taken while the mutex is
// held.
//
// Returns []byte which is the JSON encoding.
// Returns error when the snapshot breaches an approval limit or fails to encode, leaving
// the live store's nested slices untouched.
func encodeApprovalSnapshot(snapshot Lockfile) ([]byte, error) {
	if len(snapshot.Modules) > maximumApprovalModules {
		return nil, errors.New("approval module count exceeded")
	}
	for _, module := range snapshot.Modules {
		if len(module.ApprovedCapabilities) > maximumModuleApprovals {
			return nil, errors.New("approval capability count exceeded")
		}
	}
	snapshot.Modules = slices.Clone(snapshot.Modules)
	for index := range snapshot.Modules {
		snapshot.Modules[index].ApprovedCapabilities = slices.Clone(snapshot.Modules[index].ApprovedCapabilities)
	}
	snapshot.Schema = LockfileSchema
	snapshot.GeneratedAt = time.Now().UTC()
	slices.SortFunc(snapshot.Modules, func(first, second LockedModule) int {
		return cmp.Or(cmp.Compare(first.Path, second.Path), cmp.Compare(first.Version, second.Version))
	})
	for _, module := range snapshot.Modules {
		slices.Sort(module.ApprovedCapabilities)
	}
	return json.MarshalIndent(snapshot, "", "  ")
}

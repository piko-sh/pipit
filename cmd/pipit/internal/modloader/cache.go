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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/lockedfile"
	"golang.org/x/mod/sumdb/dirhash"
)

// cacheDirMode is the permission bits pipit creates cache directories with.
const cacheDirMode = 0o750

const (
	// CacheOff disables on-disk persistence; every miss hits the network. The in-process
	// modfs map cache still applies.
	CacheOff CacheMode = "off"

	// CacheOn stores artefacts under .pipit/cache/download next to the script.
	CacheOn CacheMode = "on"

	// CacheHome stores artefacts under $XDG_CACHE_HOME/pipit/cache/download (or
	// ~/.cache/pipit/...). Shared across scripts on the same host.
	CacheHome CacheMode = "home"

	// CacheGopath stores artefacts under the user's $GOPATH/pkg/mod/cache/download so a
	// later go get of the same module is a no-op.
	CacheGopath CacheMode = "gopath"

	// CacheCustom stores artefacts under a path supplied by the operator (any value
	// beginning with /, ./, ../ or ~/ on the --cache flag). An internal sentinel, not a flag
	// keyword in its own right.
	CacheCustom CacheMode = "custom"
)

// errCacheMissOffline is the sentinel returned when allowNet is false and the requested
// artefact is not on disk.
var errCacheMissOffline = errors.New("artefact not cached and --allow-network is not set")

// CacheMode selects where on-disk module artefacts are persisted, or whether they are
// persisted at all.
type CacheMode string

// CacheSpec is the resolved form of a --cache flag value.
type CacheSpec struct {
	// Mode selects the cache kind (off, on, home, gopath, or custom).
	Mode CacheMode

	// Path holds the expanded absolute path for custom mode; empty otherwise.
	Path string
}

// CacheRecorder collects hit and miss events emitted by cachingFS. Implementations must
// be safe for concurrent use.
type CacheRecorder interface {
	// RecordCacheHit notes that a module artefact was served from the on-disk cache.
	//
	// Takes modulePath (string) which is the module import path.
	// Takes version (string) which is the module version, or empty.
	RecordCacheHit(modulePath, version string)

	// RecordCacheMiss notes that a module artefact had to be fetched from the network.
	//
	// Takes modulePath (string) which is the module import path.
	// Takes version (string) which is the module version, or empty.
	RecordCacheMiss(modulePath, version string)
}

// MemoryRecorder is a default CacheRecorder that aggregates hit and miss events per
// module and version for the run summary.
type MemoryRecorder struct {
	// hits tracks module@version keys that were served from cache.
	hits map[string]struct{}

	// misses tracks module@version keys that required a network fetch.
	misses map[string]struct{}

	// mu guards hits and misses.
	mu sync.Mutex
}

// NewMemoryRecorder constructs an empty MemoryRecorder.
//
// Returns *MemoryRecorder which is ready to record hits and misses.
func NewMemoryRecorder() *MemoryRecorder {
	return &MemoryRecorder{
		hits:   make(map[string]struct{}),
		misses: make(map[string]struct{}),
		mu:     sync.Mutex{}}
}

// RecordCacheHit records that a module artefact was served from the on-disk cache.
// Implements CacheRecorder.
//
// Takes modulePath (string) which is the module import path.
// Takes version (string) which is the module version, or empty.
//
// Concurrency: safe for concurrent use; the mutex guards the hits set.
func (r *MemoryRecorder) RecordCacheHit(modulePath, version string) {
	if modulePath == "" {
		return
	}
	key := modulePath
	if version != "" {
		key = modulePath + "@" + version
	}
	r.mu.Lock()
	r.hits[key] = struct{}{}
	r.mu.Unlock()
}

// RecordCacheMiss records that a module artefact had to be fetched from the network.
// Implements CacheRecorder.
//
// Takes modulePath (string) which is the module import path.
// Takes version (string) which is the module version, or empty.
//
// Concurrency: safe for concurrent use; the mutex guards the misses set.
func (r *MemoryRecorder) RecordCacheMiss(modulePath, version string) {
	if modulePath == "" {
		return
	}
	key := modulePath
	if version != "" {
		key = modulePath + "@" + version
	}
	r.mu.Lock()
	r.misses[key] = struct{}{}
	r.mu.Unlock()
}

// Hits returns the sorted set of module@version keys served from the on-disk cache during
// this run.
//
// Returns []string which is the sorted set of cache-hit keys.
//
// Concurrency: safe for concurrent use; the mutex guards the read.
func (r *MemoryRecorder) Hits() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.hits))
	for k := range r.hits {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// cachingFS fronts an upstream fs.FS with an on-disk Go-compatible module cache.
type cachingFS struct {
	// upstream serves artefacts on a cache miss.
	upstream fs.FS

	// recorder receives hit and miss events for diagnostics.
	recorder CacheRecorder

	// root is the on-disk cache directory.
	root string

	// allowNet permits network fetches when the cache misses.
	allowNet bool
}

// Open implements fs.FS over the on-disk cache.
//
// The contract is:
//
//   - Cache hit -> return an *os.File over the cached artefact. An atomic rename in the
//     writer guarantees no partial file is read.
//   - Cache miss with allowNet -> fetch upstream, persist (locked and atomically
//     renamed), then open the cached copy.
//   - Cache miss without allowNet -> return a PathError wrapping errCacheMissOffline.
//
// Takes name (string) which is the slash-separated artefact path.
//
// Returns fs.File which is the opened cached artefact.
// Returns error when the path is invalid or the fetch or open fails.
func (c *cachingFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	cachePath := filepath.Join(c.root, filepath.FromSlash(name))

	if _, err := os.Stat(cachePath); err == nil {
		c.report(name, true)
		return os.Open(cachePath) //nolint:gosec // path derived from the cache root
	}

	if !c.allowNet {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fmt.Errorf("%w: %s", errCacheMissOffline, name),
		}
	}

	if err := c.fetchAndStore(name, cachePath); err != nil {
		return nil, err
	}
	c.report(name, false)
	return os.Open(cachePath) //nolint:gosec // path derived from the cache root
}

// report forwards a cache event to the recorder, when one is configured.
//
// Takes name (string) which is the artefact path that was resolved.
// Takes hit (bool) which is true for a cache hit and false for a miss.
func (c *cachingFS) report(name string, hit bool) {
	if c.recorder == nil {
		return
	}
	modulePath, version := splitArtifactPath(name)
	if hit {
		c.recorder.RecordCacheHit(modulePath, version)
	} else {
		c.recorder.RecordCacheMiss(modulePath, version)
	}
}

// fetchAndStore opens the upstream artefact, persists it under a lock to cachePath via
// temp and rename, and writes a sibling .ziphash for zips.
//
// The lock-then-recheck pattern makes it idempotent under concurrent callers, so only one
// fetch happens per module and version.
//
// Takes name (string) which is the upstream artefact path.
// Takes cachePath (string) which is the destination path on disk.
//
// Returns error when the directory, lock, fetch, or write fails.
func (c *cachingFS) fetchAndStore(name, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), cacheDirMode); err != nil {
		return fmt.Errorf("modloader: preparing cache dir for %s: %w", name, err)
	}

	lockPath := lockPathFor(cachePath)
	lf, err := lockedfile.Edit(lockPath)
	if err != nil {
		return fmt.Errorf("modloader: locking %s: %w", lockPath, err)
	}
	defer lf.Close()

	if _, err := os.Stat(cachePath); err == nil {
		return nil
	}

	source, err := c.upstream.Open(name)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := streamToFile(source, cachePath); err != nil {
		return err
	}

	if strings.HasSuffix(cachePath, ".zip") {
		if err := writeZipHashSibling(cachePath); err != nil {
			return err
		}
	}
	return nil
}

// ParseCacheMode validates a --cache flag keyword.
//
// For path inputs (anything starting with /, ./, ../, or ~/) prefer ParseCacheSpec.
//
// Takes s (string) which is the flag keyword to validate.
//
// Returns CacheMode which is the parsed mode when s is a valid keyword.
// Returns error when s is not one of off, on, home, or gopath.
func ParseCacheMode(s string) (CacheMode, error) {
	switch CacheMode(s) {
	case CacheOff, CacheOn, CacheHome, CacheGopath:
		return CacheMode(s), nil
	default:
		return "", fmt.Errorf("invalid cache mode %q (want one of: off, on, home, gopath, or a filesystem path: absolute (/foo, C:\\foo) or relative (./foo, ../foo, ~/foo))", s)
	}
}

// ParseCacheSpec parses a --cache flag value into a CacheSpec. Path-shaped values
// (absolute, home-relative, or cwd-relative) produce CacheCustom with an expanded path;
// everything else is treated as a keyword mode.
//
// Takes s (string) which is the raw --cache flag value.
//
// Returns CacheSpec which holds the resolved mode and any expanded path.
// Returns error when s is neither a valid keyword nor a usable path.
func ParseCacheSpec(s string) (CacheSpec, error) {
	if isPathSpec(s) {
		expanded, err := expandPathSpec(s)
		if err != nil {
			return CacheSpec{}, fmt.Errorf("invalid cache path %q: %w", s, err)
		}
		return CacheSpec{Mode: CacheCustom, Path: expanded}, nil
	}
	mode, err := ParseCacheMode(s)
	if err != nil {
		return CacheSpec{}, err
	}
	return CacheSpec{Mode: mode, Path: ""}, nil
}

// ResolveCacheSpec maps a requested spec to an absolute cache root directory, applying
// the documented fallback chain.
//
// The fallback chain is:
//
//   - gopath -> on -> off
//   - home -> on -> off
//   - on -> off
//   - custom -> off (no fallback to on; operator picked the path)
//
// An empty root means no cache (CacheOff). Each fallback transition is logged to stderr
// so the operator can see why their requested mode was not honoured.
//
// Takes spec (CacheSpec) which is the requested cache configuration.
// Takes scriptPath (string) which locates the script for on-mode roots.
// Takes stderr (io.Writer) which receives fallback diagnostics.
//
// Returns string which is the absolute cache root, empty for CacheOff.
// Returns CacheMode which is the mode that actually applied.
func ResolveCacheSpec(spec CacheSpec, scriptPath string, stderr io.Writer) (string, CacheMode) {
	log := func(format string, args ...any) {
		if stderr == nil {
			return
		}
		fmt.Fprintf(stderr, "pipit: "+format+"\n", args...)
	}
	switch spec.Mode {
	case CacheOff:
		return "", CacheOff
	case CacheCustom:
		root := spec.Path
		if root == "" {
			log("custom cache path is empty, disabling cache")
			return "", CacheOff
		}
		if !ensureWritable(root) {
			log("custom cache %s not writable, disabling cache", root)
			return "", CacheOff
		}
		return root, CacheCustom
	case CacheGopath:
		gopath := filepath.Join(lookupGopath(), "pkg", "mod", "cache", "download")
		if root, ok := sharedCacheRoot(lookupGopath(), gopath, "GOPATH", log); ok {
			return root, CacheGopath
		}
		return ResolveCacheSpec(CacheSpec{Mode: CacheOn, Path: ""}, scriptPath, stderr)
	case CacheHome:
		xdg := filepath.Join(lookupXDGCacheHome(), "pipit", "cache", "download")
		if root, ok := sharedCacheRoot(lookupXDGCacheHome(), xdg, "XDG cache", log); ok {
			return root, CacheHome
		}
		return ResolveCacheSpec(CacheSpec{Mode: CacheOn, Path: ""}, scriptPath, stderr)
	case CacheOn:
		root := filepath.Join(filepath.Dir(scriptPath), ".pipit", "cache", "download")
		if !ensureWritable(root) {
			log("local .pipit cache %s not writable, disabling cache", root)
			return "", CacheOff
		}
		return root, CacheOn
	default:
		log("unknown cache mode %q, disabling cache", string(spec.Mode))
		return "", CacheOff
	}
}

// IsCacheMissOffline reports whether err originated from a cache miss that could not be
// filled because --allow-network was not set.
//
// Callers such as the resolver use this to surface a friendlier message.
//
// Takes err (error) which is the error to inspect.
//
// Returns bool which is true when err wraps errCacheMissOffline.
func IsCacheMissOffline(err error) bool {
	return errors.Is(err, errCacheMissOffline)
}

// isPathSpec reports whether s looks like a filesystem path rather than a keyword.
//
// The grammar is deliberately strict so a typo like --cache=localdir is rejected with a
// helpful error, while --cache=./localdir works. It recognises both forward- and
// back-slash variants so Windows paths (C:\foo, .\foo, ~\foo) work as naturally as POSIX
// ones.
//
// Takes s (string) which is the candidate flag value.
//
// Returns bool which is true when s is shaped like a path.
func isPathSpec(s string) bool {
	if s == "" {
		return false
	}
	if s == "~" {
		return true
	}
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		return true
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	if strings.HasPrefix(s, `.\`) || strings.HasPrefix(s, `..\`) {
		return true
	}

	return filepath.IsAbs(s)
}

// expandPathSpec turns a user-supplied path spec into an absolute path.
//
// ~, ~/..., and ~\... resolve against [os.UserHomeDir]; everything else resolves relative
// to the current working directory via [filepath.Abs], which also canonicalises
// platform-specific separators.
//
// Takes s (string) which is the path spec to expand.
//
// Returns string which is the resulting absolute path.
// Returns error when the home directory or absolute path cannot resolve.
func expandPathSpec(s string) (string, error) {
	if s == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving ~: %w", err)
		}

		s = filepath.Join(home, s[2:])
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// sharedCacheRoot validates a host-shared cache root, logging why it was rejected.
//
// Takes base (string) which is the platform directory, empty when it was not found.
// Takes root (string) which is the cache root derived from base.
// Takes label (string) which names the cache in the fallback diagnostic.
// Takes log (func(string, ...any)) which writes the fallback diagnostic.
//
// Returns string which is root when it is usable, otherwise empty.
// Returns bool which is true when root is usable.
func sharedCacheRoot(base, root, label string, log func(string, ...any)) (string, bool) {
	if base == "" {
		log("%s directory not found, falling back to --cache=on", label)
		return "", false
	}
	if !ensureWritable(root) {
		log("%s %s not writable, falling back to --cache=on", label, root)
		return "", false
	}
	return root, true
}

// lookupGopath returns the first entry of $GOPATH, or $HOME/go as the Go toolchain's
// documented default.
//
// Returns string which is the GOPATH root, or empty when none is found.
func lookupGopath() string {
	if g := os.Getenv("GOPATH"); g != "" {
		if before, _, ok := strings.Cut(g, ":"); ok {
			return before
		}
		return g
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "go")
	}
	return ""
}

// lookupXDGCacheHome returns $XDG_CACHE_HOME with the conventional fallbacks
// ($HOME/.cache, then os.UserCacheDir).
//
// Returns string which is the cache home, or empty when none resolves.
func lookupXDGCacheHome() string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return x
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return dir
	}
	return ""
}

// ensureWritable creates root if needed and confirms a probe file can be written under
// it.
//
// Takes root (string) which is the directory to create and test.
//
// Returns bool which is true when root exists and is writable.
func ensureWritable(root string) bool {
	if err := os.MkdirAll(root, cacheDirMode); err != nil {
		return false
	}
	probe, err := os.CreateTemp(root, ".pipit-write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}

// newCachingFS wraps upstream with the disk cache.
//
// When root is empty the returned FS is upstream verbatim, so callers can unconditionally
// wrap and let mode resolution decide whether the cache participates.
//
// Takes upstream (fs.FS) which serves artefacts on a cache miss.
// Takes root (string) which is the on-disk cache directory, or empty.
// Takes allowNet (bool) which permits network fetches on a miss.
// Takes recorder (CacheRecorder) which receives hit and miss events.
//
// Returns fs.FS which caches upstream, or upstream itself when root is empty.
func newCachingFS(upstream fs.FS, root string, allowNet bool, recorder CacheRecorder) fs.FS {
	if root == "" {
		return upstream
	}
	return &cachingFS{
		upstream: upstream,
		root:     root,
		allowNet: allowNet,
		recorder: recorder,
	}
}

// streamToFile copies source into cachePath via a temp file in the same directory, then
// atomically renames it into place.
//
// It matches the pattern used by lockfile.go:199-203 so behaviour stays uniform across
// the modloader package.
//
// Takes source (io.Reader) which supplies the artefact bytes.
// Takes cachePath (string) which is the destination path on disk.
//
// Returns error when the temp file cannot be created, written, or renamed.
func streamToFile(source io.Reader, cachePath string) error {
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), filepath.Base(cachePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("modloader: creating temp for %s: %w", cachePath, err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("modloader: writing %s: %w", cachePath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("modloader: closing temp for %s: %w", cachePath, err)
	}
	if err := os.Rename(tmpName, cachePath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("modloader: renaming temp into %s: %w", cachePath, err)
	}
	return nil
}

// writeZipHashSibling computes Go's h1: dirhash over the zip and writes it atomically to
// <zip>hash.
//
// For example, v1.2.3.zip yields v1.2.3.ziphash. This lets Go's own go get accept the
// cached zip without re-downloading it.
//
// Takes zipPath (string) which is the path to the cached zip file.
//
// Returns error when the zip cannot be hashed or the sibling write fails.
func writeZipHashSibling(zipPath string) error {
	h, err := dirhash.HashZip(zipPath, dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("modloader: hashing %s: %w", zipPath, err)
	}

	hashPath := zipPath + "hash"
	return streamToFile(strings.NewReader(h), hashPath)
}

// lockPathFor maps an artefact path to the .lock file used to coordinate writes.
//
// It mirrors Go's GOMODCACHE convention: every versioned artefact
// (v1.2.3.{zip,mod,info,ziphash}) is gated by v1.2.3.lock; standalone files (@latest,
// @v/list) get their own sibling .lock.
//
// Takes cachePath (string) which is the artefact path being written.
//
// Returns string which is the .lock path guarding that artefact.
func lockPathFor(cachePath string) string {
	base := filepath.Base(cachePath)
	dir := filepath.Dir(cachePath)
	switch filepath.Ext(base) {
	case ".zip", ".mod", ".info", ".ziphash":
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return filepath.Join(dir, base+".lock")
}

// splitArtifactPath extracts the module path and version from a proxy path such as
// github.com/foo/bar/@v/v1.2.3.zip.
//
// This lets cache events be aggregated per module and version.
//
// Takes name (string) which is the proxy artefact path.
//
// Returns string which is the module path, or a trimmed @latest path.
// Returns string which is the version, or empty when it cannot be parsed.
func splitArtifactPath(name string) (modulePath, version string) {
	before, after, ok := strings.Cut(name, "/@v/")
	if !ok {
		return strings.TrimSuffix(name, "/@latest"), ""
	}
	mod := before
	rest := after
	if strings.HasSuffix(rest, ".zip") || strings.HasSuffix(rest, ".mod") ||
		strings.HasSuffix(rest, ".info") || strings.HasSuffix(rest, ".ziphash") {
		return mod, strings.TrimSuffix(rest, filepath.Ext(rest))
	}
	return mod, ""
}

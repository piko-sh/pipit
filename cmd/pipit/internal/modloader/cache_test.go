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
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseCacheMode(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"off", "on", "home", "gopath"} {
		mode, err := ParseCacheMode(in)
		if err != nil {
			t.Fatalf("ParseCacheMode(%q) error: %v", in, err)
		}
		if string(mode) != in {
			t.Fatalf("ParseCacheMode(%q) = %q", in, mode)
		}
	}
	if _, err := ParseCacheMode("localdir"); err == nil {
		t.Fatalf("ParseCacheMode(%q) accepted unknown keyword", "localdir")
	}
}

func TestParseCacheSpec_Keywords(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"off", "on", "home", "gopath"} {
		spec, err := ParseCacheSpec(in)
		if err != nil {
			t.Fatalf("ParseCacheSpec(%q): %v", in, err)
		}
		if string(spec.Mode) != in || spec.Path != "" {
			t.Fatalf("ParseCacheSpec(%q) = %+v", in, spec)
		}
	}
}

func TestParseCacheSpec_PathsPosix(t *testing.T) {
	t.Parallel()
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	cases := []struct {
		in   string
		want string
	}{
		{"/tmp/foo", "/tmp/foo"},
		{"./relative", filepath.Join(cwd, "relative")},
		{"../sibling", filepath.Clean(filepath.Join(cwd, "..", "sibling"))},
	}
	for _, c := range cases {
		spec, err := ParseCacheSpec(c.in)
		if err != nil {
			t.Fatalf("ParseCacheSpec(%q): %v", c.in, err)
		}
		if spec.Mode != CacheCustom {
			t.Fatalf("ParseCacheSpec(%q).Mode = %q, want custom", c.in, spec.Mode)
		}
		if spec.Path != c.want {
			t.Fatalf("ParseCacheSpec(%q).Path = %q, want %q", c.in, spec.Path, c.want)
		}
	}
	if home != "" {
		spec, err := ParseCacheSpec("~/.pipit-test")
		if err != nil {
			t.Fatalf("ParseCacheSpec(~/.pipit-test): %v", err)
		}
		want := filepath.Join(home, ".pipit-test")
		if spec.Path != want {
			t.Fatalf("ParseCacheSpec(~/.pipit-test).Path = %q, want %q", spec.Path, want)
		}
		spec, err = ParseCacheSpec("~")
		if err != nil {
			t.Fatalf("ParseCacheSpec(~): %v", err)
		}
		if spec.Path != home {
			t.Fatalf("ParseCacheSpec(~).Path = %q, want %q", spec.Path, home)
		}
	}
}

func TestParseCacheSpec_BareWordRejected(t *testing.T) {
	t.Parallel()
	_, err := ParseCacheSpec("localdir")
	if err == nil {
		t.Fatalf("ParseCacheSpec(localdir) should reject bare word; want explicit ./ or /")
	}
	if !strings.Contains(err.Error(), "localdir") {
		t.Fatalf("error message %q should mention the input", err)
	}
}

func TestIsPathSpec_WindowsLike(t *testing.T) {
	t.Parallel()

	winLike := []string{`.\foo`, `..\foo`, `~\foo`}
	for _, in := range winLike {
		if !isPathSpec(in) {
			t.Fatalf("isPathSpec(%q) = false, want true", in)
		}
	}
	if runtime.GOOS == "windows" {
		for _, in := range []string{`C:\foo`, `\\server\share`} {
			if !isPathSpec(in) {
				t.Fatalf("isPathSpec(%q) = false on Windows, want true", in)
			}
		}
	}
}

func TestResolveCacheSpec_Off(t *testing.T) {
	t.Parallel()
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheOff}, "", io.Discard)
	if root != "" || mode != CacheOff {
		t.Fatalf("got (%q, %q), want (\"\", off)", root, mode)
	}
}

func TestResolveCacheSpec_OnWritable(t *testing.T) {
	t.Parallel()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "script.go")
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheOn}, scriptPath, io.Discard)
	if mode != CacheOn {
		t.Fatalf("mode = %q, want on", mode)
	}
	want := filepath.Join(scriptDir, ".pipit", "cache", "download")
	if root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("cache root not created: %v", err)
	}
}

func TestResolveCacheSpec_OnUnwritableFallsBackToOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based writability checks don't apply cleanly on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX permissions")
	}
	t.Parallel()
	scriptDir := t.TempDir()
	if err := os.Chmod(scriptDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(scriptDir, 0o700) })
	var stderr bytes.Buffer
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheOn}, filepath.Join(scriptDir, "s.go"), &stderr)
	if root != "" || mode != CacheOff {
		t.Fatalf("got (%q, %q), want (\"\", off)", root, mode)
	}
	if !strings.Contains(stderr.String(), "disabling cache") {
		t.Fatalf("stderr missing fallback log: %q", stderr.String())
	}
}

func TestResolveCacheSpec_HomeUsesXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheHome}, "", io.Discard)
	want := filepath.Join(xdg, "pipit", "cache", "download")
	if root != want || mode != CacheHome {
		t.Fatalf("got (%q, %q), want (%q, home)", root, mode, want)
	}
}

func TestResolveCacheSpec_GopathFallsBackWhenUnset(t *testing.T) {
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", t.TempDir())
	scriptDir := t.TempDir()
	var stderr bytes.Buffer
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheGopath}, filepath.Join(scriptDir, "s.go"), &stderr)

	if mode != CacheGopath {
		t.Fatalf("mode = %q, want gopath (HOME default kicked in)", mode)
	}
	if !strings.Contains(root, "go/pkg/mod/cache/download") {
		t.Fatalf("unexpected gopath root: %q", root)
	}
}

func TestResolveCacheSpec_Custom(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheCustom, Path: dir}, "", io.Discard)
	if mode != CacheCustom {
		t.Fatalf("mode = %q, want custom", mode)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
}

func TestResolveCacheSpec_CustomEmptyPathDisables(t *testing.T) {
	t.Parallel()
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheCustom, Path: ""}, "", io.Discard)
	if root != "" || mode != CacheOff {
		t.Fatalf("got (%q, %q), want (\"\", off)", root, mode)
	}
}

func TestLockPathFor(t *testing.T) {
	t.Parallel()
	root := filepath.FromSlash("/cache/github.com/foo/bar/@v")
	for _, ext := range []string{".zip", ".mod", ".info", ".ziphash"} {
		got := lockPathFor(filepath.Join(root, "v1.2.3"+ext))
		want := filepath.Join(root, "v1.2.3.lock")
		if got != want {
			t.Fatalf("lockPathFor(v1.2.3%s) = %q, want %q", ext, got, want)
		}
	}
	atLatest := lockPathFor(filepath.FromSlash("/cache/github.com/foo/bar/@latest"))
	if !strings.HasSuffix(atLatest, "@latest.lock") {
		t.Fatalf("@latest lock derivation wrong: %q", atLatest)
	}
}

func TestSplitArtifactPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, mod, ver string
	}{
		{"github.com/foo/bar/@v/v1.2.3.zip", "github.com/foo/bar", "v1.2.3"},
		{"github.com/foo/bar/@v/v1.2.3.mod", "github.com/foo/bar", "v1.2.3"},
		{"github.com/foo/bar/@latest", "github.com/foo/bar", ""},
		{"github.com/foo/bar/@v/list", "github.com/foo/bar", ""},
	}
	for _, c := range cases {
		mod, ver := splitArtifactPath(c.in)
		if mod != c.mod || ver != c.ver {
			t.Fatalf("splitArtifactPath(%q) = (%q,%q), want (%q,%q)", c.in, mod, ver, c.mod, c.ver)
		}
	}
}

type hitCountingFS struct {
	files fstest.MapFS
	opens int
}

func (h *hitCountingFS) Open(name string) (fs.File, error) {
	h.opens++
	return h.files.Open(name)
}

func TestCachingFS_HitDoesNotReachUpstream(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	relPath := filepath.FromSlash("github.com/foo/bar/@v/v1.0.0.mod")
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := []byte("module github.com/foo/bar\n")
	if err := os.WriteFile(full, want, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	upstream := &hitCountingFS{files: fstest.MapFS{}}
	cfs := newCachingFS(upstream, root, true, nil)
	file, err := cfs.Open("github.com/foo/bar/@v/v1.0.0.mod")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("contents = %q, want %q", got, want)
	}
	if upstream.opens != 0 {
		t.Fatalf("upstream Open count = %d, want 0 (cache hit should skip upstream)", upstream.opens)
	}
}

func TestCachingFS_MissOfflineErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	upstream := &hitCountingFS{files: fstest.MapFS{}}
	cfs := newCachingFS(upstream, root, false, nil)

	_, err := cfs.Open("github.com/foo/bar/@v/v1.0.0.mod")
	if err == nil {
		t.Fatalf("expected error on offline miss")
	}
	if !IsCacheMissOffline(err) {
		t.Fatalf("error %v should satisfy IsCacheMissOffline", err)
	}
	if upstream.opens != 0 {
		t.Fatalf("upstream should not be opened when offline")
	}
}

func TestCachingFS_MissPersistsAndComputesZiphash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	f, err := zw.Create("github.com/foo/bar@v1.0.0/go.mod")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := f.Write([]byte("module github.com/foo/bar\n")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	upstream := fstest.MapFS{
		"github.com/foo/bar/@v/v1.0.0.zip": {Data: buffer.Bytes()},
	}
	rec := NewMemoryRecorder()
	cfs := newCachingFS(upstream, root, true, rec)

	file, err := cfs.Open("github.com/foo/bar/@v/v1.0.0.zip")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gotZip, _ := io.ReadAll(file)
	file.Close()
	if !bytes.Equal(gotZip, buffer.Bytes()) {
		t.Fatalf("served zip differs from upstream bytes")
	}

	zipPath := filepath.Join(root, filepath.FromSlash("github.com/foo/bar/@v/v1.0.0.zip"))
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip not persisted: %v", err)
	}
	hashPath := zipPath + "hash"
	hashBytes, err := os.ReadFile(hashPath)
	if err != nil {
		t.Fatalf("ziphash not written: %v", err)
	}
	if !bytes.HasPrefix(hashBytes, []byte("h1:")) {
		t.Fatalf("ziphash should start with h1:, got %q", hashBytes)
	}

	if len(rec.misses) != 1 {
		t.Fatalf("expected 1 recorded miss, got %d", len(rec.misses))
	}
}

func TestCachingFS_EmptyRootIsPassthrough(t *testing.T) {
	t.Parallel()
	upstream := &hitCountingFS{
		files: fstest.MapFS{
			"foo/bar/@latest": {Data: []byte(`{"Version":"v1.0.0"}`)},
		},
	}
	cfs := newCachingFS(upstream, "", true, nil)
	file, err := cfs.Open("foo/bar/@latest")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	file.Close()
	if upstream.opens != 1 {
		t.Fatalf("empty root should delegate exactly once, got %d opens", upstream.opens)
	}
}

func TestIsCacheMissOffline_FalseOnUnrelated(t *testing.T) {
	t.Parallel()
	if IsCacheMissOffline(errors.New("other")) {
		t.Fatalf("unrelated error should not satisfy IsCacheMissOffline")
	}
	if IsCacheMissOffline(nil) {
		t.Fatalf("nil error should not satisfy IsCacheMissOffline")
	}
}

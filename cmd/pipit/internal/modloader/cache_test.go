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

	"github.com/stretchr/testify/require"
)

func TestParseCacheMode(t *testing.T) {
	t.Parallel()
	_, err := ParseCacheMode("localdir")
	require.Error(t, err, "an unknown keyword is rejected")
	for _, in := range []string{"off", "on", "home", "gopath"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			mode, err := ParseCacheMode(in)
			require.NoError(t, err)
			require.Equal(t, in, string(mode))
		})
	}
}

func TestParseCacheSpec_Keywords(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"off", "on", "home", "gopath"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			spec, err := ParseCacheSpec(in)
			require.NoError(t, err)
			require.Equal(t, in, string(spec.Mode))
			require.Empty(t, spec.Path)
		})
	}
}

func TestParseCacheSpec_PathsPosix(t *testing.T) {
	t.Parallel()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	if home != "" {
		spec, err := ParseCacheSpec("~/.pipit-test")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, ".pipit-test"), spec.Path)
		spec, err = ParseCacheSpec("~")
		require.NoError(t, err)
		require.Equal(t, home, spec.Path)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "absolute", in: "/tmp/foo", want: "/tmp/foo"},
		{name: "relative", in: "./relative", want: filepath.Join(cwd, "relative")},
		{name: "parent-relative", in: "../sibling", want: filepath.Clean(filepath.Join(cwd, "..", "sibling"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, err := ParseCacheSpec(tt.in)
			require.NoError(t, err)
			require.Equal(t, CacheCustom, spec.Mode)
			require.Equal(t, tt.want, spec.Path)
		})
	}
}

func TestParseCacheSpec_BareWordRejected(t *testing.T) {
	t.Parallel()
	_, err := ParseCacheSpec("localdir")
	require.Error(t, err, "a bare word is not a cache path; it needs ./ or /")
	require.Contains(t, err.Error(), "localdir", "the error %q names the input", err)
}

func TestIsPathSpec_WindowsLike(t *testing.T) {
	t.Parallel()

	winLike := []string{`.\foo`, `..\foo`, `~\foo`}
	for _, in := range winLike {
		require.True(t, isPathSpec(in), "isPathSpec(%q)", in)
	}
	if runtime.GOOS == "windows" {
		for _, in := range []string{`C:\foo`, `\\server\share`} {
			require.True(t, isPathSpec(in), "isPathSpec(%q) on Windows", in)
		}
	}
}

func TestResolveCacheSpec_Off(t *testing.T) {
	t.Parallel()
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheOff}, "", io.Discard)
	require.Empty(t, root)
	require.Equal(t, CacheOff, mode)
}

func TestResolveCacheSpec_OnWritable(t *testing.T) {
	t.Parallel()
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "script.go")
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheOn}, scriptPath, io.Discard)
	require.Equal(t, CacheOn, mode)
	want := filepath.Join(scriptDir, ".pipit", "cache", "download")
	require.Equal(t, want, root)
	_, err := os.Stat(root)
	require.NoError(t, err, "the cache root is created")
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
	require.NoError(t, os.Chmod(scriptDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(scriptDir, 0o700) })
	var stderr bytes.Buffer
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheOn}, filepath.Join(scriptDir, "s.go"), &stderr)
	require.Empty(t, root)
	require.Equal(t, CacheOff, mode)
	require.Contains(t, stderr.String(), "disabling cache", "stderr holds the fallback log")
}

func TestResolveCacheSpec_HomeUsesXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheHome}, "", io.Discard)
	want := filepath.Join(xdg, "pipit", "cache", "download")
	require.Equal(t, want, root)
	require.Equal(t, CacheHome, mode)
}

func TestResolveCacheSpec_GopathFallsBackWhenUnset(t *testing.T) {
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", t.TempDir())
	scriptDir := t.TempDir()
	var stderr bytes.Buffer
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheGopath}, filepath.Join(scriptDir, "s.go"), &stderr)

	require.Equal(t, CacheGopath, mode)
	require.Contains(t, root, "go/pkg/mod/cache/download")
}

func TestResolveCacheSpec_Custom(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheCustom, Path: dir}, "", io.Discard)
	require.Equal(t, CacheCustom, mode)
	require.Equal(t, dir, root)
}

func TestResolveCacheSpec_CustomEmptyPathDisables(t *testing.T) {
	t.Parallel()
	root, mode := ResolveCacheSpec(CacheSpec{Mode: CacheCustom, Path: ""}, "", io.Discard)
	require.Empty(t, root)
	require.Equal(t, CacheOff, mode)
}

func TestLockPathFor(t *testing.T) {
	t.Parallel()
	atLatest := lockPathFor(filepath.FromSlash("/cache/github.com/foo/bar/@latest"))
	require.True(t, strings.HasSuffix(atLatest, "@latest.lock"), "the @latest lock path: %q", atLatest)
	root := filepath.FromSlash("/cache/github.com/foo/bar/@v")
	for _, ext := range []string{".zip", ".mod", ".info", ".ziphash"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, filepath.Join(root, "v1.2.3.lock"), lockPathFor(filepath.Join(root, "v1.2.3"+ext)))
		})
	}
}

func TestSplitArtifactPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          string
		wantModule  string
		wantVersion string
	}{
		{name: "zip", in: "github.com/foo/bar/@v/v1.2.3.zip", wantModule: "github.com/foo/bar", wantVersion: "v1.2.3"},
		{name: "mod", in: "github.com/foo/bar/@v/v1.2.3.mod", wantModule: "github.com/foo/bar", wantVersion: "v1.2.3"},
		{name: "latest", in: "github.com/foo/bar/@latest", wantModule: "github.com/foo/bar", wantVersion: ""},
		{name: "list", in: "github.com/foo/bar/@v/list", wantModule: "github.com/foo/bar", wantVersion: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			module, version := splitArtifactPath(tt.in)
			require.Equal(t, tt.wantModule, module)
			require.Equal(t, tt.wantVersion, version)
		})
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
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	want := []byte("module github.com/foo/bar\n")
	require.NoError(t, os.WriteFile(full, want, 0o644))

	upstream := &hitCountingFS{files: fstest.MapFS{}}
	cfs := newCachingFS(upstream, root, true, nil)
	file, err := cfs.Open("github.com/foo/bar/@v/v1.0.0.mod")
	require.NoError(t, err)
	got, err := io.ReadAll(file)
	file.Close()
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 0, upstream.opens)
}

func TestCachingFS_MissOfflineErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	upstream := &hitCountingFS{files: fstest.MapFS{}}
	cfs := newCachingFS(upstream, root, false, nil)

	_, err := cfs.Open("github.com/foo/bar/@v/v1.0.0.mod")
	require.Error(t, err, "an offline miss fails")
	require.True(t, IsCacheMissOffline(err), "%v is an offline cache miss", err)
	require.Equal(t, 0, upstream.opens, "offline mode never opens upstream")
}

func TestCachingFS_MissPersistsAndComputesZiphash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	f, err := zw.Create("github.com/foo/bar@v1.0.0/go.mod")
	require.NoError(t, err)
	_, err = f.Write([]byte("module github.com/foo/bar\n"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	upstream := fstest.MapFS{
		"github.com/foo/bar/@v/v1.0.0.zip": {Data: buffer.Bytes()},
	}
	rec := NewMemoryRecorder()
	cfs := newCachingFS(upstream, root, true, rec)

	file, err := cfs.Open("github.com/foo/bar/@v/v1.0.0.zip")
	require.NoError(t, err)
	gotZip, err := io.ReadAll(file)
	file.Close()
	require.NoError(t, err)
	require.Equal(t, buffer.Bytes(), gotZip, "the cache serves the upstream bytes")

	zipPath := filepath.Join(root, filepath.FromSlash("github.com/foo/bar/@v/v1.0.0.zip"))
	require.FileExists(t, zipPath, "the zip is persisted")
	hashPath := zipPath + "hash"
	hashBytes, err := os.ReadFile(hashPath)
	require.NoError(t, err, "the ziphash is written")
	require.True(t, bytes.HasPrefix(hashBytes, []byte("h1:")), "the ziphash is an h1 hash: %q", hashBytes)

	require.Len(t, rec.misses, 1)
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
	require.NoError(t, err)
	file.Close()
	require.Equal(t, 1, upstream.opens, "an empty root delegates exactly once")
}

func TestIsCacheMissOffline_FalseOnUnrelated(t *testing.T) {
	t.Parallel()
	require.False(t, IsCacheMissOffline(errors.New("other")), "an unrelated error is not an offline cache miss")
	require.False(t, IsCacheMissOffline(nil), "a nil error is not an offline cache miss")
}

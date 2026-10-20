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

//go:build integration

package interop_test

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const memoryFSSource = `
import ("io"; "io/fs"; "path"; "sort"; "strings"; "time")

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { if i.dir { return fs.ModeDir | 0o755 }; return 0o644 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }

type memEntry struct{ info memInfo }

func (e memEntry) Name() string               { return e.info.name }
func (e memEntry) IsDir() bool                { return e.info.dir }
func (e memEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e memEntry) Info() (fs.FileInfo, error) { return e.info, nil }

type memFile struct {
	info    memInfo
	reader  *strings.Reader
	entries []fs.DirEntry
	offset  int
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Read(p []byte) (int, error) {
	if f.info.dir {
		return 0, &fs.PathError{Op: "read", Path: f.info.name, Err: fs.ErrInvalid}
	}
	return f.reader.Read(p)
}
func (f *memFile) Close() error { return nil }
func (f *memFile) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := f.entries[f.offset:]
	if n > 0 && n < len(rest) {
		rest = rest[:n]
	}
	f.offset += len(rest)
	if n > 0 && len(rest) == 0 {
		return nil, io.EOF
	}
	return rest, nil
}

type memFS struct{ files map[string]string }

func (m memFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := m.files[name]; ok {
		return &memFile{info: memInfo{name: path.Base(name), size: int64(len(data))}, reader: strings.NewReader(data)}, nil
	}
	prefix := name + "/"
	if name == "." {
		prefix = ""
	}
	seen := map[string]bool{}
	var entries []fs.DirEntry
	for file := range m.files {
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		rest := strings.TrimPrefix(file, prefix)
		child, _, nested := strings.Cut(rest, "/")
		if seen[child] {
			continue
		}
		seen[child] = true
		entries = append(entries, memEntry{info: memInfo{name: child, dir: nested}})
	}
	if len(entries) == 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return &memFile{info: memInfo{name: path.Base(name), dir: true}, entries: entries}, nil
}

func (m memFS) ReadFile(name string) ([]byte, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return []byte("rf:" + data), nil
}

type openOnly struct{ inner memFS }

func (o openOnly) Open(name string) (fs.File, error) { return o.inner.Open(name) }

type dirOnly struct{ inner memFS }

func (d dirOnly) Open(name string) (fs.File, error) { return d.inner.Open(name) }

func (d dirOnly) ReadDir(name string) ([]fs.DirEntry, error) {
	file, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return file.(*memFile).ReadDir(-1)
}

type stuckFile struct{}

func (stuckFile) Stat() (fs.FileInfo, error) { return memInfo{name: "x", size: 4}, nil }
func (stuckFile) Read(p []byte) (int, error) { return 0, nil }
func (stuckFile) Close() error               { return nil }

type stuckFS struct{}

func (stuckFS) Open(name string) (fs.File, error) { return stuckFile{}, nil }

type mapFS map[string]string

func (m mapFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
func (m mapFS) ReadFile(name string) ([]byte, error) { return []byte("map:" + name), nil }

type namedMapSub struct{}

func (namedMapSub) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
func (namedMapSub) Sub(dir string) (fs.FS, error)     { return mapFS{}, nil }

var site = memFS{files: map[string]string{"a.txt": "alpha", "docs/b.md": "bravo", "docs/c.md": "charlie"}}
`

func TestNativeFSHeldInAnInterpretedInterface(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		code string
		want string
	}{
		"struct field": {
			code: `
import ("io/fs"; "testing/fstest")
type contentFS interface { fs.FS }
type siteData struct { content contentFS }
s := siteData{content: fstest.MapFS{"a.txt": {Data: []byte("hi")}}}
data, err := fs.ReadFile(s.content, "a.txt")
string(data) + "|" + fmt.Sprint(err)`,
			want: "hi|<nil>",
		},
		"variable": {
			code: `
import ("io/fs"; "testing/fstest")
type contentFS interface { fs.ReadFileFS }
var content contentFS = fstest.MapFS{"a.txt": {Data: []byte("hi")}}
matches, err := fs.Glob(content, "*.txt")
fmt.Sprint(matches) + "|" + fmt.Sprint(err)`,
			want: "[a.txt]|<nil>",
		},
		"empty interface": {
			code: `
import ("io/fs"; "testing/fstest")
var content any = fstest.MapFS{"a.txt": {Data: []byte("hi")}}
data, err := fs.ReadFile(content.(fs.FS), "a.txt")
string(data) + "|" + fmt.Sprint(err)`,
			want: "hi|<nil>",
		},
		"map value": {
			code: `
import ("io/fs"; "testing/fstest")
type contentFS interface { fs.FS; fs.ReadFileFS }
var byName = map[string]contentFS{"x": fstest.MapFS{"a.txt": {Data: []byte("hi")}}}
matches, err := fs.Glob(byName["x"], "*.txt")
fmt.Sprint(matches) + "|" + fmt.Sprint(err)`,
			want: "[a.txt]|<nil>",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := newInteropService(t).Eval(t.Context(), "import \"fmt\"\n"+test.code)
			require.NoError(t, err)
			require.Equal(t, test.want, result)
		})
	}
}

func TestInterpretedFSPassedToNativeCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		imports string
		code    string
		want    string
	}{
		"read file through open": {
			code: `data, err := fs.ReadFile(openOnly{inner: site}, "a.txt")
string(data) + "|" + fmt.Sprint(err)`,
			want: "alpha|<nil>",
		},
		"read file fs": {
			code: `data, err := fs.ReadFile(site, "a.txt")
string(data) + "|" + fmt.Sprint(err)`,
			want: "rf:alpha|<nil>",
		},
		"missing file": {
			imports: "import \"errors\"\n",
			code: `_, err := fs.ReadFile(site, "nope.txt")
fmt.Sprint(errors.Is(err, fs.ErrNotExist))`,
			want: "true",
		},
		"read dir": {
			code: `entries, err := fs.ReadDir(site, "docs")
names := []string{}
for _, entry := range entries { names = append(names, entry.Name()) }
strings.Join(names, ",") + "|" + fmt.Sprint(err)`,
			want: "b.md,c.md|<nil>",
		},
		"stat": {
			code: `info, err := fs.Stat(site, "docs/b.md")
fmt.Sprintf("%s %d %t %v", info.Name(), info.Size(), info.IsDir(), err)`,
			want: "b.md 5 false <nil>",
		},
		"walk": {
			code: `var visited []string
err := fs.WalkDir(site, ".", func(p string, d fs.DirEntry, err error) error {
	if err != nil { return err }
	visited = append(visited, p)
	return nil
})
strings.Join(visited, ",") + "|" + fmt.Sprint(err)`,
			want: ".,a.txt,docs,docs/b.md,docs/c.md|<nil>",
		},
		"sub": {
			code: `sub, err := fs.Sub(site, "docs")
if err != nil { panic(err) }
data, err := fs.ReadFile(sub, "c.md")
string(data) + "|" + fmt.Sprint(err)`,
			want: "rf:charlie|<nil>",
		},
		"sub through open": {
			code: `sub, err := fs.Sub(openOnly{inner: site}, "docs")
if err != nil { panic(err) }
data, err := fs.ReadFile(sub, "c.md")
string(data) + "|" + fmt.Sprint(err)`,
			want: "charlie|<nil>",
		},
		"a read that makes no progress": {
			imports: "import \"errors\"\n",
			code: `_, err := fs.ReadFile(stuckFS{}, "x")
fmt.Sprint(errors.Is(err, io.ErrNoProgress))`,
			want: "true",
		},
		"a sub result of a named map type": {
			code: `sub, err := fs.Sub(namedMapSub{}, "docs")
data, readErr := fs.ReadFile(sub, "x")
string(data) + "|" + fmt.Sprint(err) + "|" + fmt.Sprint(readErr)`,
			want: "map:x|<nil>|<nil>",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code := "import \"fmt\"\n" + test.imports + memoryFSSource + "\n" + test.code
			result, err := newInteropService(t).Eval(t.Context(), code)
			require.NoError(t, err)
			require.Equal(t, test.want, result)
		})
	}
}

func TestInterpretedFSPassedToHostFunctions(t *testing.T) {
	t.Parallel()

	service := newInteropService(t)
	service.RegisterPackage("fshost", map[string]reflect.Value{
		"Describe": reflect.ValueOf(func(fsys fs.ReadDirFS, name string) string {
			entries, err := fsys.ReadDir(name)
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			slices.Sort(names)
			return strings.Join(names, ",") + "|" + fmt.Sprint(err)
		}),
		"ReadAll": reflect.ValueOf(func(fsys fs.ReadFileFS, name string) string {
			data, err := fsys.ReadFile(name)
			return string(data) + "|" + fmt.Sprint(err)
		}),
		"ReadOneEntry": reflect.ValueOf(func(directory fs.ReadDirFile) string {
			entries, err := directory.ReadDir(1)
			return fmt.Sprint(len(entries), errors.Is(err, io.ErrNoProgress))
		}),
	})

	code := "import \"fshost\"\n" + memoryFSSource + `
type stuckDir struct{}

func (stuckDir) Stat() (fs.FileInfo, error)            { return memInfo{name: "d", dir: true}, nil }
func (stuckDir) Read(p []byte) (int, error)            { return 0, io.EOF }
func (stuckDir) Close() error                          { return nil }
func (stuckDir) ReadDir(n int) ([]fs.DirEntry, error) { return nil, nil }

fshost.Describe(dirOnly{inner: site}, "docs") + ";" + fshost.ReadAll(site, "a.txt") + ";" + fshost.ReadOneEntry(stuckDir{})`
	result, err := service.Eval(t.Context(), code)
	require.NoError(t, err)
	require.Equal(t, "b.md,c.md|<nil>;rf:alpha|<nil>;0 true", result,
		"a ReadDir(n) that returns no entries and no error makes no progress")
}

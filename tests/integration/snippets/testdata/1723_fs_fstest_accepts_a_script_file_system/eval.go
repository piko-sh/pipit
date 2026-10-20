package main

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing/fstest"
	"time"
)

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
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
		child, _, nested := strings.Cut(strings.TrimPrefix(file, prefix), "/")
		if seen[child] {
			continue
		}
		seen[child] = true
		entries = append(entries, memEntry{info: memInfo{name: child, size: int64(len(m.files[prefix+child])), dir: nested}})
	}
	if len(entries) == 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return &memFile{info: memInfo{name: path.Base(name), dir: true}, entries: entries}, nil
}

func run() string {
	site := memFS{files: map[string]string{"a.txt": "alpha", "docs/b.md": "bravo", "docs/c.md": "charlie"}}
	err := fstest.TestFS(site, "a.txt", "docs/b.md", "docs/c.md")
	return fmt.Sprint(err)
}

package main

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
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

type richFS struct{ base memFS }

func (r richFS) Open(name string) (fs.File, error) { return r.base.Open(name) }

func (r richFS) ReadFile(name string) ([]byte, error) {
	data, ok := r.base.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return []byte("rf:" + data), nil
}

func (r richFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{memEntry{info: memInfo{name: "rd:" + name}}}, nil
}

func (r richFS) Stat(name string) (fs.FileInfo, error) {
	return memInfo{name: "st:" + name, size: 99}, nil
}

func (r richFS) Glob(pattern string) ([]string, error) {
	return []string{"gl:" + pattern}, nil
}

func describe(fsys fs.FS) string {
	data, err := fs.ReadFile(fsys, "docs/b.md")
	out := fmt.Sprintf("readfile=%s/%v", data, err)
	entries, err := fs.ReadDir(fsys, "docs")
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	out += fmt.Sprintf(" readdir=%s/%v", strings.Join(names, ","), err)
	info, err := fs.Stat(fsys, "a.txt")
	if err == nil {
		out += fmt.Sprintf(" stat=%s:%d", info.Name(), info.Size())
	}
	matches, err := fs.Glob(fsys, "docs/*.md")
	out += fmt.Sprintf(" glob=%v/%v", matches, err)
	_, err = fs.ReadFile(fsys, "missing")
	out += fmt.Sprintf(" missing=%v", err)
	return out
}

func run() string {
	base := memFS{files: map[string]string{"a.txt": "alpha", "docs/b.md": "bravo", "docs/c.md": "charlie"}}
	return describe(base) + "\n" + describe(richFS{base: base})
}

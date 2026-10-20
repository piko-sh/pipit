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

type linkInfo struct{ base memInfo }

func (l linkInfo) Name() string       { return l.base.Name() }
func (l linkInfo) Size() int64        { return l.base.Size() }
func (l linkInfo) Mode() fs.FileMode  { return fs.ModeSymlink | 0o777 }
func (l linkInfo) ModTime() time.Time { return l.base.ModTime() }
func (l linkInfo) IsDir() bool        { return false }
func (l linkInfo) Sys() any           { return nil }

type withBoth struct{ base memFS }

func (w withBoth) Open(name string) (fs.File, error)  { return w.base.Open(name) }
func (withBoth) ReadLink(name string) (string, error) { return "target-of-" + name, nil }
func (withBoth) Lstat(name string) (fs.FileInfo, error) {
	return linkInfo{memInfo{name: "link:" + name}}, nil
}

type readLinkOnly struct{ base memFS }

func (r readLinkOnly) Open(name string) (fs.File, error)  { return r.base.Open(name) }
func (readLinkOnly) ReadLink(name string) (string, error) { return "target-of-" + name, nil }

type lstatOnly struct{ base memFS }

func (l lstatOnly) Open(name string) (fs.File, error) { return l.base.Open(name) }
func (lstatOnly) Lstat(name string) (fs.FileInfo, error) {
	return linkInfo{memInfo{name: "link:" + name}}, nil
}

func describe(fsys fs.FS) string {
	target, err := fs.ReadLink(fsys, "a.txt")
	info, statErr := fs.Lstat(fsys, "a.txt")
	_, isLinkFS := fsys.(fs.ReadLinkFS)
	return fmt.Sprintf("%q %v | %s %v %v | %t", target, err, info.Name(), info.Mode(), statErr, isLinkFS)
}

func returned(fsys fs.FS) fs.FS {
	same, _ := fs.Sub(fsys, ".")
	return same
}

func run() string {
	site := memFS{files: map[string]string{"a.txt": "alpha"}}
	return strings.Join([]string{
		describe(returned(withBoth{base: site})),
		describe(returned(readLinkOnly{base: site})),
		describe(returned(lstatOnly{base: site})),
		describe(returned(site)),
	}, "\n")
}

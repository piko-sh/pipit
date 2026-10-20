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

type statFS struct{ base memFS }

func (s statFS) Open(name string) (fs.File, error)     { return s.base.Open(name) }
func (s statFS) Stat(name string) (fs.FileInfo, error) { return memInfo{name: "st:" + name}, nil }

func run() string {
	site := memFS{files: map[string]string{"a.txt": "alpha", "docs/b.md": "bravo"}}
	same, err := fs.Sub(site, ".")
	_, isMem := same.(memFS)
	_, isStat := same.(fs.StatFS)
	_, isReadDir := same.(fs.ReadDirFS)
	out := fmt.Sprintf("dot: %v %t %t %t", err, isMem, isStat, isReadDir)

	sub, err := fs.Sub(site, "docs")
	file, openErr := sub.Open("b.md")
	_, isMemFile := file.(*memFile)
	_, isSeeker := file.(io.Seeker)
	_, isReadDirFile := file.(fs.ReadDirFile)
	out += fmt.Sprintf("\nsub: %v %v %t %t %t", err, openErr, isMemFile, isSeeker, isReadDirFile)

	withStat, _ := fs.Sub(statFS{base: site}, ".")
	_, isStat = withStat.(fs.StatFS)
	info, err := withStat.(fs.StatFS).Stat("a.txt")
	out += fmt.Sprintf("\nstat: %t %s %v", isStat, info.Name(), err)
	return out
}

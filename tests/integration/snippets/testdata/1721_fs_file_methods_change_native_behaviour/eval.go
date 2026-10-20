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

type seekFile struct{ inner *memFile }

func (f seekFile) Stat() (fs.FileInfo, error) { return f.inner.Stat() }
func (f seekFile) Read(p []byte) (int, error) { return f.inner.Read(p) }
func (f seekFile) Close() error               { return f.inner.Close() }
func (f seekFile) Seek(offset int64, whence int) (int64, error) {
	return f.inner.reader.Seek(offset, whence)
}

type writeToFile struct{ inner *memFile }

func (f writeToFile) Stat() (fs.FileInfo, error) { return f.inner.Stat() }
func (f writeToFile) Read(p []byte) (int, error) { return f.inner.Read(p) }
func (f writeToFile) Close() error               { return f.inner.Close() }
func (f writeToFile) WriteTo(w io.Writer) (int64, error) {
	n, err := io.WriteString(w, "wt:")
	rest, err2 := f.inner.reader.WriteTo(w)
	if err == nil {
		err = err2
	}
	return int64(n) + rest, err
}

type wrapFS struct {
	base memFS
	kind string
}

func (w wrapFS) Open(name string) (fs.File, error) {
	file, err := w.base.Open(name)
	if err != nil {
		return nil, err
	}
	inner := file.(*memFile)
	if inner.info.dir {
		return inner, nil
	}
	switch w.kind {
	case "seek":
		return seekFile{inner: inner}, nil
	case "writeTo":
		return writeToFile{inner: inner}, nil
	}
	return inner, nil
}

func open(fsys fs.FS) fs.File {
	sub, err := fs.Sub(fsys, "d")
	if err != nil {
		panic(err)
	}
	file, err := sub.Open("a.txt")
	if err != nil {
		panic(err)
	}
	return file
}

func copied(fsys fs.FS) string {
	file := open(fsys)
	defer file.Close()
	var out strings.Builder
	n, err := io.Copy(&out, file)
	return fmt.Sprintf("%s %d %v", out.String(), n, err)
}

func seeked(fsys fs.FS) string {
	file := open(fsys)
	seeker, ok := file.(io.Seeker)
	if !ok {
		return "no seeker"
	}
	position, err := seeker.Seek(2, io.SeekStart)
	rest, _ := io.ReadAll(file)
	return fmt.Sprintf("%d %v %s", position, err, rest)
}

func run() string {
	site := memFS{files: map[string]string{"d/a.txt": "alpha"}}
	plain, seeking, writing := wrapFS{base: site, kind: "plain"}, wrapFS{base: site, kind: "seek"}, wrapFS{base: site, kind: "writeTo"}
	return strings.Join([]string{copied(plain), copied(writing), seeked(plain), seeked(seeking)}, "\n")
}

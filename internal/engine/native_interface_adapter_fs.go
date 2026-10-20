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

package engine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"time"
)

// fileMethods is a set of the optional methods a script file declares.
type fileMethods uint8

const (
	// fileHasReadDir marks fs.ReadDirFile's ReadDir.
	fileHasReadDir fileMethods = 1 << iota

	// fileHasSeek marks io.Seeker's Seek.
	fileHasSeek

	// fileHasReadAt marks io.ReaderAt's ReadAt.
	fileHasReadAt

	// fileHasWriteTo marks io.WriterTo's WriteTo.
	fileHasWriteTo

	// fileMethodSets is the number of sets of optional file methods.
	fileMethodSets = int(fileHasWriteTo) << 1
)

const (
	// methodOpen names fs.FS's Open.
	methodOpen = "Open"

	// methodReadDir names the ReadDir method of fs.ReadDirFS and fs.ReadDirFile.
	methodReadDir = "ReadDir"

	// methodReadFile names fs.ReadFileFS's ReadFile.
	methodReadFile = "ReadFile"

	// methodStat names the Stat method of fs.StatFS and fs.File.
	methodStat = "Stat"

	// methodSub names fs.SubFS's Sub.
	methodSub = "Sub"

	// methodGlob names fs.GlobFS's Glob.
	methodGlob = "Glob"

	// methodReadLink names fs.ReadLinkFS's ReadLink.
	methodReadLink = "ReadLink"

	// methodLstat names fs.ReadLinkFS's Lstat.
	methodLstat = "Lstat"

	// methodRead names io.Reader's Read.
	methodRead = "Read"

	// methodClose names io.Closer's Close.
	methodClose = "Close"

	// methodSeek names io.Seeker's Seek.
	methodSeek = "Seek"

	// methodReadAt names io.ReaderAt's ReadAt.
	methodReadAt = "ReadAt"

	// methodWriteTo names io.WriterTo's WriteTo.
	methodWriteTo = "WriteTo"

	// methodName names the Name method of fs.FileInfo and fs.DirEntry.
	methodName = "Name"

	// methodSize names fs.FileInfo's Size.
	methodSize = "Size"

	// methodMode names fs.FileInfo's Mode.
	methodMode = "Mode"

	// methodModTime names fs.FileInfo's ModTime.
	methodModTime = "ModTime"

	// methodIsDir names the IsDir method of fs.FileInfo and fs.DirEntry.
	methodIsDir = "IsDir"

	// methodSys names fs.FileInfo's Sys.
	methodSys = "Sys"

	// methodType names fs.DirEntry's Type.
	methodType = "Type"

	// methodInfo names fs.DirEntry's Info.
	methodInfo = "Info"

	// subtreeRoot is the directory name fs.Sub treats as the file system itself.
	subtreeRoot = "."
)

var (
	// fsFSReflectType is the reflect.Type for fs.FS.
	fsFSReflectType = reflect.TypeFor[fs.FS]()

	// fsFileReflectType is the reflect.Type for fs.File.
	fsFileReflectType = reflect.TypeFor[fs.File]()

	// fsReadDirFileReflectType is the reflect.Type for fs.ReadDirFile.
	fsReadDirFileReflectType = reflect.TypeFor[fs.ReadDirFile]()

	// fsFileInfoReflectType is the reflect.Type for fs.FileInfo.
	fsFileInfoReflectType = reflect.TypeFor[fs.FileInfo]()

	// fsDirEntryReflectType is the reflect.Type for fs.DirEntry.
	fsDirEntryReflectType = reflect.TypeFor[fs.DirEntry]()

	// fileAdapterBuilders builds the file adapter for each set of optional methods, indexed
	// by the set.
	fileAdapterBuilders = [fileMethodSets]func(file *pipitFile, mixins fileMixins) reflect.Value{
		0: func(file *pipitFile, _ fileMixins) reflect.Value {
			return reflect.ValueOf(file)
		},
		fileHasReadDir: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileD{pipitFile: file, fileReadDir: mixins.readDir})
		},
		fileHasSeek: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileS{pipitFile: file, fileSeek: mixins.seek})
		},
		fileHasReadDir | fileHasSeek: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDS{pipitFile: file, fileReadDir: mixins.readDir, fileSeek: mixins.seek})
		},
		fileHasReadAt: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileA{pipitFile: file, fileReadAt: mixins.readAt})
		},
		fileHasReadDir | fileHasReadAt: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDA{pipitFile: file, fileReadDir: mixins.readDir, fileReadAt: mixins.readAt})
		},
		fileHasSeek | fileHasReadAt: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileSA{pipitFile: file, fileSeek: mixins.seek, fileReadAt: mixins.readAt})
		},
		fileHasReadDir | fileHasSeek | fileHasReadAt: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDSA{pipitFile: file, fileReadDir: mixins.readDir, fileSeek: mixins.seek, fileReadAt: mixins.readAt})
		},
		fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileW{pipitFile: file, fileWriteTo: mixins.writeTo})
		},
		fileHasReadDir | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDW{pipitFile: file, fileReadDir: mixins.readDir, fileWriteTo: mixins.writeTo})
		},
		fileHasSeek | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileSW{pipitFile: file, fileSeek: mixins.seek, fileWriteTo: mixins.writeTo})
		},
		fileHasReadDir | fileHasSeek | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDSW{pipitFile: file, fileReadDir: mixins.readDir, fileSeek: mixins.seek, fileWriteTo: mixins.writeTo})
		},
		fileHasReadAt | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileAW{pipitFile: file, fileReadAt: mixins.readAt, fileWriteTo: mixins.writeTo})
		},
		fileHasReadDir | fileHasReadAt | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDAW{pipitFile: file, fileReadDir: mixins.readDir, fileReadAt: mixins.readAt, fileWriteTo: mixins.writeTo})
		},
		fileHasSeek | fileHasReadAt | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileSAW{pipitFile: file, fileSeek: mixins.seek, fileReadAt: mixins.readAt, fileWriteTo: mixins.writeTo})
		},
		fileHasReadDir | fileHasSeek | fileHasReadAt | fileHasWriteTo: func(file *pipitFile, mixins fileMixins) reflect.Value {
			return reflect.ValueOf(&fileDSAW{
				pipitFile:   file,
				fileReadDir: mixins.readDir,
				fileSeek:    mixins.seek,
				fileReadAt:  mixins.readAt,
				fileWriteTo: mixins.writeTo,
			})
		},
	}

	_ fs.ReadDirFS = (*pipitFSAdapter)(nil)

	_ fs.ReadFileFS = (*pipitFSAdapter)(nil)

	_ fs.StatFS = (*pipitFSAdapter)(nil)

	_ fs.SubFS = (*pipitFSAdapter)(nil)

	_ fs.GlobFS = (*pipitFSAdapter)(nil)

	_ fs.ReadLinkFS = (*pipitFSAdapter)(nil)

	_ fs.FileInfo = (*pipitFileInfoAdapter)(nil)

	_ fs.DirEntry = (*pipitDirEntryAdapter)(nil)
)

// pipitFSAdapter implements fs.FS and its optional interfaces for a script file system.
// Missing optional methods fall back to io/fs helpers using a view without that method.
type pipitFSAdapter struct {
	*AdapterCore
}

// Open invokes the script Open method.
//
// Takes name (string) which is the path to open.
//
// Returns fs.File which is the opened file, adapted when it is a script value; a nil file
// passes through, as in Go.
// Returns error which is the method's error, joined with why its file cannot be used.
func (a *pipitFSAdapter) Open(name string) (fs.File, error) {
	results, err := a.Invoke(methodOpen, reflect.ValueOf(name))
	if err != nil {
		return nil, err
	}
	methodErr := a.ResultError(results, 1)
	file, err := a.adaptResult[fs.File](methodOpen, results, 0)
	if err != nil {
		return nil, errors.Join(err, methodErr)
	}
	return file, methodErr
}

// ReadDir invokes the script ReadDir method, or reads the directory through Open as
// fs.ReadDir does when the script type has none.
//
// Takes name (string) which is the directory to read.
//
// Returns []fs.DirEntry which are the directory's entries.
// Returns error which is the method's error.
func (a *pipitFSAdapter) ReadDir(name string) ([]fs.DirEntry, error) {
	if !a.has(methodReadDir) {
		return fs.ReadDir(a.openOnly(), name)
	}
	results, err := a.Invoke(methodReadDir, reflect.ValueOf(name))
	if err != nil {
		return nil, err
	}
	return dirEntriesResult(a.AdapterCore, methodReadDir, results)
}

// ReadFile invokes the script ReadFile method, or reads the file through Open as
// fs.ReadFile does when the script type has none.
//
// Takes name (string) which is the file to read.
//
// Returns []byte which is the file's content.
// Returns error which is the method's error.
func (a *pipitFSAdapter) ReadFile(name string) ([]byte, error) {
	if !a.has(methodReadFile) {
		return fs.ReadFile(a.openOnly(), name)
	}
	results, err := a.Invoke(methodReadFile, reflect.ValueOf(name))
	if err != nil {
		return nil, err
	}
	methodErr := a.ResultError(results, 1)
	content, err := a.typedResult[[]byte](methodReadFile, results, 0)
	if err != nil {
		return nil, errors.Join(err, methodErr)
	}
	return content, methodErr
}

// Stat invokes the script Stat method, or opens and stats the file as fs.Stat does when
// the script type has none.
//
// Takes name (string) which is the path to describe.
//
// Returns fs.FileInfo which describes the file.
// Returns error which is the method's error.
func (a *pipitFSAdapter) Stat(name string) (fs.FileInfo, error) {
	if !a.has(methodStat) {
		return fs.Stat(a.openOnly(), name)
	}
	results, err := a.Invoke(methodStat, reflect.ValueOf(name))
	if err != nil {
		return nil, err
	}
	return fileInfoResult(a.AdapterCore, methodStat, results)
}

// Sub invokes the script Sub method, or returns the subtree fs.Sub builds when the script
// type has none.
//
// Takes directory (string) which is the subtree root.
//
// Returns fs.FS which is the subtree.
// Returns error which is the method's error.
func (a *pipitFSAdapter) Sub(directory string) (fs.FS, error) {
	if !a.has(methodSub) {
		if directory == subtreeRoot {
			return a, nil
		}
		return fs.Sub(a.allButSub(), directory)
	}
	results, err := a.Invoke(methodSub, reflect.ValueOf(directory))
	if err != nil {
		return nil, err
	}
	methodErr := a.ResultError(results, 1)
	sub, err := a.adaptResult[fs.FS](methodSub, results, 0)
	if err != nil {
		return nil, errors.Join(err, methodErr)
	}
	return sub, methodErr
}

// Glob invokes the script Glob method, or matches through ReadDir and Stat as fs.Glob
// does when the script type has none.
//
// Takes pattern (string) which is the path.Match pattern.
//
// Returns []string which are the matching names.
// Returns error which is the method's error.
func (a *pipitFSAdapter) Glob(pattern string) ([]string, error) {
	if !a.has(methodGlob) {
		return fs.Glob(a.withReadDirAndStat(), pattern)
	}
	results, err := a.Invoke(methodGlob, reflect.ValueOf(pattern))
	if err != nil {
		return nil, err
	}
	methodErr := a.ResultError(results, 1)
	matches, err := a.typedResult[[]string](methodGlob, results, 0)
	if err != nil {
		return nil, errors.Join(err, methodErr)
	}
	return matches, methodErr
}

// ReadLink invokes the script ReadLink method, or fails as fs.ReadLink does when the
// script type does not declare both ReadLink and Lstat.
//
// Takes name (string) which is the link to read.
//
// Returns string which is the link's destination.
// Returns error which is the method's error.
func (a *pipitFSAdapter) ReadLink(name string) (string, error) {
	if !a.has(methodReadLink) {
		return fs.ReadLink(a.openOnly(), name)
	}
	results, err := a.Invoke(methodReadLink, reflect.ValueOf(name))
	if err != nil {
		return "", err
	}
	return resultString(results), a.ResultError(results, 1)
}

// Lstat invokes the script Lstat method, or stats the file as fs.Lstat does when the
// script type does not declare both ReadLink and Lstat.
//
// Takes name (string) which is the path to describe.
//
// Returns fs.FileInfo which describes the file, not following a link.
// Returns error which is the method's error.
func (a *pipitFSAdapter) Lstat(name string) (fs.FileInfo, error) {
	if !a.has(methodLstat) {
		return fs.Lstat(a.withStat(), name)
	}
	results, err := a.Invoke(methodLstat, reflect.ValueOf(name))
	if err != nil {
		return nil, err
	}
	return fileInfoResult(a.AdapterCore, methodLstat, results)
}

// openOnly returns the file system seen with Open alone.
//
// Returns openView which is the view.
func (a *pipitFSAdapter) openOnly() openView {
	return openView{fsys: a}
}

// withStat returns the file system seen with Open and Stat, which fs.Lstat falls back to.
//
// Returns statView which is the view.
func (a *pipitFSAdapter) withStat() statView {
	return statView{openView: a.openOnly()}
}

// withReadDirAndStat returns the file system seen with Open, Stat and ReadDir, which
// fs.Glob reads through.
//
// Returns globView which is the view.
func (a *pipitFSAdapter) withReadDirAndStat() globView {
	return globView{statView: a.withStat()}
}

// allButSub returns the file system seen with every method but Sub, which the subtree
// fs.Sub builds calls through.
//
// Returns subView which is the view.
func (a *pipitFSAdapter) allButSub() subView {
	return subView{globView: a.withReadDirAndStat()}
}

// openView is a script file system seen with Open alone, for the io/fs helpers' paths
// that do without the optional methods.
type openView struct {
	// fsys is the adapter whose methods the view exposes.
	fsys *pipitFSAdapter
}

// Open opens name through the adapter.
//
// Takes name (string) which is the path to open.
//
// Returns fs.File which is the opened file.
// Returns error which is the method's error.
func (v openView) Open(name string) (fs.File, error) { return v.fsys.Open(name) }

// statView adds Stat, which fs.Lstat falls back to.
type statView struct {
	openView
}

// Stat describes name through the adapter.
//
// Takes name (string) which is the path to describe.
//
// Returns fs.FileInfo which describes the file.
// Returns error which is the method's error.
func (v statView) Stat(name string) (fs.FileInfo, error) { return v.fsys.Stat(name) }

// globView adds ReadDir, which with Stat is what fs.Glob reads through.
type globView struct {
	statView
}

// ReadDir reads the directory name through the adapter.
//
// Takes name (string) which is the directory to read.
//
// Returns []fs.DirEntry which are the directory's entries.
// Returns error which is the method's error.
func (v globView) ReadDir(name string) ([]fs.DirEntry, error) { return v.fsys.ReadDir(name) }

// subView has every optional method but Sub, which the subtree fs.Sub builds calls
// through.
type subView struct {
	globView
}

// ReadFile reads name through the adapter.
//
// Takes name (string) which is the file to read.
//
// Returns []byte which is the file's content.
// Returns error which is the method's error.
func (v subView) ReadFile(name string) ([]byte, error) { return v.fsys.ReadFile(name) }

// Glob matches pattern through the adapter.
//
// Takes pattern (string) which is the path.Match pattern.
//
// Returns []string which are the matching names.
// Returns error which is the method's error.
func (v subView) Glob(pattern string) ([]string, error) { return v.fsys.Glob(pattern) }

// ReadLink reads the link name through the adapter.
//
// Takes name (string) which is the link to read.
//
// Returns string which is the link's destination.
// Returns error which is the method's error.
func (v subView) ReadLink(name string) (string, error) { return v.fsys.ReadLink(name) }

// Lstat describes name through the adapter, not following a link.
//
// Takes name (string) which is the path to describe.
//
// Returns fs.FileInfo which describes the file.
// Returns error which is the method's error.
func (v subView) Lstat(name string) (fs.FileInfo, error) { return v.fsys.Lstat(name) }

// pipitFile supplies the Stat, Read and Close methods common to file adapters.
type pipitFile struct {
	*AdapterCore

	// guard bounds the Read, ReadAt and ReadDir calls.
	guard readGuard
}

// Stat invokes the script Stat method.
//
// Returns fs.FileInfo which describes the file.
// Returns error which is the method's error.
func (f *pipitFile) Stat() (fs.FileInfo, error) {
	results, err := f.Invoke(methodStat)
	if err != nil {
		return nil, err
	}
	return fileInfoResult(f.AdapterCore, methodStat, results)
}

// Read invokes the script Read method.
//
// Takes p ([]byte) which receives the data.
//
// Returns int which is the number of bytes read.
// Returns error which is the method's error, io.ErrNoProgress for a read that returned
// nothing, or the read guard's error once it stops the file.
func (f *pipitFile) Read(p []byte) (int, error) {
	if err := f.guard.admit(); err != nil {
		return 0, err
	}
	results, err := f.Invoke(methodRead, reflect.ValueOf(p))
	if err != nil {
		return 0, err
	}
	return f.guard.settle(int64Result(results), len(p), f.ResultError(results, 1))
}

// Close invokes the script Close method.
//
// Returns error which is the method's error.
func (f *pipitFile) Close() error {
	results, err := f.Invoke(methodClose)
	if err != nil {
		return err
	}
	return f.ResultError(results, 0)
}

// readDir invokes the script ReadDir method.
//
// Takes n (int) which bounds the number of entries, or is at most zero for all of them.
//
// Returns []fs.DirEntry which are the next entries.
// Returns error which is the method's error, io.ErrNoProgress when n is positive and the
// method returned no entries and no error, or the read guard's error once it stops the
// file.
func (f *pipitFile) readDir(n int) ([]fs.DirEntry, error) {
	if err := f.guard.admit(); err != nil {
		return nil, err
	}
	results, err := f.Invoke(methodReadDir, reflect.ValueOf(n))
	if err != nil {
		return nil, err
	}
	entries, err := dirEntriesResult(f.AdapterCore, methodReadDir, results)
	_, err = f.guard.settle(int64(len(entries)), max(n, 0), err)
	return entries, err
}

// seek invokes the script Seek method.
//
// Takes offset (int64) which is the offset relative to whence.
// Takes whence (int) which is io.SeekStart, io.SeekCurrent or io.SeekEnd.
//
// Returns int64 which is the new offset.
// Returns error which is the method's error.
func (f *pipitFile) seek(offset int64, whence int) (int64, error) {
	results, err := f.Invoke(methodSeek, reflect.ValueOf(offset), reflect.ValueOf(whence))
	if err != nil {
		return 0, err
	}
	return int64Result(results), f.ResultError(results, 1)
}

// readAt invokes the script ReadAt method.
//
// Takes p ([]byte) which receives the data.
// Takes offset (int64) which is where reading starts.
//
// Returns int which is the number of bytes read.
// Returns error which is the method's error, io.ErrNoProgress for a read that returned
// nothing, or the read guard's error once it stops the file.
func (f *pipitFile) readAt(p []byte, offset int64) (int, error) {
	if err := f.guard.admit(); err != nil {
		return 0, err
	}
	results, err := f.Invoke(methodReadAt, reflect.ValueOf(p), reflect.ValueOf(offset))
	if err != nil {
		return 0, err
	}
	return f.guard.settle(int64Result(results), len(p), f.ResultError(results, 1))
}

// writeTo invokes the script WriteTo method.
//
// Takes w (io.Writer) which receives the file's remaining data.
//
// Returns int64 which is the number of bytes written.
// Returns error which is the method's error.
func (f *pipitFile) writeTo(w io.Writer) (int64, error) {
	results, err := f.Invoke(methodWriteTo, writerArgument(w))
	if err != nil {
		return 0, err
	}
	return max(int64Result(results), 0), f.ResultError(results, 1)
}

// fileReadDir adds fs.ReadDirFile's ReadDir to a file adapter.
type fileReadDir struct {
	// file dispatches the method.
	file *pipitFile
}

// ReadDir reads the next n directory entries.
//
// Takes n (int) which bounds the number of entries, or is at most zero for all of them.
//
// Returns []fs.DirEntry which are the entries.
// Returns error which is the method's error.
func (m fileReadDir) ReadDir(n int) ([]fs.DirEntry, error) { return m.file.readDir(n) }

// fileSeek adds io.Seeker's Seek to a file adapter.
type fileSeek struct {
	// file dispatches the method.
	file *pipitFile
}

// Seek sets the offset for the next Read.
//
// Takes offset (int64) which is the offset relative to whence.
// Takes whence (int) which is io.SeekStart, io.SeekCurrent or io.SeekEnd.
//
// Returns int64 which is the new offset.
// Returns error which is the method's error.
func (m fileSeek) Seek(offset int64, whence int) (int64, error) { return m.file.seek(offset, whence) }

// fileReadAt adds io.ReaderAt's ReadAt to a file adapter.
type fileReadAt struct {
	// file dispatches the method.
	file *pipitFile
}

// ReadAt reads len(p) bytes starting at offset.
//
// Takes p ([]byte) which receives the data.
// Takes offset (int64) which is where reading starts.
//
// Returns int which is the number of bytes read.
// Returns error which is the method's error.
func (m fileReadAt) ReadAt(p []byte, offset int64) (int, error) { return m.file.readAt(p, offset) }

// fileWriteTo adds io.WriterTo's WriteTo to a file adapter.
type fileWriteTo struct {
	// file dispatches the method.
	file *pipitFile
}

// WriteTo writes the file's remaining data to w.
//
// Takes w (io.Writer) which receives the data.
//
// Returns int64 which is the number of bytes written.
// Returns error which is the method's error.
func (m fileWriteTo) WriteTo(w io.Writer) (int64, error) { return m.file.writeTo(w) }

// fileMixins holds one of each optional file method for a file adapter to embed.
type fileMixins struct {
	// readDir adds ReadDir.
	readDir fileReadDir

	// seek adds Seek.
	seek fileSeek

	// readAt adds ReadAt.
	readAt fileReadAt

	// writeTo adds WriteTo.
	writeTo fileWriteTo
}

// The file adapters for each set of optional methods, named by the letters of the methods
// they add: D for ReadDir, S for Seek, A for ReadAt and W for WriteTo.
type (
	// fileD adds ReadDir.
	fileD struct {
		*pipitFile
		fileReadDir
	}

	// fileS adds Seek.
	fileS struct {
		*pipitFile
		fileSeek
	}

	// fileDS adds ReadDir and Seek.
	fileDS struct {
		*pipitFile
		fileReadDir
		fileSeek
	}

	// fileA adds ReadAt.
	fileA struct {
		*pipitFile
		fileReadAt
	}

	// fileDA adds ReadDir and ReadAt.
	fileDA struct {
		*pipitFile
		fileReadDir
		fileReadAt
	}

	// fileSA adds Seek and ReadAt.
	fileSA struct {
		*pipitFile
		fileSeek
		fileReadAt
	}

	// fileDSA adds ReadDir, Seek and ReadAt.
	fileDSA struct {
		*pipitFile
		fileReadDir
		fileSeek
		fileReadAt
	}

	// fileW adds WriteTo.
	fileW struct {
		*pipitFile
		fileWriteTo
	}

	// fileDW adds ReadDir and WriteTo.
	fileDW struct {
		*pipitFile
		fileReadDir
		fileWriteTo
	}

	// fileSW adds Seek and WriteTo.
	fileSW struct {
		*pipitFile
		fileSeek
		fileWriteTo
	}

	// fileDSW adds ReadDir, Seek and WriteTo.
	fileDSW struct {
		*pipitFile
		fileReadDir
		fileSeek
		fileWriteTo
	}

	// fileAW adds ReadAt and WriteTo.
	fileAW struct {
		*pipitFile
		fileReadAt
		fileWriteTo
	}

	// fileDAW adds ReadDir, ReadAt and WriteTo.
	fileDAW struct {
		*pipitFile
		fileReadDir
		fileReadAt
		fileWriteTo
	}

	// fileSAW adds Seek, ReadAt and WriteTo.
	fileSAW struct {
		*pipitFile

		fileSeek

		fileReadAt

		fileWriteTo
	}

	// fileDSAW adds every optional method.
	fileDSAW struct {
		*pipitFile

		fileReadDir

		fileSeek

		fileReadAt

		fileWriteTo
	}
)

// pipitFileInfoAdapter lets a script type satisfy fs.FileInfo.
type pipitFileInfoAdapter struct {
	*AdapterCore
}

// Name invokes the script Name method.
//
// Returns string which is the base name.
func (a *pipitFileInfoAdapter) Name() string {
	return resultString(a.InvokeRecordingError(methodName))
}

// Size invokes the script Size method.
//
// Returns int64 which is the length in bytes.
func (a *pipitFileInfoAdapter) Size() int64 {
	return int64Result(a.InvokeRecordingError(methodSize))
}

// Mode invokes the script Mode method.
//
// Returns fs.FileMode which is the file's mode bits.
func (a *pipitFileInfoAdapter) Mode() fs.FileMode {
	return resultFileMode(a.InvokeRecordingError(methodMode))
}

// ModTime invokes the script ModTime method.
//
// Returns time.Time which is the modification time, or the zero time when the method
// failed, which InvokeRecordingError has recorded.
func (a *pipitFileInfoAdapter) ModTime() time.Time {
	modified, err := a.typedResult[time.Time](methodModTime, a.InvokeRecordingError(methodModTime), 0)
	if err != nil {
		a.recordError(err)
	}
	return modified
}

// IsDir invokes the script IsDir method.
//
// Returns bool which is true for a directory.
func (a *pipitFileInfoAdapter) IsDir() bool {
	return resultBool(a.InvokeRecordingError(methodIsDir))
}

// Sys invokes the script Sys method.
//
// Returns any which is the underlying data source, or nil.
func (a *pipitFileInfoAdapter) Sys() any {
	results := a.InvokeRecordingError(methodSys)
	if len(results) == 0 || !results[0].IsValid() || !results[0].CanInterface() {
		return nil
	}
	return results[0].Interface()
}

// pipitDirEntryAdapter lets a script type satisfy fs.DirEntry.
type pipitDirEntryAdapter struct {
	*AdapterCore
}

// Name invokes the script Name method.
//
// Returns string which is the entry's base name.
func (a *pipitDirEntryAdapter) Name() string {
	return resultString(a.InvokeRecordingError(methodName))
}

// IsDir invokes the script IsDir method.
//
// Returns bool which is true for a directory.
func (a *pipitDirEntryAdapter) IsDir() bool {
	return resultBool(a.InvokeRecordingError(methodIsDir))
}

// Type invokes the script Type method.
//
// Returns fs.FileMode which holds the entry's type bits.
func (a *pipitDirEntryAdapter) Type() fs.FileMode {
	return resultFileMode(a.InvokeRecordingError(methodType))
}

// Info invokes the script Info method.
//
// Returns fs.FileInfo which describes the entry.
// Returns error which is the method's error.
func (a *pipitDirEntryAdapter) Info() (fs.FileInfo, error) {
	results, err := a.Invoke(methodInfo)
	if err != nil {
		return nil, err
	}
	return fileInfoResult(a.AdapterCore, methodInfo, results)
}

// newFileAdapter exposes exactly the optional methods the script file declares, since
// native callers change behaviour based on which methods exist.
//
// Takes core (*AdapterCore) which dispatches the script methods.
// Takes methods (fileMethods) which are the optional methods the script file declares.
//
// Returns reflect.Value which holds the adapter.
func newFileAdapter(core *AdapterCore, methods fileMethods) reflect.Value {
	file := &pipitFile{AdapterCore: core, guard: readGuard{}}
	mixins := fileMixins{
		readDir: fileReadDir{file: file},
		seek:    fileSeek{file: file},
		readAt:  fileReadAt{file: file},
		writeTo: fileWriteTo{file: file},
	}
	return fileAdapterBuilders[int(methods)%fileMethodSets](file, mixins)
}

// declaredFileMethods returns the optional file methods core's value declares.
//
// Takes core (*AdapterCore) which holds the resolved methods.
//
// Returns fileMethods which is the set.
func declaredFileMethods(core *AdapterCore) fileMethods {
	var methods fileMethods
	if core.has(methodReadDir) {
		methods |= fileHasReadDir
	}
	if core.has(methodSeek) {
		methods |= fileHasSeek
	}
	if core.has(methodReadAt) {
		methods |= fileHasReadAt
	}
	if core.has(methodWriteTo) {
		methods |= fileHasWriteTo
	}
	return methods
}

// fileInfoResult unpacks the (fs.FileInfo, error) results of a script method.
//
// Takes core (*AdapterCore) which builds adapters.
// Takes method (string) which names the method, for errors.
// Takes results ([]reflect.Value) which are the method's results.
//
// Returns fs.FileInfo which is the description, adapted when it is a script value.
// Returns error which is the method's error, joined with why its description cannot be
// used.
func fileInfoResult(core *AdapterCore, method string, results []reflect.Value) (fs.FileInfo, error) {
	methodErr := core.ResultError(results, 1)
	info, err := core.adaptResult[fs.FileInfo](method, results, 0)
	if err != nil {
		return nil, errors.Join(err, methodErr)
	}
	return info, methodErr
}

// dirEntriesResult unpacks the ([]fs.DirEntry, error) results of a script method,
// adapting each entry that is a script value.
//
// Takes core (*AdapterCore) which builds adapters.
// Takes method (string) which names the method, for errors.
// Takes results ([]reflect.Value) which are the method's results.
//
// Returns []fs.DirEntry which are the entries.
// Returns error which is the method's error, joined with why an entry cannot be used.
func dirEntriesResult(core *AdapterCore, method string, results []reflect.Value) ([]fs.DirEntry, error) {
	methodErr := core.ResultError(results, 1)
	list, present := presentResult(results, 0)
	if !present {
		return nil, methodErr
	}
	if entries, ok := reflect.TypeAssert[[]fs.DirEntry](list); ok {
		return entries, methodErr
	}
	if list.Kind() != reflect.Slice {
		return nil, errors.Join(fmt.Errorf("pipit adapter for %s: %s returned %s, not a slice of fs.DirEntry",
			core.typeName, method, scriptTypeString(list.Type())), methodErr)
	}
	entries := make([]fs.DirEntry, list.Len())
	for index := range entries {
		entry, err := core.adaptResult[fs.DirEntry](method, []reflect.Value{list.Index(index)}, 0)
		if err != nil {
			return nil, errors.Join(err, methodErr)
		}
		entries[index] = entry
	}
	return entries, methodErr
}

// resultString returns the first result as a string, or "".
//
// Takes results ([]reflect.Value) which are a script method's results.
//
// Returns string which is the result.
func resultString(results []reflect.Value) string {
	if len(results) == 0 || !results[0].IsValid() || results[0].Kind() != reflect.String {
		return ""
	}
	return results[0].String()
}

// resultBool returns the first result as a bool, or false.
//
// Takes results ([]reflect.Value) which are a script method's results.
//
// Returns bool which is the result.
func resultBool(results []reflect.Value) bool {
	if len(results) == 0 || !results[0].IsValid() || results[0].Kind() != reflect.Bool {
		return false
	}
	return results[0].Bool()
}

// resultFileMode returns the first result as an fs.FileMode, accepting any integer
// representation the interpreter used for it.
//
// Takes results ([]reflect.Value) which are a script method's results.
//
// Returns fs.FileMode which is the result, or zero.
func resultFileMode(results []reflect.Value) fs.FileMode {
	if len(results) == 0 || !results[0].IsValid() {
		return 0
	}
	value := results[0]
	switch {
	case value.CanUint():
		return fs.FileMode(value.Uint()) //nolint:gosec // FileMode is uint32 by definition
	case value.CanInt():
		return fs.FileMode(value.Int()) //nolint:gosec // FileMode is uint32 by definition
	default:
		return 0
	}
}

func init() {
	fsOptional := []reflect.Type{
		reflect.TypeFor[fs.ReadDirFS](), reflect.TypeFor[fs.ReadFileFS](), reflect.TypeFor[fs.StatFS](),
		reflect.TypeFor[fs.SubFS](), reflect.TypeFor[fs.GlobFS](), reflect.TypeFor[fs.ReadLinkFS](),
	}
	registerAdapterFamily(append([]reflect.Type{fsFSReflectType}, fsOptional...), fsOptional, func(core *AdapterCore) reflect.Value {
		return reflect.ValueOf(&pipitFSAdapter{AdapterCore: core})
	})
	fileOptional := []reflect.Type{
		fsReadDirFileReflectType, reflect.TypeFor[io.Seeker](), reflect.TypeFor[io.ReaderAt](), reflect.TypeFor[io.WriterTo](),
	}
	registerAdapterFamily([]reflect.Type{fsFileReflectType, fsReadDirFileReflectType}, fileOptional, func(core *AdapterCore) reflect.Value {
		return newFileAdapter(core, declaredFileMethods(core))
	})
	RegisterInterfaceAdapter(fsFileInfoReflectType, func(core *AdapterCore) reflect.Value {
		return reflect.ValueOf(&pipitFileInfoAdapter{AdapterCore: core})
	})
	RegisterInterfaceAdapter(fsDirEntryReflectType, func(core *AdapterCore) reflect.Value {
		return reflect.ValueOf(&pipitDirEntryAdapter{AdapterCore: core})
	})
}

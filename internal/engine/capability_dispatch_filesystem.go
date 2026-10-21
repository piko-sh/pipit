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
	"context"
	"os"
	"path/filepath"
	"reflect"

	"pipit.sh/pipit/internal/policy"
)

const (
	// defaultFileCreateMode is the permission bitset os.Create passes to the underlying
	// OpenFile, surfaced to the capability gate so a host sees the same mode the stdlib
	// would apply.
	defaultFileCreateMode os.FileMode = 0o666
)

// filesystemGate checks one call of a path-taking function against the typed gates.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns error which is the first denial, or nil when every check passes or the
// arguments carry no path the gate can read.
type filesystemGate func(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error

// filesystemGates maps the registered identity of each path-taking function to its gate.
//
// Reads go to CheckFileOpen with os.O_RDONLY, and writes, creations and metadata changes
// go to CheckFileWrite. A function that takes two paths has both checked. Listing and
// inspection (Stat, ReadDir, Readlink, Walk) count as reads, so a hook that confines
// reads to a directory also confines what a script can discover.
var filesystemGates = map[string]filesystemGate{
	"os.Open":     readPathGate(0),
	"os.ReadFile": readPathGate(0),
	"os.DirFS":    readPathGate(0),
	"os.OpenRoot": readPathGate(0),
	"os.Stat":     readPathGate(0),
	"os.Lstat":    readPathGate(0),
	"os.ReadDir":  readPathGate(0),
	"os.Readlink": readPathGate(0),
	"os.Chdir":    readPathGate(0),

	"os.OpenInRoot": readPathGate(1),
	"os.Create":     createGate,
	"os.OpenFile":   openFileGate,

	"os.WriteFile": writePathGate(0),
	"os.Remove":    writePathGate(0),
	"os.RemoveAll": writePathGate(0),
	"os.Mkdir":     writePathGate(0),
	"os.MkdirAll":  writePathGate(0),
	"os.Truncate":  writePathGate(0),
	"os.Chmod":     writePathGate(0),
	"os.Chown":     writePathGate(0),
	"os.Lchown":    writePathGate(0),
	"os.Chtimes":   writePathGate(0),
	"os.CopyFS":    writePathGate(0),
	"os.Rename":    writePathsGate(0, 1),
	"os.Link":      writePathsGate(0, 1),
	"os.Symlink":   symlinkGate,

	"os.CreateTemp": temporaryDirectoryGate,
	"os.MkdirTemp":  temporaryDirectoryGate,

	"io/ioutil.ReadFile":  readPathGate(0),
	"io/ioutil.ReadDir":   readPathGate(0),
	"io/ioutil.WriteFile": writePathGate(0),
	"io/ioutil.TempFile":  temporaryDirectoryGate,
	"io/ioutil.TempDir":   temporaryDirectoryGate,

	"path/filepath.Walk":         readPathGate(0),
	"path/filepath.WalkDir":      readPathGate(0),
	"path/filepath.Glob":         readPathGate(0),
	"path/filepath.EvalSymlinks": readPathGate(0),

	"archive/zip.OpenReader":     readPathGate(0),
	"crypto/tls.LoadX509KeyPair": readPathsFromGate(0),
	"debug/buildinfo.ReadFile":   readPathGate(0),
	"debug/elf.Open":             readPathGate(0),
	"debug/macho.Open":           readPathGate(0),
	"debug/pe.Open":              readPathGate(0),
	"go/build.ImportDir":         readPathGate(0),
	"go/parser.ParseDir":         readPathGate(1),
	"go/parser.ParseFile":        parseFileGate,
	"html/template.ParseFiles":   readPathsFromGate(0),
	"html/template.ParseGlob":    readPathGate(0),
	"net/http.ServeFile":         readPathGate(2),
	"text/template.ParseFiles":   readPathsFromGate(0),
	"text/template.ParseGlob":    readPathGate(0),
}

// gateFilesystemCapability routes the path-taking stdlib symbols to CheckFileOpen or
// CheckFileWrite. It reports matched=true when functionPath names a filesystem primitive
// (even if the arguments cannot be classified, in which case the specialised gate is
// skipped and err is nil), so the caller stops probing further categories.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes functionPath (string) which is the registered dotted native symbol.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns bool which reports whether functionPath is a filesystem symbol.
// Returns error which is the gate's denial, or nil.
func gateFilesystemCapability(ctx context.Context, hook policy.CapabilityHook, modulePath, functionPath string, arguments []reflect.Value) (bool, error) {
	gate, ok := filesystemGates[functionPath]
	if !ok {
		return false, nil
	}
	return true, gate(ctx, hook, modulePath, arguments)
}

// readPathGate builds a gate that checks the path at index as a read-only open.
//
// Takes index (int) which is the position of the path argument.
//
// Returns filesystemGate which consults CheckFileOpen.
func readPathGate(index int) filesystemGate {
	return func(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
		path, ok := capabilityStringArg(arguments, index)
		if !ok {
			return nil
		}
		return hook.CheckFileOpen(ctx, modulePath, path, os.O_RDONLY, 0)
	}
}

// readPathsFromGate builds a gate that checks every path from index onwards as a
// read-only open, whether the paths are passed one by one or spread from a []string.
//
// Takes start (int) which is the position of the first path argument.
//
// Returns filesystemGate which consults CheckFileOpen once per path.
func readPathsFromGate(start int) filesystemGate {
	return func(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
		for _, path := range capabilityStringsFrom(arguments, start) {
			if err := hook.CheckFileOpen(ctx, modulePath, path, os.O_RDONLY, 0); err != nil {
				return err
			}
		}
		return nil
	}
}

// writePathGate builds a gate that checks the path at index with CheckFileWrite.
//
// Takes index (int) which is the position of the path argument.
//
// Returns filesystemGate which consults CheckFileWrite.
func writePathGate(index int) filesystemGate {
	return func(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
		path, ok := capabilityStringArg(arguments, index)
		if !ok {
			return nil
		}
		return hook.CheckFileWrite(ctx, modulePath, path)
	}
}

// writePathsGate builds a gate for a function that writes at two paths, such as
// os.Rename, and checks both with CheckFileWrite.
//
// Takes indices (...int) which are the positions of the path arguments.
//
// Returns filesystemGate which consults CheckFileWrite once per path.
func writePathsGate(indices ...int) filesystemGate {
	return func(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
		for _, index := range indices {
			path, ok := capabilityStringArg(arguments, index)
			if !ok {
				continue
			}
			if err := hook.CheckFileWrite(ctx, modulePath, path); err != nil {
				return err
			}
		}
		return nil
	}
}

// symlinkGate checks os.Symlink(target, name).
//
// The new name is a write. The target is checked as a read, resolved against the new
// name's directory when it is relative, because that is where the link will point.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns error which is the first denial, or nil.
func symlinkGate(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
	name, nameOK := capabilityStringArg(arguments, 1)
	if nameOK {
		if err := hook.CheckFileWrite(ctx, modulePath, name); err != nil {
			return err
		}
	}
	target, targetOK := capabilityStringArg(arguments, 0)
	if !targetOK {
		return nil
	}
	if !filepath.IsAbs(target) && nameOK {
		target = filepath.Join(filepath.Dir(name), target)
	}
	return hook.CheckFileOpen(ctx, modulePath, target, os.O_RDONLY, 0)
}

// temporaryDirectoryGate checks a temporary file or directory creation against the
// directory it will be created in, which is os.TempDir() when the script passes "".
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns error which is the hook's denial, or nil.
func temporaryDirectoryGate(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
	directory, ok := capabilityStringArg(arguments, 0)
	if !ok {
		return nil
	}
	if directory == "" {
		directory = os.TempDir()
	}
	return hook.CheckFileWrite(ctx, modulePath, directory)
}

// createGate checks os.Create as the open it performs.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns error which is the hook's denial, or nil.
func createGate(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
	path, ok := capabilityStringArg(arguments, 0)
	if !ok {
		return nil
	}
	return hook.CheckFileOpen(ctx, modulePath, path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, defaultFileCreateMode)
}

// openFileGate passes os.OpenFile's path, flag and mode to CheckFileOpen.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns error which is the hook's denial, or nil when the arguments cannot be read.
func openFileGate(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
	path, pathOK := capabilityStringArg(arguments, 0)
	flag, flagOK := capabilityIntArg(arguments, 1)
	mode, modeOK := capabilityFileModeArg(arguments, 2)
	if !pathOK || !flagOK || !modeOK {
		return nil
	}
	return hook.CheckFileOpen(ctx, modulePath, path, flag, mode)
}

// parseFileGate checks go/parser.ParseFile, which reads its file name only when no source
// is passed.
//
// Takes hook (policy.CapabilityHook) which exposes the typed capability gates.
// Takes modulePath (string) which identifies the calling pipit module.
// Takes arguments ([]reflect.Value) which are the prepared call arguments.
//
// Returns error which is the hook's denial, or nil.
func parseFileGate(ctx context.Context, hook policy.CapabilityHook, modulePath string, arguments []reflect.Value) error {
	const sourceIndex = 2
	if sourceIndex < len(arguments) && capabilityNonNil(arguments[sourceIndex]) {
		return nil
	}
	return readPathGate(1)(ctx, hook, modulePath, arguments)
}

// capabilityNonNil reports whether value holds something other than nil.
//
// Takes value (reflect.Value) which is the argument to inspect.
//
// Returns bool which is true for a valid, non-nil value.
func capabilityNonNil(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return !value.IsNil()
	default:
		return true
	}
}

// capabilityStringsFrom collects the string arguments from start onwards, expanding a
// []string the caller spread.
//
// Takes arguments ([]reflect.Value) which is the argument slice to read.
// Takes start (int) which is the position of the first string argument.
//
// Returns []string which holds the strings in call order.
func capabilityStringsFrom(arguments []reflect.Value, start int) []string {
	var paths []string
	for index := start; index < len(arguments); index++ {
		value := arguments[index]
		if !value.IsValid() {
			continue
		}
		switch value.Kind() {
		case reflect.String:
			paths = append(paths, value.String())
		case reflect.Slice:
			if value.Type().Elem().Kind() == reflect.String {
				for element := range value.Len() {
					paths = append(paths, value.Index(element).String())
				}
			}
		default:
		}
	}
	return paths
}

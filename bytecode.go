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

package pipit

import (
	"context"
	"fmt"
	"go/format"
	"os"
	"path/filepath"

	"pipit.sh/pipit/internal/debug"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/schema"
)

var (
	// LoadCompiledFromBytes deserialises a packed bytecode payload. Pass it to
	// Interpreter.LoadModule as the bytecode unpacker.
	LoadCompiledFromBytes = adapters.LoadCompiledFromBytes

	// PackCompiledFileSetToBytes serialises a compiled program into the schema-versioned
	// wire format. Pass it to Interpreter.PackageModule as the bytecode packer.
	PackCompiledFileSetToBytes = adapters.PackCompiledFileSetToBytes
)

// DisassembleAssembly renders a whole compiled program as readable pkasm.
//
// Takes cfs (*CompiledFileSet) which holds the program to render.
//
// Returns string containing the pkasm listing, empty when cfs is nil.
func DisassembleAssembly(cfs *CompiledFileSet) string {
	return debug.DisassembleAssembly(cfs)
}

// DisassembleFunctionAssembly renders one compiled function as readable pkasm.
//
// Takes compiledFunction (*CompiledFunction) which is the function to render.
//
// Returns string containing the pkasm listing, empty when compiledFunction is nil.
func DisassembleFunctionAssembly(compiledFunction *CompiledFunction) string {
	return debug.DisassembleFunctionAssembly(compiledFunction)
}

// NewDirectoryBytecodeStore creates a bytecode store rooted at directory, creating it
// when absent. Keys map to files named "bytecode-<key>.bin".
//
// Takes directory (string) which is the on-disk root for the store.
//
// Returns BytecodeStorePort which is rooted at directory.
// Returns error when directory is empty, cannot be created, or cannot be opened as a
// root.
func NewDirectoryBytecodeStore(directory string) (BytecodeStorePort, error) {
	if directory == "" {
		return nil, ErrEmptyBytecodeDirectory
	}

	if err := os.MkdirAll(directory, bytecodeStoreDirPerm); err != nil {
		return nil, fmt.Errorf("pipit: creating bytecode store directory: %w", err)
	}

	store, err := rootfs.Open(directory)
	if err != nil {
		return nil, fmt.Errorf("pipit: opening bytecode store directory: %w", err)
	}

	return adapters.NewBytecodeStore(store), nil
}

// SaveCompiledToFile writes cfs to a caller-chosen path.
//
// The interpreter must have been built with WithBytecodeStore using
// NewDirectoryBytecodeStore rooted at the file's parent directory. The store names files
// from a key, so when path does not match that derived name the artefact is renamed into
// place afterwards.
//
// Takes interpreter (*Interpreter) which provides the configured bytecode store.
// Takes path (string) which is the caller-chosen output path.
// Takes cfs (*CompiledFileSet) which holds the compiled program to persist.
//
// Returns error when saving or renaming fails.
func SaveCompiledToFile(ctx context.Context, interpreter *Interpreter, path string, cfs *CompiledFileSet) error {
	directory, key, natural := bytecodePathParts(path)
	if err := interpreter.SaveCompiled(ctx, key, cfs); err != nil {
		return err
	}

	if natural == path {
		return nil
	}

	if err := os.Rename(filepath.Join(directory, natural), path); err != nil {
		return fmt.Errorf("pipit: renaming bytecode artefact: %w", err)
	}

	return nil
}

// LoadCompiledFromFile reads a CompiledFileSet from a caller-chosen path.
//
// The interpreter must have been built with WithBytecodeStore using
// NewDirectoryBytecodeStore rooted at the file's parent directory. When path does not
// match the name the store derives from the key, the file is staged under that name for
// the duration of the load and removed afterwards.
//
// Takes interpreter (*Interpreter) which provides the configured bytecode store.
// Takes path (string) which is the caller-chosen input path.
//
// Returns *CompiledFileSet which is reconstructed from the file.
// Returns error when the file is missing, the schema version has changed, or
// reconstruction fails.
func LoadCompiledFromFile(ctx context.Context, interpreter *Interpreter, path string) (*CompiledFileSet, error) {
	directory, key, natural := bytecodePathParts(path)
	naturalPath := filepath.Join(directory, natural)

	if naturalPath != path {
		staging, err := rootfs.Open(directory)
		if err != nil {
			return nil, fmt.Errorf("pipit: opening bytecode directory: %w", err)
		}
		defer func() { _ = staging.Close() }()

		data, err := staging.ReadFile(filepath.Base(path))
		if err != nil {
			return nil, fmt.Errorf("pipit: reading bytecode artefact: %w", err)
		}

		if err := staging.WriteFileAtomic(natural, data, bytecodeStoreFilePerm); err != nil {
			return nil, fmt.Errorf("pipit: staging bytecode artefact: %w", err)
		}

		defer func() { _ = os.Remove(naturalPath) }()
	}

	return interpreter.LoadCompiled(ctx, key)
}

// FormatGoSource formats Go source the way gofmt does, wrapping go/format.Source.
//
// Takes source ([]byte) which is the raw Go source.
//
// Returns []byte which holds the formatted source.
// Returns error when the source cannot be parsed.
func FormatGoSource(source []byte) ([]byte, error) {
	return format.Source(source)
}

// UnpackBytecode strips the schema-version header from a packed bytecode payload.
//
// Takes data ([]byte) which is the packed payload.
//
// Returns []byte which holds the unwrapped FlatBuffers payload.
// Returns error when the header is missing or names a different schema version.
func UnpackBytecode(data []byte) ([]byte, error) {
	return schema.Unpack(data)
}

// InspectBytecode decodes an unwrapped bytecode payload into its FlatBuffers
// representation. The concrete type is not part of pipit's contract.
//
// Takes payload ([]byte) which is the unwrapped payload from UnpackBytecode().
//
// Returns any which holds the decoded representation.
// Returns error when the payload cannot be decoded.
func InspectBytecode(payload []byte) (any, error) {
	return schema.ConvertBytecode(payload)
}

// bytecodePathParts splits a caller-supplied bytecode path into its directory, the store
// key (the filename stem), and the filename the store derives from that key.
//
// Takes path (string) which is the caller-chosen bytecode path.
//
// Returns directory which is the parent directory of path.
// Returns key which is the filename stem with its extension trimmed.
// Returns natural which is the on-disk filename the store derives from key.
func bytecodePathParts(path string) (directory, key, natural string) {
	directory = filepath.Dir(path)
	base := filepath.Base(path)
	key = base[:len(base)-len(filepath.Ext(base))]
	key = trimBytecodePrefix(key)
	natural = bytecodeFilePrefix + key + bytecodeFileSuffix

	return directory, key, natural
}

// trimBytecodePrefix removes the store's filename prefix so a round trip through
// SaveCompiledToFile and LoadCompiledFromFile does not accumulate it.
//
// Takes name (string) which is a filename stem.
//
// Returns string which is name without a leading store prefix.
func trimBytecodePrefix(name string) string {
	if len(name) > len(bytecodeFilePrefix) && name[:len(bytecodeFilePrefix)] == bytecodeFilePrefix {
		return name[len(bytecodeFilePrefix):]
	}

	return name
}

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
//
// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package pipit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func compileReturning(t *testing.T, interpreter *pipit.Interpreter, value string) *pipit.CompiledFileSet {
	t.Helper()
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nfunc run() int { return " + value + " }\n"})
	require.NoError(t, err)
	return compiled
}

func directoryNames(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestBytecodeFilesLeaveTheStoresOwnArtefactAlone(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := pipit.NewDirectoryBytecodeStore(directory)
	require.NoError(t, err)
	interpreter := pipit.NewInterpreter(pipit.WithBytecodeStore(store))
	t.Cleanup(func() { _ = interpreter.Close() })

	require.NoError(t, interpreter.SaveCompiled(t.Context(), "prog", compileReturning(t, interpreter, "1")))
	storeArtefact := filepath.Join(directory, "bytecode-prog.bin")
	original, err := os.ReadFile(storeArtefact)
	require.NoError(t, err)

	chosen := filepath.Join(directory, "prog.pbc")
	require.NoError(t, pipit.SaveCompiledToFile(t.Context(), interpreter, chosen, compileReturning(t, interpreter, "2")))
	afterSave, err := os.ReadFile(storeArtefact)
	require.NoError(t, err)
	require.Equal(t, original, afterSave, "saving to a chosen path leaves the store's artefact for the same key alone")

	loaded, err := pipit.LoadCompiledFromFile(t.Context(), interpreter, chosen)
	require.NoError(t, err)
	result, err := interpreter.ExecuteEntrypoint(t.Context(), loaded, "run")
	require.NoError(t, err)
	require.EqualValues(t, 2, result)

	afterLoad, err := os.ReadFile(storeArtefact)
	require.NoError(t, err, "loading from a chosen path neither overwrites nor deletes the store's artefact")
	require.Equal(t, original, afterLoad)
	require.ElementsMatch(t, []string{"bytecode-prog.bin", "prog.pbc"}, directoryNames(t, directory), "no staging file is left behind")

	fromStore, err := interpreter.LoadCompiled(t.Context(), "prog")
	require.NoError(t, err)
	result, err = interpreter.ExecuteEntrypoint(t.Context(), fromStore, "run")
	require.NoError(t, err)
	require.EqualValues(t, 1, result)
}

func TestBytecodeFilesUnderTheStoresOwnName(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := pipit.NewDirectoryBytecodeStore(directory)
	require.NoError(t, err)
	interpreter := pipit.NewInterpreter(pipit.WithBytecodeStore(store))
	t.Cleanup(func() { _ = interpreter.Close() })

	natural := filepath.Join(directory, "bytecode-direct.bin")
	require.NoError(t, pipit.SaveCompiledToFile(t.Context(), interpreter, natural, compileReturning(t, interpreter, "3")))
	loaded, err := pipit.LoadCompiledFromFile(t.Context(), interpreter, natural)
	require.NoError(t, err)
	result, err := interpreter.ExecuteEntrypoint(t.Context(), loaded, "run")
	require.NoError(t, err)
	require.EqualValues(t, 3, result)
	require.Equal(t, []string{"bytecode-direct.bin"}, directoryNames(t, directory))
}

func TestBytecodeFileErrors(t *testing.T) {
	t.Parallel()
	_, err := pipit.NewDirectoryBytecodeStore("")
	require.ErrorIs(t, err, pipit.ErrEmptyBytecodeDirectory)

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	_, err = pipit.NewDirectoryBytecodeStore(filepath.Join(blocker, "store"))
	require.Error(t, err, "a store cannot be rooted beneath a regular file")

	directory := t.TempDir()
	store, err := pipit.NewDirectoryBytecodeStore(directory)
	require.NoError(t, err)
	interpreter := pipit.NewInterpreter(pipit.WithBytecodeStore(store))
	t.Cleanup(func() { _ = interpreter.Close() })

	_, err = pipit.LoadCompiledFromFile(t.Context(), interpreter, filepath.Join(directory, "missing.pbc"))
	require.Error(t, err)
	_, err = pipit.LoadCompiledFromFile(t.Context(), interpreter, filepath.Join(directory, "absent", "missing.pbc"))
	require.Error(t, err, "the file's directory must exist")

	corrupt := filepath.Join(directory, "corrupt.pbc")
	require.NoError(t, os.WriteFile(corrupt, []byte("not bytecode"), 0o600))
	_, err = pipit.LoadCompiledFromFile(t.Context(), interpreter, corrupt)
	require.Error(t, err)
	require.ElementsMatch(t, []string{"corrupt.pbc"}, directoryNames(t, directory), "a failed load leaves no staging file")

	err = pipit.SaveCompiledToFile(t.Context(), interpreter, filepath.Join(directory, "absent", "out.pbc"), compileReturning(t, interpreter, "4"))
	require.Error(t, err)
}

func TestBytecodeInspectionHelpers(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter()
	compiled := compileReturning(t, interpreter, "5")

	packed, err := pipit.PackCompiledFileSetToBytes(t.Context(), compiled)
	require.NoError(t, err)
	payload, err := pipit.UnpackBytecode(packed)
	require.NoError(t, err)
	inspected, err := pipit.InspectBytecode(payload)
	require.NoError(t, err)
	require.NotNil(t, inspected)
	_, err = pipit.UnpackBytecode([]byte("short"))
	require.Error(t, err)

	require.Contains(t, pipit.DisassembleAssembly(compiled), "run")
	require.Empty(t, pipit.DisassembleAssembly(nil))
	require.Empty(t, pipit.DisassembleFunctionAssembly(nil))

	formatted, err := pipit.FormatGoSource([]byte("package p\nfunc f( ) {}\n"))
	require.NoError(t, err)
	require.Equal(t, "package p\n\nfunc f() {}\n", string(formatted))
	_, err = pipit.FormatGoSource([]byte("package"))
	require.Error(t, err)
}

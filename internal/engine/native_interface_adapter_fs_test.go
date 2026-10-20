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
	"io"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type probeMethod struct {
	name       string
	parameters []isa.RegisterKind
	results    []isa.RegisterKind
	signature  reflect.Type
	pointer    bool
}

var (
	nameParameter = []isa.RegisterKind{isa.RegisterString}
	valueAndError = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral}
	probeOpen     = probeMethod{name: "Open", parameters: nameParameter, results: valueAndError}
	probeStat     = probeMethod{name: "Stat", parameters: nameParameter, results: valueAndError}
	probeReadLink = probeMethod{name: "ReadLink", parameters: nameParameter, results: []isa.RegisterKind{isa.RegisterString, isa.RegisterGeneral}}
	probeLstat    = probeMethod{name: "Lstat", parameters: nameParameter, results: valueAndError}
)

func probeVM(t *testing.T, typeName string, methods ...probeMethod) *VM {
	t.Helper()

	root := program.NewNamedFunction("root")
	for index, method := range methods {
		function := program.NewNamedFunction(typeName + "." + method.name)
		function.HasReceiver = true
		function.IsPointerReceiver = method.pointer
		function.ParameterKinds = append([]isa.RegisterKind{isa.RegisterGeneral}, method.parameters...)
		function.ResultKinds = method.results
		function.SignatureReflectType = method.signature
		root.Functions = append(root.Functions, function)
		require.NoError(t, program.RegisterMethod(root, typeName+"."+method.name, uint16(index)))
	}
	vm := NewVM(t.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	vm.rootFunction = root
	return vm
}

func builtFSCore(t *testing.T, vm *VM, value reflect.Value) *AdapterCore {
	t.Helper()

	adapter := buildRegisteredAdapter(vm, value, fsFSReflectType, "Site")
	require.True(t, adapter.IsValid(), "a type with Open gets the fs.FS adapter")
	core, ok := registeredAdapterCore(adapter)
	require.True(t, ok)
	return core
}

func TestFileAdapterHasExactlyTheDeclaredMethods(t *testing.T) {
	t.Parallel()

	optional := []struct {
		method fileMethods
		iface  reflect.Type
	}{
		{method: fileHasReadDir, iface: fsReadDirFileReflectType},
		{method: fileHasSeek, iface: reflect.TypeFor[io.Seeker]()},
		{method: fileHasReadAt, iface: reflect.TypeFor[io.ReaderAt]()},
		{method: fileHasWriteTo, iface: reflect.TypeFor[io.WriterTo]()},
	}
	seen := make(map[reflect.Type]bool)
	for methods := range fileMethods(16) {
		adapter := newFileAdapter(&AdapterCore{}, methods)
		adapterType := adapter.Type()
		require.False(t, seen[adapterType], "set %04b reuses the adapter of another set", methods)
		seen[adapterType] = true
		require.True(t, adapterType.Implements(fsFileReflectType), "set %04b", methods)
		require.True(t, adapterType.Implements(adapterUnwrapperReflectType), "set %04b", methods)
		_, registered := registeredAdapterCore(adapter)
		require.True(t, registered, "set %04b", methods)
		for _, member := range optional {
			require.Equal(t, methods&member.method != 0, adapterType.Implements(member.iface), "set %04b and %v", methods, member.iface)
		}
	}
}

func TestDeclaredFileMethodsReadsTheResolvedMethods(t *testing.T) {
	t.Parallel()

	core := &AdapterCore{methods: map[string]adapterMethodRef{"Stat": {}, "Read": {}, "Close": {}, "Seek": {}, "WriteTo": {}}}
	require.Equal(t, fileHasSeek|fileHasWriteTo, declaredFileMethods(core))
}

func TestDirEntriesResult(t *testing.T) {
	t.Parallel()

	vm := probeVM(t, "Site", probeOpen)
	core := &AdapterCore{vm: vm, methods: nil, typeName: "Site", underlying: reflect.Value{}}
	native, err := fs.ReadDir(fstest.MapFS{"a": {}}, ".")
	require.NoError(t, err)

	t.Run("a native slice is returned as it is", func(t *testing.T) {
		t.Parallel()

		entries, err := dirEntriesResult(core, "ReadDir", []reflect.Value{reflect.ValueOf(native), reflect.ValueOf(io.EOF)})
		require.ErrorIs(t, err, io.EOF)
		require.Len(t, entries, 1)
	})

	t.Run("an entry that cannot be adapted is an error", func(t *testing.T) {
		t.Parallel()

		_, err := dirEntriesResult(core, "ReadDir", []reflect.Value{reflect.ValueOf([]any{3})})
		require.EqualError(t, err, "pipit adapter for Site: ReadDir returned int, which does not implement fs.DirEntry")
	})

	t.Run("a result that is not a slice is an error", func(t *testing.T) {
		t.Parallel()

		_, err := dirEntriesResult(core, "ReadDir", []reflect.Value{reflect.ValueOf("x")})
		require.EqualError(t, err, "pipit adapter for Site: ReadDir returned string, not a slice of fs.DirEntry")
	})

	t.Run("no entries", func(t *testing.T) {
		t.Parallel()

		entries, err := dirEntriesResult(core, "ReadDir", nil)
		require.NoError(t, err)
		require.Nil(t, entries)
	})

	t.Run("a nil interface is no entries", func(t *testing.T) {
		t.Parallel()

		entries, err := dirEntriesResult(core, "ReadDir", []reflect.Value{reflect.Zero(reflect.TypeFor[any]()), reflect.ValueOf(io.EOF)})
		require.ErrorIs(t, err, io.EOF)
		require.Nil(t, entries)
	})

	t.Run("the method's error is kept when an entry cannot be adapted", func(t *testing.T) {
		t.Parallel()

		_, err := dirEntriesResult(core, "ReadDir", []reflect.Value{reflect.ValueOf([]any{3}), reflect.ValueOf(io.ErrUnexpectedEOF)})
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.ErrorContains(t, err, "does not implement fs.DirEntry")
	})
}

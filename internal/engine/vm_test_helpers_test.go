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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func newTestVM(t *testing.T) *VM {
	t.Helper()
	return NewVM(t.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
}

func executeTestBytecode(t *testing.T, compiledFunction *program.CompiledFunction) (any, error) {
	t.Helper()
	vm := newTestVM(t)
	return vm.Execute(compiledFunction)
}

func testNamedCompositeType(t *testing.T, name string, underlying reflect.Type) reflect.Type {
	t.Helper()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("this build gives named composite types their underlying runtime type")
	}
	identity := typemodel.NamedCompositeIdentity{PkgPath: "main", PkgName: "main", Name: name, BareName: name}
	named, err := typemodel.NamedCompositeTypeFor(identity, underlying)
	require.NoError(t, err)
	return named
}

func testNamedFuncTypeID(t *testing.T, name string) uint32 {
	t.Helper()
	return typemodel.NamedCompositeID(testNamedCompositeType(t, name, reflect.TypeFor[func()]()))
}

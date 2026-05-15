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

//go:build integration

package bytecode_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

func newTestVM(t *testing.T) *engine.VM {
	t.Helper()
	return engine.NewVM(context.Background(), engine.NewGlobalStore(), symtab.NewSymbolRegistry(nil))
}

func compileExpression(t *testing.T, code string) *program.CompiledFunction {
	t.Helper()
	service := app.NewService()
	compiledFunction, err := service.Compile(context.Background(), code)
	require.NoError(t, err)
	return compiledFunction
}

func testRegCounts() [isa.NumRegisterKinds]uint32 {
	var counts [isa.NumRegisterKinds]uint32
	counts[isa.RegisterInt] = 4
	counts[isa.RegisterFloat] = 4
	counts[isa.RegisterString] = 4
	counts[isa.RegisterGeneral] = 4
	counts[isa.RegisterBool] = 4
	counts[isa.RegisterUint] = 4
	counts[isa.RegisterComplex] = 4
	return counts
}

func compileFileSource(t *testing.T, source string) *program.CompiledFileSet {
	t.Helper()
	service := app.NewService()
	sources := map[string]string{"main.go": source}
	cfs, err := service.CompileFileSet(context.Background(), sources)
	require.NoError(t, err)
	return cfs
}

func newTestServiceWithFunctions(t *testing.T, packagePath string, fns map[string]reflect.Value) *app.Service {
	t.Helper()
	return newTestServiceWithSymbols(t, symtab.SymbolExports{packagePath: fns})
}

func newTestServiceWithSymbols(t *testing.T, exports symtab.SymbolExports) *app.Service {
	t.Helper()
	service := app.NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	return service
}

func requireSyntheticResult(t *testing.T, compiledFunction *program.CompiledFunction, expect any) {
	t.Helper()
	result, err := execSynthetic(t, compiledFunction)
	require.NoError(t, err)
	require.Equal(t, expect, result)
}

func execSynthetic(t *testing.T, compiledFunction *program.CompiledFunction) (any, error) {
	t.Helper()
	service := app.NewService()
	return service.Execute(context.Background(), compiledFunction)
}

func requireSyntheticError(t *testing.T, compiledFunction *program.CompiledFunction) {
	t.Helper()
	_, err := execSynthetic(t, compiledFunction)
	require.Error(t, err)
}

func newTestService(t *testing.T, opts ...app.Option) *app.Service {
	t.Helper()
	return app.NewService(opts...)
}

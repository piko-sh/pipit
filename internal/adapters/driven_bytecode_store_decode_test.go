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

//go:build !js || !wasm

package adapters

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"

	"github.com/stretchr/testify/require"
)

func packTrivialProgram(t *testing.T) []byte {
	t.Helper()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": "package main\n\nfunc helper(n int) int {\n\treturn n + 1\n}\n\nfunc main() {\n\tx := helper(1)\n\t_ = x\n}\n"})
	require.NoError(t, err)
	return PackCompiledFileSetToBytes(compiled)
}

func TestLoadCompiledFromBytesRoundTripsTrivialProgram(t *testing.T) {
	t.Parallel()
	data := packTrivialProgram(t)
	loaded, err := LoadCompiledFromBytes(data, symtab.NewSymbolRegistry(nil))
	require.NoError(t, err)
	require.NotNil(t, loaded)
}

func TestLoadCompiledFromBytesRejectsTruncatedPayloads(t *testing.T) {
	t.Parallel()
	data := packTrivialProgram(t)
	registry := symtab.NewSymbolRegistry(nil)
	whole, err := LoadCompiledFromBytes(data, registry)
	require.NoError(t, err)
	entrypoints := len(whole.Entrypoints())

	for cut := 0; cut < len(data); cut += 64 {
		truncated := data[:cut]
		require.NotPanics(t, func() {
			loaded, err := LoadCompiledFromBytes(truncated, registry)
			if err != nil {
				require.Nil(t, loaded, "payload truncated at %d bytes must not yield a file set", cut)
				return
			}

			require.NotNil(t, loaded, "payload truncated at %d bytes decoded to nothing", cut)
			require.Len(t, loaded.Entrypoints(), entrypoints,
				"payload truncated at %d bytes decoded to a partial program", cut)
		}, "payload truncated at %d bytes must not panic", cut)
	}
}

func TestDecodeCompiledFileSetRecoversFromAccessorPanic(t *testing.T) {
	t.Parallel()
	registry := symtab.NewSymbolRegistry(nil)
	malformed := [][]byte{
		{0xff, 0xff, 0xff, 0xff},
		{0x08, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04},
		{0x04, 0x00, 0x00, 0x00, 0xff, 0xff},
	}
	for index, payload := range malformed {
		require.NotPanics(t, func() {
			loaded, err := decodeCompiledFileSet(context.Background(), payload, registry)
			require.ErrorIs(t, err, errCorruptBytecodePayload, "payload %d", index)
			require.Nil(t, loaded, "payload %d", index)
		}, "payload %d must not unwind into the caller", index)
	}
}

const nilClauseProgram = `package main

func classify(value any) string {
	switch value.(type) {
	case bool:
		return "bool"
	case nil:
		return "nil"
	case int:
		return "int"
	case string:
		return "string"
	}
	return "other"
}

func run() string {
	return classify(true) + " " + classify(nil) + " " + classify(1) + " " + classify("s") + " " + classify(2.5)
}
`

func TestLoadCompiledFromBytesKeepsNilTypeSwitchClause(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": nilClauseProgram})
	require.NoError(t, err)
	original, err := compiled.FindFunction("classify")
	require.NoError(t, err)
	require.Contains(t, original.TypeTable, nil, "the compiler records the nil clause as a nil type")

	loaded, err := LoadCompiledFromBytes(PackCompiledFileSetToBytes(compiled), symtab.NewSymbolRegistry(nil))
	require.NoError(t, err)
	reloaded, err := loaded.FindFunction("classify")
	require.NoError(t, err)
	require.Equal(t, original.TypeTable, reloaded.TypeTable)

	run, err := loaded.FindFunction("run")
	require.NoError(t, err)
	result, err := service.Execute(context.Background(), run)
	require.NoError(t, err)
	require.Equal(t, "bool nil int string other", result)
}

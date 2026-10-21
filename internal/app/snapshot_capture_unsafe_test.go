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

//go:build !safe

package app

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/symtab"
)

func TestSnapshotCaptureWithNativeAliasedBytes(t *testing.T) {
	t.Parallel()
	for name, options := range map[string][]Option{
		"assembly":       {},
		"go":             {WithForceGoDispatch()},
		"debug metadata": {WithDebugInfo(), WithForceGoDispatch()},
		"debugger":       {WithDebugger(debug.NewDebugger())},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service := NewService(options...)
			service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
				"Raw": reflect.ValueOf(func(value any) []byte {
					field := reflect.ValueOf(value).Index(0).Field(0)
					return unsafe.Slice((*byte)(unsafe.Pointer(field.UnsafeAddr())), 1)
				}),
			}}))
			files, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": `package main
import "host"
type color struct { Red byte }
var source = []color{{3}}
func run() int {
 raw := host.Raw(source)
 copied := source[0]
 raw[0] = 7
 return int(copied.Red)*100 + int(source[0].Red)*10 + int(raw[0])
}`})
			require.NoError(t, err)
			value, err := service.ExecuteEntrypoint(t.Context(), files, "run")
			require.NoError(t, err)
			require.Equal(t, 377, value)
		})
	}
}

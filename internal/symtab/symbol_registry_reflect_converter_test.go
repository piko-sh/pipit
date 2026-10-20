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

package symtab

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReflectRegisteredErrorIsThePredeclaredError(t *testing.T) {
	t.Parallel()
	registry := NewSymbolRegistry(SymbolExports{"host": {
		"Try": reflect.ValueOf(func(fn func() error) error { return fn() }),
	}})

	pkg, err := registry.Import("host")
	require.NoError(t, err)
	signature, ok := pkg.Scope().Lookup("Try").Type().(*types.Signature)
	require.True(t, ok)

	predeclared := types.Universe.Lookup("error").Type()
	require.True(t, types.Identical(predeclared, signature.Results().At(0).Type()))
	callback, ok := signature.Params().At(0).Type().(*types.Signature)
	require.True(t, ok)
	require.True(t, types.Identical(predeclared, callback.Results().At(0).Type()),
		"a func() error parameter must accept a script's func() error literal, which needs the named error type")
}

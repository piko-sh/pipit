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

package pipit_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestDeclaredPackageNamesThroughThePublicAPI(t *testing.T) {
	t.Parallel()

	interp := pipit.NewInterpreterWithSymbols(pipit.SymbolExports{
		"github.com/example/go-yaml": {
			pipit.PackageNameSymbol: reflect.ValueOf("goyaml"),
			"Double":                reflect.ValueOf(func(n int) int { return 2 * n }),
		},
		"gopkg.in/yaml.v3":          {"Triple": reflect.ValueOf(func(n int) int { return 3 * n })},
		"github.com/example/mod/v2": {"One": reflect.ValueOf(func() int { return 1 })},
	})
	value, err := interp.Eval(t.Context(), `import "github.com/example/go-yaml"
import "gopkg.in/yaml.v3"
import "github.com/example/mod/v2"
goyaml.Double(2) + yaml.Triple(1) + mod.One()`)
	require.NoError(t, err)
	require.EqualValues(t, 8, value)
}

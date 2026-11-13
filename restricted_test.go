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
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestNewRestrictedInterpreterNeedsAProvider(t *testing.T) {
	t.Parallel()
	_, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{})
	require.ErrorIs(t, err, pipit.ErrNoSymbolProvider)
}

func TestRestrictedInterpreterRefusesFileAccess(t *testing.T) {
	t.Parallel()
	first := staticProvider{"os": {"ReadFile": reflect.ValueOf(os.ReadFile), "Stat": reflect.ValueOf(os.Stat)}}
	second := staticProvider{"strings": {"ToUpper": reflect.ValueOf(strings.ToUpper)}}
	for name, providers := range map[string][]pipit.SymbolProviderPort{
		"one provider":  {first},
		"two providers": {first, second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			restricted, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{Imports: []string{"os"}}, providers...)
			require.NoError(t, err)

			result, err := restricted.Eval(t.Context(), "6 * 7")
			require.NoError(t, err)
			require.JSONEq(t, "42", string(result.Value))

			result, err = restricted.EvalFile(t.Context(), "package main\n\nimport \"os\"\n\nfunc run() string {\n\tdata, err := os.ReadFile(\"/etc/hostname\")\n\tif err != nil {\n\t\treturn err.Error()\n\t}\n\treturn string(data)\n}\n", "run")
			if err == nil {
				require.Contains(t, string(result.Value), pipit.ErrCapabilityDenied.Error(), "the default restricted hook refuses a file read")
				return
			}
			require.ErrorIs(t, err, pipit.ErrCapabilityDenied, "the default restricted hook refuses a file read")
		})
	}
}

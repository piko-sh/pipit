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

package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/symtab"
)

func TestRestrictedInterpreterRejectsAStaleTypesPackage(t *testing.T) {
	_, err := NewRestrictedInterpreterWithProvider(RestrictedConfig{Imports: []string{"layouthost"}}, staleLayoutProvider())
	require.ErrorIs(t, err, symtab.ErrTypesPackageLayout)
	require.ErrorIs(t, err, ErrInvalidRestrictedConfig)

	_, err = NewRestrictedInterpreterWithProvider(RestrictedConfig{}, staleLayoutProvider())
	require.NoError(t, err, "a package outside the allowlist is not registered, so its types are not checked")
}

func TestRestrictedSubmissionsShareTheInterpretersNamedCompositeQuota(t *testing.T) {
	t.Parallel()
	interpreter, err := NewRestrictedInterpreter(RestrictedConfig{})
	require.NoError(t, err)

	first, err := interpreter.newService(t.Context(), &restrictedOutput{})
	require.NoError(t, err)
	second, err := interpreter.newService(t.Context(), &restrictedOutput{})
	require.NoError(t, err)

	require.NotNil(t, interpreter.namedCompositeQuota)
	require.Same(t, interpreter.namedCompositeQuota, first.symbols.NamedCompositeQuota())
	require.Same(t, interpreter.namedCompositeQuota, second.symbols.NamedCompositeQuota(),
		"every submission charges the one quota")
}

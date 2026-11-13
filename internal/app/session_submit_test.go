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
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

func TestSessionEnforcesImportPolicyBeforeExecution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		option Option
		code   string
	}{
		{name: "empty allowlist", option: WithImportAllowlist(), code: "import \"math\""},
		{name: "other allowlist", option: WithImportAllowlist("math"), code: "import \"os\""},
		{name: "denied", option: WithDeniedImports("math"), code: "import \"math\""},
		{name: "raw import", option: WithDeniedImports("math"), code: "import alias " + "`math`"},
		{name: "escaped import", option: WithDeniedImports("math"), code: "import \"\\x6dath\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			service := NewService(tt.option, WithStderr(&output))
			symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
				"math": {"Value": reflect.ValueOf(42)},
				"os":   {"Value": reflect.ValueOf(42)},
			})
			symbols.SynthesiseAll(t.Context())
			service.UseSymbols(symbols)
			session := service.NewSession()
			_, err := session.Submit(t.Context(), tt.code+"\nvar value = func() int { println(42); return 1 }()")
			require.Error(t, err, "the session applies the import policy")
			require.Contains(t, err.Error(), "not permitted")
			require.Equal(t, 0, output.Len(), "a denied submission neither runs nor changes the session history")
			require.Empty(t, session.imports)
			require.Equal(t, 0, session.submitCount)
		})
	}
}

func TestSessionEnforcesSubmissionSourceLimitBeforeParsing(t *testing.T) {
	t.Parallel()
	session := NewService(WithMaxSourceSize(32)).NewSession()
	_, err := session.Submit(t.Context(), strings.Repeat("?", 33))
	require.ErrorIs(t, err, fault.ErrSourceSizeLimit, "oversized source is rejected before preprocessing")
	require.Nil(t, session.rootFunction)
}

func TestSessionEnforcesCombinedSourceLimit(t *testing.T) {
	t.Parallel()
	session := NewService(WithMaxSourceSize(180)).NewSession()
	_, err := session.Submit(t.Context(), "var first = \""+strings.Repeat("x", 60)+"\"")
	require.NoError(t, err)
	_, err = session.Submit(t.Context(), "var second = \""+strings.Repeat("x", 60)+"\"")
	require.ErrorIs(t, err, fault.ErrSourceSizeLimit, "retained declarations count towards the source budget")
	require.Equal(t, 1, session.submitCount, "oversized combined source is not committed")
	require.Len(t, session.declaredNames, 1)
}

func TestSessionCancelledBeforePreprocessing(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := session.Submit(ctx, "var value = 42")
	require.ErrorIs(t, err, context.Canceled, "a cancelled submission neither allocates nor commits session state")
	require.Nil(t, session.rootFunction)
	require.Equal(t, 0, session.submitCount)
}

func TestSessionCompilesAllCodeBeforeExecution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "function", code: "var value = func() int { println(42); return 1 }()\nfunc denied() { go func() {}() }"},
		{name: "body", code: "var value = func() int { println(42); return 1 }()\ngo func() {}()"},
		{name: "init", code: "func init() { println(42) }\ngo func() {}()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			session := NewService(WithFeatures(policy.InterpFeaturesRestricted), WithStderr(&output)).NewSession()
			_, err := session.Submit(t.Context(), tt.code)
			require.Error(t, err, "a forbidden goroutine fails compilation")
			require.Empty(t, output.String(), "no code runs before compilation completes")
			require.Equal(t, 0, session.submitCount, "a failed compilation leaves the session history unchanged")
			require.Empty(t, session.declaredNames)
		})
	}
}

func TestSessionCompilationBarrierPreservesInitialisationOrder(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	session := NewService(WithFeatures(policy.InterpFeaturesRestricted), WithStderr(&output)).NewSession()
	code := "var value = initial()\nfunc initial() int { println(1); return 40 }\nfunc init() { println(2); value++ }\nprintln(3)\nfunc() int { return value + 1 }()"
	result, err := session.Submit(t.Context(), code)
	require.NoError(t, err)
	require.Equal(t, 42, result)
	require.Equal(t, "1\n2\n3\n", output.String(), "initialisers run in order before the body")
	output.Reset()
	result, err = session.Submit(t.Context(), "value")
	require.NoError(t, err)
	require.Equal(t, 41, result)
	require.Empty(t, output.String(), "initialisers do not run again")
}

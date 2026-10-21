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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestCompileProblemsListsEveryFailedPackage(t *testing.T) {
	t.Parallel()

	_, err := pipit.NewInterpreter().CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"a":  {"a.go": "package a\n\nfunc A() int { return missing }\n"},
		"b":  {"b.go": "package b\n\n//go:embed b.txt\nvar content string\n"},
		"ok": {"ok.go": "package ok\n\nfunc OK() int { return 1 }\n"},
	})
	require.ErrorIs(t, err, pipit.ErrTypeCheck)
	require.ErrorIs(t, err, pipit.ErrEmbedUnsupported)
	var list *pipit.ProblemList
	require.ErrorAs(t, err, &list)
	require.Len(t, pipit.CompileProblems(err), 2)
	require.Nil(t, pipit.CompileProblems(nil))
}

func TestUncaughtPanicValueAndStack(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter()
	t.Cleanup(func() { _ = interpreter.Close() })
	compiled, err := interpreter.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nfunc explode() int { panic(\"boom\") }\n\nfunc run() int { return explode() }\n"})
	require.NoError(t, err)
	_, err = interpreter.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.Error(t, err)

	value, ok := pipit.UncaughtPanicValue(err)
	require.True(t, ok)
	require.Equal(t, "boom", value)
	require.Contains(t, pipit.UncaughtPanicStack(err), "explode")

	_, ok = pipit.UncaughtPanicValue(errors.New("plain"))
	require.False(t, ok)
	require.Empty(t, pipit.UncaughtPanicStack(errors.New("plain")))
}

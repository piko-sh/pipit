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

// Copyright 2025 The Pipit Authors
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

package liveness_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/compile/liveness"
)

func bodyStatements(t *testing.T, body string) []ast.Stmt {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "probe.go", "package p\nfunc f() {\n"+body+"\n}", 0)
	require.NoError(t, err)
	return file.Decls[0].(*ast.FuncDecl).Body.List
}

func TestLastUseExtendsPastNestedShadow(t *testing.T) {
	t.Parallel()
	statements := bodyStatements(t, `
	x := 1
	y := 2
	{
		x := 3
		_ = x
	}
	z := y
	_ = z
	_ = x`)
	lastUse := liveness.ComputeLastUseIndices(statements)
	require.Equal(t, 5, lastUse["x"], "the outer x is used by the final statement")
	require.Equal(t, 3, lastUse["y"], "y is last used when z is declared")
	require.Equal(t, 4, lastUse["z"])
}

func TestLastUseTreatsShadowOnlyUseAsLive(t *testing.T) {
	t.Parallel()
	statements := bodyStatements(t, `
	x := 1
	{
		x := 2
		_ = x
	}
	y := 3
	_ = y`)
	lastUse := liveness.ComputeLastUseIndices(statements)
	require.Equal(t, 1, lastUse["x"], "the shadowing block is the last statement naming x")
}

func TestLastUseDisabledByGoto(t *testing.T) {
	t.Parallel()
	statements := bodyStatements(t, `
	x := 1
	_ = x
	goto done
done:
	_ = x`)
	require.Nil(t, liveness.ComputeLastUseIndices(statements))
}

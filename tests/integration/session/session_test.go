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

package session_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib"
)

func newTestSession(t *testing.T, opts ...app.Option) *app.Session {
	t.Helper()
	s := app.NewService(opts...)
	s.UseSymbolProviders(stdlib.Providers()...)
	return s.NewSession()
}

func mustSubmit(t *testing.T, sess *app.Session, code string) any {
	t.Helper()
	result, err := sess.Submit(context.Background(), code)
	require.NoError(t, err, "submit %q", code)
	return result
}

func TestSession_BareExpression(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	require.Equal(t, 3, mustSubmit(t, sess, "1+2"))
}

func TestSession_ShortVarThenUse(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "a := 1")
	require.Equal(t, 3, mustSubmit(t, sess, "a + 2"))
}

func TestSession_VarThenUse(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "var x = 5")
	require.Equal(t, 10, mustSubmit(t, sess, "x * 2"))
}

func TestSession_VarReassignment(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "a := 1")
	mustSubmit(t, sess, "a = 2")
	require.Equal(t, 2, mustSubmit(t, sess, "a"))
}

func TestSession_FuncThenCall(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "func double(i int) int { return i * 2 }")
	require.Equal(t, 42, mustSubmit(t, sess, "double(21)"))
}

func TestSession_FuncThenFuncThenCall(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "func double(i int) int { return i * 2 }")
	mustSubmit(t, sess, "func addOne(i int) int { return i + 1 }")
	require.Equal(t, 43, mustSubmit(t, sess, "addOne(double(21))"))
}

func TestSession_TypeThenLiteral(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "type T struct{ N int }")
	require.Equal(t, 7, mustSubmit(t, sess, "T{N: 7}.N"))
}

func TestSession_ConstThenUse(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "const C = 10")
	require.Equal(t, 30, mustSubmit(t, sess, "C * 3"))
}

func TestSession_MultilineFuncDecl(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, `func square(i int) int {
	result := i * i
	return result
}`)
	require.Equal(t, 49, mustSubmit(t, sess, "square(7)"))
}

func TestSession_NestedClosureCapturesGlobal(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	mustSubmit(t, sess, "var a = 1")
	mustSubmit(t, sess, "var f = func() int { return a }")
	mustSubmit(t, sess, "a = 2")
	require.Equal(t, 2, mustSubmit(t, sess, "f()"))
}

func TestSession_StatementsAndDeclsMixedInOneSubmit(t *testing.T) {
	t.Parallel()
	sess := newTestSession(t)
	require.Equal(t, 8, mustSubmit(t, sess, `func quad(i int) int { return i * 4 }
quad(2)`))
}

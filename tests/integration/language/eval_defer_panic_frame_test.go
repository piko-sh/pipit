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

package language_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func TestDeferredPanicOnNormalReturnSurfaces(t *testing.T) {
	t.Parallel()
	_, err := app.NewService().Eval(context.Background(), `func run() int { defer func() { panic("late boom") }(); return 7 }
run()`)
	require.ErrorContains(t, err, "late boom")
}

func TestDeferredPanicCaughtByOuterFrame(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `func inner() int { defer func() { panic("boom") }(); return 7 }
func outer() (r string) { defer func() { if recover() != nil { r = "caught" } }(); inner(); return "no" }
outer()`)
	require.NoError(t, err)
	require.Equal(t, "caught", result)
}

func TestRePanicRecoverDeliversCorrectReturn(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `func f() (r int) {
	defer func() { recover(); r = 5 }()
	defer func() { panic("second") }()
	panic("first")
	return 1
}
f()`)
	require.NoError(t, err)
	require.EqualValues(t, 5, result)
}

func TestRePanicDoesNotRunPostPanicStatements(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `func f(s []int) {
	defer func() { recover() }()
	defer func() { panic("second") }()
	panic("first")
	s[0] = 99
}
s := make([]int, 1); f(s); s[0]`)
	require.NoError(t, err)
	require.EqualValues(t, 0, result)
}

func TestRecoveredNativePanicResumesAncestor(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `func mid() (r string) { defer func() { if recover() != nil { r = "ok" } }(); s := []int{1}; _ = s[9]; return "no" }
func top() string { x := mid(); return x + "-done" }
top()`)
	require.NoError(t, err)
	require.Equal(t, "ok-done", result)
}

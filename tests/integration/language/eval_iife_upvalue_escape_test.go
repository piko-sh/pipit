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

func TestEvalIIFEEscapedUpvalueSurvivesArenaReuse(t *testing.T) {
	t.Parallel()

	result, err := app.NewService().Eval(context.Background(), `func mk() func() int {
	x := 10
	f := func() func() int { return func() int { return x } }()
	return f
}
a := mk()
y := 5
g := func() int { return y }()
_ = g
a()`)
	require.NoError(t, err)
	require.Equal(t, 10, result)
}

func TestEvalIIFEEscapedStringUpvalueSurvivesArenaReuse(t *testing.T) {
	t.Parallel()

	result, err := app.NewService().Eval(context.Background(), `func mk() func() string {
	s := "keep-me"
	return func() func() string { return func() string { return s } }()
}
a := mk()
b := func() string { return "clobber-the-slab" }()
_ = b
a()`)
	require.NoError(t, err)
	require.Equal(t, "keep-me", result)
}

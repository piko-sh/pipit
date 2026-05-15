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

func evalFmt(t *testing.T, source string) any {
	t.Helper()
	s := app.NewService()
	s.UseSymbolProviders(stdlib.Providers()...)
	result, err := s.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	return result
}

func TestFmtVerbTypeInterceptCursor(t *testing.T) {
	t.Parallel()

	require.Equal(t, "   42 string", evalFmt(t, `package main

import "fmt"

func run() string { return fmt.Sprintf("%*d %T", 5, 42, "hi") }`))

	require.Equal(t, "7 float64", evalFmt(t, `package main

import "fmt"

func run() string { return fmt.Sprintf("%[2]d %T", "a", 7, 3.5) }`))

	require.Equal(t, "plain 1 int", evalFmt(t, `package main

import "fmt"

func run() string { return fmt.Sprintf("plain %d %T", 1, 2) }`))
}

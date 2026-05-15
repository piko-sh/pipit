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

func TestGenericMethodTypeChecks(t *testing.T) {
	t.Parallel()

	const source = `package main

type Holder struct{}

func (h Holder) Identity[T any](value T) T { return value }

func run() int {
	return Holder{}.Identity[int](7)
}
`

	service := app.NewService()
	_, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err, "a generic method must reach the compiler, not fail type checking")
}

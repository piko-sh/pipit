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

package bytecode_test

import (
	"context"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
)

func TestSelectCaseBodyBeyondJumpRangeFailsCompile(t *testing.T) {
	t.Parallel()
	var source strings.Builder
	source.WriteString("package main\n\nfunc run(a, b chan int) int {\n\tx := 0\n\tselect {\n\tcase <-a:\n")
	for range 34_000 {
		source.WriteString("\t\tx++\n")
	}
	source.WriteString("\tcase <-b:\n\t\tx--\n\t}\n\treturn x\n}\n")
	service := app.NewService()
	_, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source.String()})
	require.ErrorIs(t, err, program.ErrCompileJumpRange)
}

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

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func TestBytecodeRunOfACompiledProgramWithPackageVariables(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "globals.go")
	out := filepath.Join(tmpDir, "globals.pbc")
	const program = `package main

import (
	"fmt"
	"reflect"
)

type Namer interface{ Name() string }

var counter = 40

var names = []string{"a", "b"}

func bump() int { counter++; return counter }

func Report() string {
	bump()
	return fmt.Sprint(bump(), names, reflect.TypeOf((*Namer)(nil)).Elem().NumMethod())
}

func main() { fmt.Println(Report()) }
`
	require.NoError(t, os.WriteFile(source, []byte(program), 0o644))

	compile := clitest.Run(t.Context(), RunCompile, []string{"-o", out, source}, "")
	require.Equal(t, 0, compile.Code, "compile stderr: %q", compile.Stderr)

	run := clitest.Run(t.Context(), RunBytecode, []string{"run", "--entrypoint", "Report", out}, "")
	require.Equal(t, 0, run.Code, "run stderr: %q", run.Stderr)
	require.Equal(t, "42 [a b] 1", strings.TrimSpace(run.Stdout))
}

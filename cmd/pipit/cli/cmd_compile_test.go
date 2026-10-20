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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func TestRunCompileAndBytecodeRoundtrip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "hello.go")
	out := filepath.Join(tmpDir, "hello.pbc")
	const program = `package main

import "fmt"

func main() {
	fmt.Println("Hello, roundtrip!")
}
`
	require.NoError(t, os.WriteFile(source, []byte(program), 0o644))

	compile := clitest.Run(t.Context(), RunCompile, []string{"-o", out, source}, "")
	require.Equal(t, 0, compile.Code, "compile stderr: %q", compile.Stderr)
	_, err := os.Stat(out)
	require.NoError(t, err, "the output is created")

	inspect := clitest.Run(t.Context(), RunBytecode, []string{"inspect", out}, "")
	require.Equal(t, 0, inspect.Code, "inspect stderr: %q", inspect.Stderr)
	require.Contains(t, inspect.Stdout, "main")

	run := clitest.Run(t.Context(), RunBytecode, []string{"run", out}, "")
	require.Equal(t, 0, run.Code, "run stderr: %q", run.Stderr)

	disasm := clitest.Run(t.Context(), RunBytecode, []string{"disasm", "--no-colour", out}, "")
	require.Equal(t, 0, disasm.Code, "disasm stderr: %q", disasm.Stderr)
	require.NotEmpty(t, disasm.Stdout)
}

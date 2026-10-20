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

func writeScript(t *testing.T, source string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(file, []byte(source), 0o644))
	return file
}

func TestRunExitCodeUncaughtPanic(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() { panic("boom") }
`)
	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, exitPanic, result.Code, "stderr: %q", result.Stderr)
	require.True(t, strings.HasPrefix(result.Stderr, "panic: boom\n\ngoroutine 1 [running]:"), "stderr holds Go's panic report: %q", result.Stderr)
}

func TestRunExitCodeGoroutinePanic(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() {
	done := make(chan bool)
	go func() { panic("in goroutine") }()
	<-done
}
`)
	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, exitPanic, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stderr, "panic: in goroutine")
}

func TestRunExitCodeTimeout(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() {
	for {
	}
}
`)
	result := clitest.Run(t.Context(), RunRun, []string{"-timeout", "200ms", file}, "")
	require.Equal(t, exitTimeout, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stderr, "execution cancelled")
}

func TestRunExitCodeDeadlock(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() {
	ch := make(chan int)
	<-ch
}
`)
	result := clitest.Run(t.Context(), RunRun, []string{"-timeout", "30s", file}, "")
	require.Equal(t, exitPanic, result.Code, "stderr: %q", result.Stderr)
	require.True(t, strings.HasPrefix(result.Stderr, "fatal error: all goroutines are asleep - deadlock!"))
}

func TestRunExitCodeCompileError(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() { var x int = "s"; _ = x }
`)
	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, 1, result.Code, "stderr: %q", result.Stderr)
}

func TestRunDeepRecursionWithinDefaultDepth(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func depth(n int) int {
	if n == 0 {
		return 0
	}
	return 1 + depth(n-1)
}

func main() {
	if depth(50000) != 50000 {
		panic("wrong depth")
	}
}
`)
	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	tight := clitest.Run(t.Context(), RunRun, []string{"-max-call-depth", "1000", file}, "")
	require.Equal(t, exitPanic, tight.Code, "a call depth of 1000 overflows the stack")
	require.Contains(t, tight.Stderr, "stack overflow")
}

func TestRunExitCodeGoEmbed(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

import "embed"

//go:embed main.go
var self embed.FS

func main() { _ = self }
`)
	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, 1, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stderr, "//go:embed is not supported")
	require.Contains(t, result.Stderr, "PIPIT_SCRIPT_DIR")
}

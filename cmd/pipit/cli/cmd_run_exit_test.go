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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func writeScript(t *testing.T, source string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return file
}

func TestRunExitCodeUncaughtPanic(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() { panic("boom") }
`)
	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != exitPanic {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", result.Code, exitPanic, result.Stderr)
	}
	if !strings.HasPrefix(result.Stderr, "panic: boom\n\ngoroutine 1 [running]:") {
		t.Fatalf("stderr = %q, want Go's panic report", result.Stderr)
	}
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
	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != exitPanic {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", result.Code, exitPanic, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "panic: in goroutine") {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunExitCodeTimeout(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() {
	for {
	}
}
`)
	result := clitest.Run(context.Background(), RunRun, []string{"-timeout", "200ms", file}, "")
	if result.Code != exitTimeout {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", result.Code, exitTimeout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "execution cancelled") {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunExitCodeDeadlock(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() {
	ch := make(chan int)
	<-ch
}
`)
	result := clitest.Run(context.Background(), RunRun, []string{"-timeout", "30s", file}, "")
	if result.Code != exitPanic {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", result.Code, exitPanic, result.Stderr)
	}
	if !strings.HasPrefix(result.Stderr, "fatal error: all goroutines are asleep - deadlock!") {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunExitCodeCompileError(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

func main() { var x int = "s"; _ = x }
`)
	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr=%q)", result.Code, result.Stderr)
	}
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
	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	tight := clitest.Run(context.Background(), RunRun, []string{"-max-call-depth", "1000", file}, "")
	if tight.Code != exitPanic || !strings.Contains(tight.Stderr, "stack overflow") {
		t.Fatalf("with -max-call-depth 1000: code=%d stderr=%q", tight.Code, tight.Stderr)
	}
}

func TestRunExitCodeGoEmbed(t *testing.T) {
	t.Parallel()
	file := writeScript(t, `package main

import "embed"

//go:embed main.go
var self embed.FS

func main() { _ = self }
`)
	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr=%q)", result.Code, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "//go:embed is not supported") || !strings.Contains(result.Stderr, "PIPIT_SCRIPT_DIR") {
		t.Fatalf("stderr = %q, want the go:embed diagnostic", result.Stderr)
	}
}

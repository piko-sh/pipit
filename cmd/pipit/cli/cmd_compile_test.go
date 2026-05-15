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
	if err := os.WriteFile(source, []byte(program), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	compile := clitest.Run(context.Background(), RunCompile, []string{"-o", out, source}, "")
	if compile.Code != 0 {
		t.Fatalf("compile failed: code=%d stderr=%q", compile.Code, compile.Stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not created: %v", err)
	}

	inspect := clitest.Run(context.Background(), RunBytecode, []string{"inspect", out}, "")
	if inspect.Code != 0 {
		t.Fatalf("inspect failed: code=%d stderr=%q", inspect.Code, inspect.Stderr)
	}
	if !strings.Contains(inspect.Stdout, "main") {
		t.Fatalf("expected 'main' in inspect output: %q", inspect.Stdout)
	}

	run := clitest.Run(context.Background(), RunBytecode, []string{"run", out}, "")
	if run.Code != 0 {
		t.Fatalf("run failed: code=%d stderr=%q", run.Code, run.Stderr)
	}

	disasm := clitest.Run(context.Background(), RunBytecode, []string{"disasm", "--no-colour", out}, "")
	if disasm.Code != 0 {
		t.Fatalf("disasm failed: code=%d stderr=%q", disasm.Code, disasm.Stderr)
	}
	if disasm.Stdout == "" {
		t.Fatal("expected disasm output")
	}
}

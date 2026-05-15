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

func TestRunRunHelloFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "hello.go")
	const source = `package main

import "fmt"

func main() {
	fmt.Println("Hello, Pipit!")
}
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := clitest.Run(context.Background(), RunRun, []string{file}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}

}

func TestRunRunMissingFile(t *testing.T) {
	t.Parallel()

	result := clitest.Run(context.Background(), RunRun, []string{"/nope/does/not/exist.go"}, "")
	if result.Code == 0 {
		t.Fatalf("expected non-zero exit, got %d", result.Code)
	}
}

func TestRunRunCacheFlagAcceptsAllModes(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "hello.go")
	const source = `package main

import "fmt"

func main() { fmt.Println("ok") }
`
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pathSpec := filepath.Join(t.TempDir(), "custom-cache")

	specs := []string{
		"off",
		"on",
		"home",
		"gopath",
		pathSpec,
		"./" + filepath.Base(t.TempDir()),
		"~/.pipit-cache-flag-test",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			result := clitest.Run(context.Background(), RunRun,
				[]string{"--cache=" + spec, file}, "")
			if result.Code != 0 {
				t.Fatalf("--cache=%s exit=%d stderr=%q", spec, result.Code, result.Stderr)
			}
		})
	}
}

func TestRunRunCacheFlagRejectsBareWord(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "hello.go")
	if err := os.WriteFile(file, []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result := clitest.Run(context.Background(), RunRun,
		[]string{"--cache=localdir", file}, "")
	if result.Code == 0 {
		t.Fatalf("expected non-zero exit for invalid cache value, got 0")
	}
	if !strings.Contains(result.Stderr, "localdir") {
		t.Fatalf("stderr should mention the bad value: %q", result.Stderr)
	}
}

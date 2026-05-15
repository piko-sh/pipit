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

func writeLocalModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/app\n\nreplace example.com/greeting => ./greeting\n",
		"main.go": `package main

import (
	"fmt"

	"example.com/app/internal/util"
)

func main() { fmt.Println(run()) }

// run is the entrypoint the tests invoke: its result reaches the command's stdout
// writer, whereas a script's own prints go to the process stdout.
func run() string { return util.Greet("pipit") }
`,
		"internal/util/util.go": `package util

import (
	"strings"

	"example.com/greeting"
)

func Greet(name string) string { return strings.ToUpper(greeting.Hello(name)) }
`,
		"greeting/greeting.go": "package greeting\n\nfunc Hello(name string) string { return \"hello, \" + name }\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunScriptImportsLocalModulePackages(t *testing.T) {
	root := writeLocalModule(t)
	result := clitest.Run(context.Background(), RunRun, []string{"-entrypoint", "run", filepath.Join(root, "main.go")}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "HELLO, PIPIT" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "local module(s): example.com/greeting, example.com/app") {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunDirectoryImportsLocalModulePackages(t *testing.T) {
	root := writeLocalModule(t)
	result := clitest.Run(context.Background(), RunRun, []string{"-entrypoint", "run", root}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "HELLO, PIPIT" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunScriptMissingLocalPackageFails(t *testing.T) {
	root := writeLocalModule(t)
	script := filepath.Join(root, "broken.go")
	if err := os.WriteFile(script, []byte("package main\n\nimport _ \"example.com/app/nowhere\"\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := clitest.Run(context.Background(), RunRun, []string{script}, "")
	if result.Code == 0 {
		t.Fatalf("expected a failure, stdout=%q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "example.com/app/nowhere") {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

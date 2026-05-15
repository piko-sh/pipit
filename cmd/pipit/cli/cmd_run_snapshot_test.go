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
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

func TestGatedRunExecutesCapturedSource(t *testing.T) {
	for _, directoryTarget := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directoryTarget], func(t *testing.T) {
			directory := t.TempDir()
			sourcePath := filepath.Join(directory, "main.go")
			target := sourcePath
			if directoryTarget {
				target = directory
			}
			original := "package main; func answer() int { return 42 }"
			if err := os.WriteFile(sourcePath, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			flags := flag.NewFlagSet("fixture", flag.ContinueOnError)
			options := registerRunFlags(flags)
			if err := flags.Parse([]string{"--gate=all", "--entrypoint=answer", "--autodeny", "--lockfile=" + filepath.Join(t.TempDir(), "approval.lock")}); err != nil {
				t.Fatal(err)
			}
			gateOptions, _, snapshot, err := capabilityGateOptions(options, target, nil, output.IO{Stdin: nil, Stdout: io.Discard, Stderr: io.Discard})
			if err != nil || snapshot == nil {
				t.Fatalf("capture failed: %v", err)
			}
			if err := os.Remove(sourcePath); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "replacement.go"), []byte("package main; func answer() int { return 99 }"), 0600); err != nil {
				t.Fatal(err)
			}
			interpreter := pipit.NewInterpreter(gateOptions...)
			var stdout bytes.Buffer
			if err := runTarget(context.Background(), interpreter, target, "answer", &stdout, moduleRunOptions{Snapshot: snapshot}); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "42\n" {
				t.Fatalf("executed live replacement instead of approved bytes: %q", stdout.String())
			}
		})
	}
}

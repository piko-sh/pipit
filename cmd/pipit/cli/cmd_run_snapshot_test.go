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
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

func TestGatedRunExecutesCapturedSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		directoryTarget bool
	}{
		{name: "file", directoryTarget: false},
		{name: "directory", directoryTarget: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			sourcePath := filepath.Join(directory, "main.go")
			target := sourcePath
			if tt.directoryTarget {
				target = directory
			}
			original := "package main; func answer() int { return 42 }"
			require.NoError(t, os.WriteFile(sourcePath, []byte(original), 0600))
			flags := flag.NewFlagSet("fixture", flag.ContinueOnError)
			options := registerRunFlags(flags)
			require.NoError(t, flags.Parse([]string{"--gate=all", "--entrypoint=answer", "--autodeny", "--lockfile=" + filepath.Join(t.TempDir(), "approval.lock")}))
			gateOptions, _, snapshot, err := capabilityGateOptions(options, target, nil, output.IO{Stdin: nil, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			require.NotNil(t, snapshot)
			require.NoError(t, os.Remove(sourcePath))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "replacement.go"), []byte("package main; func answer() int { return 99 }"), 0600))
			interpreter := pipit.NewInterpreter(gateOptions...)
			var stdout bytes.Buffer
			require.NoError(t, runTarget(t.Context(), interpreter, target, "answer", &stdout, moduleRunOptions{Snapshot: snapshot}))
			require.Equal(t, "42\n", stdout.String(), "the approved bytes run, not a live replacement")
		})
	}
}

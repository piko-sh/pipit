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

package runner

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScriptEnvSetsTheArguments(t *testing.T) {
	t.Parallel()

	env := ScriptEnv("main.go", []string{"-v", "two words"})
	require.Equal(t, "2", env[ArgumentCountEnv])
	require.Equal(t, "-v", env[ArgumentEnvPrefix+"0"])
	require.Equal(t, "two words", env[ArgumentEnvPrefix+"1"])
	require.Len(t, env, 4)
}

func TestScriptEnvSetsTheScriptDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	program := filepath.Join(directory, "main.go")
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "file", target: program, want: directory},
		{name: "directory", target: directory, want: directory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := ScriptEnv(tt.target, nil)
			require.Equal(t, tt.want, env[ScriptDirectoryEnv])
			require.Equal(t, "0", env[ArgumentCountEnv])
		})
	}
}

func TestScriptEnvMakesARelativeTargetAbsolute(t *testing.T) {
	t.Parallel()

	env := ScriptEnv("main.go", nil)
	require.True(t, filepath.IsAbs(env[ScriptDirectoryEnv]), "a relative target still gives an absolute directory")
}

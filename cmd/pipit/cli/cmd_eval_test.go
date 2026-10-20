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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func TestRunEvalInlineExpression(t *testing.T) {
	t.Parallel()

	result := clitest.Run(t.Context(), RunEval, []string{"-e", "1 + 2 * 3"}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, "7", strings.TrimSpace(result.Stdout), "stdout: %q", result.Stdout)
}

func TestRunEvalStdin(t *testing.T) {
	t.Parallel()

	const source = "import \"strings\"\nstrings.ToUpper(\"hi\")\n"
	result := clitest.Run(t.Context(), RunEval, []string{"-"}, source)
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stdout, "HI")
}

func TestRunEvalEmptyExitsOne(t *testing.T) {
	t.Parallel()

	result := clitest.Run(t.Context(), RunEval, nil, "")
	require.Equal(t, 1, result.Code, "stderr: %q", result.Stderr)
}

func TestRunEvalGoEmbedHasNoScriptDirectoryHint(t *testing.T) {
	t.Parallel()

	const source = "import \"embed\"\n\n//go:embed x.txt\nvar files embed.FS\n\nfiles\n"
	result := clitest.Run(t.Context(), RunEval, []string{"-"}, source)
	require.NotEqual(t, 0, result.Code, "stdout=%q", result.Stdout)
	require.Contains(t, result.Stderr, "//go:embed is not supported")
	require.NotContains(t, result.Stderr, "PIPIT_SCRIPT_DIR", "eval has no script directory")
}

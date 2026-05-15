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
	"strings"
	"testing"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func TestRunEvalInlineExpression(t *testing.T) {
	t.Parallel()

	result := clitest.Run(context.Background(), RunEval, []string{"-e", "1 + 2 * 3"}, "")
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "7" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunEvalStdin(t *testing.T) {
	t.Parallel()

	const source = "import \"strings\"\nstrings.ToUpper(\"hi\")\n"
	result := clitest.Run(context.Background(), RunEval, []string{"-"}, source)
	if result.Code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", result.Code, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "HI") {
		t.Fatalf("expected HI in stdout, got %q", result.Stdout)
	}
}

func TestRunEvalEmptyExitsOne(t *testing.T) {
	t.Parallel()

	result := clitest.Run(context.Background(), RunEval, nil, "")
	if result.Code != 1 {
		t.Fatalf("expected exit 1, got %d (stderr=%q)", result.Code, result.Stderr)
	}
}

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

package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/fault"
)

func TestCompileFileSetRejectsGoEmbed(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"group": "package main\n\nimport \"embed\"\n\n//go:embed hello.txt\nvar (\n\tf embed.FS\n)\n\nfunc main() {}\n",
		"spec":  "package main\n\nimport \"embed\"\n\nvar (\n\t//go:embed hello.txt\n\tf embed.FS\n)\n\nfunc main() {}\n",
		"plain": "package main\n\n//go:embed hello.txt\nvar data string\n\nfunc main() {}\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			_, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
			require.Error(t, err)
			require.True(t, errors.Is(err, fault.ErrCompileEmbedUnsupported), "%v", err)
			require.True(t, errors.Is(err, fault.ErrCompilation), "%v", err)
			require.Contains(t, err.Error(), "PIPIT_SCRIPT_DIR")
			require.Contains(t, err.Error(), "main.go:")
		})
	}
	t.Run("mention", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		source := "package main\n\n// The go:embed directive is not used here.\nvar data = \"x\"\n\nfunc main() {}\n"
		_, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
		require.NoError(t, err)
	})
}

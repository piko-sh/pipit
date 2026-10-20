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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func TestRunSymbolsListMentionsStrings(t *testing.T) {
	t.Parallel()

	result := clitest.Run(t.Context(), RunSymbols, []string{"list"}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stdout, "strings")
}

func TestRunSymbolsShowStrings(t *testing.T) {
	t.Parallel()

	result := clitest.Run(t.Context(), RunSymbols, []string{"show", "strings"}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Contains(t, result.Stdout, "HasPrefix")
}

func TestRunSymbolsShowMissingPackage(t *testing.T) {
	t.Parallel()

	result := clitest.Run(t.Context(), RunSymbols, []string{"show", "no-such-pkg"}, "")
	require.Equal(t, 1, result.Code)
	require.Contains(t, result.Stderr, "no-such-pkg")
}

func TestRunSymbolsShowsTheDeclaredNameApart(t *testing.T) {
	t.Parallel()

	ctx := withConfig(t.Context(), newConfig([]Option{WithSymbols(pipit.SymbolExports{
		"github.com/example/go-widgets": {
			pipit.PackageNameSymbol: reflect.ValueOf("widgetkit"),
			"Count":                 reflect.ValueOf(func() int { return 0 }),
		},
	})}))

	show := clitest.Run(ctx, RunSymbols, []string{"show", "github.com/example/go-widgets"}, "")
	require.Equal(t, 0, show.Code, "stderr: %q", show.Stderr)
	require.Contains(t, show.Stdout, "(1 symbols)")
	require.Contains(t, show.Stdout, "declared name widgetkit")
	require.NotContains(t, show.Stdout, pipit.PackageNameSymbol, "the name entry is listed as a symbol")

	list := clitest.Run(ctx, RunSymbols, []string{"list", "-filter", "go-widgets"}, "")
	require.Contains(t, list.Stdout, "   1  github.com/example/go-widgets")
}

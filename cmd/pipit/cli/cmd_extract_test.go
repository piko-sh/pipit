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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/extract"
)

func TestExtractDefaultFileFilterMatchesTheInterpreter(t *testing.T) {
	t.Parallel()

	files := []struct {
		name   string
		source string
	}{
		{name: "a.go", source: "package p\n"},
		{name: "a_test.go", source: "package p_test\n"},
		{name: "a_windows.go", source: "package p\n"},
		{name: "a_linux_amd64.go", source: "package p\n"},
		{name: "a_windows.pb.go", source: "package p\n"},
		{name: "_a.go", source: "package p\n"},
		{name: ".a.go", source: "package p\n"},
		{name: "dir/a_plan9.go", source: "package p\n"},
		{name: "a.go", source: "//go:build interp\n\npackage p\n"},
		{name: "a.go", source: "//go:build !interp\n\npackage p\n"},
		{name: "a.go", source: "//go:build cgo\n\npackage p\n"},
		{name: "a.go", source: "//go:build gc && go1.21\n\npackage p\n"},
		{name: "a.go", source: "//go:build unix\n\npackage p\n"},
		{name: "a.go", source: "// +build ignore\n\npackage p\n"},
		{name: "a.go", source: "//go:build linux\n// +build windows\n\npackage p\n"},
		{name: "a.go", source: "package p\n\nimport \"C\"\n"},
		{name: "a_test.go", source: "package p\n\nimport \"C\"\n"},
		{name: "doc.go", source: "package documentation\n"},
		{name: "a.go", source: "//go:build (\n\npackage p\n"},
		{name: "a.go", source: "//go:build linux\n//go:build amd64\n\npackage p\n"},
		{name: "a.go", source: "package p\n\nimport (\n"},
		{name: "a.go", source: "package p\x00\n"},
	}
	interpreter := pipit.NewFileFilter()
	discovery := extract.DefaultFileFilter(nil)
	for _, file := range files {
		want, wantErr := interpreter(file.name, []byte(file.source))
		got, gotErr := discovery(file.name, []byte(file.source))
		require.Equal(t, want, got, "%s: %q", file.name, file.source)
		require.Equal(t, wantErr != nil, gotErr != nil, "%s: %q", file.name, file.source)
	}
}

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

package modloader

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestCollectModuleZipSourcesLeavesOutCommands(t *testing.T) {
	t.Parallel()
	zip := fstest.MapFS{
		"lib.go":              {Data: []byte("package lib\n\nfunc Lib() {}\n")},
		"lib_test.go":         {Data: []byte("package lib\n")},
		"codegen/main.go":     {Data: []byte("package main\n\nimport _ \"embed\"\n\n//go:embed template.txt\nvar template string\n\nfunc main() {}\n")},
		"codegen/helpers.go":  {Data: []byte("package main\n")},
		"internal/x/x.go":     {Data: []byte("package x\n")},
		"testdata/data.go":    {Data: []byte("package data\n")},
		"tools/mixed/a.go":    {Data: []byte("package mixed\n")},
		"tools/mixed/main.go": {Data: []byte("package main\n")},
	}

	packages, err := collectModuleZipSources(zip)

	require.NoError(t, err)
	require.ElementsMatch(t, []string{"", "internal/x", "tools/mixed"}, sortedKeys(packages),
		"a command directory is left out; a directory mixing packages is kept for the compiler to report")
	require.Len(t, packages[""], 1, "test files are not bundled")
}

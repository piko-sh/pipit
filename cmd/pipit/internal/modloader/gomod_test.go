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

	"github.com/stretchr/testify/require"
)

func TestParseGoModReadsModuleRequireAndDirectoryReplaces(t *testing.T) {
	t.Parallel()

	file := parseGoMod([]byte(`module example.com/app // the main module

go 1.27

require example.com/single v1.0.0
require (
	example.com/a v1.2.3 // indirect
	example.com/b v0.0.0-20240101000000-abcdef123456
)

replace example.com/local => ./local
replace (
	example.com/parent => ../parent
	example.com/abs => /srv/abs
	example.com/pinned v1.0.0 => ./pinned
	example.com/moved => example.com/moved/v2 v2.0.0
)
`))

	require.Equal(t, "example.com/app", file.Module)
	require.Equal(t, map[string]string{
		"example.com/single": "v1.0.0",
		"example.com/a":      "v1.2.3",
		"example.com/b":      "v0.0.0-20240101000000-abcdef123456",
	}, file.Require)
	require.Equal(t, map[string]string{
		"example.com/local":  "./local",
		"example.com/parent": "../parent",
		"example.com/abs":    "/srv/abs",
		"example.com/pinned": "./pinned",
	}, file.Replace, "a version replacement is not a directory replacement")
}

func TestParseGoModToleratesMissingDirectives(t *testing.T) {
	t.Parallel()

	file := parseGoMod([]byte("go 1.27\n"))
	require.Empty(t, file.Module)
	require.Nil(t, file.Require)
	require.Nil(t, file.Replace)
	require.Nil(t, parseAdjacentVersions([]byte("module x\n")))
}

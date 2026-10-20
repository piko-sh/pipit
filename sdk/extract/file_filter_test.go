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

package extract

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultFileFilter(t *testing.T) {
	t.Parallel()

	otherOS := "windows"
	if runtime.GOOS == otherOS {
		otherOS = "linux"
	}
	tests := []struct {
		name    string
		file    string
		source  string
		tags    []string
		want    bool
		wantErr error
	}{
		{name: "an ordinary file", file: "a.go", source: "package p\n", want: true},
		{name: "the interp tag", file: "a.go", source: "//go:build interp\n\npackage p\n", want: true},
		{name: "not interp", file: "a.go", source: "//go:build !interp\n\npackage p\n", want: false},
		{name: "a tag passed in", file: "a.go", source: "//go:build pro\n\npackage p\n", tags: []string{"pro"}, want: true},
		{name: "cgo is off", file: "a.go", source: "//go:build cgo\n\npackage p\n", want: false},
		{name: "a file that imports C", file: "a.go", source: "package p\n\nimport \"C\"\n", want: false},
		{name: "cgo in a test file", file: "a_test.go", source: "package p\n\nimport \"C\"\n", wantErr: errCgoInTest},
		{name: "package documentation", file: "doc.go", source: "package documentation\n", want: false},
		{name: "a dot prefix in a path", file: "dir/.a.go", source: "package p\n", want: false},
		{name: "a malformed constraint", file: "a.go", source: "//go:build (\n\npackage p\n", wantErr: errMalformedConstraint},
		{name: "a file for this OS", file: "a_" + runtime.GOOS + ".go", source: "package p\n", want: true},
		{name: "a file for another OS", file: "a_" + otherOS + ".go", source: "package p\n", want: false},
		{name: "a header that does not parse", file: "a.go", source: "package p\n\nimport (\n", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matched, err := DefaultFileFilter(tt.tags)(tt.file, []byte(tt.source))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, matched)
		})
	}
}

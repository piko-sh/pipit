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

//go:build crosslang

package harness

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOutputDirectory(t *testing.T) {
	t.Parallel()
	benchmarks := filepath.Join("suite", "cross_language", "benchmarks")
	scratch := t.TempDir()
	tests := []struct {
		name       string
		configured string
		want       string
	}{
		{name: "default sits beside the benchmarks", configured: "results", want: filepath.Join("suite", "cross_language", "results")},
		{name: "relative subdirectory sits beside the benchmarks", configured: filepath.Join("out", "ab"), want: filepath.Join("suite", "cross_language", "out", "ab")},
		{name: "absolute path is used as given", configured: filepath.Join(scratch, "ab"), want: filepath.Join(scratch, "ab")},
		{name: "absolute path is cleaned, not nested under the suite", configured: scratch + string(filepath.Separator), want: scratch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveOutputDirectory(benchmarks, tt.configured)
			require.Equal(t, tt.want, got)
			if filepath.IsAbs(tt.configured) {
				require.False(t, filepath.IsLocal(got), "an absolute output directory must not land inside the suite")
			}
		})
	}
}

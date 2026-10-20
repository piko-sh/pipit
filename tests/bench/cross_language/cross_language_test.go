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

package crosslang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/tests/bench/cross_language/harness"
)

func TestCrossLanguage(t *testing.T) {
	if os.Getenv("RUN_CROSS_LANG_BENCH") != "1" {
		t.Skip("set RUN_CROSS_LANG_BENCH=1 to run cross-language suite")
	}
	benchmarksDirectory := mustResolveBenchmarksDir(t)
	suite := harness.Load(t, benchmarksDirectory)
	suite.Run(t)
}

func mustResolveBenchmarksDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err, "getwd")
	return filepath.Join(cwd, "benchmarks")
}

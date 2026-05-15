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

package goversion_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/goversion"
)

func testedGoMinorPrefixes(t *testing.T) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "go.mod"))
	require.NoError(t, err)
	for line := range strings.Lines(string(contents)) {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "go" {
			continue
		}
		parts := strings.Split(fields[1], ".")
		require.GreaterOrEqual(t, len(parts), 2, "go directive %q lacks a minor", fields[1])
		return []string{"go" + parts[0] + "." + parts[1]}
	}
	require.FailNow(t, "go.mod has no go directive")
	return nil
}

func TestSupportedPrefixesAreExactlyTested(t *testing.T) {
	require.Equal(t, testedGoMinorPrefixes(t), goversion.SupportedPrefixes[:])
}

func TestPrefixSupported(t *testing.T) {
	cases := []struct {
		name          string
		version       string
		allowUntested string
		want          bool
	}{
		{name: "tested minor", version: "go1.27.1", allowUntested: "", want: true},
		{name: "untested minor", version: "go1.28rc1", allowUntested: "", want: false},
		{name: "untested minor with escape hatch", version: "go1.28rc1", allowUntested: "1", want: true},
		{name: "escape hatch requires the exact value", version: "go1.28rc1", allowUntested: "true", want: false},
		{name: "devel build", version: "devel go1.29-abcdef", allowUntested: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, goversion.PrefixSupported(tc.version, tc.allowUntested))
		})
	}
}

func TestCheckEscapeHatchSkipsOnlyPrefixAllowlist(t *testing.T) {
	passingProbe := func() error { return nil }
	probeFailure := errors.New("probe failed")
	failingProbe := func() error { return probeFailure }

	require.NoError(t, goversion.Check("go1.27.1", "", passingProbe))

	var versionErr *goversion.VersionError
	err := goversion.Check("go1.28.0", "", passingProbe)
	require.ErrorAs(t, err, &versionErr)
	require.Contains(t, err.Error(), goversion.AllowUntestedEnvVar)

	require.NoError(t, goversion.Check("go1.28.0", "1", passingProbe))

	var layoutErr *goversion.LayoutError
	err = goversion.Check("go1.28.0", "1", failingProbe)
	require.ErrorAs(t, err, &layoutErr)
	require.ErrorIs(t, err, probeFailure)
	require.Contains(t, err.Error(), "TestUnsafeRuntimeLayoutSelfCheck")

	err = goversion.Check("go1.27.1", "", failingProbe)
	require.ErrorAs(t, err, &layoutErr)
}

func TestInterpretedGoVersionTracksSupportedRuntime(t *testing.T) {
	require.Len(t, goversion.SupportedPrefixes, 1,
		"InterpretedGoVersion names a single version, so the allowlist must too")
	require.Equal(t, goversion.SupportedPrefixes[0], goversion.InterpretedGoVersion)
}

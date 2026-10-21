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

//go:build !pipit_opcode_census

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRefusesToMeasureWithoutTheCensusBeforeItCompilesAnything(t *testing.T) {
	t.Parallel()

	err := run(t.Context(), []string{"-root", t.TempDir()}, &bytes.Buffer{})

	require.ErrorIs(t, err, errCensusNotBuilt, "the missing build tag is reported before a benchmark is looked for")
}

func TestCaptureCensusReportsABuildWithoutTheCensus(t *testing.T) {
	t.Parallel()

	resetCensus()
	_, err := captureCensus(true)

	require.ErrorIs(t, err, errCensusNotBuilt)
}

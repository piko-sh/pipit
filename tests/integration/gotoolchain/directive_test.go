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

//go:build integration

package gotoolchain_test

import (
	"bufio"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRunDirective(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		source   string
		wantOK   bool
		wantArgs []string
		wantSkip string
	}{
		{name: "bare run", source: "// run\n\npackage main\n", wantOK: true, wantArgs: nil, wantSkip: ""},
		{name: "run without space", source: "//run\n", wantOK: true, wantArgs: nil, wantSkip: ""},
		{name: "compiler flag ignored", source: "// run -gcflags=-l\n", wantOK: true, wantArgs: nil, wantSkip: ""},
		{name: "program arguments", source: "// run a b\n", wantOK: true, wantArgs: []string{"a", "b"}, wantSkip: ""},
		{name: "flags then arguments", source: "// run -gcflags=-d=ssa/check/on x\n", wantOK: true, wantArgs: []string{"x"}, wantSkip: ""},
		{name: "race flag skips", source: "// run -race\n", wantOK: true, wantArgs: nil, wantSkip: "toolchain flag -race"},
		{name: "experiment flag skips", source: "// run -goexperiment=fieldtrack\n", wantOK: true, wantArgs: nil, wantSkip: "toolchain flag -goexperiment=fieldtrack"},
		{name: "build constraint before action", source: "//go:build linux\n\n// run\n", wantOK: true, wantArgs: nil, wantSkip: ""},
		{name: "legacy build constraint before action", source: "// +build linux\n// run\n", wantOK: true, wantArgs: nil, wantSkip: ""},
		{name: "other action", source: "// errorcheck\n", wantOK: false, wantArgs: nil, wantSkip: ""},
		{name: "rundir action", source: "// rundir\n", wantOK: false, wantArgs: nil, wantSkip: ""},
		{name: "code before comment", source: "package main\n// run\n", wantOK: false, wantArgs: nil, wantSkip: ""},
		{name: "empty file", source: "", wantOK: false, wantArgs: nil, wantSkip: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directive, ok := parseRunDirective(bufio.NewScanner(strings.NewReader(tc.source)))
			require.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantArgs, directive.args)
			assert.Equal(t, tc.wantSkip, directive.skip)
		})
	}
}

func TestClassifyFailure(t *testing.T) {
	t.Parallel()
	generic := errors.New("exit status 1")
	cases := []struct {
		name       string
		output     string
		err        error
		wantBucket bucket
		wantDetail string
	}{
		{name: "runtime panic", output: "panic: boom\n", err: generic, wantBucket: bucketRuntime, wantDetail: "panic: boom"},
		{name: "compiler panic", output: "panic: x\n\ngoroutine 1 [running]:\npipit.sh/pipit/internal/compile.(*Compiler).f()\n", err: generic, wantBucket: bucketPanic, wantDetail: "panic: x at pipit.sh/pipit/internal/compile.(*Compiler).f()"},
		{name: "interpreter invariant by text", output: "pipit: evaluating file: panic: interp: handleAddr - general[0] is zero reflect.Value\n", err: generic, wantBucket: bucketInternal, wantDetail: "pipit: evaluating file: panic: interp: handleAddr - general[0] is zero reflect.Value"},
		{name: "undefined method is internal", output: "pipit: evaluating file: undefined method: Base.Sound\n", err: generic, wantBucket: bucketInternal, wantDetail: "pipit: evaluating file: undefined method: Base.Sound"},
		{name: "cancelled text", output: "pipit: execution cancelled\n", err: generic, wantBucket: bucketTimeout, wantDetail: "pipit: execution cancelled"},
		{name: "deadline text", output: "pipit: evaluating file: context deadline exceeded\n", err: generic, wantBucket: bucketTimeout, wantDetail: "pipit: evaluating file: context deadline exceeded"},
		{name: "timeout exit code", output: "", err: exitError(t, exitTimeout), wantBucket: bucketTimeout, wantDetail: "exit status 124"},
		{name: "internal exit code", output: "", err: exitError(t, exitInternal), wantBucket: bucketInternal, wantDetail: "exit status 3"},
		{name: "unsupported import", output: "pipit: package not registered with interpreter: unsafe\n", err: generic, wantBucket: bucketUnsupported, wantDetail: "pipit: package not registered with interpreter: unsafe"},
		{name: "compilation failure", output: "pipit: compilation failed: copy requires exactly 2 arguments\n", err: generic, wantBucket: bucketCompile, wantDetail: "pipit: compilation failed: copy requires exactly 2 arguments"},
		{name: "program output precedes failure", output: "Ones: 1 1 1\npipit: evaluating file: panic: bad\n", err: generic, wantBucket: bucketRuntime, wantDetail: "pipit: evaluating file: panic: bad"},
		{name: "diagnostic line stripped", output: "2026/09/17 06:00:00 WARN Type cycle detected\npipit: evaluating file: panic: bad\n", err: generic, wantBucket: bucketRuntime, wantDetail: "pipit: evaluating file: panic: bad"},
		{name: "no output falls back to error", output: "", err: generic, wantBucket: bucketRuntime, wantDetail: "exit status 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := classifyFailure("f.go", tc.output, tc.err)
			assert.Equal(t, tc.wantBucket, result.bucket)
			assert.Equal(t, tc.wantDetail, result.detail)
		})
	}
}

func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.CommandContext(t.Context(), "sh", "-c", "exit "+strconv.Itoa(code)).Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, code, exitErr.ExitCode())
	return err
}

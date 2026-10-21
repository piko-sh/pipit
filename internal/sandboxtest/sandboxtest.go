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

// Package sandboxtest provides child-process and environment helpers for sandbox tests.
// It uses only the standard library to keep sandboxlinux tests free of cgo.
package sandboxtest

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Command re-executes the test binary to run one named test with env added to the
// inherited environment. It omits -test.v so standard output contains only the test's own
// writes.
//
// Takes test (string) which is the exact name of the test the child runs.
// Takes env (...string) which holds the extra KEY=value entries, usually the variable
// that tells the test it is the child.
//
// Returns *exec.Cmd which the caller wires up and starts.
func Command(ctx context.Context, test string, env ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+test+"$") //nolint:gosec // re-executes the test binary
	command.Env = append(os.Environ(), env...)
	return command
}

// IsChild reports whether this process is a child that a test started with the given
// environment variable set.
//
// Takes variable (string) which names the environment variable the parent sets.
//
// Returns bool which is true when the variable is set and not empty.
func IsChild(variable string) bool {
	return os.Getenv(variable) != ""
}

// WaitForLine fails the test unless the next line from the child's standard output
// matches want.
//
// Takes tb (testing.TB) which reports a missing or unexpected readiness line.
// Takes reader (*bufio.Reader) which reads the child's standard output.
// Takes want (string) which is the expected line without its newline.
// Takes what (string) which says what the line means, for the failure message.
func WaitForLine(tb testing.TB, reader *bufio.Reader, want, what string) {
	tb.Helper()
	line, err := reader.ReadString('\n')
	if err != nil || strings.TrimSuffix(line, "\n") != want {
		tb.Fatalf("%s: read %q (%v), want %q", what, line, err, want)
	}
}

// CgroupParent returns the delegated cgroup-v2 parent named by PIPIT_TEST_CGROUP_PARENT
// and skips the test when it is not set.
//
// Takes tb (testing.TB) which skips tests without a delegated cgroup.
//
// Returns string which is the cgroup parent path.
func CgroupParent(tb testing.TB) string {
	tb.Helper()
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		tb.Skip("requires an explicitly delegated cgroup-v2 parent")
	}
	return parent
}

// RequiredBinary returns a static fixture path from an environment variable, failing the
// test if it is unset. Call CgroupParent first to skip hosts without a delegated cgroup.
//
// Takes tb (testing.TB) which reports a missing fixture path.
// Takes variable (string) which names the environment variable that holds the path.
// Takes what (string) which names the fixture, for the failure message.
//
// Returns string which is the fixture path.
func RequiredBinary(tb testing.TB, variable, what string) string {
	tb.Helper()
	path := os.Getenv(variable)
	if path == "" {
		tb.Fatalf("requires %s (%s)", what, variable)
	}
	return path
}

// SkipUnlessRequired skips the test because err shows the host lacks a feature, unless
// the environment variable demanding the feature is set to 1, in which case the test
// fails.
//
// Takes tb (testing.TB) which skips or fails the test when the feature is unavailable.
// Takes variable (string) which names the environment variable that makes the feature
// mandatory.
// Takes err (error) which says why the feature is unavailable.
func SkipUnlessRequired(tb testing.TB, variable string, err error) {
	tb.Helper()
	if os.Getenv(variable) == "1" {
		tb.Fatalf("%s=1 but the feature is unavailable: %v", variable, err)
	}
	tb.Skip(err)
}

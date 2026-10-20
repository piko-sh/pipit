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

//go:build linux

package modloader

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInvocationExecutableHash(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "executable")
	data := []byte("approved executable bytes")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	actual, err := hashInvocationExecutable(file)
	expected := fmt.Sprintf("linux-executable-sha256:%x", sha256.Sum256(data))
	require.NoError(t, err, "the executable identity")
	require.Equal(t, expected, actual)
}

func TestInvocationExecutableRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	for _, size := range []int64{0, maximumApprovalExecutableBytes + 1} {
		file, err := os.CreateTemp(t.TempDir(), "executable")
		require.NoError(t, err)
		require.NoError(t, file.Truncate(size))
		identity, err := hashInvocationExecutable(file)
		require.Error(t, err, "an invalid executable size %d is rejected", size)
		require.Empty(t, identity)
		require.NoError(t, file.Close())
		identity, err = hashInvocationExecutable(file)
		require.Error(t, err, "a closed executable is rejected")
		require.Empty(t, identity)
	}
	directory, err := os.Open(t.TempDir())
	require.NoError(t, err)
	defer directory.Close()
	identity, err := hashInvocationExecutable(directory)
	require.Error(t, err, "a directory is not an executable")
	require.Empty(t, identity)
}

func TestInvocationExecutableUsesRunningImage(t *testing.T) {
	t.Parallel()
	trailers := []string{"", "changed executable bytes"}
	identities := make([]string, 0, len(trailers))
	for _, trailer := range trailers {
		path := filepath.Join(t.TempDir(), "approval-test")
		identity := copyApprovalTestExecutable(t, path, trailer)
		identities = append(identities, identity)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		command := exec.CommandContext(ctx, path, "-test.run=^TestInvocationExecutableChild$")
		command.Env = append(os.Environ(), "PIPIT_APPROVAL_EXECUTABLE_CHILD="+identity)
		output, err := command.CombinedOutput()
		cancel()
		require.NoError(t, err, "running-image measurement:\n%s", output)
	}
	require.NotEqual(t, identities[0], identities[1], "a changed executable changes its identity")
	require.NotEqual(t, invocationHash("source", nil, "main", nil, identities[:1]), invocationHash("source", nil, "main", nil, identities[1:]),
		"a changed executable loses its approval identity")
}

func TestInvocationExecutableChild(t *testing.T) {
	expected := os.Getenv("PIPIT_APPROVAL_EXECUTABLE_CHILD")
	if expected == "" {
		t.Skip("subprocess helper")
	}
	path, err := os.Executable()
	require.NoError(t, err)
	require.NoError(t, os.Rename(path, path+".running"))
	require.NoError(t, os.WriteFile(path, []byte("replacement executable"), 0o600))
	actual, err := invocationExecutable()
	require.NoError(t, err, "measurement does not follow a replaced pathname")
	require.Equal(t, expected, actual)
	again, err := invocationExecutable()
	require.NoError(t, err, "the cached measurement is stable")
	require.Equal(t, actual, again)
}

func copyApprovalTestExecutable(t *testing.T, path, trailer string) string {
	t.Helper()
	source, err := os.Open("/proc/self/exe")
	require.NoError(t, err)
	defer source.Close()
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o700)
	require.NoError(t, err)
	defer target.Close()
	_, err = io.Copy(target, source)
	require.NoError(t, err)
	_, err = io.WriteString(target, trailer)
	require.NoError(t, err)
	_, err = target.Seek(0, io.SeekStart)
	require.NoError(t, err)
	identity, err := hashInvocationExecutable(target)
	require.NoError(t, err)
	require.NoError(t, target.Close())
	return identity
}

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

package rootfs_test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/rootfs"
)

func TestBoundedReadRejectsFIFOWithoutWriter(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	require.NoError(t, unix.Mkfifo(filepath.Join(directory, "pipe"), 0o600))
	require.NoError(t, os.Symlink("pipe", filepath.Join(directory, "alias")))
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestBoundedReadFIFOChild$")
	command.Env = append(os.Environ(), "PIPIT_ROOTFS_FIFO_TEST_ROOT="+directory)
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "FIFO read waited for a writer: %s", output)
	require.NoError(t, err, "%s", output)
}

func TestBoundedReadFIFOChild(t *testing.T) {
	directory := os.Getenv("PIPIT_ROOTFS_FIFO_TEST_ROOT")
	if directory == "" {
		t.Skip("subprocess fixture")
	}
	store, err := rootfs.Open(directory)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	for _, name := range []string{"pipe", "alias"} {
		data, err := rootfs.ReadFileBounded(store, name, 1024)
		require.ErrorIs(t, err, fs.ErrInvalid)
		require.Nil(t, data)
	}
}

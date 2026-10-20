//go:build unix

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
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestIdentityReadRejectsFIFOWithoutBlocking(t *testing.T) {
	if os.Getenv("PIPIT_IDENTITY_FIFO_FIXTURE") == "1" {
		scan, path, expected := identityFileFixture(t)
		require.NoError(t, os.Remove(path))
		require.NoError(t, unix.Mkfifo(path, 0600))
		require.Error(t, scan.addFile(path, expected), "a FIFO replacement is not regular source")
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestIdentityReadRejectsFIFOWithoutBlocking", "PIPIT_IDENTITY_FIFO_FIXTURE=1")
	output, err := command.CombinedOutput()
	require.NoError(t, ctx.Err(), "a FIFO source is rejected without blocking: %s", output)
	require.NoError(t, err)
}

func TestIdentityReadRefusesSymlinkEvenToOriginal(t *testing.T) {
	t.Parallel()
	scan, path, expected := identityFileFixture(t)
	require.NoError(t, os.Rename(path, path+".original"))
	require.NoError(t, os.Symlink(path+".original", path))
	require.Error(t, scan.addFile(path, expected), "source hashing does not follow a final symlink")
}

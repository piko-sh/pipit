//go:build linux && (amd64 || arm64)

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

package sandboxbroker

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestRecoveryClaimProcessDeath(t *testing.T) {
	if journal := os.Getenv("PIPIT_TEST_CLAIM_JOURNAL"); journal != "" {
		claim, err := OpenLinuxRecoveryClaim(journal, os.Getenv("PIPIT_TEST_CLAIM_CHECKPOINT"),
			os.Getenv("PIPIT_TEST_CLAIM_APPROVAL"), [sha256.Size]byte{1})
		require.NoError(t, err)
		defer claim.Close()
		require.NoError(t, claim.RecordServiceReleaseReady())
		fmt.Println("ready")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
		return
	}
	directories, binding := recoveryClaimFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestRecoveryClaimProcessDeath", "PIPIT_TEST_CLAIM_JOURNAL="+directories[0],
		"PIPIT_TEST_CLAIM_CHECKPOINT="+directories[1], "PIPIT_TEST_CLAIM_APPROVAL="+directories[2])
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	defer output.Close()
	require.NoError(t, command.Start())
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	sandboxtest.WaitForLine(t, bufio.NewReader(output), "ready", "the claim owner becomes ready")
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if claim != nil {
		_ = claim.Close()
	}
	require.Error(t, err, "another process bypassed the live claim")
	require.Nil(t, claim)
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait(), "killed claimant exited successfully")
	claim, err = OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	require.NoError(t, err, "a terminated claimant keeps no leases")
	released, err := claim.ServiceReleaseReady()
	require.NoError(t, err, "claimant death keeps durable release readiness")
	require.True(t, released)
	require.NoError(t, claim.Close())
}

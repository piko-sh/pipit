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
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryCheckpointApproval(t *testing.T) {
	t.Parallel()
	owner := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	encoded, err := owner.checkpoint(journal, binding)
	require.NoError(t, err)
	approved := sha256.Sum256(encoded)
	require.NoError(t, ValidateRecoveryCheckpoint(encoded, approved, binding))
	again, err := owner.checkpoint(journal, binding)
	require.NoError(t, err, "checkpoint was not deterministic")
	require.True(t, bytes.Equal(encoded, again))
	require.Error(t, ValidateRecoveryCheckpoint(encoded, [sha256.Size]byte{}, binding), "missing independent approval accepted")
	require.Error(t, ValidateRecoveryCheckpoint(encoded, approved, [sha256.Size]byte{2}), "different host binding accepted")
	changed := bytes.Clone(encoded)
	changed[len(changed)-1] ^= 1
	require.ErrorIs(t, ValidateRecoveryCheckpoint(changed, approved, binding), ErrDenied, "altered checkpoint accepted")
	_, err = owner.checkpoint(journal, [sha256.Size]byte{})
	require.Error(t, err, "empty host binding accepted")
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	_, err = owner.checkpoint(journal, binding)
	require.Error(t, err, "checkpoint captured after admission")
	require.NoError(t, call.Finish(0))
	_, err = owner.checkpoint(journal, binding)
	require.Error(t, err, "finished operation reset checkpoint eligibility")
	require.NoError(t, owner.Close())
	_, err = owner.checkpoint(journal, binding)
	require.ErrorIs(t, err, ErrClosed, "closed authority checkpoint")
	var absent *LinuxRecoveryAuthority
	_, err = absent.checkpoint(journal, binding)
	require.ErrorIs(t, err, ErrClosed, "nil authority checkpoint")
}

func TestRecoveryCheckpointRejectsMalformedApprovedPolicy(t *testing.T) {
	t.Parallel()
	owner := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	encoded, err := owner.checkpoint(journal, binding)
	require.NoError(t, err)
	for _, mode := range []string{"profile", "old-profile", "namespace", "missing-roots", "zero-inode", "zero-mount", "journal-inode", "journal-mount", "duplicate-roots", "invalid-rights", "default-limits", "excessive-limits", "zero-binding"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var policy recoveryCheckpoint
			require.NoError(t, json.Unmarshal(encoded, &policy))
			switch mode {
			case "profile":
				policy.Profile = "other"
			case "old-profile":
				policy.Profile = "filesystem-recovery-checkpoint-v1"
			case "journal-inode":
				policy.Journal.Inode = 0
			case "journal-mount":
				policy.Journal.MountID = 0
			case "namespace":
				policy.Namespace = "invalid"
			case "missing-roots":
				policy.Roots = nil
			case "zero-inode":
				policy.Roots[0].Inode = 0
			case "zero-mount":
				policy.Roots[0].MountID = 0
			case "duplicate-roots":
				policy.Roots = append(policy.Roots, policy.Roots[0])
			case "invalid-rights":
				policy.Roots[0].Rights = 0
			case "default-limits":
				policy.Limits = FilesystemLimits{}
			case "excessive-limits":
				policy.Limits.Calls = defaultCalls + 1
			case "zero-binding":
				policy.Binding = [sha256.Size]byte{}
			}
			malformed, err := json.Marshal(policy)
			require.NoError(t, err)
			require.Error(t, ValidateRecoveryCheckpoint(malformed, sha256.Sum256(malformed), binding), "malformed checkpoint accepted")
		})
	}
	for _, malformed := range [][]byte{
		nil,
		append(bytes.Clone(encoded), '\n'),
		[]byte("{}"),
		[]byte(strings.Replace(string(encoded), `"binding":[1,`, `"binding":[null,`, 1)),
		[]byte(strings.Replace(string(encoded), `"profile":`, `"profile":"duplicate","profile":`, 1)),
		[]byte(strings.Repeat("x", maximumBootstrapBytes+1)),
	} {
		require.Error(t, ValidateRecoveryCheckpoint(malformed, sha256.Sum256(malformed), binding), "ambiguous checkpoint accepted")
	}
}

func TestRecoveryCheckpointSortsRootIdentities(t *testing.T) {
	t.Parallel()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{
		{Name: "zebra", Path: t.TempDir(), Rights: Read},
		{Name: "alpha", Path: t.TempDir(), Rights: Read | Write},
	}, FilesystemLimits{})
	require.NoError(t, err)
	defer bootstrap.Close()
	owner, err := bootstrap.RecoveryAuthority()
	require.NoError(t, err)
	defer owner.Close()
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	encoded, err := owner.checkpoint(journal, binding)
	require.NoError(t, err)
	require.NoError(t, ValidateRecoveryCheckpoint(encoded, sha256.Sum256(encoded), binding))
	var policy recoveryCheckpoint
	require.NoError(t, json.Unmarshal(encoded, &policy))
	require.Len(t, policy.Roots, 2, "roots not sorted")
	require.Equal(t, "alpha", policy.Roots[0].Name)
	require.Equal(t, "zebra", policy.Roots[1].Name)
}

func FuzzRecoveryCheckpointValidation(fuzzer *testing.F) {
	budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Write}}, FilesystemLimits{})
	require.NoError(fuzzer, err)
	seed, err := json.Marshal(recoveryCheckpoint{
		Profile: recoveryCheckpointProfile, Namespace: strings.Repeat("a", 32), Binding: [sha256.Size]byte{1},
		Roots: []filesystemBootstrapRoot{{Name: "data", Device: 1, Inode: 1, MountID: 1, Rights: Write}}, Limits: budget.limits,
		Journal: recoveryCheckpointJournal{Device: 1, Inode: 1, MountID: 1},
	})
	require.NoError(fuzzer, err)
	fuzzer.Add(seed)
	fuzzer.Add([]byte("{}"))
	fuzzer.Add([]byte("null"))
	fuzzer.Add([]byte(`{"profile":"filesystem-recovery-checkpoint-v1"}`))
	fuzzer.Fuzz(func(t *testing.T, encoded []byte) {
		binding := [sha256.Size]byte{1}
		if err := ValidateRecoveryCheckpoint(encoded, sha256.Sum256(encoded), binding); err == nil {
			require.NotEmpty(t, encoded, "unbounded checkpoint accepted")
			require.LessOrEqual(t, len(encoded), maximumBootstrapBytes)
		}
	})
}

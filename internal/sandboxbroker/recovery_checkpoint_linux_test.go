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
	"errors"
	"strings"
	"testing"
)

func TestRecoveryCheckpointApproval(t *testing.T) {
	owner := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	encoded, err := owner.checkpoint(journal, binding)
	if err != nil {
		t.Fatal(err)
	}
	approved := sha256.Sum256(encoded)
	if err := ValidateRecoveryCheckpoint(encoded, approved, binding); err != nil {
		t.Fatal(err)
	}
	again, err := owner.checkpoint(journal, binding)
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatal("checkpoint was not deterministic:", err)
	}
	if err := ValidateRecoveryCheckpoint(encoded, [sha256.Size]byte{}, binding); err == nil {
		t.Fatal("missing independent approval accepted")
	}
	if err := ValidateRecoveryCheckpoint(encoded, approved, [sha256.Size]byte{2}); err == nil {
		t.Fatal("different host binding accepted")
	}
	changed := bytes.Clone(encoded)
	changed[len(changed)-1] ^= 1
	if err := ValidateRecoveryCheckpoint(changed, approved, binding); !errors.Is(err, ErrDenied) {
		t.Fatal("altered checkpoint accepted:", err)
	}
	if _, err := owner.checkpoint(journal, [sha256.Size]byte{}); err == nil {
		t.Fatal("empty host binding accepted")
	}
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.checkpoint(journal, binding); err == nil {
		t.Fatal("checkpoint captured after admission")
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.checkpoint(journal, binding); err == nil {
		t.Fatal("finished operation reset checkpoint eligibility")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.checkpoint(journal, binding); !errors.Is(err, ErrClosed) {
		t.Fatal("closed authority checkpoint:", err)
	}
	var absent *LinuxRecoveryAuthority
	if _, err := absent.checkpoint(journal, binding); !errors.Is(err, ErrClosed) {
		t.Fatal("nil authority checkpoint:", err)
	}
}

func TestRecoveryCheckpointRejectsMalformedApprovedPolicy(t *testing.T) {
	owner := testRecoveryAuthority(t, Read|Write)
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	encoded, err := owner.checkpoint(journal, binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"profile", "old-profile", "namespace", "missing-roots", "zero-inode", "zero-mount", "journal-inode", "journal-mount", "duplicate-roots", "invalid-rights", "default-limits", "excessive-limits", "zero-binding"} {
		t.Run(mode, func(t *testing.T) {
			var policy recoveryCheckpoint
			if err := json.Unmarshal(encoded, &policy); err != nil {
				t.Fatal(err)
			}
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
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRecoveryCheckpoint(malformed, sha256.Sum256(malformed), binding); err == nil {
				t.Fatal("malformed checkpoint accepted")
			}
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
		if err := ValidateRecoveryCheckpoint(malformed, sha256.Sum256(malformed), binding); err == nil {
			t.Fatal("ambiguous checkpoint accepted")
		}
	}
}

func TestRecoveryCheckpointSortsRootIdentities(t *testing.T) {
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{
		{Name: "zebra", Path: t.TempDir(), Rights: Read},
		{Name: "alpha", Path: t.TempDir(), Rights: Read | Write},
	}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	owner, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	journal := testRecoveryAuthorityJournal(t, owner)
	binding := [sha256.Size]byte{1}
	encoded, err := owner.checkpoint(journal, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecoveryCheckpoint(encoded, sha256.Sum256(encoded), binding); err != nil {
		t.Fatal(err)
	}
	var policy recoveryCheckpoint
	if err := json.Unmarshal(encoded, &policy); err != nil {
		t.Fatal(err)
	}
	if len(policy.Roots) != 2 || policy.Roots[0].Name != "alpha" || policy.Roots[1].Name != "zebra" {
		t.Fatal("roots not sorted")
	}
}

func FuzzRecoveryCheckpointValidation(fuzzer *testing.F) {
	budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Write}}, FilesystemLimits{})
	if err != nil {
		fuzzer.Fatal(err)
	}
	seed, err := json.Marshal(recoveryCheckpoint{
		Profile: recoveryCheckpointProfile, Namespace: strings.Repeat("a", 32), Binding: [sha256.Size]byte{1},
		Roots: []filesystemBootstrapRoot{{Name: "data", Device: 1, Inode: 1, MountID: 1, Rights: Write}}, Limits: budget.limits,
		Journal: recoveryCheckpointJournal{Device: 1, Inode: 1, MountID: 1},
	})
	if err != nil {
		fuzzer.Fatal(err)
	}
	fuzzer.Add(seed)
	fuzzer.Add([]byte("{}"))
	fuzzer.Add([]byte("null"))
	fuzzer.Add([]byte(`{"profile":"filesystem-recovery-checkpoint-v1"}`))
	fuzzer.Fuzz(func(t *testing.T, encoded []byte) {
		binding := [sha256.Size]byte{1}
		if err := ValidateRecoveryCheckpoint(encoded, sha256.Sum256(encoded), binding); err == nil {
			if len(encoded) == 0 || len(encoded) > maximumBootstrapBytes {
				t.Fatal("unbounded checkpoint accepted")
			}
		}
	})
}

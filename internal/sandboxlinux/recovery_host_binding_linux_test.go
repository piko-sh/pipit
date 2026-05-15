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

//go:build linux && (amd64 || arm64)

package sandboxlinux

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoveryHostBindingSurvivesHostRestart(t *testing.T) {
	policy := [sha256.Size]byte{1}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	binding, err := RecoveryHostBinding(ctx, policy)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PIPIT_TEST_RECOVERY_BINDING_CHILD") == "1" {
		if _, err := os.Stdout.WriteString(hex.EncodeToString(binding[:]) + "\n"); err != nil {
			t.Fatal(err)
		}
		return
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryHostBindingSurvivesHostRestart$")
	command.Env = append(os.Environ(), "PIPIT_TEST_RECOVERY_BINDING_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("replacement host failed: %v %s", err, output)
	}
	line, _, _ := strings.Cut(string(output), "\n")
	if line != hex.EncodeToString(binding[:]) {
		t.Fatalf("replacement process changed stable host identity: %q", output)
	}
}

func TestRecoveryHostBindingCurrentIdentity(t *testing.T) {
	policy := [sha256.Size]byte{1}
	first, err := RecoveryHostBinding(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RecoveryHostBinding(context.Background(), policy)
	if err != nil || first != second || first == ([sha256.Size]byte{}) {
		t.Fatal("unstable host binding:", err)
	}
	changed, err := RecoveryHostBinding(context.Background(), [sha256.Size]byte{2})
	if err != nil || changed == first {
		t.Fatal("host binding omitted approved policy:", err)
	}
}

func TestRecoveryHostBindingRejectsMissingApprovalAndCancellation(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		digest, err := RecoveryHostBinding(ctx, [sha256.Size]byte{})
		if err == nil || digest != ([sha256.Size]byte{}) {
			t.Fatal("missing policy approved")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if digest, err := RecoveryHostBinding(ctx, [sha256.Size]byte{1}); !errors.Is(err, context.Canceled) || digest != ([sha256.Size]byte{}) {
		t.Fatal("cancelled identity capture returned approval:", err)
	}
}

func TestRecoveryBootIdentityValidation(t *testing.T) {
	valid := []byte("12345678-1234-1234-1234-123456789abc\n")
	if !validRecoveryBootIdentity(valid) {
		t.Fatal("canonical boot UUID rejected")
	}
	for _, invalid := range [][]byte{
		nil, valid[:len(valid)-1], append(bytes.Clone(valid), '\n'),
		[]byte(strings.ToUpper(string(valid))),
		[]byte("1234567811234-1234-1234-123456789abc\n"),
	} {
		if validRecoveryBootIdentity(invalid) {
			t.Fatal("malformed boot UUID accepted:", string(invalid))
		}
	}
}

func TestRecoveryHostSecurityCanonicalisation(t *testing.T) {
	data := []byte("Uid:\t1 2 3 4\nGid:\t5 6 7 8\nGroups:\t8 2 8\n" +
		"CapInh:\t0000000000000000\nCapPrm:\t0000000000000001\nCapEff:\t0000000000000001\n" +
		"CapBnd:\t000000000000000f\nCapAmb:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\nSeccomp_filters:\t1\n")
	fields, err := decodeRecoveryHostSecurity(data)
	if err != nil {
		t.Fatal(err)
	}
	if fields["Uid"] != "1 2 3 4" || fields["Gid"] != "5 6 7 8" || fields["Groups"] != "2 8" || fields["CapBnd"] != "f" {
		t.Fatal("lost saved IDs, filesystem IDs, groups or capabilities:", fields)
	}
	for _, invalid := range [][]byte{
		nil, []byte("Uid: 1 2 3 4\n"),
		append(bytes.Clone(data), []byte("Uid: 1 2 3 4\n")...),
		bytes.Replace(data, []byte("Uid:\t1 2 3 4"), []byte("Uid:\t1 2"), 1),
		bytes.Replace(data, []byte("NoNewPrivs:\t1"), []byte("NoNewPrivs:\t2"), 1),
		bytes.Replace(data, []byte("Seccomp:\t2"), []byte("Seccomp:\t3"), 1),
		bytes.Replace(data, []byte("CapBnd:\t000000000000000f"), []byte("CapBnd:\tnot-hex"), 1),
		bytes.Repeat([]byte("x"), maximumRecoveryStatusBytes+1),
	} {
		if partial, err := decodeRecoveryHostSecurity(invalid); err == nil || partial != nil {
			t.Fatal("malformed security status accepted")
		}
	}
}

func TestRecoveryHostIdentityRejectsOrdinaryFilesystem(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"sys/kernel/random", "thread-self/ns"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "sys/kernel/random/boot_id"), []byte("12345678-1234-1234-1234-123456789abc\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "thread-self/ns/user"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "thread-self/status"), []byte("Uid: 1 2 3 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fake, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	if _, err := recoveryBootIdentity(fake); err == nil {
		t.Fatal("ordinary boot metadata accepted")
	}
	if _, err := recoveryNamespace(fake, "user"); err == nil {
		t.Fatal("ordinary namespace inode accepted")
	}
	if _, err := recoveryHostSecurity(fake); err == nil {
		t.Fatal("ordinary principal metadata accepted")
	}
}

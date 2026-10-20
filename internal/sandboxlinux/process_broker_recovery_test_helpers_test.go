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

package sandboxlinux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func runNativeRecoveryBatch(t *testing.T, config WorkerConfig, original *WorkerProcess, authority *sandboxbroker.LinuxRecoveryAuthority,
	journal *sandboxbroker.LinuxRecoveryJournal, root, mode string, checkpoint nativeRecoveryCheckpoint,
) {
	t.Helper()
	records, err := journal.Records()
	if err != nil || len(records) != 1 {
		t.Fatal("missing original recovery intent:", records, err)
	}
	name, err := sandboxbroker.StagingName(records[0].Namespace, records[0].Operation)
	if err != nil {
		t.Fatal(err)
	}
	staging, target := filepath.Join(root, name), filepath.Join(root, "file")
	if mode != "journalled-recovery-collision" {
		if err := os.Rename(target, staging); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(staging, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	var bootstrap *sandboxbroker.LinuxBootstrap
	if mode == "journalled-recovery-checkpoint" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := original.reapForRecovery(ctx); err != nil {
			t.Fatal(err)
		}
		if err := authority.Close(); err != nil {
			t.Fatal(err)
		}
		binding, bindingErr := RecoveryHostBinding(ctx, config.Digest)
		if bindingErr != nil {
			t.Fatal(bindingErr)
		}
		bootstrap, err = sandboxbroker.PrepareCheckpointRecovery(checkpoint.encoded, checkpoint.approved, binding, journal,
			[]sandboxbroker.LinuxRootGrant{{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List}})
	} else {
		bootstrap, err = authority.RecoveryBootstrap(journal)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if process, err := LaunchFilesystemBroker(ctx, config, bootstrap); process != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("unguarded launcher accepted recovery role:", process, err)
	}
	var unrelated WorkerProcess
	unrelated.brokerNamespace = "unrelated"
	if process, err := LaunchFilesystemRecovery(ctx, config, &unrelated, bootstrap); process != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("recovery accepted unrelated original owner:", process, err)
	}
	var unrelatedParent Group
	if process, err := launchFilesystemRecoveryInGroup(ctx, config, original, bootstrap, &unrelatedParent); process != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("recovery moved to an unrelated aggregate:", process, err)
	}
	if mode == "journalled-recovery-approval" {
		unapproved := config
		unapproved.Digest[0] ^= 1
		failed, err := LaunchFilesystemRecovery(ctx, unapproved, original, bootstrap)
		if err == nil || failed == nil {
			t.Fatal("failed recovery launch lost cleanup ownership:", failed, err)
		}
		_ = failed.Close()
		if failed.releaseAdmission != nil {
			t.Fatal("failed recovery image approval retained admission")
		}
	}
	if mode == "journalled-recovery-cleanup" {
		if process, err := LaunchFilesystemRecovery(ctx, config, original, bootstrap); err == nil || process != nil {
			t.Fatal("recovery started before original cleanup:", process, err)
		}
		if original.releaseAdmission == nil {
			t.Fatal("failed cleanup discarded original admission ownership")
		}
		if err := unix.Unlinkat(int(original.group.directory.Fd()), "recovery-blocker", unix.AT_REMOVEDIR); err != nil {
			t.Fatal(err)
		}
	}
	var process *WorkerProcess
	if original.brokerParent == nil {
		process, err = LaunchFilesystemRecovery(ctx, config, original, bootstrap)
	} else {
		if escaped, err := LaunchFilesystemRecovery(ctx, config, original, bootstrap); escaped != nil || !errors.Is(err, ErrInvalidLimits) {
			t.Fatal("aggregate recovery fell back to standalone:", escaped, err)
		}
		process, err = launchFilesystemRecoveryInGroup(ctx, config, original, bootstrap, original.brokerParent)
	}
	if process != nil {
		defer process.Close()
	}
	if err != nil {
		t.Fatal("recovery launch failed:", err)
	}
	if mode == "journalled-recovery-failed-broker" && original.Wait() == nil {
		t.Fatal("recovery suppressed original execution failure")
	}
	if original.brokerParent != nil {
		if process.container == nil || process.watchdog == nil {
			t.Fatal("recovery missing protected aggregate")
		}
		parent, err := process.container.parent.Stat()
		if err != nil {
			t.Fatal(err)
		}
		expected, err := original.brokerParent.directory.Stat()
		if err != nil || !os.SameFile(parent, expected) {
			t.Fatal("recovery escaped original aggregate:", err)
		}
		protected, err := process.container.directory.Stat()
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range []*Group{process.group, process.watchdog.group} {
			placement, err := child.parent.Stat()
			if err != nil || !os.SameFile(placement, protected) {
				t.Fatal("recovery or watchdog escaped protected aggregate:", err)
			}
		}
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	if err := process.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(process, process, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	hello := readBrokerFrame(t, codec, sandboxwire.Hello, 0)
	var configuration sandboxbroker.FilesystemConfiguration
	if err := hello.DecodePayload(&configuration); err != nil || configuration.Profile != sandboxbroker.FilesystemRecoveryProfile {
		t.Fatal("native recovery role mismatch:", configuration, err)
	}
	writeBrokerFrame(t, codec, sandboxwire.Configure, 0, `{"profile":"filesystem-broker-recovery-v1"}`)
	readBrokerFrame(t, codec, sandboxwire.Ready, 0)
	writeBrokerFrame(t, codec, sandboxwire.Run, 1, "{}")
	reply, err := codec.Read()
	if mode == "journalled-recovery-collision" {
		if err == nil || process.Wait() == nil {
			t.Fatal("native recovery accepted colliding inode")
		}
		data, err := os.ReadFile(staging)
		if err != nil || string(data) != "new" {
			t.Fatal("native recovery changed collision:", string(data), err)
		}
		return
	}
	if err != nil || reply.Kind != sandboxwire.Result || reply.ID != 1 || reply.DecodePayload(&struct{}{}) != nil {
		t.Fatal("native recovery failed:", reply, err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal("native recovery did not exit cleanly:", err)
	}
	if err := process.Close(); err != nil {
		t.Fatal("native recovery cleanup failed:", err)
	}
	if _, err := os.Lstat(staging); !os.IsNotExist(err) {
		t.Fatal("native recovery retained remnant:", err)
	}
}

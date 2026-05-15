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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRecoveryJournalAncestorPolicy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		uid     uint32
		mode    uint32
		final   bool
		allowed bool
	}{
		{name: "private-owner", uid: 1000, mode: unix.S_IFDIR | 0700, final: true, allowed: true},
		{name: "root-ancestor", uid: 0, mode: unix.S_IFDIR | 0755, allowed: true},
		{name: "owner-ancestor", uid: 1000, mode: unix.S_IFDIR | 0755, allowed: true},
		{name: "root-sticky-ancestor", uid: 0, mode: unix.S_IFDIR | unix.S_ISVTX | 0777, allowed: true},
		{name: "foreign-owner", uid: 1001, mode: unix.S_IFDIR | 0700},
		{name: "foreign-sticky", uid: 1001, mode: unix.S_IFDIR | unix.S_ISVTX | 0777},
		{name: "owner-sticky", uid: 1000, mode: unix.S_IFDIR | unix.S_ISVTX | 0777},
		{name: "writable-root", uid: 0, mode: unix.S_IFDIR | 0777},
		{name: "group-writable", uid: 1000, mode: unix.S_IFDIR | 0770},
		{name: "public-final", uid: 1000, mode: unix.S_IFDIR | 0755, final: true},
		{name: "sticky-final", uid: 0, mode: unix.S_IFDIR | unix.S_ISVTX | 0777, final: true},
		{name: "foreign-final", uid: 0, mode: unix.S_IFDIR | 0700, final: true},
		{name: "regular-file", uid: 1000, mode: unix.S_IFREG | 0700},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateRecoveryJournalAncestor(unix.Stat_t{Uid: test.uid, Mode: test.mode}, test.final, 1000)
			if test.allowed && err != nil || !test.allowed && !errors.Is(err, ErrDenied) {
				t.Fatalf("ancestor policy: allowed=%v error=%v", test.allowed, err)
			}
		})
	}
}

func TestRecoveryJournalRejectsUnsafeAncestors(t *testing.T) {
	for _, mode := range []string{"group-writable", "world-writable", "symlink", "dotdot", "excessive-depth", "relative"} {
		t.Run(mode, func(t *testing.T) {
			parent := t.TempDir()
			directory := filepath.Join(parent, "journal")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := directory
			switch mode {
			case "group-writable":
				if err := os.Chmod(parent, 0770); err != nil {
					t.Fatal(err)
				}
			case "world-writable":
				if err := os.Chmod(parent, 0777); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(parent, alias); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(alias, "journal")
			case "dotdot":
				path = parent + "/journal/../journal"
			case "excessive-depth":
				path = "/" + strings.Repeat("./", maximumJournalAncestorSteps+1) + strings.TrimPrefix(directory, "/")
			case "relative":
				path = "journal"
			}
			owner, err := OpenLinuxRecoveryJournal(path, recoveryRecordFixture().Namespace)
			if owner != nil {
				_ = owner.Close()
			}
			if err == nil || owner != nil {
				t.Fatal("unsafe recovery path accepted")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected path modified journal:", entries, err)
			}
		})
	}
}

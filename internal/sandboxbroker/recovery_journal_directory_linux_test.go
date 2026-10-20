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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRecoveryJournalAncestorPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateRecoveryJournalAncestor(unix.Stat_t{Uid: tt.uid, Mode: tt.mode}, tt.final, 1000)
			if tt.allowed {
				require.NoError(t, err, "the ancestor policy allows the directory")
			} else {
				require.ErrorIs(t, err, ErrDenied, "the ancestor policy denies the directory")
			}
		})
	}
}

func TestRecoveryJournalRejectsUnsafeAncestors(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"group-writable", "world-writable", "symlink", "dotdot", "excessive-depth", "relative"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			directory := filepath.Join(parent, "journal")
			require.NoError(t, os.Mkdir(directory, 0700))
			path := directory
			switch mode {
			case "group-writable":
				require.NoError(t, os.Chmod(parent, 0770))
			case "world-writable":
				require.NoError(t, os.Chmod(parent, 0777))
			case "symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				require.NoError(t, os.Symlink(parent, alias))
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
			require.Error(t, err, "unsafe recovery path accepted")
			require.Nil(t, owner)
			entries, err := os.ReadDir(directory)
			require.NoError(t, err, "rejected path modified journal")
			require.Empty(t, entries)
		})
	}
}

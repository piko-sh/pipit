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
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxtest"
)

func TestLinuxBrokerLandlock(t *testing.T) {
	if os.Getenv("PIPIT_TEST_BROKER_LANDLOCK") == "child" {
		exerciseBrokerLandlock(t)
		return
	}
	if os.Getenv("PIPIT_TEST_BROKER_LANDLOCK") == "recovery" {
		exerciseRecoveryLandlock(t)
		return
	}
	if testing.CoverMode() != "" {
		t.Skip("the re-executed child cannot write a coverage profile through its own Landlock confinement")
	}
	if _, err := brokerLandlockABI(); err != nil {
		sandboxtest.SkipUnlessRequired(t, "PIPIT_REQUIRE_BROKER_LANDLOCK", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, mode := range []string{"child", "recovery"} {
		command := sandboxtest.Command(ctx, "TestLinuxBrokerLandlock", "PIPIT_TEST_BROKER_LANDLOCK="+mode, "PIPIT_TEST_BROKER_ROOT="+t.TempDir())
		command.Args = append(command.Args, "-test.v")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "the Landlock %s child succeeds: %s", mode, output)
		t.Logf("%s", output)
	}
}

func exerciseBrokerLandlock(t *testing.T) {
	t.Helper()
	root := os.Getenv("PIPIT_TEST_BROKER_ROOT")
	grants := make([]LinuxRootGrant, 0, 3)
	for _, grant := range []struct {
		name   string
		rights Rights
	}{{name: "read", rights: Read}, {name: "write", rights: Write}, {name: "list", rights: List}} {
		directory := filepath.Join(root, grant.name)
		require.NoError(t, os.Mkdir(directory, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(directory, "file"), []byte("original"), 0600))
		grants = append(grants, LinuxRootGrant{Name: grant.name, Path: directory, Rights: grant.rights})
	}
	outside := filepath.Join(root, "outside")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0600))
	bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{})
	require.NoError(t, err)
	defer bootstrap.Close()
	files, err := bootstrap.Files()
	require.NoError(t, err)
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	require.NoError(t, err)
	defer backend.Close()
	require.NoError(t, bootstrap.Close())
	ready := make(chan struct{}, 4)
	sealed := make(chan struct{})
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ready <- struct{}{}
			<-sealed
			_, err := os.ReadFile(outside)
			assert.ErrorIs(t, err, os.ErrPermission, "existing thread escaped")
			value, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
			assert.NoError(t, err, "the existing thread has no_new_privs")
			assert.Equal(t, 1, value)
		})
	}
	for range 4 {
		<-ready
	}
	sealErr := backend.SealProcess()
	if sealErr != nil {
		close(sealed)
		group.Wait()
	}
	require.NoError(t, sealErr)
	close(sealed)
	group.Wait()
	testLandlockDirectAccess(t, root, outside)
	testLandlockBackendAccess(t, backend)
	require.ErrorIs(t, backend.SealProcess(), ErrInvalidPolicy, "resealed mutable policy")
}

func testLandlockDirectAccess(t *testing.T, root, outside string) {
	t.Helper()
	for _, name := range []string{outside, filepath.Join(root, "write", "file"), filepath.Join(root, "list", "file")} {
		_, err := os.ReadFile(name)
		assert.ErrorIs(t, err, os.ErrPermission, "read allowed %q", name)
	}
	approvedData, approvedErr := os.ReadFile(filepath.Join(root, "read", "file"))
	assert.NoError(t, approvedErr, "the approved read works")
	assert.Equal(t, "original", string(approvedData))
	_, err := os.ReadDir(filepath.Join(root, "read"))
	assert.ErrorIs(t, err, os.ErrPermission, "read grant listed directory")
	_, err = os.ReadDir(filepath.Join(root, "list"))
	assert.NoError(t, err, "approved listing")
	for _, name := range []string{outside, filepath.Join(root, "read", "file"), filepath.Join(root, "list", "file")} {
		assert.ErrorIs(t, os.WriteFile(name, []byte("changed"), 0600), os.ErrPermission, "write allowed %q", name)
	}
	for _, name := range []string{"read", "list", "write"} {
		assert.ErrorIs(t, unix.Mkfifo(filepath.Join(root, name, "fifo"), 0600), unix.EACCES, "special file allowed")
	}
	descriptor, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	defer unix.Close(descriptor)
	assert.ErrorIs(t, unix.Bind(descriptor, &unix.SockaddrInet4{Port: 0, Addr: [4]byte{127, 0, 0, 1}}), unix.EACCES, "TCP bind allowed")
	assert.ErrorIs(t, unix.Connect(descriptor, &unix.SockaddrInet4{Port: 9, Addr: [4]byte{127, 0, 0, 1}}), unix.EACCES, "TCP connect allowed")
}

func testLandlockBackendAccess(t *testing.T, backend *LinuxFilesystem) {
	t.Helper()
	read := brokerMessage(1, `{"operation":"fs.read","root":"read","path":"file","max_bytes":64}`)
	result, err := backend.Execute(read)
	require.NoError(t, err, "confined read: %+v", result)
	require.Equal(t, "original", string(result.Data))
	write := brokerMessage(2, `{"operation":"fs.write","root":"write","path":"file","data":"bmV3"}`)
	result, err = backend.Execute(write)
	require.NoError(t, err, "confined staged write: %+v", result)
	require.Equal(t, 3, result.Written)
	list := brokerMessage(3, `{"operation":"fs.list","root":"list","path":".","max_entries":16}`)
	result, err = backend.Execute(list)
	require.NoError(t, err, "confined listing: %+v", result)
	require.Len(t, result.Entries, 1)
	denied := brokerMessage(4, `{"operation":"fs.list","root":"write","path":".","max_entries":16}`)
	_, err = backend.Execute(denied)
	require.ErrorIs(t, err, ErrDenied, "native write metadata permission leaked to protocol")
	root, record := stageRecoveryRemnant(t, backend, "write", "file")
	for range 2 {
		require.NoError(t, recoverLinuxStaging(root, backend.namespace, record), "confined staging cleanup and retry succeed")
	}
}

func TestLinuxBrokerRejectsLateSealing(t *testing.T) {
	backend := openTestFilesystem(t, t.TempDir(), Read, FilesystemLimits{})
	_, err := backend.Execute(brokerMessage(1, readPayload))
	require.Error(t, err, "missing file read succeeded")
	require.ErrorIs(t, backend.SealProcess(), ErrInvalidPolicy)
	_, err = backend.Execute(brokerMessage(2, readPayload))
	require.ErrorIs(t, err, ErrConfinement, "failed sealing did not stop admission")
	require.ErrorIs(t, backend.SealProcess(), ErrInvalidPolicy, "retry accepted")
	require.NoError(t, backend.Close())
	require.ErrorIs(t, backend.SealProcess(), ErrClosed)
	var absent *LinuxFilesystem
	require.ErrorIs(t, absent.SealProcess(), ErrClosed)
}

func TestBrokerLandlockRights(t *testing.T) {

	const (
		readFile = unix.LANDLOCK_ACCESS_FS_READ_FILE
		readDir  = unix.LANDLOCK_ACCESS_FS_READ_DIR
		writeSet = unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE
	)
	tests := []struct {
		name   string
		rights Rights
		want   uint64
	}{
		{name: "none", rights: 0, want: 0},
		{name: "read", rights: Read, want: readFile},
		{name: "list", rights: List, want: readDir},
		{name: "write", rights: Write, want: writeSet},
		{name: "read+list", rights: Read | List, want: readFile | readDir},
		{name: "read+write", rights: Read | Write, want: readFile | writeSet},
		{name: "list+write", rights: List | Write, want: readDir | writeSet},
		{name: "read+list+write", rights: Read | List | Write, want: readFile | writeSet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := brokerLandlockRights(tt.rights)
			require.Equal(t, tt.want, got, "rights %b", tt.rights)

			require.Equal(t, uint64(0), got & ^uint64(brokerFilesystemAccess), "the mask stays inside the allowed set")
			for _, forbidden := range []uint64{unix.LANDLOCK_ACCESS_FS_EXECUTE, unix.LANDLOCK_ACCESS_FS_REFER, unix.LANDLOCK_ACCESS_FS_TRUNCATE} {
				require.Equal(t, uint64(0), got&forbidden, "mask granted a forbidden right %#x", forbidden)
			}
		})
	}
}

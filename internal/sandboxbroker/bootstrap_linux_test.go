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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxtest"
)

func testBrokerProc(t *testing.T) *os.File {
	t.Helper()
	directory, err := openLinuxDirectory("/proc/" + strconv.Itoa(os.Getpid()) + "/fd")
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, directory.Close())
	})
	return directory
}

func testLinuxBootstrap(t *testing.T, root string) *LinuxBootstrap {
	t.Helper()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read}}, FilesystemLimits{ReadBytes: 4})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, bootstrap.Close())
	})
	return bootstrap
}

func TestLinuxBootstrapSeals(t *testing.T) {
	root := t.TempDir()
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	configuration := files[0]
	encoded := make([]byte, maximumBootstrapBytes)
	count, _ := configuration.ReadAt(encoded, 0)
	require.False(t, bytes.Contains(encoded[:count], []byte(root)), "host pathname leaked into policy")
	_, err = configuration.WriteAt([]byte("x"), 0)
	require.ErrorIs(t, err, unix.EPERM, "policy remained writable")
	for _, size := range []int64{0, maximumBootstrapBytes} {
		require.ErrorIs(t, configuration.Truncate(size), unix.EPERM, "policy remained resizable")
	}
	require.ErrorIs(t, configuration.Chmod(0700), unix.EPERM, "policy became executable")
	_, err = unix.FcntlInt(configuration.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_FUTURE_WRITE)
	require.ErrorIs(t, err, unix.EPERM, "seals remained mutable")
	mapping, err := unix.Mmap(int(configuration.Fd()), 0, count, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err == nil {
		_ = unix.Munmap(mapping)
	}
	require.ErrorIs(t, err, unix.EPERM, "a writable mapping is refused")
	files[0] = nil
	independent, err := bootstrap.Files()
	require.NoError(t, err, "caller mutated descriptor order")
	require.Same(t, configuration, independent[0])
	for _, file := range independent {
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		require.NoError(t, err, "descriptor would leak across unintended exec")
		require.NotEqual(t, 0, flags&unix.FD_CLOEXEC)
	}
}

func TestLinuxBootstrapAdoption(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600))
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	require.NoError(t, err)
	defer backend.Close()
	require.True(t, validStagingNamespace(bootstrap.StagingNamespace()), "staging namespace changed during sealed adoption")
	require.Equal(t, bootstrap.StagingNamespace(), backend.namespace)
	require.NoError(t, bootstrap.Close())
	_, err = bootstrap.Files()
	require.ErrorIs(t, err, ErrClosed)
	result, err := backend.Execute(brokerMessage(1, readPayload))
	require.NoError(t, err, "independent handle ownership: %+v", result)
	require.Equal(t, "data", string(result.Data))
	_, err = backend.Execute(brokerMessage(2, readPayload))
	require.ErrorIs(t, err, errLimit, "quota changed during handoff")
	var absent *LinuxBootstrap
	require.NoError(t, absent.Close())
	_, err = absent.Files()
	require.ErrorIs(t, err, ErrClosed)
}

func TestLinuxBootstrapRejectsMalformedPolicy(t *testing.T) {
	const namespace = `"namespace":"0123456789abcdef0123456789abcdef",`
	for _, body := range []string{
		`{}`,
		`{` + namespace + `"profile":"filesystem-bootstrap-v1","roots":[],"limits":{}}`,
		`{` + namespace + `"profile":"filesystem-bootstrap-v2","roots":null,"limits":{}}`,
		`{` + namespace + `"profile":"filesystem-bootstrap-v2","roots":[],"limits":null}`,
		`{` + namespace + `"profile":"filesystem-bootstrap-v2","roots":[],"limits":{},"path":"/"}`,
		`{` + namespace + `"profile":"filesystem-bootstrap-v2","Profile":"filesystem-bootstrap-v2","roots":[],"limits":{}}`,
		`{` + namespace + `"profile":"filesystem-bootstrap-v2","profile":"filesystem-bootstrap-v2","roots":[],"limits":{}}`,
		`{"profile":"filesystem-bootstrap-v2","roots":[],"limits":{}}`,
		`{"namespace":null,"profile":"filesystem-bootstrap-v2","roots":[],"limits":{}}`,
		`{"namespace":"../file","profile":"filesystem-bootstrap-v2","roots":[],"limits":{}}`,
	} {
		configuration, err := sealLinuxBootstrap([]byte(body))
		require.NoError(t, err)
		_, err = readLinuxBootstrap(configuration)
		closeErr := configuration.Close()
		require.Error(t, err, "a malformed policy is refused: %s", body)
		require.NoError(t, closeErr)
	}
	_, err := readLinuxBootstrap(nil)
	require.ErrorIs(t, err, ErrInvalidPolicy)
	descriptor, err := unix.MemfdCreate("unsealed-test", unix.MFD_CLOEXEC|unix.MFD_NOEXEC_SEAL)
	require.NoError(t, err)
	configuration := os.NewFile(uintptr(descriptor), "unsealed")
	defer configuration.Close()
	_, err = configuration.WriteString("{}")
	require.NoError(t, err)
	_, err = readLinuxBootstrap(configuration)
	require.ErrorIs(t, err, ErrInvalidPolicy, "unsealed policy accepted")
	require.NoError(t, configuration.Truncate(maximumBootstrapBytes+1))
	_, err = unix.FcntlInt(configuration.Fd(), unix.F_ADD_SEALS, requiredBootstrapSeals)
	require.NoError(t, err)
	_, err = readLinuxBootstrap(configuration)
	require.ErrorIs(t, err, ErrInvalidPolicy, "oversized policy accepted")
}

func TestLinuxBootstrapRejectsDescriptorSubstitution(t *testing.T) {
	root := t.TempDir()
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	proc := testBrokerProc(t)
	ordinary, err := os.Open(root)
	require.NoError(t, err)
	defer ordinary.Close()
	replacement, err := openLinuxDirectory(t.TempDir())
	require.NoError(t, err)
	defer replacement.Close()
	for _, offered := range [][]*os.File{nil, {files[0]}, {files[0], nil}, {files[0], ordinary}, {files[0], files[0]}, {files[0], replacement}} {
		backend, err := adoptLinuxFilesystem(offered, proc)
		require.Error(t, err, "invalid roots adopted")
		require.Nil(t, backend)
	}
	for _, offered := range []*os.File{nil, files[0], ordinary} {
		backend, err := adoptLinuxFilesystem(files, offered)
		require.Error(t, err, "invalid proc adopted")
		require.Nil(t, backend)
	}
	_, err = ordinary.Stat()
	require.NoError(t, err, "borrowed directory closed")
	_, err = files[1].Stat()
	require.NoError(t, err, "borrowed root closed")
	_, err = proc.Stat()
	require.NoError(t, err, "borrowed proc closed")
}

func TestLinuxBootstrapExecHandoff(t *testing.T) {
	if os.Getenv("PIPIT_TEST_BOOTSTRAP") == "child" {
		configuration := os.NewFile(3, "policy")
		root := os.NewFile(4, "root")
		backend, err := adoptLinuxFilesystem([]*os.File{configuration, root}, testBrokerProc(t))
		require.NoError(t, err)
		defer backend.Close()
		require.NoError(t, configuration.Close())
		require.NoError(t, root.Close())
		result, err := backend.Execute(brokerMessage(1, readPayload))
		require.NoError(t, err, "exec handoff: %+v", result)
		require.Equal(t, "data", string(result.Data))
		return
	}
	t.Run("ordinary", func(t *testing.T) { testLinuxBootstrapExec(t, false) })
	t.Run("namespaced", func(t *testing.T) { testLinuxBootstrapExec(t, true) })
}

func testLinuxBootstrapExec(t *testing.T, namespaced bool) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600))
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := sandboxtest.Command(ctx, "TestLinuxBootstrapExecHandoff", "PIPIT_TEST_BOOTSTRAP=child")
	command.ExtraFiles = files
	if namespaced {
		command.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags:                 unix.CLONE_NEWUSER,
			Unshareflags:               unix.CLONE_NEWNS,
			UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
			GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
			GidMappingsEnableSetgroups: false,
		}
	}
	output, err := command.CombinedOutput()
	if namespaced && os.Getenv("PIPIT_TEST_REQUIRE_USERNS") != "1" &&
		(errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS)) {
		t.Skipf("native namespaces unavailable: %v", err)
	}
	require.NoError(t, err, "the bootstrap subprocess succeeds\n%s", output)
}

func TestLinuxBootstrapRejectsInvalidGrants(t *testing.T) {
	root := t.TempDir()
	for _, grants := range [][]LinuxRootGrant{
		{{Name: "data", Path: root, Rights: 0}},
		{{Name: "data", Path: "relative", Rights: Read}},
		{{Name: "data", Path: root, Rights: Read}, {Name: "data", Path: root, Rights: Write}},
		{{Name: "data", Path: root, Rights: Read}, {Name: "missing", Path: filepath.Join(root, "missing"), Rights: Read}},
	} {
		bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{})
		require.Error(t, err, "invalid bootstrap grants accepted")
		require.Nil(t, bootstrap)
	}
}

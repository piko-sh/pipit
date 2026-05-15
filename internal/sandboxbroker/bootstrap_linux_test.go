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
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func testBrokerProc(t *testing.T) *os.File {
	t.Helper()
	directory, err := openLinuxDirectory("/proc/" + strconv.Itoa(os.Getpid()) + "/fd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})
	return directory
}

func testLinuxBootstrap(t *testing.T, root string) *LinuxBootstrap {
	t.Helper()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read}}, FilesystemLimits{ReadBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := bootstrap.Close(); err != nil {
			t.Error(err)
		}
	})
	return bootstrap
}

func TestLinuxBootstrapSeals(t *testing.T) {
	root := t.TempDir()
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	configuration := files[0]
	encoded := make([]byte, maximumBootstrapBytes)
	count, _ := configuration.ReadAt(encoded, 0)
	if bytes.Contains(encoded[:count], []byte(root)) {
		t.Fatal("host pathname leaked into policy")
	}
	if _, err := configuration.WriteAt([]byte("x"), 0); !errors.Is(err, unix.EPERM) {
		t.Fatal("policy remained writable:", err)
	}
	for _, size := range []int64{0, maximumBootstrapBytes} {
		if err := configuration.Truncate(size); !errors.Is(err, unix.EPERM) {
			t.Fatal("policy remained resizable:", err)
		}
	}
	if err := configuration.Chmod(0700); !errors.Is(err, unix.EPERM) {
		t.Fatal("policy became executable:", err)
	}
	if _, err := unix.FcntlInt(configuration.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_FUTURE_WRITE); !errors.Is(err, unix.EPERM) {
		t.Fatal("seals remained mutable:", err)
	}
	mapping, err := unix.Mmap(int(configuration.Fd()), 0, count, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err == nil {
		_ = unix.Munmap(mapping)
		t.Fatal("writable mapping permitted")
	}
	if !errors.Is(err, unix.EPERM) {
		t.Fatal(err)
	}
	files[0] = nil
	independent, err := bootstrap.Files()
	if err != nil || independent[0] != configuration {
		t.Fatal("caller mutated descriptor order:", err)
	}
	for _, file := range independent {
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("descriptor would leak across unintended exec:", err)
		}
	}
}

func TestLinuxBootstrapAdoption(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if !validStagingNamespace(bootstrap.StagingNamespace()) || backend.namespace != bootstrap.StagingNamespace() {
		t.Fatal("staging namespace changed during sealed adoption")
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Files(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	result, err := backend.Execute(brokerMessage(1, readPayload))
	if err != nil || string(result.Data) != "data" {
		t.Fatalf("independent handle ownership: %+v: %v", result, err)
	}
	if _, err := backend.Execute(brokerMessage(2, readPayload)); !errors.Is(err, errLimit) {
		t.Fatal("quota changed during handoff:", err)
	}
	var absent *LinuxBootstrap
	if err := absent.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := absent.Files(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		_, err = readLinuxBootstrap(configuration)
		closeErr := configuration.Close()
		if err == nil {
			t.Errorf("accepted malformed policy: %s", body)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if _, err := readLinuxBootstrap(nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal(err)
	}
	descriptor, err := unix.MemfdCreate("unsealed-test", unix.MFD_CLOEXEC|unix.MFD_NOEXEC_SEAL)
	if err != nil {
		t.Fatal(err)
	}
	configuration := os.NewFile(uintptr(descriptor), "unsealed")
	defer configuration.Close()
	if _, err := configuration.WriteString("{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := readLinuxBootstrap(configuration); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("unsealed policy accepted:", err)
	}
	if err := configuration.Truncate(maximumBootstrapBytes + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(configuration.Fd(), unix.F_ADD_SEALS, requiredBootstrapSeals); err != nil {
		t.Fatal(err)
	}
	if _, err := readLinuxBootstrap(configuration); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("oversized policy accepted:", err)
	}
}

func TestLinuxBootstrapRejectsDescriptorSubstitution(t *testing.T) {
	root := t.TempDir()
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	proc := testBrokerProc(t)
	ordinary, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	replacement, err := openLinuxDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	for _, offered := range [][]*os.File{nil, {files[0]}, {files[0], nil}, {files[0], ordinary}, {files[0], files[0]}, {files[0], replacement}} {
		if backend, err := adoptLinuxFilesystem(offered, proc); err == nil || backend != nil {
			t.Fatal("invalid roots adopted:", err)
		}
	}
	for _, offered := range []*os.File{nil, files[0], ordinary} {
		if backend, err := adoptLinuxFilesystem(files, offered); err == nil || backend != nil {
			t.Fatal("invalid proc adopted:", err)
		}
	}
	if _, err := ordinary.Stat(); err != nil {
		t.Fatal("borrowed directory closed:", err)
	}
	if _, err := files[1].Stat(); err != nil {
		t.Fatal("borrowed root closed:", err)
	}
	if _, err := proc.Stat(); err != nil {
		t.Fatal("borrowed proc closed:", err)
	}
}

func TestLinuxBootstrapExecHandoff(t *testing.T) {
	if os.Getenv("PIPIT_TEST_BOOTSTRAP") == "child" {
		configuration := os.NewFile(3, "policy")
		root := os.NewFile(4, "root")
		backend, err := adoptLinuxFilesystem([]*os.File{configuration, root}, testBrokerProc(t))
		if err != nil {
			t.Fatal(err)
		}
		defer backend.Close()
		if err := configuration.Close(); err != nil {
			t.Fatal(err)
		}
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
		result, err := backend.Execute(brokerMessage(1, readPayload))
		if err != nil || string(result.Data) != "data" {
			t.Fatalf("exec handoff: %+v: %v", result, err)
		}
		return
	}
	t.Run("ordinary", func(t *testing.T) { testLinuxBootstrapExec(t, false) })
	t.Run("namespaced", func(t *testing.T) { testLinuxBootstrapExec(t, true) })
}

func testLinuxBootstrapExec(t *testing.T, namespaced bool) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap := testLinuxBootstrap(t, root)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxBootstrapExecHandoff$")
	command.Env = append(os.Environ(), "PIPIT_TEST_BOOTSTRAP=child")
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
	if err != nil {
		t.Fatalf("bootstrap subprocess: %v\n%s", err, output)
	}
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
		if err == nil || bootstrap != nil {
			t.Fatal("invalid bootstrap grants accepted:", err)
		}
	}
}

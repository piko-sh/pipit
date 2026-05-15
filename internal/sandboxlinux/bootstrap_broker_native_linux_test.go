//go:build linux && (amd64 || arm64) && !race

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
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestNativeFilesystemBrokerBootstrap(t *testing.T) {
	skipUnderCoverage(t)
	if os.Getenv("PIPIT_BROKER_BOOTSTRAP_CHILD") == "1" {
		exerciseNativeBrokerBootstrap(t)
		return
	}
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || version < 8 {
		if os.Getenv("PIPIT_REQUIRE_BROKER_LANDLOCK") == "1" {
			t.Fatalf("Landlock TSYNC unavailable: %d: %v", version, errno)
		}
		t.Skipf("Landlock TSYNC unavailable: %d: %v", version, errno)
	}
	for _, mode := range []string{"valid", "wrong-root", "unsealed-policy"} {
		t.Run(mode, func(t *testing.T) { runNativeBrokerBootstrap(t, mode) })
	}
}

func runNativeBrokerBootstrap(t *testing.T, mode string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	image, err := PrepareWorkerImage(ctx, executable, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	imageRoot, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	grantRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(grantRoot, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := sandboxbroker.PrepareLinuxBootstrap([]sandboxbroker.LinuxRootGrant{
		{Name: "data", Path: grantRoot, Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List},
	}, sandboxbroker.FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	substituteBrokerBootstrap(t, mode, files)
	command := exec.CommandContext(ctx, "/worker", "-test.run=^TestNativeFilesystemBrokerBootstrap$")
	command.Dir = "/"
	command.Env = []string{"PIPIT_BROKER_BOOTSTRAP_CHILD=1", "PIPIT_BROKER_BOOTSTRAP_MODE=" + mode}
	configureDescriptorFixture(t, command)
	inherited := slices.Clone(command.ExtraFiles)
	command.ExtraFiles = append(inherited[:1:1], files...)
	command.ExtraFiles = append(command.ExtraFiles, inherited[1:]...)
	command.SysProcAttr, err = WorkerAttributes(imageRoot)
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.CombinedOutput()
	if os.Getenv("PIPIT_TEST_REQUIRE_USERNS") != "1" && (errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS)) {
		t.Skipf("native namespaces unavailable: %v", err)
	}
	if err != nil {
		t.Fatalf("broker bootstrap %s: %v\n%s", mode, err, output)
	}
	expected := "original"
	if mode == "valid" {
		expected = "new"
	}
	data, err = os.ReadFile(filepath.Join(grantRoot, "file"))
	if err != nil || string(data) != expected {
		t.Fatalf("unexpected host result: %q: %v", data, err)
	}
}

func substituteBrokerBootstrap(t *testing.T, mode string, files []*os.File) {
	t.Helper()
	switch mode {
	case "valid":
		return
	case "wrong-root":
		descriptor, err := unix.Open(t.TempDir(), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		replacement := os.NewFile(uintptr(descriptor), "wrong-root")
		t.Cleanup(func() { _ = replacement.Close() })
		files[1] = replacement
	case "unsealed-policy":
		replacement, err := os.CreateTemp(t.TempDir(), "unsealed-policy")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = replacement.Close() })
		files[0] = replacement
	default:
		t.Fatal("unknown fixture mode")
	}
}

func exerciseNativeBrokerBootstrap(t *testing.T) {
	high, err := strconv.Atoi(os.Getenv("PIPIT_INHERITED_HIGH_FD"))
	if err != nil || high < 512 {
		t.Fatal("missing high descriptor:", err)
	}
	for _, descriptor := range []int{7, high} {
		flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC != 0 {
			t.Fatalf("fixture did not inherit %d: %v", descriptor, err)
		}
	}
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 64, Max: 64}); err != nil {
		t.Fatal(err)
	}
	workerIO, backend, err := BootstrapFilesystemBroker()
	if os.Getenv("PIPIT_BROKER_BOOTSTRAP_MODE") != "valid" {
		if err == nil || workerIO != nil || backend != nil {
			t.Fatal("invalid bootstrap retained usable authority:", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer workerIO.Stream().Close()
	defer backend.Close()
	for _, descriptor := range []int{0, sandboxbroker.LinuxBootstrapFD, sandboxbroker.LinuxBootstrapFD + 1, sandboxbroker.LinuxBootstrapFD + 2, high} {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("inherited authority retained at %d: %v", descriptor, err)
		}
	}
	if _, err := workerIO.Stream().Write([]byte("ready")); err != nil {
		t.Fatal("private channel lost:", err)
	}
	descriptor, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err == nil {
		_ = unix.Close(descriptor)
	}
	if !errors.Is(err, unix.EPERM) {
		t.Fatal("broker retained network creation:", err)
	}
	result, err := backend.Execute(sandboxwire.Message{Kind: sandboxwire.Call, ID: 1,
		Payload: []byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":64}`)})
	if err != nil || string(result.Data) != "original" {
		t.Fatalf("bootstrapped read: %+v: %v", result, err)
	}
	result, err = backend.Execute(sandboxwire.Message{Kind: sandboxwire.Call, ID: 2,
		Payload: []byte(`{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`)})
	if err != nil || result.Written != 3 {
		t.Fatalf("bootstrapped write: %+v: %v", result, err)
	}
	result, err = backend.Execute(sandboxwire.Message{Kind: sandboxwire.Call, ID: 3,
		Payload: []byte(`{"operation":"fs.list","root":"data","path":".","max_entries":16}`)})
	if err != nil || len(result.Entries) != 1 || result.Entries[0] != "file" {
		t.Fatalf("bootstrapped list: %+v: %v", result, err)
	}
}

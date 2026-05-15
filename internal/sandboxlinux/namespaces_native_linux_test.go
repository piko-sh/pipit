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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeWorkerNamespaces(t *testing.T) {
	skipUnderCoverage(t)
	if os.Getenv("PIPIT_NAMESPACE_CHILD") == "1" {
		checkNativeWorkerNamespaces(t)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, source); err != nil {
		t.Fatal(err)
	}
	image, err := PrepareWorkerImage(context.Background(), executable, [sha256.Size]byte(digest.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := image.Close(); err != nil {
			t.Errorf("removing worker image: %v", err)
		}
	}()
	root, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	hostName, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/worker", "-test.run=^TestNativeWorkerNamespaces$")
	command.Dir = "/"
	command.Env = []string{"PIPIT_NAMESPACE_CHILD=1"}
	configureDescriptorFixture(t, command)
	command.SysProcAttr, err = WorkerAttributes(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.CombinedOutput()
	if os.Getenv("PIPIT_TEST_REQUIRE_USERNS") != "1" &&
		(errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS)) {
		t.Skipf("native namespaces unavailable: %v", err)
	}
	if err != nil {
		t.Fatalf("namespace fixture failed: %v\n%s", err, output)
	}
	if after, err := os.Hostname(); err != nil || after != hostName {
		t.Fatalf("worker altered host hostname: %q, %v", after, err)
	}
	if err := os.WriteFile(filepath.Join(root, "host-probe"), []byte("host mount remains writable"), 0600); err != nil {
		t.Fatalf("worker mount escaped its namespace: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "host-probe")); err != nil {
		t.Fatal(err)
	}
}

func checkNativeWorkerNamespaces(t *testing.T) {
	t.Helper()
	workerIO, err := prepareWorkerIO()
	if err != nil {
		t.Fatal(err)
	}
	ipc := workerIO.Stream()
	defer ipc.Close()
	checkNativeWorkerDescriptors(t)
	if os.Getpid() != 1 || os.Getuid() != 0 || os.Getgid() != 0 {
		t.Fatal("worker did not enter its PID and identity namespaces")
	}
	for _, path := range []string{"/proc", "/sys", "/dev", "/etc", "/../../etc/passwd"} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("host path visible: %s: %v", path, err)
		}
	}
	if err := unix.Sethostname([]byte("pipit-worker")); err != nil {
		t.Fatalf("setting namespaced hostname: %v", err)
	}
	socket, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("creating namespaced socket: %v", err)
	}
	request, err := unix.NewIfreq("lo")
	if err != nil {
		t.Fatal(err)
	}
	err = unix.IoctlIfreq(socket, unix.SIOCGIFFLAGS, request)
	_ = unix.Close(socket)
	if err != nil || request.Uint16()&unix.IFF_UP != 0 {
		t.Fatalf("worker inherited an active loopback interface: %v", err)
	}
	if err := sealWorkerFilesystem(); err != nil {
		t.Fatal(err)
	}
	if err := sealWorkerPrivileges(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/escape", []byte("denied"), 0600); !errors.Is(err, unix.EROFS) {
		t.Fatalf("worker root remained writable: %v", err)
	}
	if err := unix.Mount("", "/", "", unix.MS_REMOUNT|unix.MS_BIND, ""); !errors.Is(err, unix.EPERM) {
		t.Fatalf("worker could remount its root: %v", err)
	}
	if err := unix.Chroot(".."); !errors.Is(err, unix.EPERM) {
		t.Fatalf("worker could change its root: %v", err)
	}
	if err := workerIO.installSyscallFilter(); err != nil {
		t.Fatal(err)
	}
	checkWorkerIPCDeadlines(t, ipc)
}

func checkWorkerIPCDeadlines(t *testing.T, ipc *os.File) {
	t.Helper()
	if err := ipc.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	changed := make(chan error, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		changed <- ipc.SetReadDeadline(time.Now())
	}()
	if _, err := ipc.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("IPC deadline failed: %v", err)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if err := ipc.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := ipc.SetWriteDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := ipc.Write(make([]byte, 1<<20)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("blocked write deadline failed: %v", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		changed <- ipc.Close()
	}()
	if _, err := ipc.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closing IPC did not wake its reader: %v", err)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
}

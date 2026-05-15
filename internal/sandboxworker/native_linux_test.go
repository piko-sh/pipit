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

package sandboxworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxlinux"
)

var nativeWorkerBinary = flag.String("pipit-worker-test-binary", "", "host-selected static worker fixture")

func TestNativeWorkerSource(t *testing.T) {
	executable := os.Getenv("PIPIT_TEST_WORKER_BINARY")
	if executable == "" {
		executable = *nativeWorkerBinary
	}
	if executable == "" {
		t.Skip("requires a separately built, static worker executable")
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	if err := errors.Join(readErr, source.Close()); err != nil {
		t.Fatal(err)
	}
	image, err := sandboxlinux.PrepareWorkerImageInStore(context.Background(), executable, [sha256.Size]byte(digest.Sum(nil)), nativeImageStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	root, err := image.Root()
	if err != nil {
		t.Fatal(err)
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent := os.NewFile(uintptr(pair[0]), "host-ipc")
	child := os.NewFile(uintptr(pair[1]), "worker-ipc")
	defer parent.Close()
	defer child.Close()
	proc, err := sandboxlinux.OpenWorkerProc()
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/worker")
	command.Dir = "/"
	command.Env = []string{}
	command.ExtraFiles = []*os.File{child, proc}
	command.SysProcAttr, err = sandboxlinux.WorkerAttributes(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	_ = child.Close()
	_ = proc.Close()
	if err := parent.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	response, protocolErr := nativeSourceExchange(&accountingFixture{transport: parent, charged: 0, err: nil})
	if protocolErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if protocolErr != nil || waitErr != nil {
		t.Fatalf("confined evaluation failed: protocol=%v process=%v\n%s", protocolErr, waitErr, output.String())
	}
	if response.Code != "ok" || string(response.Value) != "7" {
		t.Fatalf("unexpected confined result: %+v", response)
	}
}

func TestNativeWorkerSupervised(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires an explicitly delegated cgroup-v2 parent")
	}
	executable := os.Getenv("PIPIT_TEST_WORKER_BINARY")
	if executable == "" {
		executable = *nativeWorkerBinary
	}
	if executable == "" {
		t.Fatal("supervised integration requires a static worker executable")
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	if err := errors.Join(readErr, source.Close()); err != nil {
		t.Fatal(err)
	}
	var config sandboxlinux.WorkerConfig
	config.Tenant = "worker-test"
	config.ImageStore = nativeImageStore(t)
	config.Executable = executable
	config.WatchdogExecutable = os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(config.WatchdogExecutable)
	if err != nil {
		t.Fatal(err)
	}
	config.WatchdogDigest = sha256.Sum256(watchdogImage)
	config.CgroupParent = parent
	config.Digest = [sha256.Size]byte(digest.Sum(nil))
	process, err := sandboxlinux.LaunchWorker(context.Background(), config)
	if process != nil {
		t.Cleanup(func() {
			if err := process.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	response, err := nativeSourceExchange(process)
	if err != nil {
		t.Fatalf("supervised protocol: %v; diagnostics: %s", err, process.Output())
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("supervised cleanup: %v; diagnostics: %s", err, process.Output())
	}
	if response.Code != "ok" || string(response.Value) != "7" {
		t.Fatalf("unexpected supervised result: %+v", response)
	}
	t.Run("broadened imports", func(t *testing.T) {

		for _, tc := range []struct{ pkg, src, want string }{
			{"strconv", "import \"strconv\"\nstrconv.Itoa(42)", "\"42\""},
			{"strings", "import \"strings\"\nstrings.ToUpper(\"hi\")", "\"HI\""},
		} {
			w, err := sandboxlinux.LaunchWorker(context.Background(), config)
			if w != nil {
				defer w.Close()
			}
			if err != nil {
				t.Fatalf("launch %s: %v", tc.pkg, err)
			}
			resp, err := Exchange(w, Configuration{Profile: Profile, Imports: []string{tc.pkg}},
				Request{Kind: "expression", Source: tc.src, Entrypoint: ""})
			if err != nil {
				t.Fatalf("%s in confined worker: %v; diagnostics: %s", tc.pkg, err, w.Output())
			}
			if resp.Code != "ok" || string(resp.Value) != tc.want {
				t.Fatalf("%s result: %+v", tc.pkg, resp)
			}
		}
	})
	t.Run("cached image reuse", func(t *testing.T) {

		cache, err := sandboxlinux.StageWorkerImageCache(context.Background(), t.TempDir(),
			[]sandboxlinux.WorkerImageRequest{
				{Executable: config.Executable, Digest: config.Digest},
				{Executable: config.WatchdogExecutable, Digest: config.WatchdogDigest},
			})
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()
		cached := config
		cached.Cache = cache
		for range 2 {
			w, err := sandboxlinux.LaunchWorker(context.Background(), cached)
			if w != nil {
				defer w.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			resp, err := nativeSourceExchange(w)
			if err != nil {
				t.Fatalf("cached worker: %v; diagnostics: %s", err, w.Output())
			}
			if err := w.Wait(); err != nil {
				t.Fatalf("cached worker cleanup: %v; diagnostics: %s", err, w.Output())
			}
			if resp.Code != "ok" || string(resp.Value) != "7" {
				t.Fatalf("cached worker result: %+v", resp)
			}
		}
	})
	t.Run("persistent session", func(t *testing.T) { testNativeSession(t, config) })
	t.Run("script output budget", func(t *testing.T) {
		boundedConfig := config
		boundedConfig.OutputBytes = 4
		bounded, err := sandboxlinux.LaunchWorker(context.Background(), boundedConfig)
		if bounded != nil {
			defer bounded.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		response, err := Exchange(bounded, Configuration{Profile: Profile, Imports: nil},
			Request{Kind: "expression", Source: "println(\"hello\")", Entrypoint: ""})
		if !errors.Is(err, sandboxlinux.ErrWorkerOutput) || response.Output != "" {
			t.Fatalf("script output escaped host budget: response=%+v error=%v", response, err)
		}
		if err := bounded.Wait(); !errors.Is(err, sandboxlinux.ErrWorkerOutput) {
			t.Fatalf("worker completion lost output overflow: %v", err)
		}
	})
	t.Run("idle phase deadline", func(t *testing.T) {
		idle, err := sandboxlinux.LaunchWorker(context.Background(), config)
		if idle != nil {
			defer idle.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := idle.SetDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if err := idle.Wait(); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("idle worker escaped its phase deadline: %v", err)
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "pipit-") {
				t.Fatalf("deadline left a worker resource group: %s", entry.Name())
			}
		}
	})
}

func nativeSourceExchange(stream HostTransport) (Response, error) {
	return Exchange(stream,
		Configuration{Profile: Profile, Imports: []string{"math"}},
		Request{Kind: "expression", Source: "import \"math\"\nmath.Sqrt(49)", Entrypoint: ""},
	)
}

func nativeImageStore(t *testing.T) *sandboxbroker.LinuxImageStore {
	t.Helper()
	return nativeImageStoreWithGrants(t, nil)
}

func nativeImageStoreWithGrants(t *testing.T, grants []sandboxbroker.LinuxRootGrant) *sandboxbroker.LinuxImageStore {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, grants, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

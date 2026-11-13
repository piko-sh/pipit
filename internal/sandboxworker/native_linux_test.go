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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxlinux"
	"pipit.sh/pipit/internal/sandboxtest"
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
	require.NoError(t, err)
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	require.NoError(t, errors.Join(readErr, source.Close()))
	image, err := sandboxlinux.PrepareWorkerImageInStore(t.Context(), executable, [sha256.Size]byte(digest.Sum(nil)), nativeImageStore(t))
	require.NoError(t, err)
	defer image.Close()
	root, err := image.Root()
	require.NoError(t, err)
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	parent := os.NewFile(uintptr(pair[0]), "host-ipc")
	child := os.NewFile(uintptr(pair[1]), "worker-ipc")
	defer parent.Close()
	defer child.Close()
	proc, err := sandboxlinux.OpenWorkerProc()
	require.NoError(t, err)
	defer proc.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/worker")
	command.Dir = "/"
	command.Env = []string{}
	command.ExtraFiles = []*os.File{child, proc}
	command.SysProcAttr, err = sandboxlinux.WorkerAttributes(root)
	require.NoError(t, err)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	require.NoError(t, command.Start())
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	_ = child.Close()
	_ = proc.Close()
	require.NoError(t, parent.SetDeadline(time.Now().Add(10*time.Second)))
	response, protocolErr := nativeSourceExchange(&accountingFixture{transport: parent, charged: 0, err: nil})
	if protocolErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if protocolErr != nil || waitErr != nil {
		require.Failf(t, "the confined evaluation succeeds", "protocol=%v process=%v\n%s", protocolErr, waitErr, output.String())
	}
	require.Equal(t, "ok", response.Code, "unexpected confined result: %+v", response)
	require.Equal(t, "7", string(response.Value))
}

func TestNativeWorkerSupervised(t *testing.T) {
	parent := sandboxtest.CgroupParent(t)
	executable := os.Getenv("PIPIT_TEST_WORKER_BINARY")
	if executable == "" {
		executable = *nativeWorkerBinary
	}
	require.NotEmpty(t, executable, "supervised integration requires a static worker executable")
	source, err := os.Open(executable)
	require.NoError(t, err)
	digest := sha256.New()
	_, readErr := io.Copy(digest, source)
	require.NoError(t, errors.Join(readErr, source.Close()))
	var config sandboxlinux.WorkerConfig
	config.Tenant = "worker-test"
	config.ImageStore = nativeImageStore(t)
	config.Executable = executable
	config.WatchdogExecutable = os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY")
	watchdogImage, err := os.ReadFile(config.WatchdogExecutable)
	require.NoError(t, err)
	config.WatchdogDigest = sha256.Sum256(watchdogImage)
	config.CgroupParent = parent
	config.Digest = [sha256.Size]byte(digest.Sum(nil))
	process, err := sandboxlinux.LaunchWorker(t.Context(), config)
	if process != nil {
		t.Cleanup(func() {
			assert.NoError(t, process.Close(t.Context()))
		})
	}
	require.NoError(t, err)
	response, err := nativeSourceExchange(process)
	requireWorkerOK(t, err, process, "supervised protocol")
	requireWorkerOK(t, process.Wait(), process, "supervised cleanup")
	require.Equal(t, "ok", response.Code, "unexpected supervised result: %+v", response)
	require.Equal(t, "7", string(response.Value))
	t.Run("broadened imports", func(t *testing.T) {

		for _, tc := range []struct{ pkg, src, want string }{
			{pkg: "strconv", src: "import \"strconv\"\nstrconv.Itoa(42)", want: "\"42\""},
			{pkg: "strings", src: "import \"strings\"\nstrings.ToUpper(\"hi\")", want: "\"HI\""},
		} {
			w, err := sandboxlinux.LaunchWorker(t.Context(), config)
			if w != nil {
				defer w.Close(t.Context())
			}
			require.NoError(t, err, "launch %s", tc.pkg)
			resp, err := Exchange(w, Configuration{Profile: Profile, Imports: []string{tc.pkg}},
				Request{Kind: "expression", Source: tc.src, Entrypoint: ""})
			requireWorkerOK(t, err, w, tc.pkg+" in confined worker")
			require.Equal(t, "ok", resp.Code, "%s result: %+v", tc.pkg, resp)
			require.Equal(t, tc.want, string(resp.Value))
		}
	})
	t.Run("cached image reuse", func(t *testing.T) {

		cache, err := sandboxlinux.StageWorkerImageCache(t.Context(), t.TempDir(),
			[]sandboxlinux.WorkerImageRequest{
				{Executable: config.Executable, Digest: config.Digest},
				{Executable: config.WatchdogExecutable, Digest: config.WatchdogDigest},
			})
		require.NoError(t, err)
		defer cache.Close()
		cached := config
		cached.Cache = cache
		for range 2 {
			w, err := sandboxlinux.LaunchWorker(t.Context(), cached)
			if w != nil {
				defer w.Close(t.Context())
			}
			require.NoError(t, err)
			resp, err := nativeSourceExchange(w)
			requireWorkerOK(t, err, w, "cached worker")
			requireWorkerOK(t, w.Wait(), w, "cached worker cleanup")
			require.Equal(t, "ok", resp.Code, "cached worker result: %+v", resp)
			require.Equal(t, "7", string(resp.Value))
		}
	})
	t.Run("persistent session", func(t *testing.T) { testNativeSession(t, config) })
	t.Run("script output budget", func(t *testing.T) {
		boundedConfig := config
		boundedConfig.OutputBytes = 4
		bounded, err := sandboxlinux.LaunchWorker(t.Context(), boundedConfig)
		if bounded != nil {
			defer bounded.Close(t.Context())
		}
		require.NoError(t, err)
		response, err := Exchange(bounded, Configuration{Profile: Profile, Imports: nil},
			Request{Kind: "expression", Source: "println(\"hello\")", Entrypoint: ""})
		require.ErrorIs(t, err, sandboxlinux.ErrWorkerOutput, "script output escaped host budget: response=%+v", response)
		require.Empty(t, response.Output)
		require.ErrorIs(t, bounded.Wait(), sandboxlinux.ErrWorkerOutput, "worker completion reports the output overflow")
	})
	t.Run("idle phase deadline", func(t *testing.T) {
		idle, err := sandboxlinux.LaunchWorker(t.Context(), config)
		if idle != nil {
			defer idle.Close(t.Context())
		}
		require.NoError(t, err)
		require.NoError(t, idle.SetDeadline(time.Now().Add(50*time.Millisecond)))
		require.ErrorIs(t, idle.Wait(), context.DeadlineExceeded, "an idle worker keeps to its phase deadline")
		entries, err := os.ReadDir(parent)
		require.NoError(t, err)
		for _, entry := range entries {
			require.False(t, strings.HasPrefix(entry.Name(), "pipit-"), "deadline left a worker resource group")
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
	require.NoError(t, os.Chmod(directory, 0700))
	store, err := sandboxbroker.OpenLinuxImageStore(directory, grants, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func requireWorkerOK(t *testing.T, err error, worker *sandboxlinux.WorkerProcess, what string) {
	t.Helper()
	if err != nil {
		require.NoError(t, err, "%s; worker diagnostics: %s", what, worker.Output())
	}
}

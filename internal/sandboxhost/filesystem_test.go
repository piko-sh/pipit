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

package sandboxhost

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxworker"
)

type filesystemAPIFixture struct {
	response sandboxworker.Response
	err      error
	closes   atomic.Int32
}

func (fixture *filesystemAPIFixture) Close(_ context.Context) error {
	fixture.closes.Add(1)
	return fixture.err
}
func (fixture *filesystemAPIFixture) Diagnostics() string { return "bounded diagnostics" }
func (fixture *filesystemAPIFixture) evaluate(_ context.Context, _ sandboxworker.Request) (sandboxworker.Response, error) {
	return fixture.response, fixture.err
}

func TestIsolatedFilesystemAPI(t *testing.T) {
	t.Parallel()
	fixture := &filesystemAPIFixture{
		response: sandboxworker.Response{Output: "ok", Error: "", Code: "ok", Value: nil, CostUsed: 1, OutputTruncated: false},
		err:      nil, closes: atomic.Int32{},
	}
	worker := &IsolatedFilesystemWorker{process: fixture, used: atomic.Bool{}}
	result, err := worker.Eval(t.Context(), "1")
	require.NoError(t, err, "%+v", result)
	require.Equal(t, "ok", result.Output)
	_, err = worker.EvalFile(t.Context(), "package main", "main")
	require.ErrorIs(t, err, ErrIsolatedUsed)
	require.Equal(t, "bounded diagnostics", worker.Diagnostics(), "diagnostics unavailable")
	require.NoError(t, worker.Close(t.Context()))
	failure := errors.New("cleanup failed")
	fixture.err = failure
	worker = &IsolatedFilesystemWorker{process: fixture, used: atomic.Bool{}}
	result, err = worker.Eval(t.Context(), "1")
	require.ErrorIs(t, err, failure, "failed result escaped: %v", result)
	require.Empty(t, result.Output)
	require.Nil(t, result.Value)
	var absent *IsolatedFilesystemWorker
	require.NoError(t, absent.Close(t.Context()))
	require.Empty(t, absent.Diagnostics())
	_, err = absent.Eval(t.Context(), "1")
	require.ErrorIs(t, err, ErrIsolatedUsed)
}

func TestIsolatedFilesystemConfig(t *testing.T) {
	t.Parallel()
	var config IsolatedFilesystemConfig
	config.Worker.WorkerPath = filepath.Join(t.TempDir(), "worker")
	config.Worker.WorkerSHA256 = [sha256.Size]byte{1}
	config.Worker.WatchdogPath = config.Worker.WorkerPath
	config.Worker.WatchdogSHA256 = config.Worker.WorkerSHA256
	config.Worker.Imports = []string{"math"}
	config.BrokerPath = filepath.Join(t.TempDir(), "broker")
	config.BrokerSHA256 = [sha256.Size]byte{2}
	config.Worker.StateDirectory = t.TempDir()
	config.Worker.Tenant = "test-tenant"
	config.Roots = []FilesystemRoot{{Name: "data", HostPath: t.TempDir(), Rights: FilesystemRead}}
	copied := config
	require.NoError(t, validateIsolatedFilesystemConfig(t.Context(), &copied))
	copied.Roots[0].Name = "changed"
	copied.Worker.Imports[0] = "os"
	require.Equal(t, "data", config.Roots[0].Name, "policy aliases caller memory")
	require.Equal(t, "math", config.Worker.Imports[0])
	for _, mutate := range []func(*IsolatedFilesystemConfig){
		func(config *IsolatedFilesystemConfig) { config.Worker.StateDirectory = "" },
		func(config *IsolatedFilesystemConfig) { config.Worker.StateDirectory = "relative" },
		func(config *IsolatedFilesystemConfig) { config.Worker.StateDirectory = config.Roots[0].HostPath },
		func(config *IsolatedFilesystemConfig) {
			config.Worker.StateDirectory = filepath.Join(config.Roots[0].HostPath, "state")
		},
		func(config *IsolatedFilesystemConfig) {
			config.Worker.StateDirectory = filepath.Dir(config.Roots[0].HostPath)
		},
		func(config *IsolatedFilesystemConfig) { config.Worker.Tenant = "" },
		func(config *IsolatedFilesystemConfig) {
			config.Worker.Tenant = strings.Repeat("a", maximumTenant+1)
		},
		func(config *IsolatedFilesystemConfig) { config.Worker.Tenant = "bad\x00tenant" },
		func(config *IsolatedFilesystemConfig) { config.Worker.Tenant = string([]byte{0xff}) },
		func(config *IsolatedFilesystemConfig) { config.BrokerPath += string([]byte{0xff}) },
		func(config *IsolatedFilesystemConfig) { config.Worker.WorkerPath += "\x00" },
		func(config *IsolatedFilesystemConfig) {
			config.Worker.WatchdogPath += strings.Repeat("a", maximumFilesystemHostPath)
		},
		func(config *IsolatedFilesystemConfig) { config.BrokerPath = "relative" },
		func(config *IsolatedFilesystemConfig) { config.BrokerSHA256 = [sha256.Size]byte{} },
		func(config *IsolatedFilesystemConfig) {
			config.Roots = []FilesystemRoot{{Name: "data", HostPath: "relative", Rights: FilesystemRead}}
		},
		func(config *IsolatedFilesystemConfig) {
			config.Roots = []FilesystemRoot{{Name: "data", HostPath: t.TempDir(), Rights: 0}}
		},
		func(config *IsolatedFilesystemConfig) { config.Limits.Calls = -1 },
		func(config *IsolatedFilesystemConfig) { config.Worker.Imports = []string{"pipit/fs"} },
	} {
		invalid := config
		mutate(&invalid)
		_, err := NewIsolatedFilesystemWorker(t.Context(), invalid)
		require.ErrorIs(t, err, ErrInvalidIsolatedConfig, "invalid config accepted")
	}
	_, err := NewIsolatedFilesystemWorker(nil, config)
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = NewIsolatedFilesystemWorker(ctx, config)
	require.ErrorIs(t, err, context.Canceled)
}

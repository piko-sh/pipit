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

package app

import (
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestRestrictedFilesystemProxy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    string
		reply     string
		operation sandboxbroker.Operation
		want      string
		failure   bool
	}{
		{name: "read-initialiser", source: `package main
import "pipit/fs"
var value = load()
func load() string { data, _ := fs.Read("data", "file", 4); return string(data) }
func run() string { return value }`, reply: `{"code":"","data":"dGVzdA==","entries":null,"written":0,"skipped":0}`, operation: sandboxbroker.ReadFile, want: `"test"`, failure: false},
		{name: "write", source: `package main
import "pipit/fs"
func run() bool { return fs.Write("data", "file", []byte("ok")) == nil }`, reply: `{"code":"","data":null,"entries":null,"written":2,"skipped":0}`, operation: sandboxbroker.WriteFile, want: "true", failure: false},
		{name: "list", source: `package main
import "pipit/fs"
func run() string { names, _, _ := fs.List("data", ".", 2); return names[0] }`, reply: `{"code":"","data":null,"entries":["file"],"written":0,"skipped":0}`, operation: sandboxbroker.ListDirectory, want: `"file"`, failure: false},
		{name: "ignored-failure", source: `package main
import "pipit/fs"
func run() bool { _, _ = fs.Read("data", "file", 4); return true }`, reply: `{"code":"io","data":null,"entries":null,"written":0,"skipped":0}`, operation: sandboxbroker.ReadFile, want: "", failure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			granted := new(atomic.Bool)
			proxy, machine, done := restrictedFilesystemFixture(t, tt.operation, tt.reply, granted)
			interpreter, err := NewFilesystemRestrictedInterpreter(RestrictedConfig{Imports: []string{"pipit/fs"}}, proxy)
			require.NoError(t, err)
			result, err := EvaluateRestrictedPhased(t.Context(), interpreter, tt.source, "run", true, func() error {
				if err := machine.Observe(sandboxwire.WorkerToHost, sandboxwire.Message{Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage("{}")}); err != nil {
					return err
				}
				if err := machine.Observe(sandboxwire.HostToWorker, sandboxwire.Message{Kind: sandboxwire.Run, ID: 2, Payload: json.RawMessage("{}")}); err != nil {
					return err
				}
				granted.Store(true)
				return nil
			})
			if tt.failure {
				require.Error(t, err, "a broker error fails the evaluation")
				require.Error(t, proxy.Err())
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, string(result.Value))
			}
			require.NoError(t, <-done)
		})
	}
}

func restrictedFilesystemFixture(t *testing.T, operation sandboxbroker.Operation, reply string,
	granted *atomic.Bool,
) (*sandboxbroker.FilesystemProxy, *sandboxwire.Machine, <-chan error) {
	t.Helper()
	worker, host := net.Pipe()
	t.Cleanup(func() { _ = worker.Close(); _ = host.Close() })
	require.NoError(t, worker.SetDeadline(time.Now().Add(3*time.Second)))
	require.NoError(t, host.SetDeadline(time.Now().Add(3*time.Second)))
	codec, err := sandboxwire.New(worker, worker, sandboxwire.Limits{})
	require.NoError(t, err)
	machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{})
	require.NoError(t, err)
	for _, event := range []struct {
		direction sandboxwire.Direction
		kind      sandboxwire.Kind
		identity  uint64
	}{
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identity: 0},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Configure, identity: 0},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Ready, identity: 0},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identity: 1},
	} {
		require.NoError(t, machine.Observe(event.direction, sandboxwire.Message{Kind: event.kind, ID: event.identity, Payload: json.RawMessage("{}")}))
	}
	grants := []sandboxbroker.RootGrant{{Name: "data", Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List}}
	proxy, err := sandboxbroker.NewFilesystemProxy(codec, machine, grants, sandboxbroker.FilesystemLimits{})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		hostCodec, err := sandboxwire.New(host, host, sandboxwire.Limits{})
		if err != nil {
			done <- err
			return
		}
		message, err := hostCodec.Read()
		if err != nil {
			done <- err
			return
		}
		if !granted.Load() {
			done <- errors.New("filesystem call before execution grant")
			return
		}
		budget, err := sandboxbroker.NewFilesystemBudget(grants, sandboxbroker.FilesystemLimits{})
		if err != nil {
			done <- err
			return
		}
		call, err := budget.Admit(message)
		if err != nil {
			done <- err
			return
		}
		if call.Operation() != operation {
			done <- errors.New("unexpected filesystem operation")
			return
		}
		if err := call.Finish(0); err != nil {
			done <- err
			return
		}
		done <- hostCodec.Write(sandboxwire.Message{Kind: sandboxwire.Reply, ID: message.ID, Payload: json.RawMessage(reply)})
	}()
	return proxy, machine, done
}

func TestOrdinaryRestrictedRejectsFilesystemImport(t *testing.T) {
	t.Parallel()
	_, err := NewRestrictedInterpreter(RestrictedConfig{Imports: []string{"pipit/fs"}})
	require.ErrorIs(t, err, ErrInvalidRestrictedConfig, "an ordinary restricted interpreter refuses the filesystem import")
	_, err = NewFilesystemRestrictedInterpreter(RestrictedConfig{Imports: []string{"pipit/fs"}}, nil)
	require.ErrorIs(t, err, ErrInvalidRestrictedConfig, "the filesystem import needs a proxy")
}

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
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestRestrictedFilesystemProxy(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		reply     string
		operation sandboxbroker.Operation
		want      string
		failure   bool
	}{
		{"read-initialiser", `package main
import "pipit/fs"
var value = load()
func load() string { data, _ := fs.Read("data", "file", 4); return string(data) }
func run() string { return value }`, `{"code":"","data":"dGVzdA==","entries":null,"written":0,"skipped":0}`, sandboxbroker.ReadFile, `"test"`, false},
		{"write", `package main
import "pipit/fs"
func run() bool { return fs.Write("data", "file", []byte("ok")) == nil }`, `{"code":"","data":null,"entries":null,"written":2,"skipped":0}`, sandboxbroker.WriteFile, "true", false},
		{"list", `package main
import "pipit/fs"
func run() string { names, _, _ := fs.List("data", ".", 2); return names[0] }`, `{"code":"","data":null,"entries":["file"],"written":0,"skipped":0}`, sandboxbroker.ListDirectory, `"file"`, false},
		{"ignored-failure", `package main
import "pipit/fs"
func run() bool { _, _ = fs.Read("data", "file", 4); return true }`, `{"code":"io","data":null,"entries":null,"written":0,"skipped":0}`, sandboxbroker.ReadFile, "", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			granted := new(atomic.Bool)
			proxy, machine, done := restrictedFilesystemFixture(t, test.operation, test.reply, granted)
			interpreter, err := NewFilesystemRestrictedInterpreter(RestrictedConfig{Imports: []string{"pipit/fs"}}, proxy)
			if err != nil {
				t.Fatal(err)
			}
			result, err := EvaluateRestrictedPhased(context.Background(), interpreter, test.source, "run", true, func() error {
				if err := machine.Observe(sandboxwire.WorkerToHost, sandboxwire.Message{Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage("{}")}); err != nil {
					return err
				}
				if err := machine.Observe(sandboxwire.HostToWorker, sandboxwire.Message{Kind: sandboxwire.Run, ID: 2, Payload: json.RawMessage("{}")}); err != nil {
					return err
				}
				granted.Store(true)
				return nil
			})
			if test.failure {
				if err == nil || proxy.Err() == nil {
					t.Fatal("ignored broker error produced success")
				}
			} else if err != nil || string(result.Value) != test.want {
				t.Fatalf("result %s: %v", result.Value, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func restrictedFilesystemFixture(t *testing.T, operation sandboxbroker.Operation, reply string,
	granted *atomic.Bool,
) (*sandboxbroker.FilesystemProxy, *sandboxwire.Machine, <-chan error) {
	t.Helper()
	worker, host := net.Pipe()
	t.Cleanup(func() { _ = worker.Close(); _ = host.Close() })
	if err := worker.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := host.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(worker, worker, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		direction sandboxwire.Direction
		kind      sandboxwire.Kind
		identity  uint64
	}{
		{sandboxwire.WorkerToHost, sandboxwire.Hello, 0},
		{sandboxwire.HostToWorker, sandboxwire.Configure, 0},
		{sandboxwire.WorkerToHost, sandboxwire.Ready, 0},
		{sandboxwire.HostToWorker, sandboxwire.Run, 1},
	} {
		if err := machine.Observe(event.direction, sandboxwire.Message{Kind: event.kind, ID: event.identity, Payload: json.RawMessage("{}")}); err != nil {
			t.Fatal(err)
		}
	}
	grants := []sandboxbroker.RootGrant{{Name: "data", Rights: sandboxbroker.Read | sandboxbroker.Write | sandboxbroker.List}}
	proxy, err := sandboxbroker.NewFilesystemProxy(codec, machine, grants, sandboxbroker.FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := NewRestrictedInterpreter(RestrictedConfig{Imports: []string{"pipit/fs"}}); !errors.Is(err, ErrInvalidRestrictedConfig) {
		t.Fatal("ordinary restricted interpreter admitted filesystem import:", err)
	}
	if _, err := NewFilesystemRestrictedInterpreter(RestrictedConfig{Imports: []string{"pipit/fs"}}, nil); !errors.Is(err, ErrInvalidRestrictedConfig) {
		t.Fatal("missing proxy admitted:", err)
	}
}

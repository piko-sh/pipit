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
	"context"
	"encoding/json"
	"errors"
	"testing"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestFilesystemRelayRejectsCompilationCalls(t *testing.T) {
	config := FilesystemConfiguration{
		Profile: FilesystemProfile, Imports: []string{"pipit/fs"}, Roots: []sandboxbroker.RootGrant{},
		Limits: &sandboxbroker.FilesystemLimits{},
	}
	for _, message := range []sandboxwire.Message{
		fixtureMessage(t, sandboxwire.Call, 1, map[string]any{"operation": "fs.read", "root": "data", "path": "file", "max_bytes": 1}),
		fixtureMessage(t, sandboxwire.Result, 1, Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage("1"), CostUsed: 0, OutputTruncated: false}),
	} {
		stream := sourceFixture(t,
			fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": Profile}),
			fixtureMessage(t, sandboxwire.Ready, 0, struct{}{}),
			message,
		)
		session, err := filesystemHandshake(stream, config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.exchangeFilesystem(context.Background(), stream, Request{Kind: "expression", Source: "1", Entrypoint: ""}, nil); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatal("premature capability or execution result accepted:", err)
		}
	}
}

func TestFilesystemRelayRejectsResultWithOutstandingCall(t *testing.T) {
	stream := sourceFixture(t,
		fixtureMessage(t, sandboxwire.Result, 2, Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage("1"), CostUsed: 0, OutputTruncated: false}),
	)
	session, err := newConnection(stream)
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
		{sandboxwire.HostToWorker, sandboxwire.Run, 2},
		{sandboxwire.WorkerToHost, sandboxwire.Call, 1},
	} {
		if err := session.machine.Observe(event.direction, fixtureMessage(t, event.kind, event.identity, struct{}{})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.relayFilesystem(context.Background(), nil); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal("result with outstanding call accepted:", err)
	}
}

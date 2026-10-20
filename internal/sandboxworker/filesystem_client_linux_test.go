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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

func TestFilesystemRelayRejectsCompilationCalls(t *testing.T) {
	t.Parallel()
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
		require.NoError(t, err)
		_, err = session.exchangeFilesystem(t.Context(), stream, Request{Kind: "expression", Source: "1", Entrypoint: ""}, nil)
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "premature capability or execution result accepted")
	}
}

func TestFilesystemRelayRejectsResultWithOutstandingCall(t *testing.T) {
	t.Parallel()
	stream := sourceFixture(t,
		fixtureMessage(t, sandboxwire.Result, 2, Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage("1"), CostUsed: 0, OutputTruncated: false}),
	)
	session, err := newConnection(stream)
	require.NoError(t, err)
	for _, event := range []struct {
		direction sandboxwire.Direction
		kind      sandboxwire.Kind
		identity  uint64
	}{
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identity: 0},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Configure, identity: 0},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Ready, identity: 0},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identity: 2},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identity: 1},
	} {
		require.NoError(t, session.machine.Observe(event.direction, fixtureMessage(t, event.kind, event.identity, struct{}{})))
	}
	_, err = session.relayFilesystem(t.Context(), nil)
	require.ErrorIs(t, err, sandboxwire.ErrProtocol, "result with outstanding call accepted")
}

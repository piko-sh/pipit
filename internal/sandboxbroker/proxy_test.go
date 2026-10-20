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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func proxyFixture(t *testing.T, reply string, identity uint64, rights Rights) (*FilesystemProxy, *bytes.Buffer) {
	t.Helper()
	input, output := new(bytes.Buffer), new(bytes.Buffer)
	writer, err := sandboxwire.New(input, input, sandboxwire.Limits{})
	require.NoError(t, err)
	require.NoError(t, writer.Write(sandboxwire.Message{Kind: sandboxwire.Reply, ID: identity, Payload: json.RawMessage(reply)}))
	codec, err := sandboxwire.New(input, output, sandboxwire.Limits{})
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
	proxy, err := NewFilesystemProxy(codec, machine, []RootGrant{{Name: "data", Rights: rights}}, FilesystemLimits{})
	require.NoError(t, err)
	return proxy, output
}

func TestFilesystemProxyRejectsUntrustedReplies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		payload  string
		identity uint64
	}{
		{payload: `{"code":"","data":"YWJj","entries":null,"written":0,"skipped":0}`, identity: 1},
		{payload: `{"code":"io","data":"YQ==","entries":null,"written":0,"skipped":0}`, identity: 1},
		{payload: `{"code":"","data":[65],"entries":null,"written":0,"skipped":0}`, identity: 1},
		{payload: `{"code":"","data":null,"entries":null,"written":0,"skipped":0}`, identity: 2},
	} {
		proxy, _ := proxyFixture(t, test.payload, test.identity, Read)
		data, err := proxy.Read("data", "file", 1)
		require.Error(t, err, "accepted reply")
		require.Nil(t, data)
		require.Error(t, proxy.Err())
		require.Equal(t, 0, proxy.budget.outstanding, "reservation leaked")
		_, err = proxy.Read("data", "file", 1)
		require.Error(t, err, "failed proxy reused")
	}
}

func TestFilesystemProxyRejectsBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		root   string
		path   string
		limit  int
		rights Rights
	}{
		{root: "data", path: "../file", limit: 1, rights: Read},
		{root: "missing", path: "file", limit: 1, rights: Read},
		{root: "data", path: "file", limit: maximumFileBytes + 1, rights: Read},
		{root: "data", path: "file", limit: 0, rights: Read},
		{root: "data", path: "file", limit: 1, rights: List},
	} {
		proxy, output := proxyFixture(t, `{"code":"","data":null,"entries":null,"written":0,"skipped":0}`, 1, test.rights)
		_, err := proxy.Read(test.root, test.path, test.limit)
		require.Error(t, err, "invalid request admitted")
		require.Equal(t, 0, output.Len(), "invalid request sent")
	}
	var proxy *FilesystemProxy
	require.ErrorIs(t, proxy.Write("data", "file", nil), ErrClosed)
}

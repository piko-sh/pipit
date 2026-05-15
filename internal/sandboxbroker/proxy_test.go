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
	"errors"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

func proxyFixture(t *testing.T, reply string, identity uint64, rights Rights) (*FilesystemProxy, *bytes.Buffer) {
	t.Helper()
	input, output := new(bytes.Buffer), new(bytes.Buffer)
	writer, err := sandboxwire.New(input, input, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(sandboxwire.Message{Kind: sandboxwire.Reply, ID: identity, Payload: json.RawMessage(reply)}); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(input, output, sandboxwire.Limits{})
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
	proxy, err := NewFilesystemProxy(codec, machine, []RootGrant{{Name: "data", Rights: rights}}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	return proxy, output
}

func TestFilesystemProxyRejectsUntrustedReplies(t *testing.T) {
	for _, test := range []struct {
		payload  string
		identity uint64
	}{
		{`{"code":"","data":"YWJj","entries":null,"written":0,"skipped":0}`, 1},
		{`{"code":"io","data":"YQ==","entries":null,"written":0,"skipped":0}`, 1},
		{`{"code":"","data":[65],"entries":null,"written":0,"skipped":0}`, 1},
		{`{"code":"","data":null,"entries":null,"written":0,"skipped":0}`, 2},
	} {
		proxy, _ := proxyFixture(t, test.payload, test.identity, Read)
		data, err := proxy.Read("data", "file", 1)
		if err == nil || data != nil || proxy.Err() == nil {
			t.Fatalf("accepted reply: %q: %v", data, err)
		}
		if proxy.budget.outstanding != 0 {
			t.Fatal("reservation leaked")
		}
		if _, err := proxy.Read("data", "file", 1); err == nil {
			t.Fatal("failed proxy reused")
		}
	}
}

func TestFilesystemProxyRejectsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		root   string
		path   string
		limit  int
		rights Rights
	}{
		{"data", "../file", 1, Read},
		{"missing", "file", 1, Read},
		{"data", "file", maximumFileBytes + 1, Read},
		{"data", "file", 0, Read},
		{"data", "file", 1, List},
	} {
		proxy, output := proxyFixture(t, `{"code":"","data":null,"entries":null,"written":0,"skipped":0}`, 1, test.rights)
		if _, err := proxy.Read(test.root, test.path, test.limit); err == nil {
			t.Fatal("invalid request admitted")
		}
		if output.Len() != 0 {
			t.Fatal("invalid request sent")
		}
	}
	var proxy *FilesystemProxy
	if err := proxy.Write("data", "file", nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

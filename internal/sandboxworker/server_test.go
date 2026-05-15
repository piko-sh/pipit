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
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

type fixtureTransport struct {
	account  *accountingFixture
	input    *bytes.Reader
	output   bytes.Buffer
	deadline time.Time
}

func (stream *fixtureTransport) Read(data []byte) (int, error) {
	return stream.input.Read(data)
}

func (stream *fixtureTransport) Write(data []byte) (int, error) {
	return stream.output.Write(data)
}

func (stream *fixtureTransport) SetDeadline(deadline time.Time) error {
	stream.deadline = deadline
	return nil
}

func (stream *fixtureTransport) ChargeOutput(count int) error {
	return stream.account.ChargeOutput(count)
}

func TestServeSource(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		request Request
		code    string
		value   string
	}{
		{name: "compiled math", request: Request{Kind: "expression", Source: "import \"math\"\nmath.Sqrt(49)", Entrypoint: ""}, code: "ok", value: "7"},
		{name: "file", request: Request{Kind: "file", Source: "package main\nfunc answer() int { return 42 }", Entrypoint: "answer"}, code: "ok", value: "42"},
		{name: "denied import", request: Request{Kind: "expression", Source: "import \"os\"\nos.Getpid()", Entrypoint: ""}, code: "evaluation_failed", value: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := sourceFixture(t,
				fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: Profile, Imports: []string{"math"}}),
				fixtureMessage(t, sandboxwire.Run, 1, test.request),
				fixtureMessage(t, sandboxwire.Run, 2, Request{Kind: "execute", Source: "", Entrypoint: ""}),
			)
			if err := Serve(stream); err != nil {
				t.Fatal(err)
			}
			codec, err := sandboxwire.New(bytes.NewReader(stream.output.Bytes()), io.Discard, sandboxwire.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			kinds := []sandboxwire.Kind{sandboxwire.Hello, sandboxwire.Ready, sandboxwire.Result}
			if test.code == "ok" {
				kinds = append(kinds, sandboxwire.Result)
			}
			for index, kind := range kinds {
				message, err := codec.Read()
				if err != nil || message.Kind != kind {
					t.Fatalf("unexpected response: %+v: %v", message, err)
				}
				if kind != sandboxwire.Result {
					continue
				}
				var response Response
				if err := message.DecodePayload(&response); err != nil {
					t.Fatal(err)
				}
				if test.code == "ok" && index == 2 {
					if message.ID != 1 || response.Code != "compiled" {
						t.Fatalf("unexpected compilation result: %+v", response)
					}
					continue
				}
				identity := uint64(1)
				if test.code == "ok" {
					identity = 2
				}
				if message.ID != identity || response.Code != test.code {
					t.Fatalf("unexpected execution result: %+v", response)
				}
				if test.value != "" && string(response.Value) != test.value {
					t.Fatalf("unexpected scalar result: %s", response.Value)
				}
			}
			if stream.deadline.IsZero() {
				t.Fatal("worker I/O lacks a deadline")
			}
		})
	}
}

func TestServeRejectsInvalidProtocol(t *testing.T) {
	t.Parallel()
	config := fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: Profile, Imports: nil})
	for _, messages := range [][]sandboxwire.Message{
		{fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: "trusted", Imports: nil})},
		{{Kind: sandboxwire.Configure, ID: 0, Payload: json.RawMessage(`{"profile":"restricted-source-v1","raw_exports":true}`)}},
		{fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "expression", Source: "1", Entrypoint: ""})},
		{config, config},
		{config, fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "bytecode", Source: "1", Entrypoint: ""})},
		{config, fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "expression", Source: "1", Entrypoint: "main"})},
		{config, fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "file", Source: "package main", Entrypoint: ""})},
	} {
		if err := Serve(sourceFixture(t, messages...)); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("invalid protocol accepted: %v", err)
		}
	}
}

func TestServeClosesBeforeSource(t *testing.T) {
	t.Parallel()
	stream := sourceFixture(t, fixtureMessage(t, sandboxwire.Close, 0, struct{}{}))
	if err := Serve(stream); err != nil {
		t.Fatal(err)
	}
	if err := Serve(nil); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatalf("nil transport accepted: %v", err)
	}
}

func fixtureMessage(t *testing.T, kind sandboxwire.Kind, identity uint64, payload any) sandboxwire.Message {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return sandboxwire.Message{Kind: kind, ID: identity, Payload: encoded}
}

func sourceFixture(t *testing.T, messages ...sandboxwire.Message) *fixtureTransport {
	t.Helper()
	var buffer bytes.Buffer
	codec, err := sandboxwire.New(bytes.NewReader(nil), &buffer, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if err := codec.Write(message); err != nil {
			t.Fatal(err)
		}
	}
	return &fixtureTransport{account: &accountingFixture{transport: nil, charged: 0, err: nil}, input: bytes.NewReader(buffer.Bytes()), output: bytes.Buffer{}, deadline: time.Time{}}
}

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
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	tests := []struct {
		name    string
		request Request
		code    string
		value   string
	}{
		{name: "compiled math", request: Request{Kind: "expression", Source: "import \"math\"\nmath.Sqrt(49)", Entrypoint: ""}, code: "ok", value: "7"},
		{name: "file", request: Request{Kind: "file", Source: "package main\nfunc answer() int { return 42 }", Entrypoint: "answer"}, code: "ok", value: "42"},
		{name: "denied import", request: Request{Kind: "expression", Source: "import \"os\"\nos.Getpid()", Entrypoint: ""}, code: "evaluation_failed", value: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stream := sourceFixture(t,
				fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: Profile, Imports: []string{"math"}}),
				fixtureMessage(t, sandboxwire.Run, 1, tt.request),
				fixtureMessage(t, sandboxwire.Run, 2, Request{Kind: "execute", Source: "", Entrypoint: ""}),
			)
			require.NoError(t, Serve(t.Context(), stream))
			codec, err := sandboxwire.New(bytes.NewReader(stream.output.Bytes()), io.Discard, sandboxwire.Limits{})
			require.NoError(t, err)
			kinds := []sandboxwire.Kind{sandboxwire.Hello, sandboxwire.Ready, sandboxwire.Result}
			if tt.code == "ok" {
				kinds = append(kinds, sandboxwire.Result)
			}
			for index, kind := range kinds {
				message, err := codec.Read()
				require.NoError(t, err, "unexpected response: %+v", message)
				require.Equal(t, kind, message.Kind)
				if kind != sandboxwire.Result {
					continue
				}
				var response Response
				require.NoError(t, message.DecodePayload(&response))
				if tt.code == "ok" && index == 2 {
					require.Equal(t, uint64(1), message.ID, "unexpected compilation result: %+v", response)
					require.Equal(t, "compiled", response.Code)
					continue
				}
				identity := uint64(1)
				if tt.code == "ok" {
					identity = 2
				}
				require.Equal(t, identity, message.ID, "unexpected execution result: %+v", response)
				require.Equal(t, tt.code, response.Code)
				if tt.value != "" {
					require.Equal(t, tt.value, string(response.Value), "scalar result")
				}
			}
			require.False(t, stream.deadline.IsZero(), "worker I/O lacks a deadline")
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
		require.ErrorIs(t, Serve(t.Context(), sourceFixture(t, messages...)), sandboxwire.ErrProtocol, "invalid protocol accepted")
	}
}

func TestServeClosesBeforeSource(t *testing.T) {
	t.Parallel()
	stream := sourceFixture(t, fixtureMessage(t, sandboxwire.Close, 0, struct{}{}))
	require.NoError(t, Serve(t.Context(), stream))
	require.ErrorIs(t, Serve(t.Context(), nil), sandboxwire.ErrProtocol, "nil transport accepted")
}

func fixtureMessage(t *testing.T, kind sandboxwire.Kind, identity uint64, payload any) sandboxwire.Message {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	return sandboxwire.Message{Kind: kind, ID: identity, Payload: encoded}
}

func sourceFixture(t *testing.T, messages ...sandboxwire.Message) *fixtureTransport {
	t.Helper()
	var buffer bytes.Buffer
	codec, err := sandboxwire.New(bytes.NewReader(nil), &buffer, sandboxwire.Limits{})
	require.NoError(t, err)
	for _, message := range messages {
		require.NoError(t, codec.Write(message))
	}
	return &fixtureTransport{account: &accountingFixture{transport: nil, charged: 0, err: nil}, input: bytes.NewReader(buffer.Bytes()), output: bytes.Buffer{}, deadline: time.Time{}}
}

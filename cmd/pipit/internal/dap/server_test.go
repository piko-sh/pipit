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

package dap

import (
	"bufio"
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/google/go-dap"
	"github.com/stretchr/testify/require"
)

func TestServerLoopHandshakeAndDisconnect(t *testing.T) {
	t.Parallel()
	serverFromClient, clientToServer := io.Pipe()
	defer func() { _ = clientToServer.Close() }()

	output := &threadSafeBuffer{}

	srv := newServer(ServerOptions{
		Stdin:  serverFromClient,
		Stdout: output,
		Stderr: &bytes.Buffer{},
	})

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- srv.serve(t.Context())
	}()

	mustSendRequest(t, clientToServer, 1, "initialize")
	mustSendRequest(t, clientToServer, 2, "disconnect")

	if err := <-loopDone; err != nil {
		require.ErrorIs(t, err, io.EOF, "serve stops cleanly")
	}

	messages := readMessagesFromBuffer(t, output.Bytes())
	require.GreaterOrEqual(t, len(messages), 3)
	require.IsType(t, &dap.InitializeResponse{}, messages[0])
	require.IsType(t, &dap.InitializedEvent{}, messages[1])
	lastResponse, ok := messages[len(messages)-1].(*dap.DisconnectResponse)
	require.True(t, ok, "the message is a *dap.DisconnectResponse, not %T", messages[len(messages)-1])
	require.True(t, lastResponse.Success, "disconnect succeeds")
}

func mustSendRequest(t *testing.T, w io.Writer, seq int, command string) {
	t.Helper()
	var request dap.Message
	switch command {
	case "initialize":
		request = &dap.InitializeRequest{
			Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: seq, Type: "request"}, Command: command},
		}
	case "disconnect":
		request = &dap.DisconnectRequest{
			Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: seq, Type: "request"}, Command: command},
		}
	default:
		require.Failf(t, "unknown command", "command %q is not wired in this helper", command)
	}
	require.NoError(t, dap.WriteProtocolMessage(w, request), "write request %q", command)
}

func readMessagesFromBuffer(t *testing.T, data []byte) []dap.Message {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(data))
	var messages []dap.Message
	for {
		if _, err := reader.Peek(1); err != nil {
			break
		}
		message, err := dap.ReadProtocolMessage(reader)
		require.NoError(t, err)
		messages = append(messages, message)
	}
	return messages
}

type threadSafeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *threadSafeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

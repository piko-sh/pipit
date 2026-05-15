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
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/google/go-dap"
)

func TestServerLoopHandshakeAndDisconnect(t *testing.T) {
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

	if err := <-loopDone; err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("serve returned %v", err)
	}

	messages := readMessagesFromBuffer(t, output.Bytes())
	if got := len(messages); got < 3 {
		t.Fatalf("messages: got %d, want >=3 (initialize response + initialized event + disconnect response)", got)
	}
	if _, ok := messages[0].(*dap.InitializeResponse); !ok {
		t.Fatalf("first message: got %T, want *dap.InitializeResponse", messages[0])
	}
	if _, ok := messages[1].(*dap.InitializedEvent); !ok {
		t.Fatalf("second message: got %T, want *dap.InitializedEvent", messages[1])
	}
	lastResponse, ok := messages[len(messages)-1].(*dap.DisconnectResponse)
	if !ok {
		t.Fatalf("last message: got %T, want *dap.DisconnectResponse", messages[len(messages)-1])
	}
	if !lastResponse.Success {
		t.Fatalf("disconnect success: got false, want true")
	}
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
		t.Fatalf("mustSendRequest: command %q not wired in this helper", command)
	}
	if err := dap.WriteProtocolMessage(w, request); err != nil {
		t.Fatalf("write request %q: %v", command, err)
	}
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
		if err != nil {
			t.Fatalf("ReadProtocolMessage: %v", err)
		}
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

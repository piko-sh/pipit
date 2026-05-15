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

package sandboxbroker

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

type clientTestTransport struct {
	net.Conn
	done   <-chan struct{}
	closes atomic.Int32
}

func (stream *clientTestTransport) Wait() error { <-stream.done; return nil }
func (stream *clientTestTransport) Close() error {
	stream.closes.Add(1)
	_ = stream.Conn.Close()
	<-stream.done
	return nil
}

func maliciousBrokerFixture(t *testing.T, payload string, identity uint64, stall bool) (*FilesystemClient, <-chan struct{}) {
	t.Helper()
	host, peer := net.Pipe()
	done, requested := make(chan struct{}), make(chan struct{})
	stream := &clientTestTransport{Conn: host, done: done, closes: atomic.Int32{}}
	go func() {
		defer close(done)
		defer peer.Close()
		connection, err := newFilesystemConnection(peer)
		if err != nil {
			return
		}
		if err := connection.send(sandboxwire.Hello, 0, FilesystemConfiguration{Profile: filesystemProfile}); err != nil {
			return
		}
		if _, err := connection.receive(); err != nil {
			return
		}
		if err := connection.send(sandboxwire.Ready, 0, struct{}{}); err != nil {
			return
		}
		if _, err := connection.receive(); err != nil {
			return
		}
		close(requested)
		if !stall {
			_ = connection.codec.Write(sandboxwire.Message{Kind: sandboxwire.Result, ID: identity, Payload: []byte(payload)})
		}
		_, _ = connection.codec.Read()
	}()
	client, err := OpenFilesystemClient(context.Background(), stream, []RootGrant{{Name: "data", Rights: Read}}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, requested
}

func TestFilesystemClientRejectsReplies(t *testing.T) {
	for _, payload := range []string{
		`{"code":"","data":"YWJj","entries":null,"written":0,"skipped":0}`,
		`{"code":"io","data":"YQ==","entries":null,"written":0,"skipped":0}`,
		`{"code":"","data":[65],"entries":null,"written":0,"skipped":0}`,
		`{"code":"","data":null,"entries":null,"written":null,"skipped":0}`,
	} {
		client, _ := maliciousBrokerFixture(t, payload, 1, false)
		response, err := client.Execute(context.Background(), []byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`))
		if !errors.Is(err, sandboxwire.ErrProtocol) || response.Data != nil || !client.closed.Load() {
			t.Fatalf("untrusted reply accepted: %+v: %v", response, err)
		}
		if client.budget.outstanding != 0 {
			t.Fatal("reservation leaked")
		}
	}
	client, _ := maliciousBrokerFixture(t, `{"code":"","data":"YQ==","entries":null,"written":0,"skipped":0}`, 2, false)
	if _, err := client.Execute(context.Background(), []byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`)); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal("mismatched identity accepted:", err)
	}
}

func TestFilesystemClientCancellationAndSingleFlight(t *testing.T) {
	client, requested := maliciousBrokerFixture(t, "", 1, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := client.Execute(ctx, []byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`))
		result <- err
	}()
	<-requested
	if _, err := client.Execute(context.Background(), []byte("{}")); !errors.Is(err, errBusy) {
		t.Fatal("concurrent call queued:", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost:", err)
	}
	if client.budget.outstanding != 0 {
		t.Fatal("cancelled reservation leaked")
	}
	if !client.closed.Load() {
		t.Fatal("cancelled client remains open")
	}
}

func TestFilesystemClientHostDenialBeforeSend(t *testing.T) {
	client, requested := maliciousBrokerFixture(t, "", 1, true)
	_, err := client.Execute(context.Background(), []byte(`{"operation":"fs.write","root":"data","path":"file","data":""}`))
	if !errors.Is(err, ErrDenied) {
		t.Fatal("host authority not enforced:", err)
	}
	select {
	case <-requested:
		t.Fatal("denied request reached broker")
	default:
	}
	if !client.closed.Load() || client.budget.outstanding != 0 {
		t.Fatal("denial did not close cleanly")
	}
}

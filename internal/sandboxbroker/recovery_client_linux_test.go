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
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

type recoveryTestTransport struct {
	*clientTestTransport
	waitErr  error
	closeErr error
}

func (stream *recoveryTestTransport) Wait() error {
	return errors.Join(stream.clientTestTransport.Wait(), stream.waitErr)
}

func (stream *recoveryTestTransport) Close() error {
	return errors.Join(stream.clientTestTransport.Close(), stream.closeErr)
}

func TestRecoveryExchangeRejectsInvalidResultsAndCleanup(t *testing.T) {
	for _, mode := range []string{"success", "profile", "call", "payload", "identity", "wait", "close", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			stream, requested := recoveryExchangeFixture(t, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- ExchangeFilesystemRecovery(ctx, stream) }()
			if mode == "cancel" {
				<-requested
				cancel()
			}
			err := <-result
			if (err == nil) != (mode == "success") {
				t.Fatal("incorrect recovery result:", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("recovery lost cancellation:", err)
			}
			if mode == "wait" && !errors.Is(err, stream.waitErr) {
				t.Fatal("recovery lost process failure:", err)
			}
			if mode == "close" && !errors.Is(err, stream.closeErr) {
				t.Fatal("recovery lost cleanup failure:", err)
			}
			if stream.closes.Load() == 0 {
				t.Fatal("recovery did not close its process")
			}
		})
	}
}

func recoveryExchangeFixture(t *testing.T, mode string) (*recoveryTestTransport, <-chan struct{}) {
	t.Helper()
	host, peer := net.Pipe()
	done, requested := make(chan struct{}), make(chan struct{})
	stream := &recoveryTestTransport{
		clientTestTransport: &clientTestTransport{Conn: host, done: done, closes: atomic.Int32{}},
		waitErr:             nil, closeErr: nil,
	}
	if mode == "wait" {
		stream.waitErr = errors.New("recovery process failure")
	}
	if mode == "close" {
		stream.closeErr = errors.New("recovery cleanup failure")
	}
	go func() {
		defer close(done)
		defer peer.Close()
		connection, err := newFilesystemConnection(peer)
		if err != nil {
			return
		}
		profile := FilesystemRecoveryProfile
		if mode == "profile" {
			profile = filesystemProfile
		}
		if err := connection.send(sandboxwire.Hello, 0, FilesystemConfiguration{Profile: profile}); err != nil {
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
		response := sandboxwire.Message{Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage("{}")}
		switch mode {
		case "call":
			response.Kind = sandboxwire.Call
		case "payload":
			response.Payload = json.RawMessage(`{"extra":true}`)
		case "identity":
			response.ID = 2
		case "cancel":
			_, _ = connection.codec.Read()
			return
		}
		_ = connection.codec.Write(response)
	}()
	t.Cleanup(func() { _ = stream.Close() })
	return stream, requested
}

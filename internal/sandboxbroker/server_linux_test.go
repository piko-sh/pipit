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
	"errors"
	"net"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

func filesystemProtocolFixture(t *testing.T) (*sandboxwire.Codec, <-chan error) {
	t.Helper()
	backend := openTestFilesystem(t, t.TempDir(), Read, FilesystemLimits{})
	backend.sealed = true
	host, child := net.Pipe()
	if err := host.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	codec, err := sandboxwire.New(host, host, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- ServeFilesystemBroker(child, backend); _ = child.Close() }()
	t.Cleanup(func() { _ = host.Close(); _ = child.Close() })
	hello, err := codec.Read()
	if err != nil || hello.Kind != sandboxwire.Hello {
		t.Fatalf("hello: %+v: %v", hello, err)
	}
	var profile FilesystemConfiguration
	if err := hello.DecodePayload(&profile); err != nil || profile.Profile != filesystemProfile {
		t.Fatal("incorrect profile:", err)
	}
	return codec, done
}

func TestFilesystemProtocolRejectsAuthorityChanges(t *testing.T) {
	for _, payload := range []string{
		`{"profile":"wrong"}`,
		`{"profile":"filesystem-broker-v1","roots":[]}`,
		`{"profile":"filesystem-broker-v1","limits":{}}`,
		`{"Profile":"filesystem-broker-v1"}`,
	} {
		codec, done := filesystemProtocolFixture(t)
		err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: []byte(payload)})
		if err == nil {
			_, err = codec.Read()
		}
		if err == nil {
			t.Fatal("invalid configuration accepted:", payload)
		}
		if err := <-done; err == nil {
			t.Fatal("server ignored configuration violation")
		}
	}
}

func TestFilesystemProtocolDeniedAndSanitised(t *testing.T) {
	codec, done := filesystemProtocolFixture(t)
	if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: []byte(`{"profile":"filesystem-broker-v1"}`)}); err != nil {
		t.Fatal(err)
	}
	ready, err := codec.Read()
	if err != nil || ready.Kind != sandboxwire.Ready {
		t.Fatal("ready:", err)
	}
	if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Run, ID: 1,
		Payload: []byte(`{"operation":"fs.read","root":"missing","path":"secret","max_bytes":1}`)}); err != nil {
		t.Fatal(err)
	}
	reply, err := codec.Read()
	if err != nil || reply.Kind != sandboxwire.Result || reply.ID != 1 {
		t.Fatal("result:", err)
	}
	var response FilesystemResponse
	if err := reply.DecodePayload(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "denied" || response.Data != nil || response.Entries != nil || response.Written != 0 {
		t.Fatalf("unsanitised failure: %+v", response)
	}
	if err := <-done; !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestFilesystemProtocolClose(t *testing.T) {
	codec, done := filesystemProtocolFixture(t)
	if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Close, ID: 0, Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemProtocolRequiresSealing(t *testing.T) {
	backend := openTestFilesystem(t, t.TempDir(), Read, FilesystemLimits{})
	host, child := net.Pipe()
	defer host.Close()
	defer child.Close()
	if err := ServeFilesystemBroker(child, backend); !errors.Is(err, ErrConfinement) {
		t.Fatal("unsealed server started:", err)
	}
	backend.sealed = true
	if err := backend.beginProtocol(); err != nil {
		t.Fatal(err)
	}
	if err := backend.beginProtocol(); !errors.Is(err, ErrClosed) {
		t.Fatal("duplicate protocol owner:", err)
	}
}

func TestFilesystemResponseSanitisation(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{errLimit, "limit"}, {errBusy, "limit"}, {ErrClosed, "closed"}, {ErrConfinement, "closed"}, {errors.New("private path and secret"), "io"},
	} {
		response := filesystemResponse(FilesystemResult{Data: []byte("secret"), Entries: []string{"secret"}, Written: 6, skipped: 0}, test.err)
		if response.Code != test.code || response.Data != nil || response.Entries != nil || response.Written != 0 {
			t.Fatal("partial result or native error escaped:", response)
		}
	}
}

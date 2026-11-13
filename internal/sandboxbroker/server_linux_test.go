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

//go:build linux && (amd64 || arm64)

package sandboxbroker

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func filesystemProtocolFixture(t *testing.T) (*sandboxwire.Codec, <-chan error) {
	t.Helper()
	backend := openTestFilesystem(t, t.TempDir(), Read, FilesystemLimits{})
	backend.sealed = true
	host, child := net.Pipe()
	require.NoError(t, host.SetDeadline(time.Now().Add(3*time.Second)))
	codec, err := sandboxwire.New(host, host, sandboxwire.Limits{})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- ServeFilesystemBroker(child, backend); _ = child.Close() }()
	t.Cleanup(func() { _ = host.Close(); _ = child.Close() })
	hello, err := codec.Read()
	require.NoError(t, err, "hello: %+v", hello)
	require.Equal(t, sandboxwire.Hello, hello.Kind)
	var profile FilesystemConfiguration
	require.NoError(t, hello.DecodePayload(&profile), "incorrect profile")
	require.Equal(t, filesystemProfile, profile.Profile)
	return codec, done
}

func TestFilesystemProtocolRejectsAuthorityChanges(t *testing.T) {
	t.Parallel()
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
		require.Error(t, err, "invalid configuration accepted: %v", payload)
		require.Error(t, <-done, "server ignored configuration violation")
	}
}

func TestFilesystemProtocolDeniedAndSanitised(t *testing.T) {
	t.Parallel()
	codec, done := filesystemProtocolFixture(t)
	require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: []byte(`{"profile":"filesystem-broker-v1"}`)}))
	ready, err := codec.Read()
	require.NoError(t, err, "ready")
	require.Equal(t, sandboxwire.Ready, ready.Kind)
	require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Run, ID: 1,
		Payload: []byte(`{"operation":"fs.read","root":"missing","path":"secret","max_bytes":1}`)}))
	reply, err := codec.Read()
	require.NoError(t, err, "result")
	require.Equal(t, sandboxwire.Result, reply.Kind)
	require.Equal(t, uint64(1), reply.ID)
	var response FilesystemResponse
	require.NoError(t, reply.DecodePayload(&response))
	require.Equal(t, "denied", response.Code, "unsanitised failure: %+v", response)
	require.Nil(t, response.Data)
	require.Nil(t, response.Entries)
	require.Equal(t, 0, response.Written)
	require.ErrorIs(t, <-done, ErrDenied)
}

func TestFilesystemProtocolClose(t *testing.T) {
	t.Parallel()
	codec, done := filesystemProtocolFixture(t)
	require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Close, ID: 0, Payload: []byte("{}")}))
	require.NoError(t, <-done)
}

func TestFilesystemProtocolRequiresSealing(t *testing.T) {
	t.Parallel()
	backend := openTestFilesystem(t, t.TempDir(), Read, FilesystemLimits{})
	host, child := net.Pipe()
	defer host.Close()
	defer child.Close()
	require.ErrorIs(t, ServeFilesystemBroker(child, backend), ErrConfinement, "unsealed server started")
	backend.sealed = true
	require.NoError(t, backend.beginProtocol())
	require.ErrorIs(t, backend.beginProtocol(), ErrClosed, "duplicate protocol owner")
}

func TestFilesystemResponseSanitisation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err  error
		code string
	}{
		{err: errLimit, code: "limit"}, {err: errBusy, code: "limit"}, {err: ErrClosed, code: "closed"}, {err: ErrConfinement, code: "closed"}, {err: errors.New("private path and secret"), code: "io"},
	} {
		response := filesystemResponse(FilesystemResult{Data: []byte("secret"), Entries: []string{"secret"}, Written: 6, skipped: 0}, test.err)
		require.Equal(t, test.code, response.Code, "partial result or native error escaped: %v", response)
		require.Nil(t, response.Data)
		require.Nil(t, response.Entries)
		require.Equal(t, 0, response.Written)
	}
}

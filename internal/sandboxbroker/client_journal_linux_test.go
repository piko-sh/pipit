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
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestJournalledClientRejectsUntrustedPreparation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing", "root", "namespace", "identity", "malformed", "duplicate", "read"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			authority := testRecoveryAuthority(t, Read|Write)
			journal := testRecoveryAuthorityJournal(t, authority)
			record := testRecoveryAuthorityRecord(t, authority, 1)
			switch mode {
			case "root":
				record.Root = "other"
			case "namespace":
				record.Namespace = "invalid"
			}
			encoded, err := json.Marshal(record)
			require.NoError(t, err)
			if mode == "malformed" {
				encoded = []byte("{}")
			}
			host, peer := net.Pipe()
			done := make(chan struct{})
			var acknowledgements atomic.Int32
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
				if mode == "missing" {
					_ = connection.send(sandboxwire.Result, 1, FilesystemResponse{Code: "", Data: nil, Entries: nil, Written: 3, Skipped: 0})
				} else {
					identity := uint64(1)
					if mode == "identity" {
						identity = 2
					}
					if err := connection.codec.Write(sandboxwire.Message{Kind: sandboxwire.Call, ID: identity, Payload: encoded}); err != nil {
						return
					}
					ack, err := connection.codec.Read()
					if err != nil {
						return
					}
					if ack.Kind == sandboxwire.Reply {
						acknowledgements.Add(1)
					}
					if mode == "duplicate" {
						_ = connection.codec.Write(sandboxwire.Message{Kind: sandboxwire.Call, ID: 2, Payload: encoded})
					}
				}
				_, _ = connection.codec.Read()
			}()
			stream := &clientTestTransport{Conn: host, done: done, closes: atomic.Int32{}}
			client, err := OpenJournalledFilesystemClient(t.Context(), stream, authority, journal)
			require.NoError(t, err)
			defer client.Close()
			payload := recoveryWritePayload
			if mode == "read" {
				payload = readPayload
			}
			response, err := client.Execute(t.Context(), []byte(payload))
			require.Error(t, err, "untrusted preparation accepted: %v", response)
			require.Equal(t, 0, response.Written)
			require.Nil(t, response.Data)
			require.True(t, client.closed.Load())
			expected := int32(0)
			if mode == "duplicate" {
				expected = 1
			}
			require.Equal(t, expected, acknowledgements.Load())
			records, err := journal.Records()
			require.NoError(t, err, "invalid preparation changed accounting")
			require.Len(t, records, int(expected))
			require.Equal(t, 0, authority.budget.outstanding)
		})
	}
}

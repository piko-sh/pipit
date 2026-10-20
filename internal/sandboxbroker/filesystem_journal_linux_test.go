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
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestJournalledBrokerWaitsForAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, acknowledgement := range []string{"valid", "wrong-id", "wrong-kind", "extra-field", "null", "close", "disconnect"} {
		t.Run(acknowledgement, func(t *testing.T) {
			t.Parallel()
			stream, authority, journal, root, result := journalledBrokerFixture(t)
			codec, err := sandboxwire.New(stream, stream, sandboxwire.Limits{})
			require.NoError(t, err)
			_, err = codec.Read()
			require.NoError(t, err)
			configuration, err := json.Marshal(FilesystemConfiguration{Profile: JournalledFilesystemProfile})
			require.NoError(t, err)
			require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: configuration}))
			_, err = codec.Read()
			require.NoError(t, err)
			require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Run, ID: 1, Payload: []byte(recoveryWritePayload)}))
			prepared, err := codec.Read()
			require.NoError(t, err, "missing preparation: %v", prepared)
			require.Equal(t, sandboxwire.Call, prepared.Kind)
			require.Equal(t, uint64(1), prepared.ID)
			assertJournalledTarget(t, root, "original")
			entries, err := os.ReadDir(root)
			require.NoError(t, err, "preparation published a staging name")
			require.Len(t, entries, 1)
			ack := sandboxwire.Message{Kind: sandboxwire.Reply, ID: 1, Payload: []byte("{}")}
			switch acknowledgement {
			case "valid":
				record, err := decodeRecoveryRecord(prepared.Payload)
				require.NoError(t, err)
				call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
				require.NoError(t, err)
				defer call.Finish(0)
				require.NoError(t, authority.record(journal, call, record))
			case "wrong-id":
				ack.ID = 2
			case "wrong-kind":
				ack.Kind = sandboxwire.Run
			case "extra-field":
				ack.Payload = []byte(`{"commit":true}`)
			case "null":
				ack.Payload = []byte("null")
			case "close":
				ack.Kind, ack.ID = sandboxwire.Close, 0
			case "disconnect":
				_ = stream.Conn.Close()
			}
			if acknowledgement != "disconnect" {
				writeErr := codec.Write(ack)
				if acknowledgement == "valid" {
					require.NoError(t, writeErr)
				}
			}
			if acknowledgement == "valid" {
				reply, err := codec.Read()
				require.NoError(t, err, "publication failed: %v", reply)
				require.Equal(t, sandboxwire.Result, reply.Kind)
				assertJournalledTarget(t, root, "new")
				require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Close, ID: 0, Payload: []byte("{}")}))
				require.NoError(t, <-result)
			} else {
				_, _ = codec.Read()
				require.Error(t, <-result, "invalid acknowledgement accepted")
				assertJournalledTarget(t, root, "original")
			}
			entries, err = os.ReadDir(root)
			require.NoError(t, err, "protocol failure leaked a staging name")
			require.Len(t, entries, 1)
			require.Equal(t, "file", entries[0].Name())
		})
	}
}

func TestJournalledClientPublishesOnlyAfterRecording(t *testing.T) {
	t.Parallel()
	for _, failure := range []bool{false, true} {
		stream, authority, journal, root, result := journalledBrokerFixture(t)
		client, err := OpenJournalledFilesystemClient(t.Context(), stream, authority, journal)
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		if failure {
			require.NoError(t, journal.Close())
		}
		response, err := client.Execute(t.Context(), []byte(recoveryWritePayload))
		if failure {
			require.Error(t, err, "failed journal authorised publication: %v", response)
			require.Equal(t, 0, response.Written)
			assertJournalledTarget(t, root, "original")
			require.Error(t, <-result, "broker ignored acknowledgement failure")
			continue
		}
		require.NoError(t, err, "journalled write failed: %v", response)
		require.Equal(t, 3, response.Written)
		records, err := journal.Records()
		require.NoError(t, err, "successful write has no matching intent")
		require.Len(t, records, 1)
		require.Equal(t, "file", records[0].Path)
		require.Equal(t, int64(3), records[0].Size)
		assertJournalledTarget(t, root, "new")
		response, err = client.Execute(t.Context(), []byte(readPayload))
		require.NoError(t, err, "read after journalled write failed: %v", response)
		require.Equal(t, "new", string(response.Data))
		require.NoError(t, client.Close())
		require.NoError(t, <-result)
	}
}

func journalledBrokerFixture(t *testing.T) (*clientTestTransport, *LinuxRecoveryAuthority, *LinuxRecoveryJournal, string, <-chan error) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600))
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read | Write | List}}, FilesystemLimits{})
	require.NoError(t, err)
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	require.NoError(t, err)
	t.Cleanup(func() { _ = authority.Close() })
	journal := testRecoveryAuthorityJournal(t, authority)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	require.NoError(t, err)
	backend.sealed = true
	host, peer := net.Pipe()
	require.NoError(t, host.SetDeadline(time.Now().Add(5*time.Second)))
	done, result := make(chan struct{}), make(chan error, 1)
	go func() {
		defer close(done)
		defer peer.Close()
		defer backend.Close()
		result <- ServeFilesystemBroker(peer, backend)
	}()
	stream := &clientTestTransport{Conn: host, done: done, closes: atomic.Int32{}}
	t.Cleanup(func() { _ = stream.Close() })
	return stream, authority, journal, root, result
}

func assertJournalledTarget(t *testing.T, root, expected string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "file"))
	require.NoError(t, err, "unexpected publication state")
	require.Equal(t, expected, string(data))
}

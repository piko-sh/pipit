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
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestJournalledBrokerWaitsForAcknowledgement(t *testing.T) {
	for _, acknowledgement := range []string{"valid", "wrong-id", "wrong-kind", "extra-field", "null", "close", "disconnect"} {
		t.Run(acknowledgement, func(t *testing.T) {
			stream, authority, journal, root, result := journalledBrokerFixture(t)
			codec, err := sandboxwire.New(stream, stream, sandboxwire.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.Read(); err != nil {
				t.Fatal(err)
			}
			configuration, err := json.Marshal(FilesystemConfiguration{Profile: JournalledFilesystemProfile})
			if err != nil {
				t.Fatal(err)
			}
			if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: configuration}); err != nil {
				t.Fatal(err)
			}
			if _, err := codec.Read(); err != nil {
				t.Fatal(err)
			}
			if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Run, ID: 1, Payload: []byte(recoveryWritePayload)}); err != nil {
				t.Fatal(err)
			}
			prepared, err := codec.Read()
			if err != nil || prepared.Kind != sandboxwire.Call || prepared.ID != 1 {
				t.Fatal("missing preparation:", prepared, err)
			}
			assertJournalledTarget(t, root, "original")
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 {
				t.Fatal("preparation published a staging name:", entries, err)
			}
			ack := sandboxwire.Message{Kind: sandboxwire.Reply, ID: 1, Payload: []byte("{}")}
			switch acknowledgement {
			case "valid":
				record, err := decodeRecoveryRecord(prepared.Payload)
				if err != nil {
					t.Fatal(err)
				}
				call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
				if err != nil {
					t.Fatal(err)
				}
				defer call.Finish(0)
				if err := authority.record(journal, call, record); err != nil {
					t.Fatal(err)
				}
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
				if err := codec.Write(ack); err != nil && acknowledgement == "valid" {
					t.Fatal(err)
				}
			}
			if acknowledgement == "valid" {
				reply, err := codec.Read()
				if err != nil || reply.Kind != sandboxwire.Result {
					t.Fatal("publication failed:", reply, err)
				}
				assertJournalledTarget(t, root, "new")
				if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Close, ID: 0, Payload: []byte("{}")}); err != nil {
					t.Fatal(err)
				}
				if err := <-result; err != nil {
					t.Fatal(err)
				}
			} else {
				_, _ = codec.Read()
				if err := <-result; err == nil {
					t.Fatal("invalid acknowledgement accepted")
				}
				assertJournalledTarget(t, root, "original")
			}
			entries, err = os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
				t.Fatal("protocol failure leaked a staging name:", entries, err)
			}
		})
	}
}

func TestJournalledClientPublishesOnlyAfterRecording(t *testing.T) {
	for _, failure := range []bool{false, true} {
		stream, authority, journal, root, result := journalledBrokerFixture(t)
		client, err := OpenJournalledFilesystemClient(context.Background(), stream, authority, journal)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		if failure {
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
		}
		response, err := client.Execute(context.Background(), []byte(recoveryWritePayload))
		if failure {
			if err == nil || response.Written != 0 {
				t.Fatal("failed journal authorised publication:", response, err)
			}
			assertJournalledTarget(t, root, "original")
			if err := <-result; err == nil {
				t.Fatal("broker ignored acknowledgement failure")
			}
			continue
		}
		if err != nil || response.Written != 3 {
			t.Fatal("journalled write failed:", response, err)
		}
		records, err := journal.Records()
		if err != nil || len(records) != 1 || records[0].Path != "file" || records[0].Size != 3 {
			t.Fatal("successful write has no matching intent:", records, err)
		}
		assertJournalledTarget(t, root, "new")
		response, err = client.Execute(context.Background(), []byte(readPayload))
		if err != nil || string(response.Data) != "new" {
			t.Fatal("read after journalled write failed:", response, err)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
}

func journalledBrokerFixture(t *testing.T) (*clientTestTransport, *LinuxRecoveryAuthority, *LinuxRecoveryJournal, string, <-chan error) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: root, Rights: Read | Write | List}}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	journal := testRecoveryAuthorityJournal(t, authority)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	if err != nil {
		t.Fatal(err)
	}
	backend.sealed = true
	host, peer := net.Pipe()
	if err := host.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
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
	if err != nil || string(data) != expected {
		t.Fatal("unexpected publication state:", string(data), err)
	}
}

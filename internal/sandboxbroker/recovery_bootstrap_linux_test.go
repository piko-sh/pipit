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
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestRecoveryBootstrapRequiresClosedAdmission(t *testing.T) {
	authority := testRecoveryAuthority(t, Read|Write|List)
	record := testRecoveryAuthorityRecord(t, authority, 1)
	journal := testRecoveryAuthorityJournal(t, authority)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.record(journal, call, record); err != nil {
		t.Fatal(err)
	}
	if bootstrap, err := authority.RecoveryBootstrap(journal); err == nil || bootstrap != nil {
		t.Fatal("live admission allowed recovery")
	}
	authority.budget.Close()
	if bootstrap, err := authority.RecoveryBootstrap(journal); err == nil || bootstrap != nil {
		t.Fatal("outstanding reservation allowed recovery")
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := authority.RecoveryBootstrap(journal)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	if _, err := bootstrap.RecoveryAuthority(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("recovery policy created new execution authority:", err)
	}
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := readLinuxBootstrap(files[0])
	if err != nil || policy.Profile != filesystemRecoveryBootstrapProfile || len(policy.Recovery) != 1 ||
		len(policy.Roots) != 1 || policy.Roots[0].Rights != Write {
		t.Fatal("recovery bootstrap failed to narrow authority:", policy, err)
	}
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Execute(brokerMessage(1, recoveryWritePayload)); !errors.Is(err, ErrDenied) {
		t.Fatal("recovery-only backend executed script write:", err)
	}
	if err := backend.recover(); !errors.Is(err, ErrClosed) {
		t.Fatal("unconfined recovery allowed:", err)
	}
	backend.sealed = true
	if err := backend.beginProtocol(); err != nil {
		t.Fatal(err)
	}
	if err := backend.recover(); err != nil {
		t.Fatal("sealed absent-remnant cleanup failed:", err)
	}
}

func TestRecoveryBootstrapRejectsRoleMixing(t *testing.T) {
	bootstrap := testRecoveryBootstrap(t)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	original, err := readLinuxBootstrap(files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "unknown", "empty", "null-record", "malformed-record", "wrong-right", "replay", "too-many", "write-limit", "call-limit"} {
		policy := original
		limits := *original.Limits
		policy.Limits = &limits
		policy.Roots = append([]filesystemBootstrapRoot(nil), original.Roots...)
		policy.Recovery = append([]json.RawMessage(nil), original.Recovery...)
		switch mode {
		case "normal":
			policy.Profile = filesystemBootstrapProfile
		case "unknown":
			policy.Profile = "unknown"
		case "empty":
			policy.Recovery = []json.RawMessage{}
		case "null-record":
			policy.Recovery[0] = []byte("null")
		case "malformed-record":
			policy.Recovery[0] = []byte("{}")
		case "wrong-right":
			policy.Roots[0].Rights = Read | Write
		case "replay":
			policy.Recovery = append(policy.Recovery, policy.Recovery[0])
		case "too-many":
			policy.Recovery = make([]json.RawMessage, defaultCalls+1)
			for index := range policy.Recovery {
				policy.Recovery[index] = original.Recovery[0]
			}
		case "write-limit":
			policy.Limits.WriteBytes = 2
		case "call-limit":
			record, err := decodeRecoveryRecord(original.Recovery[0])
			if err != nil {
				t.Fatal(err)
			}
			record.Operation = 2
			encoded, err := encodeRecoveryRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			policy.Recovery = append(policy.Recovery, encoded)
			policy.Limits.Calls, policy.Limits.Outstanding = 1, 1
		}
		encoded, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		file, err := sealLinuxBootstrapBytes(encoded, maximumRecoveryBootstrapBytes)
		if err != nil {
			t.Fatal(err)
		}
		_, decodeErr := readLinuxBootstrap(file)
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if decodeErr == nil {
			t.Fatal("invalid recovery role accepted:", mode)
		}
	}
}

func TestRecoveryBootstrapMaximumEscapedBatch(t *testing.T) {
	bootstrap := testRecoveryBootstrap(t)
	files, err := bootstrap.Files()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := readLinuxBootstrap(files[0])
	if err != nil {
		t.Fatal(err)
	}
	record, err := decodeRecoveryRecord(policy.Recovery[0])
	if err != nil {
		t.Fatal(err)
	}
	record.Path = strings.Repeat(strings.Repeat("&", 255)+"/", 15) + strings.Repeat("&", 255)
	policy.Recovery = make([]json.RawMessage, defaultCalls)
	for index := range policy.Recovery {
		record.Operation = uint64(index + 1)
		policy.Recovery[index], err = encodeRecoveryRecord(record)
		if err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= maximumBootstrapBytes {
		t.Fatal("test does not exercise larger recovery ceiling")
	}
	file, err := sealLinuxBootstrapBytes(encoded, maximumRecoveryBootstrapBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := readLinuxBootstrap(file)
	if err != nil || len(decoded.Recovery) != defaultCalls {
		t.Fatal("maximum recovery batch rejected:", err)
	}
	if file, err := sealLinuxBootstrap(encoded); err == nil || file != nil {
		t.Fatal("ordinary policy accepted recovery-sized metadata")
	}
}

func TestRecoveryProtocolAcceptsOnlySealedBatch(t *testing.T) {
	for _, mode := range []string{"recover", "write", "downgrade", "wrong-id", "null"} {
		t.Run(mode, func(t *testing.T) {
			bootstrap := testRecoveryBootstrap(t)
			files, err := bootstrap.Files()
			if err != nil {
				t.Fatal(err)
			}
			backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			backend.sealed = true
			host, peer := net.Pipe()
			defer host.Close()
			defer peer.Close()
			if err := host.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- ServeFilesystemBroker(peer, backend); _ = peer.Close() }()
			codec, err := sandboxwire.New(host, host, sandboxwire.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			hello, err := codec.Read()
			if err != nil {
				t.Fatal(err)
			}
			var configuration FilesystemConfiguration
			if err := hello.DecodePayload(&configuration); err != nil || configuration.Profile != FilesystemRecoveryProfile {
				t.Fatal("wrong recovery handshake:", configuration, err)
			}
			if mode == "downgrade" {
				configuration.Profile = filesystemProfile
			}
			encoded, err := json.Marshal(configuration)
			if err != nil {
				t.Fatal(err)
			}
			if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: encoded}); err != nil {
				t.Fatal(err)
			}
			if mode != "downgrade" {
				if _, err := codec.Read(); err != nil {
					t.Fatal(err)
				}
				request := sandboxwire.Message{Kind: sandboxwire.Run, ID: 1, Payload: []byte("{}")}
				switch mode {
				case "write":
					request.Payload = []byte(recoveryWritePayload)
				case "wrong-id":
					request.ID = 2
				case "null":
					request.Payload = []byte("null")
				}
				if mode == "null" {
					const invalid = `{"version":1,"kind":"run","id":1,"payload":null}`
					frame := binary.BigEndian.AppendUint32(nil, uint32(len(invalid)))
					_, err = host.Write(append(frame, invalid...))
				} else {
					err = codec.Write(request)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			reply, err := codec.Read()
			if mode == "recover" {
				if err != nil || reply.Kind != sandboxwire.Result || reply.ID != 1 || reply.DecodePayload(&struct{}{}) != nil {
					t.Fatal("sealed cleanup failed:", reply, err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			} else if err == nil || <-done == nil {
				t.Fatal("recovery role accepted script authority:", mode)
			}
		})
	}
}

func testRecoveryBootstrap(t *testing.T) *LinuxBootstrap {
	t.Helper()
	authority := testRecoveryAuthority(t, Read|Write)
	record := testRecoveryAuthorityRecord(t, authority, 1)
	journal := testRecoveryAuthorityJournal(t, authority)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.record(journal, call, record); err != nil {
		t.Fatal(err)
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	authority.budget.Close()
	bootstrap, err := authority.RecoveryBootstrap(journal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bootstrap.Close() })
	return bootstrap
}

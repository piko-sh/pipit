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
	"encoding/binary"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestRecoveryBootstrapRequiresClosedAdmission(t *testing.T) {
	t.Parallel()
	authority := testRecoveryAuthority(t, Read|Write|List)
	record := testRecoveryAuthorityRecord(t, authority, 1)
	journal := testRecoveryAuthorityJournal(t, authority)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	require.NoError(t, authority.record(journal, call, record))
	blocked, err := authority.RecoveryBootstrap(journal)
	require.Error(t, err, "live admission blocks recovery")
	require.Nil(t, blocked)
	authority.budget.Close()
	blocked, err = authority.RecoveryBootstrap(journal)
	require.Error(t, err, "an outstanding reservation blocks recovery")
	require.Nil(t, blocked)
	require.NoError(t, call.Finish(0))
	bootstrap, err := authority.RecoveryBootstrap(journal)
	require.NoError(t, err)
	defer bootstrap.Close()
	_, err = bootstrap.RecoveryAuthority()
	require.ErrorIs(t, err, ErrInvalidPolicy, "recovery policy created new execution authority")
	files, err := bootstrap.Files()
	require.NoError(t, err)
	policy, err := readLinuxBootstrap(files[0])
	require.NoError(t, err, "recovery bootstrap failed to narrow authority: %v", policy)
	require.Equal(t, filesystemRecoveryBootstrapProfile, policy.Profile)
	require.Len(t, policy.Recovery, 1)
	require.Len(t, policy.Roots, 1)
	require.Equal(t, Write, policy.Roots[0].Rights)
	backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
	require.NoError(t, err)
	defer backend.Close()
	require.NoError(t, bootstrap.Close())
	_, err = backend.Execute(brokerMessage(1, recoveryWritePayload))
	require.ErrorIs(t, err, ErrDenied, "recovery-only backend executed script write")
	require.ErrorIs(t, backend.recover(), ErrClosed, "unconfined recovery is refused")
	backend.sealed = true
	require.NoError(t, backend.beginProtocol())
	require.NoError(t, backend.recover(), "sealed absent-remnant cleanup succeeds")
}

func TestRecoveryBootstrapRejectsRoleMixing(t *testing.T) {
	t.Parallel()
	bootstrap := testRecoveryBootstrap(t)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	original, err := readLinuxBootstrap(files[0])
	require.NoError(t, err)
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
			require.NoError(t, err)
			record.Operation = 2
			encoded, err := encodeRecoveryRecord(record)
			require.NoError(t, err)
			policy.Recovery = append(policy.Recovery, encoded)
			policy.Limits.Calls, policy.Limits.Outstanding = 1, 1
		}
		encoded, err := json.Marshal(policy)
		require.NoError(t, err)
		file, err := sealLinuxBootstrapBytes(encoded, maximumRecoveryBootstrapBytes)
		require.NoError(t, err)
		_, decodeErr := readLinuxBootstrap(file)
		require.NoError(t, file.Close())
		require.Error(t, decodeErr, "invalid recovery role accepted: %v", mode)
	}
}

func TestRecoveryBootstrapMaximumEscapedBatch(t *testing.T) {
	t.Parallel()
	bootstrap := testRecoveryBootstrap(t)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	policy, err := readLinuxBootstrap(files[0])
	require.NoError(t, err)
	record, err := decodeRecoveryRecord(policy.Recovery[0])
	require.NoError(t, err)
	record.Path = strings.Repeat(strings.Repeat("&", 255)+"/", 15) + strings.Repeat("&", 255)
	policy.Recovery = make([]json.RawMessage, defaultCalls)
	for index := range policy.Recovery {
		record.Operation = uint64(index + 1)
		policy.Recovery[index], err = encodeRecoveryRecord(record)
		require.NoError(t, err)
	}
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	require.Greater(t, len(encoded), maximumBootstrapBytes, "test does not exercise larger recovery ceiling")
	file, err := sealLinuxBootstrapBytes(encoded, maximumRecoveryBootstrapBytes)
	require.NoError(t, err)
	defer file.Close()
	decoded, err := readLinuxBootstrap(file)
	require.NoError(t, err, "maximum recovery batch rejected")
	require.Len(t, decoded.Recovery, defaultCalls)
	file, err = sealLinuxBootstrap(encoded)
	require.Error(t, err, "ordinary policy accepted recovery-sized metadata")
	require.Nil(t, file)
}

func TestRecoveryProtocolAcceptsOnlySealedBatch(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"recover", "write", "downgrade", "wrong-id", "null"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			bootstrap := testRecoveryBootstrap(t)
			files, err := bootstrap.Files()
			require.NoError(t, err)
			backend, err := adoptLinuxFilesystem(files, testBrokerProc(t))
			require.NoError(t, err)
			defer backend.Close()
			backend.sealed = true
			host, peer := net.Pipe()
			defer host.Close()
			defer peer.Close()
			require.NoError(t, host.SetDeadline(time.Now().Add(3*time.Second)))
			done := make(chan error, 1)
			go func() { done <- ServeFilesystemBroker(peer, backend); _ = peer.Close() }()
			codec, err := sandboxwire.New(host, host, sandboxwire.Limits{})
			require.NoError(t, err)
			hello, err := codec.Read()
			require.NoError(t, err)
			var configuration FilesystemConfiguration
			require.NoError(t, hello.DecodePayload(&configuration), "wrong recovery handshake: %v", configuration)
			require.Equal(t, FilesystemRecoveryProfile, configuration.Profile)
			if mode == "downgrade" {
				configuration.Profile = filesystemProfile
			}
			encoded, err := json.Marshal(configuration)
			require.NoError(t, err)
			require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: encoded}))
			if mode != "downgrade" {
				_, configureErr := codec.Read()
				require.NoError(t, configureErr)
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
				require.NoError(t, err)
			}
			reply, err := codec.Read()
			if mode == "recover" {
				require.NoError(t, err, "sealed cleanup failed: %v", reply)
				require.Equal(t, sandboxwire.Result, reply.Kind)
				require.Equal(t, uint64(1), reply.ID)
				require.NoError(t, reply.DecodePayload(&struct{}{}))
				require.NoError(t, <-done)
			} else {
				require.Error(t, err, "the recovery role refuses script authority: %s", mode)
				require.Error(t, <-done)
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
	require.NoError(t, err)
	require.NoError(t, authority.record(journal, call, record))
	require.NoError(t, call.Finish(0))
	authority.budget.Close()
	bootstrap, err := authority.RecoveryBootstrap(journal)
	require.NoError(t, err)
	t.Cleanup(func() { _ = bootstrap.Close() })
	return bootstrap
}

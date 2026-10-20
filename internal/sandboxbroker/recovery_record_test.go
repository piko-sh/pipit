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
	"bytes"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryRecordRoundTrip(t *testing.T) {
	t.Parallel()
	record := recoveryRecordFixture()
	encoded, err := encodeRecoveryRecord(record)
	require.NoError(t, err)
	decoded, err := decodeRecoveryRecord(encoded)
	require.NoError(t, err, "record did not round trip")
	require.Equal(t, record, decoded)
	record.Path = strings.Repeat(strings.Repeat("&", 255)+"/", 15) + strings.Repeat("&", 255)
	encoded, err = encodeRecoveryRecord(record)
	require.NoError(t, err, "the maximum escaped path is accepted")
	decoded, err = decodeRecoveryRecord(encoded)
	require.NoError(t, err, "maximum escaped path did not round trip")
	require.Equal(t, record, decoded)
	record.Size = 0
	record.RootDevice, record.ParentDevice, record.Device = 0, 0, 0
	encoded, err = encodeRecoveryRecord(record)
	require.NoError(t, err, "zero device or empty file rejected")
	_, err = decodeRecoveryRecord(encoded)
	require.NoError(t, err)
}

func TestRecoveryRecordRejectsIncompleteFields(t *testing.T) {
	t.Parallel()
	encoded, err := encodeRecoveryRecord(recoveryRecordFixture())
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	for name, original := range fields {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields := maps.Clone(fields)
			for _, replacement := range []json.RawMessage{nil, []byte("null"), []byte("[]"), []byte("{}"), []byte("true")} {
				fields[name] = replacement
				if replacement == nil {
					delete(fields, name)
				}
				mutated, err := json.Marshal(fields)
				require.NoError(t, err)
				assertInvalidRecoveryRecord(t, mutated)
			}
			fields[name] = original
			fields[strings.ToUpper(name)] = original
			delete(fields, name)
			mutated, err := json.Marshal(fields)
			require.NoError(t, err)
			assertInvalidRecoveryRecord(t, mutated)
			delete(fields, strings.ToUpper(name))
			fields[name] = original
		})
	}
	for _, invalid := range [][]byte{
		nil, []byte("{}"), []byte("null"), []byte("[]"),
		append(bytes.Clone(encoded), encoded...),
		append([]byte("{\"size\":3,"), encoded[1:]...),
		append([]byte("{\"unknown\":0,"), encoded[1:]...),
		bytes.Replace(encoded, []byte("\"size\":3"), []byte("\"size\":3.0"), 1),
		bytes.Replace(encoded, []byte("\"size\":3"), []byte("\"size\":\"3\""), 1),
		bytes.Repeat([]byte(" "), maximumRecoveryRecordBytes+1),
	} {
		assertInvalidRecoveryRecord(t, invalid)
	}
	for length := range len(encoded) {
		assertInvalidRecoveryRecord(t, encoded[:length])
	}
}

func TestRecoveryRecordRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()
	mutations := []func(*RecoveryRecord){
		func(record *RecoveryRecord) { record.Profile = "filesystem-recovery-v0" },
		func(record *RecoveryRecord) { record.Namespace = strings.Repeat("A", 32) },
		func(record *RecoveryRecord) { record.Root = "/host" },
		func(record *RecoveryRecord) { record.Path = "../target" },
		func(record *RecoveryRecord) { record.Path = ".pipit-stage-secret" },
		func(record *RecoveryRecord) { record.Operation = 0 },
		func(record *RecoveryRecord) { record.RootInode = 0 },
		func(record *RecoveryRecord) { record.RootMountID = 0 },
		func(record *RecoveryRecord) { record.ParentInode = 0 },
		func(record *RecoveryRecord) { record.Inode = 0 },
		func(record *RecoveryRecord) { record.RootDevice++ },
		func(record *RecoveryRecord) { record.ParentDevice++ },
		func(record *RecoveryRecord) { record.Device++ },
		func(record *RecoveryRecord) { record.Size = -1 },
		func(record *RecoveryRecord) { record.Size = maximumFileBytes + 1 },
	}
	for _, mutate := range mutations {
		record := recoveryRecordFixture()
		mutate(&record)
		invalid, err := encodeRecoveryRecord(record)
		require.Error(t, err, "an invalid record is not encoded: %+v", record)
		require.Nil(t, invalid)
		encoded, err := json.Marshal(record)
		require.NoError(t, err)
		assertInvalidRecoveryRecord(t, encoded)
	}
}

func FuzzRecoveryRecord(fuzzer *testing.F) {
	encoded, err := encodeRecoveryRecord(recoveryRecordFixture())
	require.NoError(fuzzer, err)
	fuzzer.Add(encoded)
	fuzzer.Add([]byte("{}"))
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		record, err := decodeRecoveryRecord(data)
		if err != nil {
			require.Zero(t, record, "no partial record is returned")
			return
		}
		encoded, err := encodeRecoveryRecord(record)
		require.NoError(t, err, "accepted record cannot be encoded")
		decoded, err := decodeRecoveryRecord(encoded)
		require.NoError(t, err, "accepted record changed on round trip")
		require.Equal(t, record, decoded)
	})
}

func recoveryRecordFixture() RecoveryRecord {
	return RecoveryRecord{
		Profile: recoveryRecordProfile, Namespace: strings.Repeat("a", 32),
		Root: "workspace", Path: "directory/target", Operation: 1,
		RootDevice: 1, RootInode: 2, RootMountID: 3,
		ParentDevice: 1, ParentInode: 4, Device: 1, Inode: 5, Size: 3,
	}
}

func assertInvalidRecoveryRecord(t *testing.T, encoded []byte) {
	t.Helper()
	record, err := decodeRecoveryRecord(encoded)
	require.Error(t, err, "an invalid record is refused: %s", encoded)
	require.Zero(t, record, "an invalid record returns no partial record: %s", encoded)
}

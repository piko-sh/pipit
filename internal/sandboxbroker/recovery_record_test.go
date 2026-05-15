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
	"strings"
	"testing"
)

func TestRecoveryRecordRoundTrip(t *testing.T) {
	record := recoveryRecordFixture()
	encoded, err := encodeRecoveryRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRecoveryRecord(encoded)
	if err != nil || decoded != record {
		t.Fatal("record did not round trip:", decoded, err)
	}
	record.Path = strings.Repeat(strings.Repeat("&", 255)+"/", 15) + strings.Repeat("&", 255)
	encoded, err = encodeRecoveryRecord(record)
	if err != nil {
		t.Fatal("maximum escaped path rejected:", err)
	}
	decoded, err = decodeRecoveryRecord(encoded)
	if err != nil || decoded != record {
		t.Fatal("maximum escaped path did not round trip:", err)
	}
	record.Size = 0
	record.RootDevice, record.ParentDevice, record.Device = 0, 0, 0
	encoded, err = encodeRecoveryRecord(record)
	if err != nil {
		t.Fatal("zero device or empty file rejected:", err)
	}
	if _, err := decodeRecoveryRecord(encoded); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRecordRejectsIncompleteFields(t *testing.T) {
	encoded, err := encodeRecoveryRecord(recoveryRecordFixture())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for name, original := range fields {
		t.Run(name, func(t *testing.T) {
			for _, replacement := range []json.RawMessage{nil, []byte("null"), []byte("[]"), []byte("{}"), []byte("true")} {
				fields[name] = replacement
				if replacement == nil {
					delete(fields, name)
				}
				mutated, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				assertInvalidRecoveryRecord(t, mutated)
			}
			fields[name] = original
			fields[strings.ToUpper(name)] = original
			delete(fields, name)
			mutated, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
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
		if encoded, err := encodeRecoveryRecord(record); err == nil || encoded != nil {
			t.Fatal("invalid record encoded:", record)
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		assertInvalidRecoveryRecord(t, encoded)
	}
}

func FuzzRecoveryRecord(fuzzer *testing.F) {
	encoded, err := encodeRecoveryRecord(recoveryRecordFixture())
	if err != nil {
		fuzzer.Fatal(err)
	}
	fuzzer.Add(encoded)
	fuzzer.Add([]byte("{}"))
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		record, err := decodeRecoveryRecord(data)
		if err != nil {
			if record != (RecoveryRecord{}) {
				t.Fatal("partial record returned")
			}
			return
		}
		encoded, err := encodeRecoveryRecord(record)
		if err != nil {
			t.Fatal("accepted record cannot be encoded:", err)
		}
		decoded, err := decodeRecoveryRecord(encoded)
		if err != nil || decoded != record {
			t.Fatal("accepted record changed on round trip:", err)
		}
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
	if record, err := decodeRecoveryRecord(encoded); err == nil || record != (RecoveryRecord{}) {
		t.Fatal("invalid record accepted or partially returned:", string(encoded), record, err)
	}
}

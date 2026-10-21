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

package schema

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fbs"
)

func TestSchemaHashIsNonZero(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, schemaHash, "the schema hash is derived from bytecode.fbs, so it is never all zeros")
}

func TestBytecodeInputLimit(t *testing.T) {
	t.Parallel()
	payload := make([]byte, MaximumBytecodePayloadBytes+1)
	packed := pack(payload[:MaximumBytecodePayloadBytes])
	decoded, err := Unpack(packed)
	require.NoError(t, err)
	require.Len(t, decoded, MaximumBytecodePayloadBytes)
	require.True(t, validate(packed))
	packed = pack(payload)
	decoded, err = Unpack(packed)
	require.ErrorIs(t, err, ErrBytecodeTooLarge)
	require.Nil(t, decoded)
	require.False(t, validate(packed))
	inspection, err := ConvertBytecode(payload)
	require.ErrorIs(t, err, ErrBytecodeTooLarge)
	require.Nil(t, inspection)
}

func TestPackUnpackRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	packed := pack(payload)
	unpacked, err := Unpack(packed)
	require.NoError(t, err)
	require.Equal(t, payload, unpacked)
}

func TestUnpackRejectsStaleSchemaHash(t *testing.T) {
	t.Parallel()

	staleHash := [32]byte{}
	for i := range staleHash {
		staleHash[i] = byte(i ^ 0xAA)
	}
	require.NotEqual(t, schemaHash, staleHash,
		"stale hash must not collide with current SchemaHash")

	payload := []byte{1, 2, 3, 4}
	stalePacked := fbs.PackAlloc(staleHash, payload)

	_, err := Unpack(stalePacked)
	require.ErrorIs(t, err, fbs.ErrSchemaVersionMismatch, "a stale schema hash is rejected on Unpack")
}

func TestUnpackRejectsPreviousDescriptorNumbering(t *testing.T) {
	t.Parallel()
	previousSchema := append(append([]byte(nil), schemaContent...), 0, 7)
	previousHash := fbs.ComputeSchemaHash(previousSchema)
	packed := fbs.PackAlloc(previousHash, []byte{1, 2, 3, 4})
	payload, err := Unpack(packed)
	require.ErrorIs(t, err, fbs.ErrSchemaVersionMismatch)
	require.Nil(t, payload)
	require.False(t, validate(packed))
}

func TestUnpackRejectsAnotherRevisionOfTheSameFormatVersion(t *testing.T) {
	t.Parallel()

	major := []byte{byte(BytecodeFormatVersion >> 8), byte(BytecodeFormatVersion)}
	tests := []struct {
		name   string
		suffix []byte
	}{
		{name: "the format version alone, as builds before the first revision hashed it", suffix: major},
		{name: "the revision before", suffix: append(slices.Clone(major), byte((BytecodeFormatRevision-1)>>8), byte(BytecodeFormatRevision-1))},
		{name: "the revision after", suffix: append(slices.Clone(major), byte((BytecodeFormatRevision+1)>>8), byte(BytecodeFormatRevision+1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			otherHash := fbs.ComputeSchemaHash(append(slices.Clone(schemaContent), tt.suffix...))
			packed := fbs.PackAlloc(otherHash, []byte{1, 2, 3, 4})

			payload, err := Unpack(packed)
			require.ErrorIs(t, err, fbs.ErrSchemaVersionMismatch)
			require.Nil(t, payload)
		})
	}
}

func TestValidateAcceptsCurrentSchemaHash(t *testing.T) {
	t.Parallel()

	payload := []byte{42, 43, 44}
	packed := pack(payload)

	require.True(t, validate(packed),
		"Validate must accept payloads packed with the current SchemaHash")
}

func TestValidateRejectsStaleSchemaHash(t *testing.T) {
	t.Parallel()

	staleHash := [32]byte{}
	for i := range staleHash {
		staleHash[i] = byte(i ^ 0x55)
	}
	require.NotEqual(t, schemaHash, staleHash,
		"stale hash must not collide with current SchemaHash")

	payload := []byte{1, 2, 3}
	stalePacked := fbs.PackAlloc(staleHash, payload)

	require.False(t, validate(stalePacked),
		"Validate must reject payloads packed with a stale SchemaHash")
}

func TestSchemaHashIsNotTheEmptyContentHash(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, fbs.ComputeSchemaHash(nil), schemaHash,
		"SchemaHash is the hash of empty content; bytecode.fbs is not embedded (missing //go:embed)")
}

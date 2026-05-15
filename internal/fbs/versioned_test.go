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

package fbs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fbs"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("test schema"))
	payload := []byte("hello world")

	packed := fbs.PackAlloc(hash, payload)
	unpacked, err := fbs.Unpack(hash, packed)
	require.NoError(t, err)
	assert.Equal(t, payload, unpacked)
}

func TestPackUnpackEmptyPayload(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("empty"))
	packed := fbs.PackAlloc(hash, nil)
	unpacked, err := fbs.Unpack(hash, packed)
	require.NoError(t, err)
	assert.Empty(t, unpacked)
}

func TestUnpackRejectsWrongHash(t *testing.T) {
	t.Parallel()
	hashA := fbs.ComputeSchemaHash([]byte("schema A"))
	hashB := fbs.ComputeSchemaHash([]byte("schema B"))
	packed := fbs.PackAlloc(hashA, []byte("data"))

	_, err := fbs.Unpack(hashB, packed)
	require.ErrorIs(t, err, fbs.ErrSchemaVersionMismatch)
}

func TestUnpackRejectsTooShortData(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("x"))
	_, err := fbs.Unpack(hash, []byte("short"))
	require.Error(t, err)
}

func TestValidateHashAcceptsMatchingHash(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("validate test"))
	packed := fbs.PackAlloc(hash, []byte("payload"))
	assert.True(t, fbs.ValidateHash(hash, packed))
}

func TestValidateHashRejectsMismatch(t *testing.T) {
	t.Parallel()
	hashA := fbs.ComputeSchemaHash([]byte("A"))
	hashB := fbs.ComputeSchemaHash([]byte("B"))
	packed := fbs.PackAlloc(hashA, []byte("payload"))
	assert.False(t, fbs.ValidateHash(hashB, packed))
}

func TestValidateHashRejectsTooShort(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("x"))
	assert.False(t, fbs.ValidateHash(hash, nil))
	assert.False(t, fbs.ValidateHash(hash, []byte("short")))
}

func TestPackedSize(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 32, fbs.PackedSize(0))
	assert.Equal(t, 42, fbs.PackedSize(10))
}

func TestPackedSizeNegativePanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { fbs.PackedSize(-1) })
}

func TestPackDstTooShortPanics(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("x"))
	assert.Panics(t, func() { fbs.Pack(make([]byte, 10), hash, []byte("payload")) })
}

func TestPackWritesCorrectByteCount(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash([]byte("count"))
	payload := []byte("hello")
	destination := make([]byte, fbs.PackedSize(len(payload)))
	written := fbs.Pack(destination, hash, payload)
	assert.Equal(t, fbs.PackedSize(len(payload)), written)
}

func TestComputeSchemaHashDeterministic(t *testing.T) {
	t.Parallel()
	content := []byte("deterministic test content")
	hashA := fbs.ComputeSchemaHash(content)
	hashB := fbs.ComputeSchemaHash(content)
	assert.Equal(t, hashA, hashB)
}

func TestComputeSchemaHashDifferentInputsDiffer(t *testing.T) {
	t.Parallel()
	hashA := fbs.ComputeSchemaHash([]byte("input A"))
	hashB := fbs.ComputeSchemaHash([]byte("input B"))
	assert.NotEqual(t, hashA, hashB)
}

func TestComputeSchemaHashNilInput(t *testing.T) {
	t.Parallel()
	hash := fbs.ComputeSchemaHash(nil)
	var zero fbs.SchemaHash
	assert.NotEqual(t, zero, hash)
}

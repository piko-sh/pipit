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
	_ "embed"
	"errors"

	"pipit.sh/pipit/internal/fbs"
)

const (
	// MaximumBytecodePayloadBytes bounds raw bytecode input independently of expansion.
	MaximumBytecodePayloadBytes = 8 << 20

	// BytecodeFormatVersion is the major bytecode format version mixed into SchemaHash. Must
	// equal engine.BytecodeVersionMajor; a cross-package test enforces this. Bump both when
	// the opcode encoding or descriptor numbering changes.
	BytecodeFormatVersion uint16 = 12
)

var (
	// ErrBytecodeTooLarge reports an encoded payload exceeding the input ceiling.
	ErrBytecodeTooLarge = errors.New("bytecode payload exceeds 8 MiB")

	// schemaContent holds the embedded bytecode.fbs schema.
	//
	//go:embed bytecode.fbs
	schemaContent []byte

	// schemaHash is the SHA-256 hash of bytecode.fbs plus BytecodeFormatVersion, computed at
	// init time. It changes whenever the schema file is modified or the opcode encoding
	// version is bumped, so the persisted-bytecode cache invalidates automatically on
	// either.
	schemaHash = fbs.ComputeSchemaHash(append(append([]byte(nil), schemaContent...),
		byte(BytecodeFormatVersion>>8), byte(BytecodeFormatVersion)))
)

// PackInto writes the schema hash and payload into destination.
//
// Takes destination ([]byte) which must have length at least
// fbs.PackedSize(len(payload)).
// Takes payload ([]byte) which is the raw FlatBuffer bytes.
//
// Returns int which is the number of bytes written.
func PackInto(destination, payload []byte) int {
	return fbs.Pack(destination, schemaHash, payload)
}

// Unpack validates the schema hash and returns the raw FlatBuffer payload. The returned
// slice is a zero-copy view into the original data.
//
// Takes data ([]byte) which is the hash-prefixed payload.
//
// Returns []byte which is the raw FlatBuffer bytes.
// Returns error when the stored hash does not match the current schema version.
func Unpack(data []byte) ([]byte, error) {
	if len(data) > fbs.PackedSize(MaximumBytecodePayloadBytes) {
		return nil, ErrBytecodeTooLarge
	}
	return fbs.Unpack(schemaHash, data)
}

// pack wraps a serialised bytecode FlatBuffer with the schema version hash.
//
// Takes payload ([]byte) which is the raw FlatBuffer bytes.
//
// Returns []byte which is the hash-prefixed payload.
func pack(payload []byte) []byte {
	return fbs.PackAlloc(schemaHash, payload)
}

// validate checks whether data was serialised with the current schema version without
// extracting the payload.
//
// Takes data ([]byte) which is the hash-prefixed payload.
//
// Returns bool which is true when the hash matches.
func validate(data []byte) bool {
	if len(data) > fbs.PackedSize(MaximumBytecodePayloadBytes) {
		return false
	}
	return fbs.ValidateHash(schemaHash, data)
}

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
	"errors"

	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// ReadFile requests bounded bytes from a regular file beneath a named root.
	ReadFile Operation = "fs.read"

	// WriteFile requests a staged write beneath a separately writable named root.
	WriteFile Operation = "fs.write"

	// ListDirectory requests bounded names beneath a separately listable named root.
	ListDirectory Operation = "fs.list"

	// maximumRequestBytes is the upper bound on encoded request size.
	maximumRequestBytes = 128 << 10

	// maximumFileBytes is the upper bound on a single read or write.
	maximumFileBytes = 64 << 10

	// maximumListEntries is the upper bound on directory entries per listing.
	maximumListEntries = 256
)

var (
	// ErrDenied reports a request not authorised by the host's immutable grants.
	ErrDenied = errors.New("filesystem broker authority denied")

	// errLimit reports exhausted host-owned broker resources.
	errLimit = errors.New("filesystem broker quota exceeded")

	// errBusy reports an occupied outstanding-operation budget.
	errBusy = errors.New("filesystem broker outstanding limit exceeded")

	// ErrClosed reports a terminal broker budget.
	ErrClosed = errors.New("filesystem broker is closed")

	// ErrInvalidPolicy reports an invalid host-selected grant or quota.
	ErrInvalidPolicy = errors.New("invalid filesystem broker policy")
)

// Operation identifies a narrowly typed filesystem action, not an arbitrary callback.
type Operation string

// filesystemRequest holds the validated internal representation of one broker call.
type filesystemRequest struct {
	// operation identifies the admitted action type.
	operation Operation

	// root is the opaque root identifier from the host grant.
	root string

	// path is the validated relative path beneath the root.
	path string

	// data holds bounded write payload bytes.
	data []byte

	// limit is the reserved byte or entry count.
	limit int
}

// filesystemWireRequest holds the raw JSON fields before type checking.
type filesystemWireRequest struct {
	// Operation is the action type string from the wire.
	Operation Operation `json:"operation"`

	// Root is the opaque root identifier from the wire.
	Root string `json:"root"`

	// Path is the relative path from the wire.
	Path string `json:"path"`

	// Data is the raw write payload from the wire.
	Data json.RawMessage `json:"data"`

	// MaxBytes is the raw read byte limit from the wire.
	MaxBytes json.RawMessage `json:"max_bytes"`

	// MaxEntries is the raw listing entry limit from the wire.
	MaxEntries json.RawMessage `json:"max_entries"`
}

// decodeFilesystemRequest validates exact field names and operation-specific schemas.
// Null and forbidden fields are rejected rather than silently treated as absent.
//
// Takes message (sandboxwire.Message) after framing and lifecycle validation.
//
// Returns a privately owned, bounded request or a terminal protocol error.
func decodeFilesystemRequest(message sandboxwire.Message) (filesystemRequest, error) {
	var request filesystemRequest
	if message.Kind != sandboxwire.Call || message.ID == 0 || len(message.Payload) > maximumRequestBytes {
		return request, sandboxwire.ErrProtocol
	}
	var wire filesystemWireRequest
	if err := message.DecodePayload(&wire); err != nil {
		return request, err
	}
	if !validRootName(wire.Root) || !validRelativePath(wire.Path, wire.Operation == ListDirectory) {
		return request, sandboxwire.ErrProtocol
	}
	request.operation, request.root, request.path = wire.Operation, wire.Root, wire.Path
	err := decodeFilesystemArguments(wire, &request)
	return request, err
}

// decodeFilesystemArguments accepts exactly the fields required by one operation.
//
// Takes wire (filesystemWireRequest) which holds the raw JSON fields after framing.
// Takes request (*filesystemRequest) which is the privately owned request to populate.
//
// Returns error for forbidden fields, missing limits or invalid binary data.
func decodeFilesystemArguments(wire filesystemWireRequest, request *filesystemRequest) error {
	switch wire.Operation {
	case ReadFile:
		if wire.Data != nil || wire.MaxEntries != nil {
			return sandboxwire.ErrProtocol
		}
		return decodeFilesystemLimit(wire.MaxBytes, &request.limit, maximumFileBytes)
	case WriteFile:
		return decodeFilesystemWrite(wire, request)
	case ListDirectory:
		if wire.Data != nil || wire.MaxBytes != nil {
			return sandboxwire.ErrProtocol
		}
		return decodeFilesystemLimit(wire.MaxEntries, &request.limit, maximumListEntries)
	}
	return sandboxwire.ErrProtocol
}

// decodeFilesystemWrite accepts only a bounded base64 string, never a JSON byte array.
//
// Takes wire (filesystemWireRequest) which holds the raw JSON fields after framing.
// Takes request (*filesystemRequest) which is the privately owned request to populate.
//
// Returns error for forbidden metadata, null, malformed or excessive data.
func decodeFilesystemWrite(wire filesystemWireRequest, request *filesystemRequest) error {
	if wire.MaxBytes != nil || wire.MaxEntries != nil {
		return sandboxwire.ErrProtocol
	}
	data := bytes.TrimSpace(wire.Data)
	if len(data) == 0 || data[0] != '"' {
		return sandboxwire.ErrProtocol
	}
	if err := json.Unmarshal(wire.Data, &request.data); err != nil || request.data == nil || len(request.data) > maximumFileBytes {
		return sandboxwire.ErrProtocol
	}
	request.limit = len(request.data)
	return nil
}

// decodeFilesystemLimit requires a positive integer within the operation's ceiling.
//
// Takes encoded (json.RawMessage) which is the raw JSON value to decode.
// Takes limit (*int) which is the destination for the decoded positive integer.
// Takes maximum (int) which is the operation-specific upper bound.
//
// Returns error for null, missing, non-integer or excessive limits.
func decodeFilesystemLimit(encoded json.RawMessage, limit *int, maximum int) error {
	if err := json.Unmarshal(encoded, limit); err != nil || *limit <= 0 || *limit > maximum {
		return sandboxwire.ErrProtocol
	}
	return nil
}

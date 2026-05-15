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

	"pipit.sh/pipit/internal/sandboxwire"
)

// maximumBrokerFrameBytes is the upper bound on a single broker reply frame.
const maximumBrokerFrameBytes = 512 << 10

// FilesystemResponse carries bounded data and sanitised status, never native errors. A
// non-empty code is terminal.
type FilesystemResponse struct {
	// Code is the sanitised status; empty on success.
	Code string `json:"code"`

	// Data holds the read bytes, or nil for non-read operations.
	Data []byte `json:"data"`

	// Entries holds listed directory names, or nil for non-list operations.
	Entries []string `json:"entries"`

	// Written counts staged bytes for a write operation.
	Written int `json:"written"`

	// Skipped counts names a listing left out because the protocol cannot carry them, so a
	// caller knows the listing is partial; zero for every other operation.
	Skipped int `json:"skipped"`
}

// filesystemWireResponse holds the raw JSON fields before type checking.
type filesystemWireResponse struct {
	// Code is the raw status string.
	Code json.RawMessage `json:"code"`

	// Data is the raw read payload.
	Data json.RawMessage `json:"data"`

	// Entries is the raw directory listing array.
	Entries json.RawMessage `json:"entries"`

	// Written is the raw staged byte count.
	Written json.RawMessage `json:"written"`

	// Skipped is the raw skipped entry count.
	Skipped json.RawMessage `json:"skipped"`
}

// decodeFilesystemResponse checks an untrusted reply against a host reservation. The
// caller must separately enforce lifecycle, deadlines, terminal status and process
// cleanup, and finish the reservation exactly once after validation.
//
// Takes message (sandboxwire.Message) which is the untrusted reply frame.
// Takes identity (uint64) which is the expected non-zero correlation identity.
// Takes call (*FilesystemCall) which is the host-authorised reservation.
//
// Returns no partial response on malformed, mismatched or excessive output.
func decodeFilesystemResponse(message sandboxwire.Message, identity uint64, call *FilesystemCall) (FilesystemResponse, error) {
	if call == nil || call.budget == nil || call.finished == nil || identity == 0 ||
		message.Kind != sandboxwire.Result || message.ID != identity || len(message.Payload) > maximumBrokerFrameBytes {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	var wire filesystemWireResponse
	if err := message.DecodePayload(&wire); err != nil {
		return FilesystemResponse{}, err
	}
	if !validFilesystemResponseFields(wire) {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	var response FilesystemResponse
	if err := message.DecodePayload(&response); err != nil {
		return FilesystemResponse{}, err
	}
	if !validFilesystemResponse(response, call) {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	return response, nil
}

// validFilesystemResponseFields requires every field and its exact JSON type.
//
// Takes wire (filesystemWireResponse) which is the bounded, strictly decoded wire object.
//
// Returns false for missing fields, null scalars and alternative byte encodings.
func validFilesystemResponseFields(wire filesystemWireResponse) bool {
	code, data := bytes.TrimSpace(wire.Code), bytes.TrimSpace(wire.Data)
	entries, written := bytes.TrimSpace(wire.Entries), bytes.TrimSpace(wire.Written)
	skipped := bytes.TrimSpace(wire.Skipped)
	return len(code) > 0 && code[0] == '"' &&
		len(data) > 0 && (data[0] == '"' || bytes.Equal(data, []byte("null"))) &&
		len(entries) > 0 && (entries[0] == '[' || bytes.Equal(entries, []byte("null"))) &&
		len(written) > 0 && written[0] >= '0' && written[0] <= '9' &&
		len(skipped) > 0 && skipped[0] >= '0' && skipped[0] <= '9'
}

// validFilesystemResponse checks status and operation-specific reserved bounds.
//
// Takes response (FilesystemResponse) which is the decoded response.
// Takes call (*FilesystemCall) which is the immutable host reservation.
//
// Returns false for partial failures, extraneous results or invalid directory names.
func validFilesystemResponse(response FilesystemResponse, call *FilesystemCall) bool {
	if response.Skipped < 0 {
		return false
	}
	switch response.Code {
	case "":
		return validFilesystemResult(response, call)
	case "denied", "limit", "closed", "io":
		return response.Data == nil && response.Entries == nil && response.Written == 0 && response.Skipped == 0
	default:
		return false
	}
}

// validFilesystemResult checks a successful response against the operation the host
// reserved, so no result field carries data the operation cannot produce.
//
// Takes response (FilesystemResponse) which reported success.
// Takes call (*FilesystemCall) which is the immutable host reservation.
//
// Returns false for any field out of bounds for the reserved operation.
func validFilesystemResult(response FilesystemResponse, call *FilesystemCall) bool {
	switch call.Operation() {
	case ReadFile:
		return len(response.Data) <= call.limit() && response.Entries == nil && response.Written == 0 && response.Skipped == 0
	case WriteFile:
		return response.Data == nil && response.Entries == nil && response.Written == call.limit() && response.Skipped == 0
	case ListDirectory:
		return validFilesystemListing(response, call)
	default:
		return false
	}
}

// validFilesystemListing checks a directory listing's names and bounds.
//
// Takes response (FilesystemResponse) which reported a listing.
// Takes call (*FilesystemCall) which bounds the entry count.
//
// Returns false when a name is unsafe or the listing exceeds its budget.
func validFilesystemListing(response FilesystemResponse, call *FilesystemCall) bool {
	if response.Data != nil || response.Written != 0 || len(response.Entries)+response.Skipped > call.limit() {
		return false
	}
	for _, entry := range response.Entries {
		if strings.Contains(entry, "/") || !validRelativePath(entry, false) {
			return false
		}
	}
	return true
}

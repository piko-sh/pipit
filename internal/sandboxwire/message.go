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

package sandboxwire

import (
	"encoding/json"
	"errors"
)

const (
	// version is the only supported wire protocol version.
	version = 1

	// Hello begins a connection with the worker's untrusted identity claims.
	Hello Kind = "hello"

	// Configure supplies host-selected policy after confinement is established.
	Configure Kind = "configure"

	// Ready acknowledges configuration before the first submission.
	Ready Kind = "ready"

	// Run submits a source execution request.
	Run Kind = "run"

	// Result completes the active submission.
	Result Kind = "result"

	// Call requests a broker operation within the active submission.
	Call Kind = "call"

	// Reply completes an outstanding broker request.
	Reply Kind = "reply"

	// Close terminates a connection without admitting further messages.
	Close Kind = "close"
)

var (
	// ErrProtocol reports malformed framing, payload encoding or protocol order.
	ErrProtocol = errors.New("sandbox protocol violation")

	// ErrClosed reports a transport or state machine that cannot be reused.
	ErrClosed = errors.New("sandbox protocol is closed")

	// ErrInvalidLimits reports an invalid protocol resource configuration.
	ErrInvalidLimits = errors.New("invalid sandbox protocol limits")
)

// Kind identifies a message's role in the worker protocol.
type Kind string

// Message contains one validated envelope and an independently owned JSON payload. ID is
// zero for handshake and close messages, and non-zero for correlated requests and
// results.
type Message struct {
	// Kind identifies the message's role.
	Kind Kind

	// Payload contains a bounded JSON object, never executable host objects.
	Payload json.RawMessage

	// ID correlates submissions or broker calls on this connection only.
	ID uint64
}

// DecodePayload performs strict schema decoding after envelope validation.
//
// Takes destination (any) which points to a host-selected payload struct.
//
// Returns error when the payload does not match that struct's schema.
func (message Message) DecodePayload(destination any) error {
	return decodeStrict(message.Payload, destination)
}

// envelope is the versioned on-wire representation of a message.
type envelope struct {
	// ID correlates submissions or broker calls; nil for handshake messages.
	ID *uint64 `json:"id"`

	// Kind identifies the message's protocol role.
	Kind Kind `json:"kind"`

	// Payload holds the bounded JSON object for this message.
	Payload json.RawMessage `json:"payload"`

	// Version is the wire protocol version.
	Version int `json:"version"`
}

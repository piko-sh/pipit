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
	"bytes"
	"encoding/json"
	"fmt"
)

// decodeMessage validates the complete frame before decoding its envelope.
//
// Takes data ([]byte) which excludes the length prefix.
// Takes limits (Limits) which have already been normalised.
//
// Returns Message which owns its payload bytes.
// Returns error when syntax, resource, version or identifier validation fails.
func decodeMessage(data []byte, limits Limits) (Message, error) {
	if err := validateJSON(data, limits); err != nil {
		return Message{}, err
	}
	var wire envelope
	if err := decodeStrict(data, &wire); err != nil {
		return Message{}, err
	}
	if wire.Version != version || wire.ID == nil {
		return Message{}, fmt.Errorf("%w: missing identifier or unsupported version", ErrProtocol)
	}
	message := Message{Payload: wire.Payload, Kind: wire.Kind, ID: *wire.ID}
	if err := validateMessage(message); err != nil {
		return Message{}, err
	}
	return message, nil
}

// encodeMessage produces a bounded validated envelope from a host-owned message.
//
// Takes message (Message) which supplies the kind, correlation ID and payload.
// Takes limits (Limits) which have already been normalised.
//
// Returns []byte which contains one complete encoded envelope.
// Returns error when the message cannot be encoded within the protocol limits.
func encodeMessage(message Message, limits Limits) ([]byte, error) {
	if len(message.Payload) > limits.MaxFrameBytes {
		return nil, fmt.Errorf("%w: payload exceeds frame limit", ErrProtocol)
	}
	if err := validateMessage(message); err != nil {
		return nil, err
	}
	wire := envelope{Payload: message.Payload, Kind: message.Kind, ID: &message.ID, Version: version}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding envelope: %w", ErrProtocol, err)
	}
	if err := validateJSON(data, limits); err != nil {
		return nil, err
	}
	return data, nil
}

// validateMessage checks known message kinds, correlation IDs and payload shape.
//
// Takes message (Message) which has not yet been admitted by a state machine.
//
// Returns error when its kind, identifier or payload shape is invalid.
func validateMessage(message Message) error {
	payload := bytes.TrimSpace(message.Payload)
	if len(payload) == 0 || payload[0] != '{' {
		return fmt.Errorf("%w: payload must be an object", ErrProtocol)
	}
	switch message.Kind {
	case Hello, Configure, Ready, Close:
		if message.ID != 0 {
			return fmt.Errorf("%w: control identifier must be zero", ErrProtocol)
		}
	case Run, Result, Call, Reply:
		if message.ID == 0 {
			return fmt.Errorf("%w: request identifier must be non-zero", ErrProtocol)
		}
	default:
		return fmt.Errorf("%w: unknown message kind", ErrProtocol)
	}
	return nil
}

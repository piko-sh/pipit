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
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/safeconv"
)

// frameHeaderBytes is the fixed size of the length prefix before each frame.
const frameHeaderBytes = 4

// Codec reads and writes bounded, length-prefixed messages on a private transport. One
// reader and one writer may operate concurrently, and any failed operation poisons the
// codec permanently.
type Codec struct {
	// reader supplies incoming private IPC bytes.
	reader io.Reader

	// writer receives outgoing private IPC bytes.
	writer io.Writer

	// limits bounds frames and JSON traversal depth.
	limits Limits

	// readMutex serialises concurrent reads.
	readMutex sync.Mutex

	// writeMutex serialises concurrent writes.
	writeMutex sync.Mutex

	// failed is true after any operation has poisoned the codec.
	failed atomic.Bool
}

// New creates a codec without taking ownership of the underlying transport.
//
// Takes reader (io.Reader) which supplies incoming private IPC bytes.
// Takes writer (io.Writer) which receives outgoing private IPC bytes.
// Takes limits (Limits) which bound frames and JSON traversal.
//
// Returns *Codec which is ready for handshake messages.
// Returns error when transports are nil or limits are invalid.
func New(reader io.Reader, writer io.Writer, limits Limits) (*Codec, error) {
	if reader == nil || writer == nil {
		return nil, ErrInvalidLimits
	}
	limits, err := limits.normalise()
	if err != nil {
		return nil, err
	}
	return &Codec{
		reader:     reader,
		writer:     writer,
		limits:     limits,
		readMutex:  sync.Mutex{},
		writeMutex: sync.Mutex{},
		failed:     atomic.Bool{},
	}, nil
}

// Read reads exactly one frame and validates its envelope before returning it.
//
// Returns Message which owns its bounded payload bytes.
// Returns error when framing or validation fails.
//
// Concurrency: safe for one concurrent reader alongside one writer.
func (codec *Codec) Read() (Message, error) {
	codec.readMutex.Lock()
	defer codec.readMutex.Unlock()
	if codec.failed.Load() {
		return Message{}, ErrClosed
	}
	message, err := codec.read()
	if err != nil {
		codec.failed.Store(true)
	}
	return message, err
}

// Write writes a complete frame without interleaving concurrent writes.
//
// Takes message (Message) which must satisfy the envelope and JSON limits.
//
// Returns error when validation or transport writing fails.
func (codec *Codec) Write(message Message) error {
	codec.writeMutex.Lock()
	defer codec.writeMutex.Unlock()
	if codec.failed.Load() {
		return ErrClosed
	}
	data, err := encodeMessage(message, codec.limits)
	if err == nil {
		var header [frameHeaderBytes]byte
		binary.BigEndian.PutUint32(header[:], safeconv.IntToUint32(len(data)))
		err = writeAll(codec.writer, header[:])
		if err == nil {
			err = writeAll(codec.writer, data)
		}
	}
	if err != nil {
		codec.failed.Store(true)
		return fmt.Errorf("%w: writing frame: %w", ErrProtocol, err)
	}
	return nil
}

// read bounds the advertised length before allocating an incoming frame.
//
// Returns Message which has passed envelope and JSON validation.
// Returns error when the header, body or message is invalid.
func (codec *Codec) read() (Message, error) {
	var header [frameHeaderBytes]byte
	if _, err := io.ReadFull(codec.reader, header[:]); err != nil {
		return Message{}, fmt.Errorf("%w: reading header: %w", ErrProtocol, err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || int64(length) > int64(codec.limits.MaxFrameBytes) {
		return Message{}, fmt.Errorf("%w: frame size %d is outside limits", ErrProtocol, length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(codec.reader, data); err != nil {
		return Message{}, fmt.Errorf("%w: reading body: %w", ErrProtocol, err)
	}
	return decodeMessage(data, codec.limits)
}

// writeAll handles partial writes without accepting invalid writer progress.
//
// Takes writer (io.Writer) which receives the bytes.
// Takes data ([]byte) which must be written in full.
//
// Returns error when a write fails or reports impossible or zero progress.
func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written < 0 || written > len(data) {
			return fmt.Errorf("%w: invalid write count", ErrProtocol)
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrNoProgress
		}
		data = data[written:]
	}
	return nil
}

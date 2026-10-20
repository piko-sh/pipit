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

package sandboxwire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

const validHello = `{"version":1,"kind":"hello","id":0,"payload":{}}`

func newCodec(t *testing.T, reader io.Reader, writer io.Writer, limits sandboxwire.Limits) *sandboxwire.Codec {
	t.Helper()
	codec, err := sandboxwire.New(reader, writer, limits)
	require.NoError(t, err)
	return codec
}

func frame(data []byte) []byte {
	encoded := make([]byte, 4, 4+len(data))
	binary.BigEndian.PutUint32(encoded, uint32(len(data)))
	return append(encoded, data...)
}

func TestCodecRoundTrip(t *testing.T) {
	t.Parallel()
	var transport bytes.Buffer
	codec := newCodec(t, &transport, &transport, sandboxwire.Limits{})
	message := sandboxwire.Message{Kind: sandboxwire.Run, ID: 7, Payload: json.RawMessage(`{"source":"println(42)"}`)}
	require.NoError(t, codec.Write(message))
	decoded, err := codec.Read()
	require.NoError(t, err)
	require.Equal(t, message.Kind, decoded.Kind)
	require.Equal(t, message.ID, decoded.ID)
	require.Equal(t, message.Payload, decoded.Payload)
	var payload struct {
		Source string `json:"source"`
	}
	require.NoError(t, decoded.DecodePayload(&payload))
	require.Equal(t, "println(42)", payload.Source)
}

func TestCodecRejectsMalformedEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
	}{
		{name: "empty object", data: `{}`},
		{name: "unsupported version", data: `{"version":2,"kind":"hello","id":0,"payload":{}}`},
		{name: "version written as a float", data: `{"version":1.0,"kind":"hello","id":0,"payload":{}}`},
		{name: "field name in another case", data: `{"Version":1,"kind":"hello","id":0,"payload":{}}`},
		{name: "unknown field", data: `{"version":1,"kind":"hello","id":0,"payload":{},"extra":true}`},
		{name: "duplicate field", data: `{"version":1,"version":1,"kind":"hello","id":0,"payload":{}}`},
		{name: "duplicate field through an escape", data: `{"version":1,"version":1,"kind":"hello","id":0,"payload":{}}`},
		{name: "missing id", data: `{"version":1,"kind":"hello","payload":{}}`},
		{name: "null id", data: `{"version":1,"kind":"hello","id":null,"payload":{}}`},
		{name: "hello with a non-zero id", data: `{"version":1,"kind":"hello","id":1,"payload":{}}`},
		{name: "negative id", data: `{"version":1,"kind":"hello","id":-1,"payload":{}}`},
		{name: "fractional id", data: `{"version":1,"kind":"run","id":1.5,"payload":{}}`},
		{name: "id beyond uint64", data: `{"version":1,"kind":"run","id":18446744073709551616,"payload":{}}`},
		{name: "run with id zero", data: `{"version":1,"kind":"run","id":0,"payload":{}}`},
		{name: "unknown kind", data: `{"version":1,"kind":"unknown","id":0,"payload":{}}`},
		{name: "array payload", data: `{"version":1,"kind":"hello","id":0,"payload":[]}`},
		{name: "null payload", data: `{"version":1,"kind":"hello","id":0,"payload":null}`},
		{name: "duplicate key inside the payload", data: `{"version":1,"kind":"hello","id":0,"payload":{"nested":{"same":1,"same":2}}}`},
		{name: "trailing value", data: validHello + "{}"},
		{name: "trailing garbage", data: validHello + "garbage"},
		{name: "invalid UTF-8", data: "\xff"},
		{name: "truncated JSON", data: `{"version":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codec := newCodec(t, bytes.NewReader(frame([]byte(tt.data))), io.Discard, sandboxwire.Limits{})
			_, err := codec.Read()
			require.ErrorIs(t, err, sandboxwire.ErrProtocol)
			_, err = codec.Read()
			require.ErrorIs(t, err, sandboxwire.ErrClosed, "a codec that failed a read refuses further reads")
			hello := sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage("{}")}
			require.ErrorIs(t, codec.Write(hello), sandboxwire.ErrClosed, "a codec that failed a read refuses writes")
		})
	}
}

func TestCodecRejectsOversizedHeaderBeforeBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		length uint32
	}{
		{name: "zero length", length: 0},
		{name: "one byte over the limit", length: 65},
		{name: "largest length", length: ^uint32(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var header [4]byte
			binary.BigEndian.PutUint32(header[:], tt.length)
			input := bytes.NewBuffer(append(header[:], []byte("body must remain unread")...))
			codec := newCodec(t, input, io.Discard, sandboxwire.Limits{MaxFrameBytes: 64})
			_, err := codec.Read()
			require.ErrorIs(t, err, sandboxwire.ErrProtocol)
			require.Equal(t, "body must remain unread", input.String(), "the length is checked before the body is read")
		})
	}
}

func TestCodecRejectsTruncation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty input", data: nil},
		{name: "partial header", data: []byte{0, 1}},
		{name: "partial body", data: []byte{0, 0, 0, 10, '{', '}'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codec := newCodec(t, bytes.NewReader(tt.data), io.Discard, sandboxwire.Limits{})
			_, err := codec.Read()
			require.ErrorIs(t, err, sandboxwire.ErrProtocol)
			require.True(t, errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF), "the underlying I/O error is kept: %v", err)
		})
	}
}

func TestCodecJSONBudgets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		limits  sandboxwire.Limits
		payload string
	}{
		{name: "depth", limits: sandboxwire.Limits{MaxJSONDepth: 2}, payload: `{"nested":[]}`},
		{name: "tokens", limits: sandboxwire.Limits{MaxJSONValues: 5}, payload: "{}"},
		{name: "frame", limits: sandboxwire.Limits{MaxFrameBytes: 20}, payload: "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			codec := newCodec(t, bytes.NewReader(nil), &output, tt.limits)
			err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage(tt.payload)})
			require.ErrorIs(t, err, sandboxwire.ErrProtocol, "an invalid message is rejected")
			require.Zero(t, output.Len(), "an invalid message never reaches the transport")
		})
	}
}

type partialWriter struct {
	output bytes.Buffer
	chunk  int
}

func (writer *partialWriter) Write(data []byte) (int, error) {
	return writer.output.Write(data[:min(len(data), writer.chunk)])
}

func TestCodecHandlesPartialWrites(t *testing.T) {
	t.Parallel()
	writer := &partialWriter{chunk: 2}
	codec := newCodec(t, bytes.NewReader(nil), writer, sandboxwire.Limits{})
	require.NoError(t, codec.Write(sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage("{}")}))
	reader := newCodec(t, &writer.output, io.Discard, sandboxwire.Limits{})
	_, err := reader.Read()
	require.NoError(t, err)
}

type brokenWriter struct {
	count int
	err   error
}

func (writer brokenWriter) Write(_ []byte) (int, error) {
	return writer.count, writer.err
}

func TestCodecPoisonsFailedWrites(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		writer brokenWriter
	}{
		{name: "nothing written and no error", writer: brokenWriter{count: 0, err: nil}},
		{name: "negative count", writer: brokenWriter{count: -1, err: nil}},
		{name: "count beyond the buffer", writer: brokenWriter{count: 1000, err: nil}},
		{name: "error after a partial write", writer: brokenWriter{count: 1, err: io.ErrClosedPipe}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codec := newCodec(t, bytes.NewReader(nil), tt.writer, sandboxwire.Limits{})
			message := sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage("{}")}
			require.ErrorIs(t, codec.Write(message), sandboxwire.ErrProtocol, "an invalid write fails the codec")
			require.ErrorIs(t, codec.Write(message), sandboxwire.ErrClosed, "a failed codec refuses further writes")
		})
	}
}

func TestCodecSerialisesConcurrentWrites(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	codec := newCodec(t, bytes.NewReader(nil), &output, sandboxwire.Limits{})
	var workers sync.WaitGroup
	for identifier := range uint64(100) {
		workers.Go(func() {
			err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Run, ID: identifier + 1, Payload: json.RawMessage("{}")})
			assert.NoError(t, err)
		})
	}
	workers.Wait()
	reader := newCodec(t, &output, io.Discard, sandboxwire.Limits{})
	seen := make(map[uint64]bool)
	for range 100 {
		message, err := reader.Read()
		require.NoError(t, err, "frames from concurrent writers never interleave")
		require.False(t, seen[message.ID], "frame %+v arrives once", message)
		seen[message.ID] = true
	}
}

func TestCodecRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	_, err := sandboxwire.New(nil, io.Discard, sandboxwire.Limits{})
	require.ErrorIs(t, err, sandboxwire.ErrInvalidLimits, "a codec needs a reader")
	tests := []struct {
		name   string
		limits sandboxwire.Limits
	}{
		{name: "negative frame size", limits: sandboxwire.Limits{MaxFrameBytes: -1}},
		{name: "negative JSON depth", limits: sandboxwire.Limits{MaxJSONDepth: -1}},
		{name: "negative JSON value count", limits: sandboxwire.Limits{MaxJSONValues: -1}},
		{name: "frame size above the ceiling", limits: sandboxwire.Limits{MaxFrameBytes: 8<<20 + 1}},
		{name: "JSON depth above the ceiling", limits: sandboxwire.Limits{MaxJSONDepth: 65}},
		{name: "JSON value count above the ceiling", limits: sandboxwire.Limits{MaxJSONValues: 65537}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := sandboxwire.New(bytes.NewReader(nil), io.Discard, tt.limits)
			require.ErrorIs(t, err, sandboxwire.ErrInvalidLimits)
		})
	}
}

func TestDecodePayloadRejectsUnknownAndAliasedFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
	}{
		{name: "field name in another case", payload: `{"Enabled":true}`},
		{name: "duplicate field", payload: `{"enabled":true,"enabled":false}`},
		{name: "field name in another case inside a slice", payload: `{"items":[{"Enabled":true}]}`},
		{name: "field name in another case inside a map", payload: `{"named":{"entry":{"Enabled":true}}}`},
		{name: "unknown field", payload: `{"extra":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			message := sandboxwire.Message{Payload: json.RawMessage(tt.payload)}
			var target struct {
				Enabled bool `json:"enabled"`
				Items   []struct {
					Enabled bool `json:"enabled"`
				} `json:"items"`
				Named map[string]struct {
					Enabled bool `json:"enabled"`
				} `json:"named"`
			}
			require.ErrorIs(t, message.DecodePayload(&target), sandboxwire.ErrProtocol)
		})
	}
}

func TestDecodePayloadAcceptsExplicitBinaryAndNestedSchema(t *testing.T) {
	t.Parallel()
	message := sandboxwire.Message{Payload: json.RawMessage(`{"data":"cGlwaXQ=","nested":{"enabled":true}}`)}
	var target struct {
		Data   []byte `json:"data"`
		Nested *struct {
			Enabled bool `json:"enabled"`
		} `json:"nested"`
	}
	require.NoError(t, message.DecodePayload(&target))
	require.Equal(t, "pipit", string(target.Data))
	require.NotNil(t, target.Nested)
	require.True(t, target.Nested.Enabled)
}

type signallingReader struct {
	reader  io.Reader
	started chan struct{}
	once    sync.Once
}

func (reader *signallingReader) Read(data []byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	return reader.reader.Read(data)
}

func TestClosingTransportUnblocksRead(t *testing.T) {
	t.Parallel()
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
	reader := &signallingReader{reader: local, started: make(chan struct{})}
	codec := newCodec(t, reader, local, sandboxwire.Limits{})
	finished := make(chan error, 1)
	go func() {
		_, err := codec.Read()
		finished <- err
	}()
	select {
	case <-reader.started:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the reader starts within five seconds")
	}
	require.NoError(t, local.Close())
	select {
	case err := <-finished:
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "closing the transport fails a blocked read")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "closing the transport wakes a blocked reader")
	}
}

func FuzzCodecRead(fuzzer *testing.F) {
	fuzzer.Add(frame([]byte(validHello)))
	fuzzer.Add([]byte{255, 255, 255, 255})
	fuzzer.Add(frame([]byte(`{"version":1,"kind":"run","id":1,"payload":{"x":1,"x":2}}`)))
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192 {
			return
		}
		codec := newCodec(t, bytes.NewReader(data), io.Discard, sandboxwire.Limits{MaxFrameBytes: 4096, MaxJSONDepth: 16, MaxJSONValues: 1024})
		message, err := codec.Read()
		if err != nil {
			_, nextErr := codec.Read()
			require.ErrorIs(t, nextErr, sandboxwire.ErrClosed, "a failed codec stays closed")
			return
		}
		var encoded bytes.Buffer
		writer := newCodec(t, bytes.NewReader(nil), &encoded, sandboxwire.Limits{})
		require.NoError(t, writer.Write(message), "an accepted message can be encoded again")
		reader := newCodec(t, &encoded, io.Discard, sandboxwire.Limits{})
		roundTrip, err := reader.Read()
		require.NoError(t, err, "a re-encoded message decodes again")
		require.Equal(t, message.ID, roundTrip.ID, "the round trip keeps the envelope")
		require.Equal(t, message.Kind, roundTrip.Kind, "the round trip keeps the envelope")
	})
}

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

	"pipit.sh/pipit/internal/sandboxwire"
)

const validHello = `{"version":1,"kind":"hello","id":0,"payload":{}}`

func newCodec(t *testing.T, reader io.Reader, writer io.Writer, limits sandboxwire.Limits) *sandboxwire.Codec {
	t.Helper()
	codec, err := sandboxwire.New(reader, writer, limits)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := codec.Write(message); err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Read()
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != message.Kind || decoded.ID != message.ID || !bytes.Equal(decoded.Payload, message.Payload) {
		t.Fatalf("round trip changed message: %+v", decoded)
	}
	var payload struct {
		Source string `json:"source"`
	}
	if err := decoded.DecodePayload(&payload); err != nil || payload.Source != "println(42)" {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
}

func TestCodecRejectsMalformedEnvelope(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{}`,
		`{"version":2,"kind":"hello","id":0,"payload":{}}`,
		`{"version":1.0,"kind":"hello","id":0,"payload":{}}`,
		`{"Version":1,"kind":"hello","id":0,"payload":{}}`,
		`{"version":1,"kind":"hello","id":0,"payload":{},"extra":true}`,
		`{"version":1,"version":1,"kind":"hello","id":0,"payload":{}}`,
		`{"version":1,"\u0076ersion":1,"kind":"hello","id":0,"payload":{}}`,
		`{"version":1,"kind":"hello","payload":{}}`,
		`{"version":1,"kind":"hello","id":null,"payload":{}}`,
		`{"version":1,"kind":"hello","id":1,"payload":{}}`,
		`{"version":1,"kind":"hello","id":-1,"payload":{}}`,
		`{"version":1,"kind":"run","id":1.5,"payload":{}}`,
		`{"version":1,"kind":"run","id":18446744073709551616,"payload":{}}`,
		`{"version":1,"kind":"run","id":0,"payload":{}}`,
		`{"version":1,"kind":"unknown","id":0,"payload":{}}`,
		`{"version":1,"kind":"hello","id":0,"payload":[]}`,
		`{"version":1,"kind":"hello","id":0,"payload":null}`,
		`{"version":1,"kind":"hello","id":0,"payload":{"nested":{"same":1,"same":2}}}`,
		validHello + "{}",
		validHello + "garbage",
		"\xff",
		`{"version":`,
	} {
		t.Run(data, func(t *testing.T) {
			input := bytes.NewReader(frame([]byte(data)))
			codec := newCodec(t, input, io.Discard, sandboxwire.Limits{})
			if _, err := codec.Read(); !errors.Is(err, sandboxwire.ErrProtocol) {
				t.Fatalf("expected protocol failure: %v", err)
			}
			if _, err := codec.Read(); !errors.Is(err, sandboxwire.ErrClosed) {
				t.Fatalf("poisoned read allowed: %v", err)
			}
			if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage("{}")}); !errors.Is(err, sandboxwire.ErrClosed) {
				t.Fatalf("poisoned write allowed: %v", err)
			}
		})
	}
}

func TestCodecRejectsOversizedHeaderBeforeBody(t *testing.T) {
	t.Parallel()
	for _, length := range []uint32{0, 65, ^uint32(0)} {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], length)
		input := bytes.NewBuffer(append(header[:], []byte("body must remain unread")...))
		codec := newCodec(t, input, io.Discard, sandboxwire.Limits{MaxFrameBytes: 64})
		if _, err := codec.Read(); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("length=%d err=%v", length, err)
		}
		if input.String() != "body must remain unread" {
			t.Fatal("read the body before validating its length")
		}
	}
}

func TestCodecRejectsTruncation(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{nil, {0, 1}, {0, 0, 0, 10, '{', '}'}} {
		codec := newCodec(t, bytes.NewReader(data), io.Discard, sandboxwire.Limits{})
		_, err := codec.Read()
		if !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("truncation accepted: %v", err)
		}
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("underlying I/O error lost: %v", err)
		}
	}
}

func TestCodecJSONBudgets(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		limits  sandboxwire.Limits
		payload string
	}{
		{name: "depth", limits: sandboxwire.Limits{MaxJSONDepth: 2}, payload: `{"nested":[]}`},
		{name: "tokens", limits: sandboxwire.Limits{MaxJSONValues: 5}, payload: "{}"},
		{name: "frame", limits: sandboxwire.Limits{MaxFrameBytes: 20}, payload: "{}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			codec := newCodec(t, bytes.NewReader(nil), &output, test.limits)
			err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage(test.payload)})
			if !errors.Is(err, sandboxwire.ErrProtocol) || output.Len() != 0 {
				t.Fatalf("invalid message reached transport: bytes=%d err=%v", output.Len(), err)
			}
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
	if err := codec.Write(sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage("{}")}); err != nil {
		t.Fatal(err)
	}
	reader := newCodec(t, &writer.output, io.Discard, sandboxwire.Limits{})
	if _, err := reader.Read(); err != nil {
		t.Fatal(err)
	}
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
	for _, writer := range []brokenWriter{
		{count: 0, err: nil},
		{count: -1, err: nil},
		{count: 1000, err: nil},
		{count: 1, err: io.ErrClosedPipe},
	} {
		codec := newCodec(t, bytes.NewReader(nil), writer, sandboxwire.Limits{})
		message := sandboxwire.Message{Kind: sandboxwire.Hello, Payload: json.RawMessage("{}")}
		if err := codec.Write(message); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("write accepted: %v", err)
		}
		if err := codec.Write(message); !errors.Is(err, sandboxwire.ErrClosed) {
			t.Fatalf("poisoned codec reused: %v", err)
		}
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
			if err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	reader := newCodec(t, &output, io.Discard, sandboxwire.Limits{})
	seen := make(map[uint64]bool)
	for range 100 {
		message, err := reader.Read()
		if err != nil || seen[message.ID] {
			t.Fatalf("interleaved frame: %+v err=%v", message, err)
		}
		seen[message.ID] = true
	}
}

func TestCodecRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, limits := range []sandboxwire.Limits{
		{MaxFrameBytes: -1}, {MaxJSONDepth: -1}, {MaxJSONValues: -1},
		{MaxFrameBytes: 8<<20 + 1}, {MaxJSONDepth: 65}, {MaxJSONValues: 65537},
	} {
		if _, err := sandboxwire.New(bytes.NewReader(nil), io.Discard, limits); !errors.Is(err, sandboxwire.ErrInvalidLimits) {
			t.Fatalf("invalid limits accepted: %+v err=%v", limits, err)
		}
	}
	if _, err := sandboxwire.New(nil, io.Discard, sandboxwire.Limits{}); !errors.Is(err, sandboxwire.ErrInvalidLimits) {
		t.Fatal(err)
	}
}

func TestDecodePayloadRejectsUnknownAndAliasedFields(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"Enabled":true}`,
		`{"enabled":true,"enabled":false}`,
		`{"items":[{"Enabled":true}]}`,
		`{"named":{"entry":{"Enabled":true}}}`,
		`{"extra":true}`,
	} {
		message := sandboxwire.Message{Payload: json.RawMessage(payload)}
		var target struct {
			Enabled bool `json:"enabled"`
			Items   []struct {
				Enabled bool `json:"enabled"`
			} `json:"items"`
			Named map[string]struct {
				Enabled bool `json:"enabled"`
			} `json:"named"`
		}
		if err := message.DecodePayload(&target); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("ambiguous payload accepted: %s: %v", payload, err)
		}
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
	if err := message.DecodePayload(&target); err != nil {
		t.Fatal(err)
	}
	if string(target.Data) != "pipit" || target.Nested == nil || !target.Nested.Enabled {
		t.Fatalf("payload changed: %+v", target)
	}
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
		t.Fatal("reader did not start")
	}
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("closed transport did not fail: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transport closure did not wake blocked reader")
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
			if _, nextErr := codec.Read(); !errors.Is(nextErr, sandboxwire.ErrClosed) {
				t.Fatalf("failed codec reusable: %v", nextErr)
			}
			return
		}
		var encoded bytes.Buffer
		writer := newCodec(t, bytes.NewReader(nil), &encoded, sandboxwire.Limits{})
		if err := writer.Write(message); err != nil {
			t.Fatalf("accepted message cannot be re-encoded: %v", err)
		}
		reader := newCodec(t, &encoded, io.Discard, sandboxwire.Limits{})
		roundTrip, err := reader.Read()
		if err != nil || roundTrip.ID != message.ID || roundTrip.Kind != message.Kind {
			t.Fatalf("accepted envelope changed: %+v err=%v", roundTrip, err)
		}
	})
}

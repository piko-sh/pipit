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

package sandboxworker

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

type sessionPipe struct {
	net.Conn
	done      chan struct{}
	err       error
	chargeErr error
	charged   int
	mutex     sync.Mutex
}

func (stream *sessionPipe) ChargeOutput(count int) error {
	stream.mutex.Lock()
	defer stream.mutex.Unlock()
	stream.charged += count
	return stream.chargeErr
}

func (stream *sessionPipe) Wait() error {
	<-stream.done
	return stream.err
}

func (stream *sessionPipe) Close() error {
	err := stream.Conn.Close()
	<-stream.done
	return err
}

func openTestSession(t *testing.T, imports []string) (*SessionClient, *sessionPipe) {
	t.Helper()
	host, worker := net.Pipe()
	stream := &sessionPipe{Conn: host, done: make(chan struct{}), err: nil, chargeErr: nil, charged: 0, mutex: sync.Mutex{}}
	go func() {
		defer close(stream.done)
		defer worker.Close()
		stream.err = Serve(worker)
	}()
	session, err := OpenSession(stream, Configuration{Profile: SessionProfile, Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, stream
}

func TestWorkerSessionRetainsStateAndCloses(t *testing.T) {
	t.Parallel()
	imports := []string{"math"}
	session, stream := openTestSession(t, imports)
	imports[0] = "os"
	for _, source := range []string{"import \"math\"", "var value = math.Sqrt(144)", "print(\"ok\")"} {
		if _, err := session.Submit(source); err != nil {
			t.Fatal(err)
		}
	}
	response, err := session.Submit("value + 30")

	if err != nil || string(response.Value) != "42" || stream.charged != 16 {
		t.Fatalf("response=%+v charged=%d error=%v", response, stream.charged, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Submit("42"); !errors.Is(err, ErrSessionClosed) {
		t.Fatal(err)
	}
}

func TestWorkerSessionFailuresAreTerminal(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"import \"os\"", "go func() {}()", "[]int{1}", "syntax error"} {
		t.Run(source, func(t *testing.T) {
			session, _ := openTestSession(t, nil)
			if response, err := session.Submit(source); err == nil || response.Value != nil {
				t.Fatalf("failed submission published result: %+v %v", response, err)
			}
			if _, err := session.Submit("42"); !errors.Is(err, ErrSessionClosed) {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkerSessionHostOutputFailureCloses(t *testing.T) {
	t.Parallel()
	session, stream := openTestSession(t, nil)
	stream.chargeErr = errors.New("native output budget exceeded")
	if response, err := session.Submit("println(42)"); !errors.Is(err, stream.chargeErr) || response.Value != nil {
		t.Fatalf("accounting failure ignored: %+v %v", response, err)
	}
	if _, err := session.Submit("42"); !errors.Is(err, ErrSessionClosed) {
		t.Fatal(err)
	}
}

func TestWorkerSessionFinalSubmissionReaps(t *testing.T) {
	t.Parallel()
	session, stream := openTestSession(t, nil)
	for count := range sessionSubmissions {
		response, err := session.Submit("1")
		if err != nil || string(response.Value) != "1" {
			t.Fatalf("submission %d: %+v %v", count, response, err)
		}
	}
	select {
	case <-stream.done:
	default:
		t.Fatal("final result published before worker exit")
	}
	if _, err := session.Submit("1"); !errors.Is(err, ErrSessionClosed) {
		t.Fatal(err)
	}
}

type sessionReplyFixture struct {
	*fixtureTransport
	closeErr error
	waitErr  error
	closes   int
}

func (stream *sessionReplyFixture) Close() error {
	stream.closes++
	return stream.closeErr
}

func (stream *sessionReplyFixture) Wait() error { return stream.waitErr }

func sessionReplies(t *testing.T, messages ...sandboxwire.Message) *sessionReplyFixture {
	t.Helper()
	initial := make([]sandboxwire.Message, 0, 2+len(messages))
	initial = append(initial,
		fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": Profile}),
		fixtureMessage(t, sandboxwire.Ready, 0, struct{}{}),
	)
	return &sessionReplyFixture{fixtureTransport: sourceFixture(t, append(initial, messages...)...), closeErr: nil, waitErr: nil, closes: 0}
}

func sessionResponse(code string) Response {
	value := json.RawMessage("42")
	if code == "compiled" {
		value = json.RawMessage("null")
	}
	return Response{Output: "", Error: "", Code: code, Value: value, CostUsed: 0, OutputTruncated: false}
}

func TestWorkerSessionRejectsReplayedAndPrematureResults(t *testing.T) {
	t.Parallel()
	for _, identity := range []uint64{1, 2, 3, 5} {
		stream := sessionReplies(t,
			fixtureMessage(t, sandboxwire.Result, 1, sessionResponse("compiled")),
			fixtureMessage(t, sandboxwire.Result, 2, sessionResponse("ok")),
			fixtureMessage(t, sandboxwire.Result, identity, sessionResponse("ok")),
		)
		session, err := OpenSession(stream, Configuration{Profile: SessionProfile, Imports: nil})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Submit("42"); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Submit("42"); !errors.Is(err, sandboxwire.ErrProtocol) || stream.closes == 0 {
			t.Fatalf("replay/premature ID %d accepted: %v", identity, err)
		}
	}
}

func TestWorkerSessionHostBudgetsAndCleanup(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"source", "output", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			response := sessionResponse("ok")
			response.Output = "four"
			stream := sessionReplies(t,
				fixtureMessage(t, sandboxwire.Result, 1, sessionResponse("compiled")),
				fixtureMessage(t, sandboxwire.Result, 2, response),
			)
			session, err := OpenSession(stream, Configuration{Profile: SessionProfile, Imports: nil})
			if err != nil {
				t.Fatal(err)
			}
			expected := ErrSessionLimit
			if kind == "source" {
				session.sourceRemaining = 1
			}
			if kind == "output" {
				session.outputRemaining = 1
			}
			if kind == "cleanup" {
				session.submissionsRemaining = 1
				stream.waitErr = errors.New("native cleanup failed")
				expected = stream.waitErr
			}
			if response, err := session.Submit("42"); !errors.Is(err, expected) || response.Value != nil || stream.closes == 0 {
				t.Fatalf("host %s failure ignored: %+v %v", kind, response, err)
			}
		})
	}
}

func TestWorkerSessionRejectsAlternateRequestsAndGrants(t *testing.T) {
	t.Parallel()
	for _, request := range []Request{
		{Kind: "expression", Source: "42", Entrypoint: ""},
		{Kind: "file", Source: "package main", Entrypoint: "main"},
		{Kind: "submission", Source: "42", Entrypoint: "main"},
	} {
		stream := sourceFixture(t,
			fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: SessionProfile, Imports: nil}),
			fixtureMessage(t, sandboxwire.Run, 1, request))
		if err := Serve(stream); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("alternate request accepted: %v", err)
		}
	}
	for _, grant := range []sandboxwire.Message{
		fixtureMessage(t, sandboxwire.Run, 4, Request{Kind: "execute", Source: "", Entrypoint: ""}),
		fixtureMessage(t, sandboxwire.Run, 2, Request{Kind: "execute", Source: "42", Entrypoint: ""}),
		fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: SessionProfile, Imports: []string{"math"}}),
	} {
		stream := sourceFixture(t,
			fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: SessionProfile, Imports: nil}),
			fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "submission", Source: "42", Entrypoint: ""}), grant)
		if err := Serve(stream); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("alternate grant accepted: %v", err)
		}
	}
}

type gatedSessionTransport struct {
	sessionTransport
	entered chan struct{}
	release chan struct{}
	stopped chan struct{}
	once    sync.Once
	paused  atomic.Bool
}

func (stream *gatedSessionTransport) Read(data []byte) (int, error) {
	if stream.paused.CompareAndSwap(false, true) {
		close(stream.entered)
		select {
		case <-stream.release:
		case <-stream.stopped:
			return 0, io.ErrClosedPipe
		}
	}
	return stream.sessionTransport.Read(data)
}

func (stream *gatedSessionTransport) Close() error {
	stream.once.Do(func() { close(stream.stopped) })
	return stream.sessionTransport.Close()
}

func gateSessionRead(t *testing.T, session *SessionClient) *gatedSessionTransport {
	t.Helper()
	gate := &gatedSessionTransport{sessionTransport: session.stream, entered: make(chan struct{}),
		release: make(chan struct{}), stopped: make(chan struct{}), once: sync.Once{}, paused: atomic.Bool{}}
	session.stream = gate
	codec, err := sandboxwire.New(gate, gate, sandboxwire.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	session.connection.codec = codec
	return gate
}

func TestWorkerSessionConcurrentAdmissionAndClose(t *testing.T) {
	t.Parallel()
	session, _ := openTestSession(t, nil)
	gate := gateSessionRead(t, session)
	done := make(chan error, 1)
	go func() { _, err := session.Submit("42"); done <- err }()
	<-gate.entered
	if _, err := session.Submit("1"); !errors.Is(err, ErrSessionBusy) {
		t.Errorf("overlapping submission admitted: %v", err)
	}
	_ = session.Close()
	if err := <-done; err == nil {
		t.Fatal("active close allowed a successful result")
	}
	if _, err := session.Submit("42"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed worker reused: %v", err)
	}
}

func TestWorkerSessionDeadlinesNeverExtendLifetime(t *testing.T) {
	t.Parallel()
	stream := sessionReplies(t,
		fixtureMessage(t, sandboxwire.Result, 1, sessionResponse("compiled")),
		fixtureMessage(t, sandboxwire.Result, 2, sessionResponse("ok")))
	session, err := OpenSession(stream, Configuration{Profile: SessionProfile, Imports: nil})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	session.expires = time.Now().Add(time.Second)
	if _, err := session.Submit("42"); err != nil {
		t.Fatal(err)
	}
	if !stream.deadline.Equal(session.expires) {
		t.Fatal("idle deadline extended the session lifetime")
	}
	bounded := sessionDeadlineTransport{transport: stream, expires: session.expires}
	if err := bounded.SetDeadline(time.Now().Add(compilationTimeout)); err != nil {
		t.Fatal(err)
	}
	if !stream.deadline.Equal(session.expires) {
		t.Fatal("compilation deadline extended the session lifetime")
	}
}

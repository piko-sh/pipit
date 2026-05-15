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

// Package dap implements the Debug Adapter Protocol server backing the `pipit dap`
// command. It bridges DAP requests to pipit's [pipit.Debugger] API so any DAP-capable
// editor can step-debug pipit scripts.
//
// Bridged requests include initialize, launch, the three setBreakpoints forms, continue,
// step, pause, threads, stackTrace, scopes, variables, setVariable, evaluate and
// exceptionInfo. Every interpreted goroutine is a thread; a pause stops them all.
// Supported editors include GoLand, VS Code, Neovim's nvim-dap, Helix, and Emacs
// dap-mode, each driving the adapter through its native Run and Debug UI.
//
// Wire format follows the DAP spec verbatim (Content-Length-prefixed JSON over stdio) via
// [github.com/google/go-dap]. Transport is stdio-only.
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/google/go-dap"

	"pipit.sh/pipit"
)

// requestQueueDepth bounds the inbound messages waiting for the dispatch goroutine.
const requestQueueDepth = 64

// ServerOptions configures a Run invocation. The server reads requests from Stdin and
// writes responses and events to Stdout.
type ServerOptions struct {
	// Stdin is the inbound DAP message stream from the client.
	Stdin io.Reader

	// Stdout is the outbound DAP message stream to the client. Must not be shared with
	// anything else; DAP framing is strict and interleaved writes will desync the client.
	Stdout io.Writer

	// Stderr receives adapter-internal warnings and errors. The DAP client never sees this
	// stream; to surface output in the client's debug console use an `output` event instead.
	Stderr io.Writer

	// Log, if non-nil, receives a human-readable transcript of every DAP message in either
	// direction. Set by the `--debug-log <path>` flag.
	Log io.Writer

	// Symbols holds host symbol exports the script may import, beyond pipit's built-in
	// stdlib + Pipit defaults. Forwarded to the interpreter spun up by the launcher.
	Symbols pipit.SymbolExports
}

// server holds the per-connection state of the DAP adapter. One server instance exists
// per `pipit dap` invocation.
type server struct {
	// opts holds the IO streams and symbols supplied by the caller.
	opts ServerOptions

	// reader wraps opts.Stdin for buffered DAP message reads.
	reader *bufio.Reader

	// debugger is the one debugger every launch in this session runs under; breakpoints set
	// before launch land on it directly.
	debugger *pipit.Debugger

	// launcher manages the interpreter lifecycle once a launch request arrives.
	launcher *launcher

	// stop holds the per-stop variable and frame state, replaced on each new pause.
	stop *stopState

	// requests carries inbound messages from the read loop to the dispatch goroutine, so a
	// handler that waits (disconnect draining the run) never stalls reading.
	requests chan dap.Message

	// dispatchDone is closed when the dispatch goroutine has drained requests.
	dispatchDone chan struct{}

	// seq is the monotonic DAP sequence counter for messages this server originates
	// (responses + events). Per spec, sequences are per-direction.
	seq atomic.Int64

	// writeMu serialises all writes to Stdout.
	writeMu sync.Mutex

	// launcherMu guards launcher and pause.
	launcherMu sync.Mutex

	// done signals the message loop to stop after the next dispatch returns (set by the
	// disconnect handler).
	done atomic.Bool
}

// newServer constructs an idle server bound to the supplied IO.
//
// Takes opts (ServerOptions) which supplies the IO streams and symbols.
//
// Returns *server which is ready to serve but not yet reading.
func newServer(opts ServerOptions) *server {
	return &server{
		opts:         opts,
		reader:       bufio.NewReader(opts.Stdin),
		debugger:     pipit.NewDebugger(),
		launcher:     nil,
		stop:         nil,
		requests:     make(chan dap.Message, requestQueueDepth),
		dispatchDone: make(chan struct{}),
		seq:          atomic.Int64{}, writeMu: sync.Mutex{}, launcherMu: sync.Mutex{}, done: atomic.Bool{}}
}

// serve reads DAP messages from the client and dispatches them, in order, on a separate
// goroutine until the client disconnects, the input stream closes, or ctx is cancelled.
//
// Returns error when reading a protocol message fails before the session ended.
func (s *server) serve(ctx context.Context) error {
	stopWatching := context.AfterFunc(ctx, s.closeInput)
	defer stopWatching()

	go s.dispatchLoop(ctx)
	var readErr error
	for !s.done.Load() {
		message, err := dap.ReadProtocolMessage(s.reader)
		if err != nil {
			readErr = err
			break
		}
		s.logIncoming(message)
		s.requests <- message
	}
	close(s.requests)
	<-s.dispatchDone
	if s.done.Load() && (readErr == nil || errors.Is(readErr, io.ErrClosedPipe) || errors.Is(readErr, os.ErrClosed)) {
		return nil
	}
	return readErr
}

// dispatchLoop handles queued messages one at a time, in arrival order.
func (s *server) dispatchLoop(ctx context.Context) {
	defer close(s.dispatchDone)
	for message := range s.requests {
		s.dispatch(ctx, message)
	}
}

// closeInput closes the inbound stream when it can be closed, so a blocked read returns
// once the session is over.
func (s *server) closeInput() {
	if closer, ok := s.opts.Stdin.(io.Closer); ok {
		_ = closer.Close()
	}
}

// dispatch routes a single inbound message to its handler.
//
// Takes message (dap.Message) which is the decoded inbound DAP message.
//
// Unknown commands respond with a DAP error response per spec ("unsupported command").
// The dispatcher is exhaustive over the Silver-tier surface; every recognised command
// gets a handler in handlers.go and handlers_exec.go, and the default arm answers
// commands the client probes against capabilities never claimed.
func (s *server) dispatch(ctx context.Context, message dap.Message) {
	request, ok := message.(dap.RequestMessage)
	if !ok {
		return
	}
	command := request.GetRequest().Command
	if s.dispatchSession(ctx, message) {
		return
	}
	switch typed := message.(type) {
	case *dap.ThreadsRequest:
		s.handleThreads(typed)
	case *dap.StackTraceRequest:
		s.handleStackTrace(typed)
	case *dap.ScopesRequest:
		s.handleScopes(typed)
	case *dap.VariablesRequest:
		s.handleVariables(typed)
	case *dap.SetVariableRequest:
		s.handleSetVariable(typed)
	case *dap.EvaluateRequest:
		s.handleEvaluate(typed)
	case *dap.ExceptionInfoRequest:
		s.handleExceptionInfo(typed)
	case *dap.ContinueRequest:
		s.handleContinue(typed)
	case *dap.NextRequest:
		s.handleNext(typed)
	case *dap.StepInRequest:
		s.handleStepIn(typed)
	case *dap.StepOutRequest:
		s.handleStepOut(typed)
	case *dap.PauseRequest:
		s.handlePause(typed)
	default:
		s.writeError(request.GetRequest(), fmt.Sprintf("pipit dap: unsupported command %q", command))
	}
}

// dispatchSession handles the session-level requests: initialise, launch, breakpoints,
// configuration and teardown.
//
// Takes message (dap.Message) which is the decoded inbound DAP message.
//
// Returns bool which is true when the message was one of those requests.
func (s *server) dispatchSession(ctx context.Context, message dap.Message) bool {
	switch typed := message.(type) {
	case *dap.InitializeRequest:
		s.handleInitialize(typed)
	case *dap.LaunchRequest:
		s.handleLaunch(typed)
	case *dap.SetBreakpointsRequest:
		s.handleSetBreakpoints(typed)
	case *dap.SetFunctionBreakpointsRequest:
		s.handleSetFunctionBreakpoints(typed)
	case *dap.SetExceptionBreakpointsRequest:
		s.handleSetExceptionBreakpoints(typed)
	case *dap.ConfigurationDoneRequest:
		s.handleConfigurationDone(ctx, typed)
	case *dap.DisconnectRequest:
		s.handleDisconnect(typed)
	case *dap.TerminateRequest:
		s.handleTerminate(typed)
	default:
		return false
	}
	return true
}

// writeMessage serialises message to the wire under writeMu.
//
// Takes message (dap.Message) which is the DAP message to encode.
//
// Concurrency: safe for concurrent use; writeMu serialises every write.
func (s *server) writeMessage(message dap.Message) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := dap.WriteProtocolMessage(s.opts.Stdout, message); err != nil {
		fmt.Fprintf(s.opts.Stderr, "pipit dap: write failed: %v\n", err)
		return
	}
	s.logOutgoing(message)
}

// nextSeq allocates the next outbound message sequence number.
//
// Returns int which is the next monotonic DAP sequence number.
func (s *server) nextSeq() int {
	return int(s.seq.Add(1))
}

// writeError emits an `ErrorResponse` for request with the supplied human-readable
// message.
//
// Takes request (*dap.Request) which identifies the failed command.
// Takes message (string) which is the human-readable failure text.
//
// Used by the dispatcher's default arm and by handlers that bail on malformed arguments.
func (s *server) writeError(request *dap.Request, message string) {
	response := &dap.ErrorResponse{
		Response: dap.Response{
			ProtocolMessage: dap.ProtocolMessage{Seq: s.nextSeq(), Type: "response"},
			RequestSeq:      request.Seq,
			Success:         false,
			Command:         request.Command,
			Message:         message,
		},
		Body: dap.ErrorResponseBody{
			Error: &dap.ErrorMessage{
				Format:   message,
				ShowUser: true,
			},
		},
	}
	s.writeMessage(response)
}

// logIncoming writes the inbound message to the diagnostic log, if any.
//
// Takes message (dap.Message) which is the received inbound DAP message.
//
// Errors are swallowed; the log is a development aid, not a correctness dependency.
func (s *server) logIncoming(message dap.Message) {
	s.logMessage("<--", message)
}

// logOutgoing writes the outbound message to the diagnostic log, if any.
//
// Takes message (dap.Message) which is the sent outbound DAP message.
func (s *server) logOutgoing(message dap.Message) {
	s.logMessage("-->", message)
}

// logMessage is the shared formatter for logIncoming and logOutgoing.
//
// Takes direction (string) which is the arrow marker prefixing the entry.
// Takes message (dap.Message) which is the DAP message to serialise.
func (s *server) logMessage(direction string, message dap.Message) {
	if s.opts.Log == nil {
		return
	}
	body, err := json.Marshal(message)
	if err != nil {
		fmt.Fprintf(s.opts.Log, "%s <encode-error: %v>\n", direction, err)
		return
	}
	fmt.Fprintf(s.opts.Log, "%s %s\n", direction, body)
}

// Run launches the DAP server message loop. It blocks until the client disconnects, the
// script terminates, or ctx is cancelled.
//
// Takes opts (ServerOptions) which supplies the IO streams and symbols.
//
// Returns int which is the exit code: 0 on clean disconnect, 1 on error.
func Run(ctx context.Context, opts ServerOptions) int {
	server := newServer(opts)
	if err := server.serve(ctx); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(opts.Stderr, "pipit dap: %v\n", err)
		return 1
	}
	return 0
}

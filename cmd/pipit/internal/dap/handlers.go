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

package dap

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/google/go-dap"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/safeconv"
)

// handleInitialize replies with the server's capabilities and emits the `initialized`
// event so the client can send setBreakpoints and configurationDone. It is always the
// first request in a DAP session.
//
// Takes request (*dap.InitializeRequest) which carries the request sequence.
func (s *server) handleInitialize(request *dap.InitializeRequest) {
	response := &dap.InitializeResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.Capabilities{
			SupportsConfigurationDoneRequest:  true,
			SupportsTerminateRequest:          true,
			SupportTerminateDebuggee:          true,
			SupportsConditionalBreakpoints:    true,
			SupportsHitConditionalBreakpoints: true,
			SupportsFunctionBreakpoints:       true,
			SupportsEvaluateForHovers:         true,
			SupportsSetVariable:               true,
			SupportsExceptionInfoRequest:      true,
			SupportsSteppingGranularity:       false,
			ExceptionBreakpointFilters: []dap.ExceptionBreakpointsFilter{{
				Filter:      string(debug.ExceptionFilterPanic),
				Label:       "Panics",
				Description: "Pause where panic is called, before deferred functions run",
				Default:     true,
			}},
		},
	}
	s.writeMessage(response)
	s.writeMessage(&dap.InitializedEvent{
		Event: s.newEvent("initialized"),
	})
}

// handleLaunch parses the JSON launch arguments and records the launcher.
//
// The compile-and-run logic lives in launcher.go; this handler wires the parsed config
// across and surfaces parse errors to the IDE.
//
// Takes request (*dap.LaunchRequest) which holds the JSON launch arguments.
//
// Concurrency: safe for concurrent use; acquires s.launcherMu.
func (s *server) handleLaunch(request *dap.LaunchRequest) {
	var args launchArguments
	if err := json.Unmarshal(request.Arguments, &args); err != nil {
		s.writeError(&request.Request, "pipit dap: invalid launch arguments: "+err.Error())
		return
	}
	if args.Program == "" {
		s.writeError(&request.Request, "pipit dap: launch arguments missing required field \"program\"")
		return
	}
	if args.Entrypoint == "" {
		args.Entrypoint = "main"
	}
	if args.Cache == "" {
		args.Cache = "off"
	}

	s.launcherMu.Lock()
	if s.launcher != nil {
		s.launcherMu.Unlock()
		s.writeError(&request.Request, "pipit dap: launch already in progress")
		return
	}
	l := &launcher{
		args:      args,
		debugger:  s.debugger,
		done:      make(chan struct{}),
		runCancel: nil,
		stopOnce:  sync.Once{},
	}
	s.launcher = l
	s.launcherMu.Unlock()

	s.writeMessage(&dap.LaunchResponse{
		Response: s.newResponse(request.Request, true),
	})
}

// handleSetBreakpoints replaces the breakpoint set for one source file and acknowledges
// with one dap.Breakpoint per requested line, carrying the debugger's verification: a
// breakpoint on a line without code is moved to the next line that has some, and a bad
// hit condition is reported unverified with its message.
//
// Takes request (*dap.SetBreakpointsRequest) which names the source and lines.
func (s *server) handleSetBreakpoints(request *dap.SetBreakpointsRequest) {
	args := request.Arguments
	wanted := make([]pipit.Breakpoint, 0, len(args.Breakpoints))
	for _, source := range args.Breakpoints {
		wanted = append(wanted, pipit.Breakpoint{Condition: source.Condition, HitCondition: source.HitCondition, Line: source.Line})
	}
	results := s.debugger.SetBreakpoints(args.Source.Path, wanted)

	breakpoints := make([]dap.Breakpoint, 0, len(results))
	for index, result := range results {
		breakpoint := dap.Breakpoint{
			Id:       result.ID,
			Verified: result.Verified,
			Message:  result.Message,
			Source:   &args.Source,
			Line:     result.Line,
		}
		if args.Breakpoints[index].LogMessage != "" {
			breakpoint.Verified = false
			breakpoint.Message = "logpoints are not supported"
		}
		breakpoints = append(breakpoints, breakpoint)
	}

	s.writeMessage(&dap.SetBreakpointsResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.SetBreakpointsResponseBody{
			Breakpoints: breakpoints,
		},
	})
}

// handleSetFunctionBreakpoints replaces the function breakpoints with the requested set.
//
// Takes request (*dap.SetFunctionBreakpointsRequest) which names the functions.
func (s *server) handleSetFunctionBreakpoints(request *dap.SetFunctionBreakpointsRequest) {
	wanted := make([]pipit.FunctionBreakpoint, 0, len(request.Arguments.Breakpoints))
	for _, breakpoint := range request.Arguments.Breakpoints {
		wanted = append(wanted, pipit.FunctionBreakpoint{Name: breakpoint.Name, Condition: breakpoint.Condition, HitCondition: breakpoint.HitCondition})
	}
	results := s.debugger.SetFunctionBreakpoints(wanted)
	breakpoints := make([]dap.Breakpoint, 0, len(results))
	for _, result := range results {
		breakpoints = append(breakpoints, dap.Breakpoint{Id: result.ID, Verified: result.Verified, Message: result.Message})
	}
	s.writeMessage(&dap.SetFunctionBreakpointsResponse{
		Response: s.newResponse(request.Request, true),
		Body:     dap.SetFunctionBreakpointsResponseBody{Breakpoints: breakpoints},
	})
}

// handleSetExceptionBreakpoints selects the exception filters; unknown filters are
// ignored.
//
// Takes request (*dap.SetExceptionBreakpointsRequest) which lists the filters.
func (s *server) handleSetExceptionBreakpoints(request *dap.SetExceptionBreakpointsRequest) {
	filters := make([]pipit.ExceptionFilter, 0, len(request.Arguments.Filters))
	for _, filter := range request.Arguments.Filters {
		filters = append(filters, pipit.ExceptionFilter(filter))
	}
	s.debugger.SetExceptionBreakpoints(filters)
	s.writeMessage(&dap.SetExceptionBreakpointsResponse{
		Response: s.newResponse(request.Request, true),
		Body:     dap.SetExceptionBreakpointsResponseBody{Breakpoints: nil},
	})
}

// handleConfigurationDone ends the configuration phase and starts the launcher goroutine.
//
// The response is an acknowledgement; once execution begins the launcher emits stopped,
// output, and terminated events. ctx is the serve context, not a per-request one, so the
// debug session it starts lives as long as the adapter and ends when the adapter is
// cancelled.
//
// Takes request (*dap.ConfigurationDoneRequest) which carries the sequence.
func (s *server) handleConfigurationDone(ctx context.Context, request *dap.ConfigurationDoneRequest) {
	s.launcherMu.Lock()
	l := s.launcher
	s.launcherMu.Unlock()

	if l == nil {
		s.writeError(&request.Request, "pipit dap: configurationDone received before launch")
		return
	}

	if err := l.start(ctx, s); err != nil {
		s.writeMessage(&dap.ConfigurationDoneResponse{
			Response: s.newResponse(request.Request, true),
		})
		s.writeOutput("stderr", "pipit dap: "+err.Error()+"\n")
		s.emitTerminated(1)
		return
	}

	s.writeMessage(&dap.ConfigurationDoneResponse{
		Response: s.newResponse(request.Request, true),
	})
}

// handleDisconnect tears the session down: it stops the running script, drains the
// launcher goroutine, acknowledges, and exits the message loop.
//
// Takes request (*dap.DisconnectRequest) which carries the request sequence.
func (s *server) handleDisconnect(request *dap.DisconnectRequest) {
	s.launcherMu.Lock()
	l := s.launcher
	s.launcherMu.Unlock()

	if l != nil {
		l.stop()
	}
	s.writeMessage(&dap.DisconnectResponse{
		Response: s.newResponse(request.Request, true),
	})
	s.done.Store(true)
	s.closeInput()
}

// handleTerminate terminates the debuggee without shutting down the adapter.
//
// The next disconnect ends the session. The response is sent first to keep the request
// and response pairing strict.
//
// Takes request (*dap.TerminateRequest) which carries the request sequence.
//
// Concurrency: safe for concurrent use; launcherMu guards the launcher read.
func (s *server) handleTerminate(request *dap.TerminateRequest) {
	s.launcherMu.Lock()
	l := s.launcher
	s.launcherMu.Unlock()

	if l != nil {
		l.stop()
	}
	s.writeMessage(&dap.TerminateResponse{
		Response: s.newResponse(request.Request, true),
	})
}

// emitTerminated tells the IDE that debugging has ended.
//
// Most clients tear down their UI on this event. It is paired with an exited event so the
// IDE also learns the process exit code.
//
// Takes exitCode (int) which is the process exit status reported to the IDE.
func (s *server) emitTerminated(exitCode int) {
	s.writeMessage(&dap.ExitedEvent{
		Event: s.newEvent("exited"),
		Body:  dap.ExitedEventBody{ExitCode: exitCode},
	})
	s.writeMessage(&dap.TerminatedEvent{
		Event: s.newEvent("terminated"),
	})
}

// emitStopped pushes a `stopped` event describing why a thread paused. Every other thread
// is parked too, which allThreadsStopped tells the IDE.
//
// Takes event (pipit.DebugEvent) which is the pause.
func (s *server) emitStopped(event pipit.DebugEvent) {
	body := dap.StoppedEventBody{
		Reason:            stoppedReason(event.Reason),
		Description:       "",
		ThreadId:          safeconv.Uint64ToInt(event.ThreadID),
		PreserveFocusHint: false,
		Text:              event.Message,
		AllThreadsStopped: true,
		HitBreakpointIds:  event.HitBreakpointIDs,
	}
	if event.Panic != nil {
		body.Description = "panic: " + event.Panic.Text
		body.Text = event.Panic.Text
	}
	s.writeMessage(&dap.StoppedEvent{Event: s.newEvent("stopped"), Body: body})
}

// emitThread pushes a `thread` event for a goroutine starting or exiting.
//
// Takes reason (string) which is "started" or "exited".
// Takes threadID (uint64) which is the debugger thread id.
func (s *server) emitThread(reason string, threadID uint64) {
	s.writeMessage(&dap.ThreadEvent{
		Event: s.newEvent("thread"),
		Body:  dap.ThreadEventBody{Reason: reason, ThreadId: safeconv.Uint64ToInt(threadID)},
	})
}

// writeOutput emits a single `output` event in the given category.
//
// It is used by the launcher's pipe pump and by handlers that surface adapter-side
// warnings in the client's debug console.
//
// Takes category (string) which selects the output stream, such as stderr.
// Takes text (string) which holds the message body written to the client.
func (s *server) writeOutput(category, text string) {
	s.writeMessage(&dap.OutputEvent{
		Event: s.newEvent("output"),
		Body: dap.OutputEventBody{
			Category: category,
			Output:   text,
		},
	})
}

// newResponse fills the boilerplate fields of a Response so each handler can focus on the
// body.
//
// Takes request (dap.Request) which supplies the sequence and command echo.
// Takes success (bool) which reports whether the request succeeded.
//
// Returns Response which carries the shared protocol fields.
func (s *server) newResponse(request dap.Request, success bool) dap.Response {
	return dap.Response{
		ProtocolMessage: dap.ProtocolMessage{Seq: s.nextSeq(), Type: "response"},
		RequestSeq:      request.Seq,
		Success:         success,
		Command:         request.Command,
	}
}

// newEvent fills the boilerplate fields of an Event so each emitter can focus on the
// body.
//
// Takes name (string) which is the DAP event name, such as "stopped".
//
// Returns Event which carries the shared protocol fields.
func (s *server) newEvent(name string) dap.Event {
	return dap.Event{
		ProtocolMessage: dap.ProtocolMessage{Seq: s.nextSeq(), Type: "event"},
		Event:           name,
	}
}

// stoppedReason translates a debugger stop reason into the DAP `reason` string.
//
// Takes reason (pipit.StopReason) which is the debugger's reason.
//
// Returns string which is the DAP stop reason.
func stoppedReason(reason pipit.StopReason) string {
	switch reason {
	case debug.StopReasonBreakpoint:
		return "breakpoint"
	case debug.StopReasonStep:
		return "step"
	case debug.StopReasonEntry:
		return "entry"
	case debug.StopReasonPanic:
		return "exception"
	case debug.StopReasonFunctionBreakpoint:
		return "function breakpoint"
	default:

		return "pause"
	}
}

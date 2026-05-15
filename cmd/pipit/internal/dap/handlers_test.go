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
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-dap"
)

func newTestServer() (*server, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	srv := newServer(ServerOptions{
		Stdin:  strings.NewReader(""),
		Stdout: out,
		Stderr: stderr,
	})
	return srv, out, stderr
}

func readOutMessages(t *testing.T, r *bytes.Buffer) []dap.Message {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(r.Bytes()))
	var messages []dap.Message
	for {
		if _, err := reader.Peek(1); err != nil {
			break
		}
		message, err := dap.ReadProtocolMessage(reader)
		if err != nil {
			t.Fatalf("ReadProtocolMessage: %v", err)
		}
		messages = append(messages, message)
	}
	return messages
}

func TestHandleInitializeReturnsCapabilitiesAndInitializedEvent(t *testing.T) {
	srv, out, _ := newTestServer()

	srv.handleInitialize(&dap.InitializeRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 1, Type: "request"}, Command: "initialize"},
	})

	messages := readOutMessages(t, out)
	if got := len(messages); got != 2 {
		t.Fatalf("messages: got %d, want 2 (response + initialized event)", got)
	}
	response, ok := messages[0].(*dap.InitializeResponse)
	if !ok {
		t.Fatalf("message[0]: got %T, want *dap.InitializeResponse", messages[0])
	}
	if !response.Success {
		t.Fatalf("response.Success: got false, want true")
	}
	if !response.Body.SupportsConfigurationDoneRequest {
		t.Fatalf("SupportsConfigurationDoneRequest: got false, want true")
	}
	if _, ok := messages[1].(*dap.InitializedEvent); !ok {
		t.Fatalf("message[1]: got %T, want *dap.InitializedEvent", messages[1])
	}
}

func TestHandleLaunchRejectsMissingProgram(t *testing.T) {
	srv, out, _ := newTestServer()

	args, _ := json.Marshal(map[string]any{})
	srv.handleLaunch(&dap.LaunchRequest{
		Request:   dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 2, Type: "request"}, Command: "launch"},
		Arguments: args,
	})

	messages := readOutMessages(t, out)
	if got := len(messages); got != 1 {
		t.Fatalf("messages: got %d, want 1 (error response)", got)
	}
	errResponse, ok := messages[0].(*dap.ErrorResponse)
	if !ok {
		t.Fatalf("message[0]: got %T, want *dap.ErrorResponse", messages[0])
	}
	if errResponse.Success {
		t.Fatalf("errResponse.Success: got true, want false")
	}
	if !strings.Contains(errResponse.Message, "program") {
		t.Fatalf("errResponse.Message: got %q, want to mention \"program\"", errResponse.Message)
	}
}

func TestHandleSetBreakpointsReplacesPriorSet(t *testing.T) {
	srv, out, _ := newTestServer()

	srv.handleSetBreakpoints(&dap.SetBreakpointsRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 3, Type: "request"}, Command: "setBreakpoints"},
		Arguments: dap.SetBreakpointsArguments{
			Source:      dap.Source{Path: "/tmp/script.go"},
			Breakpoints: []dap.SourceBreakpoint{{Line: 10}, {Line: 20}},
		},
	})

	messages := readOutMessages(t, out)
	if got := len(messages); got != 1 {
		t.Fatalf("messages: got %d, want 1", got)
	}
	response, ok := messages[0].(*dap.SetBreakpointsResponse)
	if !ok {
		t.Fatalf("message[0]: got %T, want *dap.SetBreakpointsResponse", messages[0])
	}
	if got := len(response.Body.Breakpoints); got != 2 {
		t.Fatalf("breakpoints: got %d, want 2", got)
	}
	if response.Body.Breakpoints[0].Line != 10 || response.Body.Breakpoints[1].Line != 20 {
		t.Fatalf("breakpoint lines: got %d,%d, want 10,20",
			response.Body.Breakpoints[0].Line, response.Body.Breakpoints[1].Line)
	}

	if !response.Body.Breakpoints[0].Verified {
		t.Fatalf("breakpoint[0].Verified: got false, want true (no program is bound, so lines are taken as given)")
	}
	if response.Body.Breakpoints[0].Id == 0 || response.Body.Breakpoints[0].Id == response.Body.Breakpoints[1].Id {
		t.Fatalf("breakpoint ids: got %d,%d, want two distinct non-zero ids", response.Body.Breakpoints[0].Id, response.Body.Breakpoints[1].Id)
	}
}

func TestDispatchUnknownCommandRespondsWithError(t *testing.T) {
	srv, out, _ := newTestServer()

	srv.dispatch(t.Context(), &dap.CancelRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 4, Type: "request"}, Command: "cancel"},
	})

	messages := readOutMessages(t, out)
	if got := len(messages); got != 1 {
		t.Fatalf("messages: got %d, want 1", got)
	}
	errResponse, ok := messages[0].(*dap.ErrorResponse)
	if !ok {
		t.Fatalf("message[0]: got %T, want *dap.ErrorResponse", messages[0])
	}
	if !strings.Contains(errResponse.Message, "unsupported") {
		t.Fatalf("errResponse.Message: got %q, want to mention \"unsupported\"", errResponse.Message)
	}
}

func TestHandleThreadsIsEmptyWhileNothingRuns(t *testing.T) {
	srv, out, _ := newTestServer()

	srv.handleThreads(&dap.ThreadsRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 5, Type: "request"}, Command: "threads"},
	})

	messages := readOutMessages(t, out)
	response, ok := messages[0].(*dap.ThreadsResponse)
	if !ok {
		t.Fatalf("message[0]: got %T, want *dap.ThreadsResponse", messages[0])
	}
	if got := len(response.Body.Threads); got != 0 {
		t.Fatalf("threads: got %d, want 0 before the program runs", got)
	}
}

func TestHandleSetBreakpointsReportsBadHitCondition(t *testing.T) {
	srv, out, _ := newTestServer()

	srv.handleSetBreakpoints(&dap.SetBreakpointsRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 6, Type: "request"}, Command: "setBreakpoints"},
		Arguments: dap.SetBreakpointsArguments{
			Source:      dap.Source{Path: "/tmp/script.go"},
			Breakpoints: []dap.SourceBreakpoint{{Line: 3, HitCondition: "every third"}, {Line: 4, LogMessage: "hello"}},
		},
	})

	messages := readOutMessages(t, out)
	response, ok := messages[0].(*dap.SetBreakpointsResponse)
	if !ok {
		t.Fatalf("message[0]: got %T, want *dap.SetBreakpointsResponse", messages[0])
	}
	if response.Body.Breakpoints[0].Verified || !strings.Contains(response.Body.Breakpoints[0].Message, "hit condition") {
		t.Fatalf("bad hit condition: got %+v, want unverified with a message", response.Body.Breakpoints[0])
	}
	if response.Body.Breakpoints[1].Verified || !strings.Contains(response.Body.Breakpoints[1].Message, "logpoints") {
		t.Fatalf("logpoint: got %+v, want unverified with a message", response.Body.Breakpoints[1])
	}
}

func TestControlRequestsFailWhileNothingIsPaused(t *testing.T) {
	srv, out, _ := newTestServer()
	request := dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 7, Type: "request"}}
	srv.handleContinue(&dap.ContinueRequest{Request: request, Arguments: dap.ContinueArguments{ThreadId: 1}})
	srv.handleNext(&dap.NextRequest{Request: request, Arguments: dap.NextArguments{ThreadId: 1}})
	srv.handlePause(&dap.PauseRequest{Request: request, Arguments: dap.PauseArguments{ThreadId: 1}})
	srv.handleEvaluate(&dap.EvaluateRequest{Request: request, Arguments: dap.EvaluateArguments{Expression: "1", Context: "hover"}})

	messages := readOutMessages(t, out)
	if got := len(messages); got != 4 {
		t.Fatalf("messages: got %d, want 4 error responses", got)
	}
	for index, message := range messages {
		errResponse, ok := message.(*dap.ErrorResponse)
		if !ok {
			t.Fatalf("message[%d]: got %T, want *dap.ErrorResponse", index, message)
		}
		if index == 3 && errResponse.Body.Error.ShowUser {
			t.Fatalf("a failed hover evaluation must not be shown to the user")
		}
	}
}

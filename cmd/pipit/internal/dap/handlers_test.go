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
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err)
		messages = append(messages, message)
	}
	return messages
}

func TestHandleInitializeReturnsCapabilitiesAndInitializedEvent(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()

	srv.handleInitialize(&dap.InitializeRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 1, Type: "request"}, Command: "initialize"},
	})

	messages := readOutMessages(t, out)
	require.Len(t, messages, 2)
	response, ok := messages[0].(*dap.InitializeResponse)
	require.True(t, ok, "the message is a *dap.InitializeResponse, not %T", messages[0])
	require.True(t, response.Success, "the response succeeds")
	require.True(t, response.Body.SupportsConfigurationDoneRequest, "configurationDone is supported")
	require.IsType(t, &dap.InitializedEvent{}, messages[1])
}

func TestHandleLaunchRejectsMissingProgram(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()

	args, _ := json.Marshal(map[string]any{})
	srv.handleLaunch(&dap.LaunchRequest{
		Request:   dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 2, Type: "request"}, Command: "launch"},
		Arguments: args,
	})

	messages := readOutMessages(t, out)
	require.Len(t, messages, 1)
	errResponse, ok := messages[0].(*dap.ErrorResponse)
	require.True(t, ok, "the message is a *dap.ErrorResponse, not %T", messages[0])
	require.False(t, errResponse.Success, "the response fails")
	require.Contains(t, errResponse.Message, "program")
}

func TestHandleSetBreakpointsReplacesPriorSet(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()

	srv.handleSetBreakpoints(&dap.SetBreakpointsRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 3, Type: "request"}, Command: "setBreakpoints"},
		Arguments: dap.SetBreakpointsArguments{
			Source:      dap.Source{Path: "/tmp/script.go"},
			Breakpoints: []dap.SourceBreakpoint{{Line: 10}, {Line: 20}},
		},
	})

	messages := readOutMessages(t, out)
	require.Len(t, messages, 1)
	response, ok := messages[0].(*dap.SetBreakpointsResponse)
	require.True(t, ok, "the message is a *dap.SetBreakpointsResponse, not %T", messages[0])
	require.Len(t, response.Body.Breakpoints, 2)
	require.Equal(t, 10, response.Body.Breakpoints[0].Line)
	require.Equal(t, 20, response.Body.Breakpoints[1].Line)

	require.True(t, response.Body.Breakpoints[0].Verified, "without a bound program, lines are taken as given")
	require.NotEqual(t, 0, response.Body.Breakpoints[0].Id)
	require.NotEqual(t, response.Body.Breakpoints[1].Id, response.Body.Breakpoints[0].Id)
}

func TestDispatchUnknownCommandRespondsWithError(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()

	srv.dispatch(t.Context(), &dap.CancelRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 4, Type: "request"}, Command: "cancel"},
	})

	messages := readOutMessages(t, out)
	require.Len(t, messages, 1)
	errResponse, ok := messages[0].(*dap.ErrorResponse)
	require.True(t, ok, "the message is a *dap.ErrorResponse, not %T", messages[0])
	require.Contains(t, errResponse.Message, "unsupported")
}

func TestHandleThreadsIsEmptyWhileNothingRuns(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()

	srv.handleThreads(&dap.ThreadsRequest{
		Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 5, Type: "request"}, Command: "threads"},
	})

	messages := readOutMessages(t, out)
	response, ok := messages[0].(*dap.ThreadsResponse)
	require.True(t, ok, "the message is a *dap.ThreadsResponse, not %T", messages[0])
	require.Empty(t, response.Body.Threads)
}

func TestHandleSetBreakpointsReportsBadHitCondition(t *testing.T) {
	t.Parallel()
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
	require.True(t, ok, "the message is a *dap.SetBreakpointsResponse, not %T", messages[0])
	require.False(t, response.Body.Breakpoints[0].Verified, "a bad hit condition leaves the breakpoint unverified")
	require.Contains(t, response.Body.Breakpoints[0].Message, "hit condition")
	require.False(t, response.Body.Breakpoints[1].Verified, "a logpoint is unverified")
	require.Contains(t, response.Body.Breakpoints[1].Message, "logpoints")
}

func TestControlRequestsFailWhileNothingIsPaused(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()
	request := dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 7, Type: "request"}}
	srv.handleContinue(&dap.ContinueRequest{Request: request, Arguments: dap.ContinueArguments{ThreadId: 1}})
	srv.handleNext(&dap.NextRequest{Request: request, Arguments: dap.NextArguments{ThreadId: 1}})
	srv.handlePause(&dap.PauseRequest{Request: request, Arguments: dap.PauseArguments{ThreadId: 1}})
	srv.handleEvaluate(t.Context(), &dap.EvaluateRequest{Request: request, Arguments: dap.EvaluateArguments{Expression: "1", Context: "hover"}})

	messages := readOutMessages(t, out)
	require.Len(t, messages, 4)
	for index, message := range messages {
		errResponse, ok := message.(*dap.ErrorResponse)
		require.True(t, ok, "message %d is %T", index, message)
		if index == 3 {
			require.False(t, errResponse.Body.Error.ShowUser, "a failed hover evaluation is not shown to the user")
		}
	}
}

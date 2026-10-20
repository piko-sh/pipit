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
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-dap"
	"github.com/stretchr/testify/require"
)

const e2eProgram = `package main

import (
	"fmt"
	"sort"
)

func worker(out chan int) {
	v := 40
	out <- v + 2
}

func main() {
	out := make(chan int)
	go worker(out)
	got := <-out
	xs := []int{3, 1, 2}
	sort.Slice(xs, func(i, j int) bool {
		less := xs[i] < xs[j]
		return less
	})
	fmt.Println(got, xs)
}
`

type dapClient struct {
	toServer io.WriteCloser
	incoming chan dap.Message
	seq      int
	loopDone chan error
}

func startDAPClient(t *testing.T) *dapClient {
	t.Helper()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	srv := newServer(ServerOptions{Stdin: serverIn, Stdout: serverOut, Stderr: &bytes.Buffer{}, Log: nil, Symbols: nil})
	client := &dapClient{toServer: clientOut, incoming: make(chan dap.Message, 256), seq: 0, loopDone: make(chan error, 1)}
	go func() { client.loopDone <- srv.serve(context.Background()) }()
	var readers sync.WaitGroup
	readers.Go(func() {
		reader := bufio.NewReader(clientIn)
		for {
			message, err := dap.ReadProtocolMessage(reader)
			if err != nil {
				close(client.incoming)
				return
			}
			client.incoming <- message
		}
	})
	t.Cleanup(func() {
		_ = clientOut.Close()
		_ = serverOut.Close()
		readers.Wait()
	})
	return client
}

func (c *dapClient) send(t *testing.T, request dap.RequestMessage) int {
	t.Helper()
	c.seq++
	request.GetRequest().Seq = c.seq
	request.GetRequest().Type = "request"
	require.NoError(t, dap.WriteProtocolMessage(c.toServer, request), "write %s", request.GetRequest().Command)
	return c.seq
}

func expect[T dap.Message](t *testing.T, c *dapClient, what string) T {
	t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case message, ok := <-c.incoming:
			require.True(t, ok, "the connection stays open while waiting for %s", what)
			if typed, isWanted := message.(T); isWanted {
				return typed
			}
			if errResponse, isErr := message.(*dap.ErrorResponse); isErr {
				require.Failf(t, "error response", "while waiting for %s: %s", what, errResponse.Message)
			}
		case <-deadline:
			require.Failf(t, "timed out", "waiting for %s", what)
		}
	}
}

func TestSessionDebugsGoroutinesCallbacksAndEvaluation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(path, []byte(e2eProgram), 0o600))
	c := startDAPClient(t)

	c.send(t, &dap.InitializeRequest{Request: dap.Request{Command: "initialize"}})
	initialised := expect[*dap.InitializeResponse](t, c, "initialize response")
	require.True(t, initialised.Body.SupportsConditionalBreakpoints, "conditional breakpoints are supported")
	require.True(t, initialised.Body.SupportsFunctionBreakpoints, "function breakpoints are supported")
	require.Len(t, initialised.Body.ExceptionBreakpointFilters, 1)
	expect[*dap.InitializedEvent](t, c, "initialized event")

	launchArgs, _ := json.Marshal(map[string]any{"program": path})
	c.send(t, &dap.LaunchRequest{Request: dap.Request{Command: "launch"}, Arguments: launchArgs})
	expect[*dap.LaunchResponse](t, c, "launch response")

	c.send(t, &dap.SetBreakpointsRequest{Request: dap.Request{Command: "setBreakpoints"}, Arguments: dap.SetBreakpointsArguments{
		Source:      dap.Source{Path: path},
		Breakpoints: []dap.SourceBreakpoint{{Line: 10}, {Line: 19, Condition: "i == 1"}},
	}})
	breakpoints := expect[*dap.SetBreakpointsResponse](t, c, "setBreakpoints response")
	require.Len(t, breakpoints.Body.Breakpoints, 2)
	require.True(t, breakpoints.Body.Breakpoints[0].Verified)

	c.send(t, &dap.ConfigurationDoneRequest{Request: dap.Request{Command: "configurationDone"}})
	expect[*dap.ConfigurationDoneResponse](t, c, "configurationDone response")

	stopped := expect[*dap.StoppedEvent](t, c, "first stopped event")
	require.Equal(t, "breakpoint", stopped.Body.Reason)
	require.True(t, stopped.Body.AllThreadsStopped, "a stop pauses every thread")
	c.send(t, &dap.ThreadsRequest{Request: dap.Request{Command: "threads"}})
	threads := expect[*dap.ThreadsResponse](t, c, "threads response")
	require.GreaterOrEqual(t, len(threads.Body.Threads), 2)
	c.send(t, &dap.StackTraceRequest{Request: dap.Request{Command: "stackTrace"}, Arguments: dap.StackTraceArguments{ThreadId: stopped.Body.ThreadId}})
	stack := expect[*dap.StackTraceResponse](t, c, "stackTrace response")
	require.NotEmpty(t, stack.Body.StackFrames)
	require.Equal(t, "main.worker", stack.Body.StackFrames[0].Name)
	require.Equal(t, 10, stack.Body.StackFrames[0].Line)
	frameID := stack.Body.StackFrames[0].Id
	c.send(t, &dap.ScopesRequest{Request: dap.Request{Command: "scopes"}, Arguments: dap.ScopesArguments{FrameId: frameID}})
	scopes := expect[*dap.ScopesResponse](t, c, "scopes response")
	require.NotEmpty(t, scopes.Body.Scopes)
	require.Equal(t, "Locals", scopes.Body.Scopes[0].Name)
	c.send(t, &dap.VariablesRequest{Request: dap.Request{Command: "variables"}, Arguments: dap.VariablesArguments{VariablesReference: scopes.Body.Scopes[0].VariablesReference}})
	variables := expect[*dap.VariablesResponse](t, c, "variables response")
	require.Equal(t, "40", variableValue(variables.Body.Variables, "v"), "worker locals: %+v", variables.Body.Variables)
	c.send(t, &dap.EvaluateRequest{Request: dap.Request{Command: "evaluate"}, Arguments: dap.EvaluateArguments{Expression: "v + 2", FrameId: frameID, Context: "watch"}})
	evaluated := expect[*dap.EvaluateResponse](t, c, "evaluate response")
	require.Equal(t, "42", evaluated.Body.Result)
	require.Equal(t, "int", evaluated.Body.Type)
	c.send(t, &dap.SetVariableRequest{Request: dap.Request{Command: "setVariable"}, Arguments: dap.SetVariableArguments{VariablesReference: scopes.Body.Scopes[0].VariablesReference, Name: "v", Value: "50"}})
	updated := expect[*dap.SetVariableResponse](t, c, "setVariable response")
	require.Equal(t, "50", updated.Body.Value)
	c.send(t, &dap.EvaluateRequest{Request: dap.Request{Command: "evaluate"}, Arguments: dap.EvaluateArguments{Expression: "v", FrameId: frameID, Context: "repl"}})
	written := expect[*dap.EvaluateResponse](t, c, "evaluate after setVariable")
	require.Equal(t, "50", written.Body.Result, "the evaluator sees the new value")

	c.send(t, &dap.ContinueRequest{Request: dap.Request{Command: "continue"}, Arguments: dap.ContinueArguments{ThreadId: stopped.Body.ThreadId}})
	expect[*dap.ContinueResponse](t, c, "continue response")

	callback := expect[*dap.StoppedEvent](t, c, "callback stopped event")
	c.send(t, &dap.StackTraceRequest{Request: dap.Request{Command: "stackTrace"}, Arguments: dap.StackTraceArguments{ThreadId: callback.Body.ThreadId}})
	callbackStack := expect[*dap.StackTraceResponse](t, c, "callback stackTrace response")
	require.NotEmpty(t, callbackStack.Body.StackFrames)
	require.Equal(t, "main.main.func1", callbackStack.Body.StackFrames[0].Name)
	require.Equal(t, 19, callbackStack.Body.StackFrames[0].Line)
	c.send(t, &dap.ContinueRequest{Request: dap.Request{Command: "continue"}, Arguments: dap.ContinueArguments{ThreadId: callback.Body.ThreadId}})
	expect[*dap.ContinueResponse](t, c, "second continue response")

	for {
		message, ok := <-c.incoming
		require.True(t, ok, "the connection stays open until the program exits")
		if stop, isStop := message.(*dap.StoppedEvent); isStop && stop.Body.Reason == "breakpoint" {
			c.send(t, &dap.ContinueRequest{Request: dap.Request{Command: "continue"}, Arguments: dap.ContinueArguments{ThreadId: stop.Body.ThreadId}})
			continue
		}
		if exited, isExit := message.(*dap.ExitedEvent); isExit {
			require.Equal(t, 0, exited.Body.ExitCode)
			break
		}
	}
	expect[*dap.TerminatedEvent](t, c, "terminated event")

	c.send(t, &dap.DisconnectRequest{Request: dap.Request{Command: "disconnect"}})
	expect[*dap.DisconnectResponse](t, c, "disconnect response")
	select {
	case err := <-c.loopDone:
		if err != nil {
			require.ErrorIs(t, err, io.EOF, "serve stops cleanly")
		}
	case <-time.After(30 * time.Second):
		require.FailNow(t, "serve returns after disconnect")
	}
}

func variableValue(variables []dap.Variable, name string) string {
	for _, variable := range variables {
		if variable.Name == name {
			return variable.Value
		}
	}
	return ""
}

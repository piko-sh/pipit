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
	"reflect"
	"strconv"
	"strings"

	"github.com/google/go-dap"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
)

// handleEvaluate evaluates an expression in a paused frame. Hover evaluations fail
// quietly and may not call functions; watch and REPL evaluations report errors to the
// user and may call interpreted and host functions.
//
// Takes request (*dap.EvaluateRequest) which carries the expression, frame and context.
func (s *server) handleEvaluate(request *dap.EvaluateRequest) {
	stop := s.currentStop()
	if stop == nil {
		s.writeEvaluateError(request, "pipit dap: the program is not paused")
		return
	}
	frame, ok := stop.frameFor(request.Arguments.FrameId)
	if !ok {
		frame = frameRef{threadID: stop.event.ThreadID, frameIndex: 0}
	}
	hover := request.Arguments.Context == "hover"
	result, err := s.debugger.Evaluate(context.Background(), frame.threadID, frame.frameIndex, request.Arguments.Expression, pipit.EvalOptions{Timeout: 0, AllowCalls: !hover})
	if err != nil {
		s.writeEvaluateError(request, "pipit dap: "+err.Error())
		return
	}
	value := reflect.ValueOf(result.Value)
	variable := s.makeVariable(stop, "", result.Type, value)
	s.writeMessage(&dap.EvaluateResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.EvaluateResponseBody{
			Result:             variable.Value,
			Type:               variable.Type,
			VariablesReference: variable.VariablesReference,
			IndexedVariables:   variable.IndexedVariables,
		},
	})
}

// writeEvaluateError reports a failed evaluation; a hover failure is not shown to the
// user.
//
// Takes request (*dap.EvaluateRequest) which failed.
// Takes message (string) which explains why.
func (s *server) writeEvaluateError(request *dap.EvaluateRequest, message string) {
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
				ShowUser: request.Arguments.Context != "hover",
			},
		},
	}
	s.writeMessage(response)
}

// handleSetVariable writes a scalar or string local of a paused frame from its text.
//
// Takes request (*dap.SetVariableRequest) which names the scope container, variable and
// new value.
func (s *server) handleSetVariable(request *dap.SetVariableRequest) {
	stop := s.currentStop()
	if stop == nil {
		s.writeError(&request.Request, "pipit dap: the program is not paused")
		return
	}
	container, ok := stop.lookupContainer(request.Arguments.VariablesReference)
	if !ok || container.kind != containerKindScope {
		s.writeError(&request.Request, "pipit dap: setVariable needs a scope reference")
		return
	}
	value, err := parseVariableText(request.Arguments.Value)
	if err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	updated, err := s.debugger.SetVariable(container.frame.threadID, container.frame.frameIndex, request.Arguments.Name, value)
	if err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	variable := s.makeVariable(stop, updated.Name, updated.Type, reflect.ValueOf(updated.Value))
	s.writeMessage(&dap.SetVariableResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.SetVariableResponseBody{
			Value:              variable.Value,
			Type:               variable.Type,
			VariablesReference: variable.VariablesReference,
		},
	})
}

// parseVariableText interprets the text a client sends for a new variable value: a quoted
// string, true or false, an integer, a float, or otherwise the raw text as a string.
//
// Takes text (string) which is the client's input.
//
// Returns any which is the parsed value.
// Returns error which is nil (kept for future forms).
func parseVariableText(text string) (any, error) {
	trimmed := strings.TrimSpace(text)
	if unquoted, err := strconv.Unquote(trimmed); err == nil {
		return unquoted, nil
	}
	switch trimmed {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if integer, err := strconv.ParseInt(trimmed, 0, 64); err == nil {
		return integer, nil
	}
	if unsigned, err := strconv.ParseUint(trimmed, 0, 64); err == nil {
		return unsigned, nil
	}
	if float, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return float, nil
	}
	return trimmed, nil
}

// handleExceptionInfo describes the panic the current stop paused on.
//
// Takes request (*dap.ExceptionInfoRequest) which carries the thread and sequence number.
func (s *server) handleExceptionInfo(request *dap.ExceptionInfoRequest) {
	stop := s.currentStop()
	if stop == nil || stop.event.Panic == nil {
		s.writeError(&request.Request, "pipit dap: the program is not paused on a panic")
		return
	}
	panicInfo := stop.event.Panic
	s.writeMessage(&dap.ExceptionInfoResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.ExceptionInfoResponseBody{
			ExceptionId: string(debug.ExceptionFilterPanic),
			Description: panicInfo.Text,
			BreakMode:   "always",
			Details: &dap.ExceptionDetails{
				Message:  panicInfo.Text,
				TypeName: panicInfo.Type,
			},
		},
	})
}

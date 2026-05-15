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
	"path/filepath"

	"github.com/google/go-dap"
	"pipit.sh/pipit/internal/safeconv"
)

// handleThreads answers a DAP threads request with one thread per live interpreted
// goroutine, callback or method VM.
//
// Takes request (*dap.ThreadsRequest) which carries the request sequence number.
func (s *server) handleThreads(request *dap.ThreadsRequest) {
	threads := s.debugger.Threads()
	out := make([]dap.Thread, 0, len(threads))
	for _, thread := range threads {
		out = append(out, dap.Thread{Id: safeconv.Uint64ToInt(thread.ID), Name: thread.Name})
	}
	s.writeMessage(&dap.ThreadsResponse{
		Response: s.newResponse(request.Request, true),
		Body:     dap.ThreadsResponseBody{Threads: out},
	})
}

// handleStackTrace returns one paused thread's call stack, applying DAP startFrame/levels
// pagination.
//
// Each frame gets a DAP frameId from the per-stop allocator so downstream scopes,
// variables and evaluate requests route back to the thread and frame.
//
// Takes request (*dap.StackTraceRequest) which selects the thread and frame window.
func (s *server) handleStackTrace(request *dap.StackTraceRequest) {
	stop := s.currentStop()
	threadID := safeconv.IntToUint64(request.Arguments.ThreadId)
	frames, err := s.debugger.StackTrace(threadID)
	if stop == nil || err != nil {
		s.writeMessage(&dap.StackTraceResponse{
			Response: s.newResponse(request.Request, true),
			Body:     dap.StackTraceResponseBody{StackFrames: nil, TotalFrames: 0},
		})
		return
	}

	start := max(request.Arguments.StartFrame, 0)
	start = min(start, len(frames))
	end := len(frames)
	if request.Arguments.Levels > 0 {
		end = min(end, start+request.Arguments.Levels)
	}

	out := make([]dap.StackFrame, 0, end-start)
	for index := start; index < end; index++ {
		frame := frames[index]
		out = append(out, dap.StackFrame{
			Id:     stop.allocateFrame(frameRef{threadID: threadID, frameIndex: index}),
			Name:   frame.Function,
			Source: sourceFor(frame.File),
			Line:   frame.Line,
			Column: frame.Column,
		})
	}

	s.writeMessage(&dap.StackTraceResponse{
		Response: s.newResponse(request.Request, true),
		Body: dap.StackTraceResponseBody{
			StackFrames: out,
			TotalFrames: len(frames),
		},
	})
}

// handleScopes returns the variable scopes of a frame: locals, the closure's captures
// when there are any, and the package globals.
//
// Takes request (*dap.ScopesRequest) which identifies the frame whose scopes are
// requested.
func (s *server) handleScopes(request *dap.ScopesRequest) {
	stop := s.currentStop()
	if stop == nil {
		s.writeMessage(&dap.ScopesResponse{Response: s.newResponse(request.Request, true), Body: dap.ScopesResponseBody{Scopes: nil}})
		return
	}
	frame, ok := stop.frameFor(request.Arguments.FrameId)
	if !ok {
		s.writeError(&request.Request, "pipit dap: unknown frameId")
		return
	}
	scopes, err := s.debugger.Scopes(frame.threadID, frame.frameIndex)
	if err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	out := make([]dap.Scope, 0, len(scopes))
	for _, scope := range scopes {
		reference := stop.allocateContainer(variableContainer{kind: containerKindScope, frame: frame, scope: scope.Kind, value: reflectZero})
		out = append(out, dap.Scope{
			Name:               scope.Name,
			VariablesReference: reference,
			NamedVariables:     scope.Count,
			Expensive:          scope.Expensive,
		})
	}
	s.writeMessage(&dap.ScopesResponse{
		Response: s.newResponse(request.Request, true),
		Body:     dap.ScopesResponseBody{Scopes: out},
	})
}

// handleContinue resumes every paused thread.
//
// Per DAP the response is sent immediately; the subsequent `stopped` event (emitted by
// the launcher's event loop) signals when the program pauses again.
//
// Takes request (*dap.ContinueRequest) which carries the request sequence number.
func (s *server) handleContinue(request *dap.ContinueRequest) {
	if err := s.debugger.Continue(); err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	s.clearStop()
	s.writeMessage(&dap.ContinueResponse{
		Response: s.newResponse(request.Request, true),
		Body:     dap.ContinueResponseBody{AllThreadsContinued: true},
	})
}

// handleNext steps the named thread to the next statement at the same call depth ("step
// over"); the other threads run until it pauses again.
//
// Takes request (*dap.NextRequest) which carries the thread and sequence number.
func (s *server) handleNext(request *dap.NextRequest) {
	if err := s.debugger.StepOver(safeconv.IntToUint64(request.Arguments.ThreadId)); err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	s.clearStop()
	s.writeMessage(&dap.NextResponse{Response: s.newResponse(request.Request, true)})
}

// handleStepIn steps the named thread into the next call, including a host callback.
//
// Takes request (*dap.StepInRequest) which carries the thread and sequence number.
func (s *server) handleStepIn(request *dap.StepInRequest) {
	if err := s.debugger.StepIn(safeconv.IntToUint64(request.Arguments.ThreadId)); err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	s.clearStop()
	s.writeMessage(&dap.StepInResponse{Response: s.newResponse(request.Request, true)})
}

// handleStepOut runs the named thread to the end of its current function and pauses in
// the caller.
//
// Takes request (*dap.StepOutRequest) which carries the thread and sequence number.
func (s *server) handleStepOut(request *dap.StepOutRequest) {
	if err := s.debugger.StepOut(safeconv.IntToUint64(request.Arguments.ThreadId)); err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	s.clearStop()
	s.writeMessage(&dap.StepOutResponse{Response: s.newResponse(request.Request, true)})
}

// handlePause suspends the running program at its next safe point; the pause is reported
// through a `stopped` event with reason "pause".
//
// Takes request (*dap.PauseRequest) which carries the request sequence number.
func (s *server) handlePause(request *dap.PauseRequest) {
	if err := s.debugger.Pause(); err != nil {
		s.writeError(&request.Request, "pipit dap: "+err.Error())
		return
	}
	s.writeMessage(&dap.PauseResponse{Response: s.newResponse(request.Request, true)})
}

// currentStop returns the active stop state for IDE queries about the paused program.
//
// Returns *stopState which is the stop in effect, or nil when nothing is paused.
//
// Concurrency: safe for concurrent use; launcherMu guards the field.
func (s *server) currentStop() *stopState {
	s.launcherMu.Lock()
	defer s.launcherMu.Unlock()
	return s.stop
}

// setStop installs the stop state for a new pause.
//
// Takes stop (*stopState) which describes the pause.
//
// Concurrency: safe for concurrent use; launcherMu guards the field.
func (s *server) setStop(stop *stopState) {
	s.launcherMu.Lock()
	defer s.launcherMu.Unlock()
	s.stop = stop
}

// clearStop forgets the stop state once the program resumes.
func (s *server) clearStop() {
	s.setStop(nil)
}

// sourceFor wraps a filesystem path in a DAP Source descriptor.
//
// The IDE uses Path to locate the file on disk for breakpoint matching and Name as the
// tab title.
//
// Takes path (string) which is the filesystem path to describe.
//
// Returns *Source which describes the file, or nil when path is empty.
func sourceFor(path string) *dap.Source {
	if path == "" {
		return nil
	}
	return &dap.Source{
		Name: shortName(path),
		Path: path,
	}
}

// shortName returns the base name of a path for display.
//
// Takes path (string) which is the filesystem path.
//
// Returns string which is the final path element.
func shortName(path string) string {
	return filepath.Base(path)
}

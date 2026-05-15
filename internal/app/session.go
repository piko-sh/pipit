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

package app

import (
	"context"
	"io"
	"slices"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"
)

// SessionDeclKind classifies a session-scope declaration so the session can perform
// name-based redeclaration checks and so consumers (REPL, notebook UI) can render the
// right glyph next to each entry.
type SessionDeclKind int

const (
	// SessionDeclVar identifies a session-scope `var` declaration.
	SessionDeclVar SessionDeclKind = iota + 1

	// SessionDeclFunc identifies a session-scope function declaration.
	SessionDeclFunc

	// SessionDeclType identifies a session-scope `type` declaration.
	SessionDeclType

	// SessionDeclConst identifies a session-scope `const` declaration.
	SessionDeclConst
)

// String returns the lowercase Go keyword for the declaration kind.
//
// Returns string which is the kind's keyword, or "?" when unknown.
func (k SessionDeclKind) String() string {
	switch k {
	case SessionDeclVar:
		return "var"
	case SessionDeclFunc:
		return "func"
	case SessionDeclType:
		return "type"
	case SessionDeclConst:
		return "const"
	default:
		return "?"
	}
}

// SessionDecl describes a single session-scope declaration. The pair (Name, Kind) is the
// redeclaration key; two decls with the same Name are considered to clash regardless of
// Kind.
type SessionDecl struct {
	// Name is the bare identifier declared.
	Name string

	// Kind is the declaration kind (var / func / type / const).
	Kind SessionDeclKind
}

// SessionState is a point-in-time snapshot of what the session currently knows. It is
// intended for human-facing tooling (REPL `:inspect`, notebook outline panel) and is safe
// to retain past the Inspect() call that produced it.
type SessionState struct {
	// Imports lists every import path currently in scope. Sorted.
	Imports []string

	// Declarations lists every declared name in declaration order.
	Declarations []SessionDecl

	// SubmitCount is the number of successful Submit calls.
	SubmitCount int
}

// SessionOption configures a Session at construction.
type SessionOption func(*sessionConfig)

// Session accumulates state across Submit calls to provide REPL-style semantics on top of
// Service. Not safe for concurrent use; distinct workspaces should use distinct Services
// because they share the global value store.
type Session struct {
	// service is the backing interpreter. Symbols, limits, features, and globals come from
	// here; the session owns nothing the service also owns.
	service *Service

	// rootFunction holds the persistent function table that grows as new session-scope
	// functions compile. Lazy-initialised on first Submit so a discarded session allocates
	// nothing.
	rootFunction *program.CompiledFunction

	// imports maps each import path currently in scope to its alias ("" for none, "." for a
	// dot import, "_" for blank). Insertion order is preserved separately via importOrder.
	imports map[string]string

	// declaredNames maps a declared identifier to its kind, used for O(1) redeclaration
	// detection before re-Check.
	declaredNames map[string]SessionDeclKind

	// functionTable maps function name to index in ExportFunctions. Persistent across
	// Submits so the per-Submit Compiler resolves references to previously-declared
	// functions via the standard compileIdent path.
	functionTable map[string]uint16

	// globalVariables maps package-level variable name to its index and register kind in the
	// backing GlobalStore. Persistent across Submits so identifier resolution sees
	// previously-declared vars.
	globalVariables map[string]program.GlobalVariableInfo

	// compiledDecls tracks declaration names whose bytecode has been emitted; the per-Submit
	// compile loop skips entries in this set rather than re-walking and clashing on indices.
	compiledDecls map[string]bool

	// executedInits tracks init function indices that have already run, gating Go's
	// once-per-program init() semantics for the session.
	executedInits map[uint16]bool

	// compiledInits tracks init bodies already executed, preventing re-runs on subsequent
	// Submits.
	compiledInits map[string]bool

	// importOrder remembers the order in which imports were first introduced, for
	// deterministic Inspect rendering.
	importOrder []string

	// decls is the ordered list of session-scope declarations as the user introduced them.
	// Each entry's source is appended verbatim (post-shortVar rewrite) to the synthesised
	// file on every Submit.
	decls []declRecord

	// submitCount counts successful Submit calls. Failed Submits do not increment.
	submitCount int
}

// Service returns the underlying interpreter for callers that need to reach below the
// session abstraction (e.g. to register additional host symbols mid-session).
//
// Returns *Service which is the backing service.
func (sess *Session) Service() *Service {
	return sess.service
}

// SetStderr redirects print/println output for every subsequent Submit. REPL and notebook
// hosts use this to capture output for display in their own UI rather than letting it
// leak to the real process stderr (where it would be invisible inside a Bubble Tea
// alt-screen, for example).
//
// Takes writer (io.Writer) which receives output, or nil to reset to the default.
func (sess *Session) SetStderr(writer io.Writer) {
	sess.service.SetStderr(writer)
}

// Submit evaluates one user submission against the session. Declaration metadata is
// committed only on success.
//
// Takes code (string) which is the Go source the user submitted.
//
// Returns any which is the value of the trailing expression, or nil.
// Returns error when preprocessing, classification, type-checking, compilation, or
// execution fails.
func (sess *Session) Submit(ctx context.Context, code string) (any, error) {
	ctx = logging.ContextWithLogger(ctx, sess.service.config.logger)
	ctx, endDebug := sess.service.beginDebugExecution(ctx)
	result, err := sess.submit(ctx, code)
	endDebug(err)
	if err != nil {
		return nil, engine.NewSafeError(safeMessageSubmissionFailed, err)
	}
	return result, nil
}

// Reset clears all session-scope declarations and resets the backing service's global
// value store. The symbol registry (host packages) and the service itself are preserved.
func (sess *Session) Reset() {
	sess.imports = make(map[string]string)
	sess.importOrder = sess.importOrder[:0]
	sess.decls = sess.decls[:0]
	sess.declaredNames = make(map[string]SessionDeclKind)
	sess.rootFunction = nil
	sess.functionTable = nil
	sess.globalVariables = nil
	sess.compiledDecls = nil
	sess.executedInits = nil
	sess.compiledInits = nil
	sess.submitCount = 0
	sess.service.Reset()
}

// CompiledFunctions returns the session's accumulated function table. The returned slice
// aliases internal state and must not be mutated.
//
// Returns []*CompiledFunction in registration order, or nil before the first Submit.
func (sess *Session) CompiledFunctions() []*program.CompiledFunction {
	if sess.rootFunction == nil {
		return nil
	}
	return program.ExportFunctions(sess.rootFunction)
}

// Inspect returns a snapshot of what the session currently knows. The returned value owns
// its slices and is safe to keep past this call.
//
// Returns SessionState which is the snapshot.
func (sess *Session) Inspect() SessionState {
	imports := make([]string, len(sess.importOrder))
	copy(imports, sess.importOrder)
	slices.Sort(imports)

	declarations := make([]SessionDecl, len(sess.decls))
	for index, record := range sess.decls {
		declarations[index] = SessionDecl{Name: record.name, Kind: record.kind}
	}

	return SessionState{
		Imports:      imports,
		Declarations: declarations,
		SubmitCount:  sess.submitCount,
	}
}

// sessionConfig holds the unexported per-session configuration.
type sessionConfig struct{}

// declRecord captures everything Session needs to reconstruct the combined source for
// re-Check and to render Inspect output.
type declRecord struct {
	// name is the bare identifier introduced by this declaration.
	name string

	// source is the verbatim source text the user submitted (after optional short-var
	// rewrite). Appended to the synthesised file on every Submit.
	source string

	// kind is the declaration's keyword class.
	kind SessionDeclKind
}

// NewSession returns a fresh session backed by this service.
//
// Takes opts (SessionOption variadic) configuring the session.
//
// Returns *Session ready to accept Submit calls.
func (s *Service) NewSession(opts ...SessionOption) *Session {
	config := &sessionConfig{}
	for _, opt := range opts {
		opt(config)
	}
	return &Session{service: s,
		imports:         make(map[string]string),
		declaredNames:   make(map[string]SessionDeclKind),
		rootFunction:    nil,
		functionTable:   nil,
		globalVariables: nil,
		compiledDecls:   nil,
		executedInits:   nil,
		compiledInits:   nil,
		importOrder:     nil,
		decls:           nil,
		submitCount:     0}
}

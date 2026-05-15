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

// Package sandboxworker implements restricted one-shot and persistent source protocols.
// The caller must establish OS confinement before allowing protocol input.
package sandboxworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/sandboxwire"
	"pipit.sh/pipit/internal/symtab"
)

const (
	// Profile names the initial single-submission, reviewed-source worker protocol.
	Profile = "restricted-source-v2"

	// startupTimeout is the deadline for initial handshake completion.
	startupTimeout = 10 * time.Second

	// resultTimeout is the deadline for transmitting a final response.
	resultTimeout = 2 * time.Second

	// maximumErrorBytes is the largest diagnostic text accepted per response.
	maximumErrorBytes = 4096
)

// transport is the private, deadline-capable worker channel, never stdout.
type transport interface {
	io.Reader

	io.Writer

	// SetDeadline enforces per-phase I/O deadlines on the channel.
	//
	// Takes deadline (time.Time) which bounds the next I/O phase.
	//
	// Returns error when the deadline cannot be enforced.
	SetDeadline(time.Time) error
}

// Configuration selects an immutable reviewed import policy for this worker.
type Configuration struct {
	// Profile names the reviewed worker protocol.
	Profile string `json:"profile"`

	// Imports lists host-approved package paths for this worker.
	Imports []string `json:"imports"`
}

// Request contains source bytes, never a host pathname or executable selection.
type Request struct {
	// Kind selects the source mode: "expression", "file" or "submission".
	Kind string `json:"kind"`

	// Source holds the raw script text to compile and evaluate.
	Source string `json:"source"`

	// Entrypoint names the function to call after compilation when Kind is "file".
	Entrypoint string `json:"entrypoint"`
}

// Response contains only bounded output, a JSON scalar and submission status.
type Response struct {
	// Output holds captured stdout text from the evaluated script.
	Output string `json:"output"`

	// Error holds untrusted diagnostic text on evaluation failure.
	Error string `json:"error"`

	// Code is the submission status: "compiled", "ok" or "evaluation_failed".
	Code string `json:"code"`

	// Value holds the JSON-encoded scalar result of the evaluated expression.
	Value json.RawMessage `json:"value"`

	// CostUsed reports the worker's self-declared execution cost.
	CostUsed int64 `json:"cost_used"`

	// OutputTruncated is true when the worker's output exceeded its budget.
	OutputTruncated bool `json:"output_truncated"`
}

// ServeOption configures one Serve invocation.
type ServeOption func(*connection)

// connection owns framing and lifecycle validation for one worker.
type connection struct {
	// codec handles length-prefixed JSON frame I/O.
	codec *sandboxwire.Codec

	// machine validates protocol state transitions.
	machine *sandboxwire.Machine

	// provider supplies native symbol packages when linked by the host.
	provider symtab.SymbolProviderPort
}

// newConnection creates private frame and state validators.
//
// Takes stream (Transport) which supplies bounded, deadline-aware I/O.
//
// Returns *connection for a fresh worker handshake.
// Returns error if protocol construction fails.
func newConnection(stream transport) (*connection, error) {
	codec, err := sandboxwire.New(stream, stream, sandboxwire.Limits{MaxFrameBytes: 0, MaxJSONDepth: 0, MaxJSONValues: 0})
	if err != nil {
		return nil, err
	}
	machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{MaxCallsPerRun: 0, MaxOutstandingCalls: 0})
	if err != nil {
		return nil, err
	}
	return &connection{codec: codec, machine: machine, provider: nil}, nil
}

// handshake sends the worker hello and reads the first request.
//
// Returns sandboxwire.Message which is the first request from the host.
// Returns error when transport or protocol fails.
func (session *connection) handshake() (sandboxwire.Message, error) {
	if err := session.send(sandboxwire.Hello, 0, struct {
		Profile string `json:"profile"`
	}{Profile: Profile}); err != nil {
		return sandboxwire.Message{}, err
	}
	return session.receive()
}

// dispatch routes the first request to the selected profile's serve loop.
//
// Takes stream (Transport) which carries the confined channel.
// Takes message (sandboxwire.Message) which is the first host request.
//
// Returns error from the selected serve loop.
func (session *connection) dispatch(stream transport, message sandboxwire.Message) error {
	var selector map[string]json.RawMessage
	if err := json.Unmarshal(message.Payload, &selector); err != nil {
		return err
	}
	var profile string
	if err := json.Unmarshal(selector["profile"], &profile); err != nil {
		return err
	}
	if profile == FilesystemProfile {
		return session.serveFilesystem(stream, message)
	}
	var config Configuration
	if err := message.DecodePayload(&config); err != nil {
		return err
	}
	if config.Profile == SessionProfile {
		return session.serveSession(stream, config)
	}
	interpreter, err := session.configure(message)
	if err != nil {
		return err
	}
	if err := session.send(sandboxwire.Ready, 0, struct{}{}); err != nil {
		return err
	}
	message, err = session.receive()
	if err != nil {
		return err
	}
	if message.Kind == sandboxwire.Close {
		return message.DecodePayload(&struct{}{})
	}
	return session.serveSubmission(stream, interpreter, message)
}

// send validates an outbound transition before writing its bounded frame.
//
// Takes kind (sandboxwire.Kind) which selects the protocol message.
// Takes identity (uint64) which correlates only this submission.
// Takes payload (any) which is a trusted response schema.
//
// Returns error if encoding, protocol order or transport writing fails.
func (session *connection) send(kind sandboxwire.Kind, identity uint64, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	message := sandboxwire.Message{Kind: kind, ID: identity, Payload: encoded}
	if err := session.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
		return err
	}
	return session.codec.Write(message)
}

// receive validates a host frame and lifecycle transition before payload handling.
//
// Returns sandboxwire.Message which still needs domain schema validation.
// Returns error when framing or protocol order is invalid.
func (session *connection) receive() (sandboxwire.Message, error) {
	message, err := session.codec.Read()
	if err != nil {
		return sandboxwire.Message{}, err
	}
	if err := session.machine.Observe(sandboxwire.HostToWorker, message); err != nil {
		return sandboxwire.Message{}, err
	}
	return message, nil
}

// configure validates the immutable, reviewed source policy.
//
// Takes message (sandboxwire.Message) which must be the configuration frame.
//
// Returns *app.RestrictedInterpreter with no raw mutable service access.
// Returns error for an unsupported profile, schema or import grant.
func (session *connection) configure(message sandboxwire.Message) (*app.RestrictedInterpreter, error) {
	var config Configuration
	if err := message.DecodePayload(&config); err != nil {
		return nil, err
	}
	if config.Profile != Profile {
		return nil, fmt.Errorf("%w: unsupported worker profile", sandboxwire.ErrProtocol)
	}
	var restricted app.RestrictedConfig
	restricted.Imports = config.Imports
	if session.provider != nil {
		return app.NewRestrictedInterpreterWithProvider(restricted, session.provider)
	}
	return app.NewRestrictedInterpreter(restricted)
}

// Serve runs one confined worker protocol exchange on a private transport after the
// caller has sealed the process. It selects the profile the host requested and never
// falls back to an unconfined execution path.
//
// Takes stream (Transport) which is the sealed private channel.
// Takes options (...ServeOption) which apply optional configuration to the worker.
//
// Returns error on any protocol, configuration or transport failure.
func Serve(stream transport, options ...ServeOption) error {
	if stream == nil {
		return sandboxwire.ErrProtocol
	}
	if err := stream.SetDeadline(time.Now().Add(startupTimeout)); err != nil {
		return err
	}
	session, err := newConnection(stream)
	if err != nil {
		return err
	}
	for _, option := range options {
		option(session)
	}
	message, err := session.handshake()
	if err != nil {
		return err
	}
	if message.Kind == sandboxwire.Close {
		return message.DecodePayload(&struct{}{})
	}
	return session.dispatch(stream, message)
}

// WithSymbols links native symbol providers into the worker so the host allowlist can
// select any registered package rather than only the reviewed math subset. The providers
// are owned by the caller and must be safe for concurrent read.
//
// Takes providers (symtab.SymbolProviderPort variadic) which supply the registry; several
// are composed, with a later one winning where two export the same symbol.
//
// Returns ServeOption which installs the providers.
func WithSymbols(providers ...symtab.SymbolProviderPort) ServeOption {
	return func(session *connection) {
		switch len(providers) {
		case 0:
			session.provider = nil
		case 1:
			session.provider = providers[0]
		default:
			session.provider = symtab.NewCompositeSymbolProvider(providers...)
		}
	}
}

// truncateError bounds diagnostic text at maximumErrorBytes without cutting a UTF-8
// sequence in half, so the host's validity check never rejects a long message.
//
// Takes text (string) which is untrusted diagnostic text.
//
// Returns string of at most maximumErrorBytes bytes ending on a rune boundary.
func truncateError(text string) string {
	if len(text) <= maximumErrorBytes {
		return text
	}
	cut := maximumErrorBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// execute evaluates source using a fresh restricted interpreter submission.
//
// Takes interpreter (*app.RestrictedInterpreter) which owns immutable policy.
// Takes request (Request) which cannot name a host file or loader.
// Takes beginExecution (func() error) which obtains host permission after compilation.
//
// Returns Response containing bounded, serialisable submission results.
// Returns error for unsupported request semantics before source evaluation.
func execute(interpreter *app.RestrictedInterpreter, request Request, beginExecution func() error) (Response, error) {
	if err := validateSubmission(Configuration{Profile: Profile, Imports: nil}, request); err != nil {
		return Response{}, err
	}
	result, err := app.EvaluateRestrictedPhased(context.Background(), interpreter,
		request.Source, request.Entrypoint, request.Kind == "file", beginExecution)
	return restrictedResponse(result, err), nil
}

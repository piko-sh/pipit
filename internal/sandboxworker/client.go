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
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// compilationTimeout is the phase deadline for source compilation.
	compilationTimeout = 5 * time.Second

	// executionTimeout is the phase deadline for compiled execution.
	executionTimeout = 2 * time.Second

	// maximumSourceBytes is the largest source payload accepted per submission.
	maximumSourceBytes = 1 << 20

	// maximumOutputBytes is the largest stdout payload accepted per response.
	maximumOutputBytes = 64 << 10

	// maximumScalarBytes is the largest JSON scalar accepted per response.
	maximumScalarBytes = 256 << 10

	// maximumSubmissionImports bounds the import list a single submission may carry so the
	// framed configuration stays small. It matches the host isolated-config bound; the
	// worker, not this host-side check, decides which packages are actually permitted,
	// rebuilding its interpreter over its registry and failing closed on any it lacks.
	maximumSubmissionImports = 256
)

// HostTransport couples protocol deadlines and script-output accounting to supervision.
// ChargeOutput must share the same budget as native stdout and stderr.
type HostTransport interface {
	transport

	// ChargeOutput deducts produced bytes from the shared output budget.
	//
	// Takes n (int) which is the byte count to deduct.
	//
	// Returns error when the budget is exceeded.
	ChargeOutput(int) error
}

// exchangeSubmission grants execution only after a validated compilation result.
//
// Takes stream (Transport) which enforces independent host phase deadlines.
// Takes request (Request) which contains the previously validated source.
// Takes identity (uint64) which identifies this submission.
//
// Returns Response for a compilation failure or completed execution.
// Returns error for premature results, invalid grants or protocol failure.
func (session *connection) exchangeSubmission(stream transport, request Request, identity uint64) (Response, error) {
	if err := stream.SetDeadline(time.Now().Add(compilationTimeout)); err != nil {
		return Response{}, err
	}
	if err := session.hostSend(sandboxwire.Run, identity, request); err != nil {
		return Response{}, err
	}
	message, err := session.hostReceive(sandboxwire.Result)
	if err != nil {
		return Response{}, err
	}
	compiled, err := decodeResponse(message)
	if err != nil {
		return Response{}, err
	}
	if compiled.Code == "evaluation_failed" {
		return compiled, nil
	}
	if compiled.Code != "compiled" {
		return Response{}, sandboxwire.ErrProtocol
	}
	if err := stream.SetDeadline(time.Now().Add(executionTimeout)); err != nil {
		return Response{}, err
	}
	if err := session.hostSend(sandboxwire.Run, identity+1, Request{Kind: "execute", Source: "", Entrypoint: ""}); err != nil {
		return Response{}, err
	}
	message, err = session.hostReceive(sandboxwire.Result)
	if err != nil {
		return Response{}, err
	}
	response, err := decodeResponse(message)
	if err != nil {
		return Response{}, err
	}
	if response.Code == "compiled" {
		return Response{}, sandboxwire.ErrProtocol
	}
	return response, nil
}

// hostSend validates the host direction before emitting a typed frame.
//
// Takes kind (sandboxwire.Kind) which selects the protocol operation.
// Takes identity (uint64) which identifies this submission.
// Takes payload (any) which is a host-owned schema.
//
// Returns error for encoding, lifecycle or transport failure.
func (session *connection) hostSend(kind sandboxwire.Kind, identity uint64, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	message := sandboxwire.Message{Kind: kind, ID: identity, Payload: encoded}
	if err := session.machine.Observe(sandboxwire.HostToWorker, message); err != nil {
		return err
	}
	return session.codec.Write(message)
}

// hostReceive validates worker direction and the exact expected message kind.
//
// Takes kind (sandboxwire.Kind) which is the only permitted response.
//
// Returns sandboxwire.Message whose payload still requires domain validation.
// Returns error for malformed frames, unexpected kinds or invalid correlation.
func (session *connection) hostReceive(kind sandboxwire.Kind) (sandboxwire.Message, error) {
	message, err := session.codec.Read()
	if err != nil {
		return sandboxwire.Message{}, err
	}
	if err := session.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
		return sandboxwire.Message{}, err
	}
	if message.Kind != kind {
		return sandboxwire.Message{}, fmt.Errorf("%w: unexpected worker reply", sandboxwire.ErrProtocol)
	}
	return message, nil
}

// Exchange performs one host-side source exchange over an already confined worker. The
// caller must launch through independent OS controls and close the worker on every error.
//
// Takes stream (HostTransport) which is the private supervised worker channel.
// Takes config (Configuration) which selects host-approved reviewed imports.
// Takes request (Request) which contains one source submission.
//
// Returns Response only after strict framing, lifecycle and domain validation.
// Returns error for invalid input, unexpected worker replies or transport failure.
func Exchange(stream HostTransport, config Configuration, request Request) (Response, error) {
	if stream == nil {
		return Response{}, sandboxwire.ErrProtocol
	}
	if err := validateSubmission(config, request); err != nil {
		return Response{}, err
	}
	session, err := handshake(stream, config)
	if err != nil {
		return Response{}, err
	}
	response, err := session.exchangeSubmission(stream, request, 1)
	if err != nil {
		return Response{}, err
	}

	if err := stream.ChargeOutput(len(response.Output) + len(response.Value)); err != nil {
		return Response{}, err
	}
	return response, nil
}

// handshake establishes framing and an immutable configuration on a confined channel.
//
// Takes stream (Transport) which carries the confined channel.
// Takes config (Configuration) which holds the previously validated host configuration.
//
// Returns the configured connection or an error for an invalid worker handshake.
func handshake(stream transport, config Configuration) (*connection, error) {
	if err := stream.SetDeadline(time.Now().Add(startupTimeout)); err != nil {
		return nil, err
	}
	session, err := newConnection(stream)
	if err != nil {
		return nil, err
	}
	hello, err := session.hostReceive(sandboxwire.Hello)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Profile string `json:"profile"`
	}
	if err := hello.DecodePayload(&identity); err != nil {
		return nil, err
	}
	if identity.Profile != Profile {
		return nil, fmt.Errorf("%w: unsupported worker profile", sandboxwire.ErrProtocol)
	}
	if err := session.hostSend(sandboxwire.Configure, 0, config); err != nil {
		return nil, err
	}
	ready, err := session.hostReceive(sandboxwire.Ready)
	if err != nil {
		return nil, err
	}
	if err := ready.DecodePayload(&struct{}{}); err != nil {
		return nil, err
	}
	return session, nil
}

// decodeResponse requires every result field with its exact wire representation.
//
// Takes message (sandboxwire.Message) which has passed framing and lifecycle checks.
//
// Returns Response only for a complete, bounded scalar result.
// Returns error for absent fields, null metadata or invalid result values.
func decodeResponse(message sandboxwire.Message) (Response, error) {
	var response Response
	if err := message.DecodePayload(&response); err != nil {
		return Response{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(message.Payload, &fields); err != nil {
		return Response{}, err
	}
	for _, name := range []string{"output", "error", "code", "value", "cost_used", "output_truncated"} {
		value, found := fields[name]
		if !found || name != "value" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Response{}, fmt.Errorf("%w: missing or null result field", sandboxwire.ErrProtocol)
		}
	}
	if err := validateResponse(response); err != nil {
		return Response{}, err
	}
	return response, nil
}

// validateSubmission rejects unsupported host settings before any protocol write.
//
// Takes config (Configuration) which must select the reviewed source profile.
// Takes request (Request) which cannot carry bytecode or host pathnames.
//
// Returns error when the request exceeds or bypasses the profile.
func validateSubmission(config Configuration, request Request) error {
	if config.Profile != Profile || len(config.Imports) > maximumSubmissionImports {
		return sandboxwire.ErrProtocol
	}

	if len(request.Source) > maximumSourceBytes {
		return sandboxwire.ErrProtocol
	}
	switch request.Kind {
	case "expression":
		if request.Entrypoint != "" {
			return sandboxwire.ErrProtocol
		}
	case "file":
		if request.Entrypoint == "" || len(request.Entrypoint) > maximumSourceBytes {
			return sandboxwire.ErrProtocol
		}
	default:
		return sandboxwire.ErrProtocol
	}
	return nil
}

// validateResponse treats worker results as untrusted data.
//
// Takes response (Response) which has passed strict JSON field validation.
//
// Returns error for excessive output, invalid status or a non-scalar result.
func validateResponse(response Response) error {
	if !boundedResponseFields(response) {
		return sandboxwire.ErrProtocol
	}
	switch response.Code {
	case "compiled":
		if response.Output != "" || response.Error != "" || response.CostUsed != 0 || response.OutputTruncated ||
			!bytes.Equal(bytes.TrimSpace(response.Value), []byte("null")) {
			return sandboxwire.ErrProtocol
		}
	case "ok":
		if response.Error != "" {
			return sandboxwire.ErrProtocol
		}
	case "evaluation_failed":
		if response.Error == "" {
			return sandboxwire.ErrProtocol
		}
	default:
		return sandboxwire.ErrProtocol
	}
	var scalar any
	if err := json.Unmarshal(response.Value, &scalar); err != nil {
		return fmt.Errorf("%w: invalid scalar: %w", sandboxwire.ErrProtocol, err)
	}
	switch scalar.(type) {
	case nil, bool, string, float64:
		return nil
	default:
		return fmt.Errorf("%w: non-scalar worker result", sandboxwire.ErrProtocol)
	}
}

// boundedResponseFields checks the size and encoding limits every response field must
// satisfy before its status-specific validation.
//
// Takes response (Response) which was strictly JSON-decoded.
//
// Returns false when any field exceeds its byte limit or is not valid UTF-8.
func boundedResponseFields(response Response) bool {
	return len(response.Output) <= maximumOutputBytes && len(response.Error) <= maximumErrorBytes &&
		len(response.Value) <= maximumScalarBytes && response.CostUsed >= 0 &&
		utf8.ValidString(response.Error) && utf8.ValidString(response.Output)
}

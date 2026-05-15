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
	"time"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/sandboxwire"
)

// serveSubmission keeps one private compiled submission across two correlated runs.
//
// Takes stream (Transport) which bounds waiting for host execution permission.
// Takes interpreter (*app.RestrictedInterpreter) which owns immutable reviewed policy.
// Takes message (sandboxwire.Message) which must be the first source request.
//
// Returns error for phase refusal, protocol failure or result transmission failure.
func (session *connection) serveSubmission(stream transport, interpreter *app.RestrictedInterpreter, message sandboxwire.Message) error {
	if message.Kind != sandboxwire.Run || message.ID != 1 {
		return sandboxwire.ErrProtocol
	}
	var request Request
	if err := message.DecodePayload(&request); err != nil {
		return err
	}
	identity := uint64(1)
	var phaseErr error
	response, err := execute(interpreter, request, func() error {
		phaseErr = session.awaitExecution(stream, 1)
		if phaseErr == nil {
			identity = 2
		}
		return phaseErr
	})
	if phaseErr != nil {
		return phaseErr
	}
	if err != nil {
		return err
	}
	if err := stream.SetDeadline(time.Now().Add(resultTimeout)); err != nil {
		return err
	}
	return session.send(sandboxwire.Result, identity, response)
}

// awaitExecution reports compilation completion and waits for a source-free grant.
//
// Takes stream (Transport) which carries the private host execution decision.
// Takes identity (uint64) which identifies this submission.
//
// Returns error if the host does not grant exactly this compiled submission.
func (session *connection) awaitExecution(stream transport, identity uint64) error {
	if err := stream.SetDeadline(time.Now().Add(resultTimeout)); err != nil {
		return err
	}
	compiled := Response{
		Output: "", Error: "", Code: "compiled", Value: nil,
		CostUsed: 0, OutputTruncated: false,
	}
	if err := session.send(sandboxwire.Result, identity, compiled); err != nil {
		return err
	}
	message, err := session.receive()
	if err != nil {
		return err
	}
	if message.Kind != sandboxwire.Run || message.ID != identity+1 {
		return sandboxwire.ErrProtocol
	}
	var request Request
	if err := message.DecodePayload(&request); err != nil {
		return err
	}
	if request.Kind != "execute" || request.Source != "" || request.Entrypoint != "" {
		return sandboxwire.ErrProtocol
	}
	return nil
}

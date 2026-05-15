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
	"context"
	"time"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// SessionProfile selects persistent source submissions with immutable grants.
	SessionProfile = "restricted-session-v1"

	// sessionIdleTimeout is the inactivity deadline between submissions.
	sessionIdleTimeout = time.Minute

	// sessionLifetime is the absolute upper bound for the entire session.
	sessionLifetime = 15 * time.Minute

	// sessionSourceBytes is the cumulative source byte budget per session.
	sessionSourceBytes = 4 << 20

	// sessionOutputBytes is the cumulative output byte budget per session.
	sessionOutputBytes = 1 << 20

	// sessionSubmissions is the maximum number of submissions per session.
	sessionSubmissions = 1024
)

// serveSession retains one restricted interpreter until closure or any submission
// failure.
//
// Takes stream (Transport) which carries the confined channel.
// Takes config (Configuration) which holds the immutable reviewed grants.
//
// Returns error for transport, schema or phase violations.
func (session *connection) serveSession(stream transport, config Configuration) error {
	if err := validateSessionSubmission(config, Request{Kind: "submission", Source: "", Entrypoint: ""}); err != nil {
		return err
	}
	var policy app.RestrictedSessionConfig
	policy.Interpreter.Imports = config.Imports
	policy.IdleTimeout = sessionIdleTimeout
	policy.Lifetime = sessionLifetime
	policy.MaxCumulativeSourceBytes = sessionSourceBytes
	policy.MaxCumulativeOutputBytes = sessionOutputBytes
	policy.MaxSubmissions = sessionSubmissions
	interpreter, err := session.newRestrictedSession(policy)
	if err != nil {
		return err
	}
	defer interpreter.Close()
	if err := session.send(sandboxwire.Ready, 0, struct{}{}); err != nil {
		return err
	}
	expires := time.Now().Add(sessionLifetime)
	for identity := uint64(1); identity <= 2*sessionSubmissions; identity += 2 {
		if err := stream.SetDeadline(earlierDeadline(time.Now().Add(sessionIdleTimeout), expires)); err != nil {
			return err
		}
		message, err := session.receive()
		if err != nil {
			return err
		}
		if message.Kind == sandboxwire.Close {
			return message.DecodePayload(&struct{}{})
		}
		response, err := session.serveSessionSubmission(stream, interpreter, config, message, identity)
		if err != nil {
			return err
		}
		if response.Code != "ok" {
			return nil
		}
	}
	return nil
}

// serveSessionSubmission compiles one submission and requires its separate execution
// grant.
//
// Takes stream (Transport) which enforces per-phase deadlines.
// Takes interpreter (*app.RestrictedSession) which owns the persistent compilation state.
// Takes config (Configuration) which holds the immutable reviewed grants.
// Takes message (sandboxwire.Message) which must be the source request.
// Takes identity (uint64) which identifies this submission.
//
// Returns Response for the transmitted result.
// Returns error for a terminal protocol failure.
func (session *connection) serveSessionSubmission(
	stream transport, interpreter *app.RestrictedSession, config Configuration,
	message sandboxwire.Message, identity uint64,
) (Response, error) {
	if message.Kind != sandboxwire.Run || message.ID != identity {
		return Response{}, sandboxwire.ErrProtocol
	}
	var request Request
	if err := message.DecodePayload(&request); err != nil {
		return Response{}, err
	}
	if err := validateSessionSubmission(config, request); err != nil {
		return Response{}, err
	}
	var phaseErr error
	result, evaluationErr := app.SubmitRestrictedSessionPhased(context.Background(), interpreter, request.Source, func() error {
		phaseErr = session.awaitExecution(stream, identity)
		if phaseErr == nil {
			identity++
		}
		return phaseErr
	})
	if phaseErr != nil {
		return Response{}, phaseErr
	}
	response := restrictedResponse(result, evaluationErr)
	if err := stream.SetDeadline(time.Now().Add(resultTimeout)); err != nil {
		return Response{}, err
	}
	if err := session.send(sandboxwire.Result, identity, response); err != nil {
		return Response{}, err
	}
	return response, nil
}

// newRestrictedSession builds the session interpreter over the injected provider when one
// is linked, otherwise over the math-only surface.
//
// Takes policy (app.RestrictedSessionConfig) which selects limits and imports.
//
// Returns the session, or a configuration error.
func (session *connection) newRestrictedSession(policy app.RestrictedSessionConfig) (*app.RestrictedSession, error) {
	if session.provider != nil {
		return app.NewRestrictedSessionWithProvider(context.Background(), policy, session.provider)
	}
	return app.NewRestrictedSession(context.Background(), policy)
}

// validateSessionSubmission rejects policy changes, alternate loaders and excessive
// source.
//
// Takes config (Configuration) which must name the session profile.
// Takes request (Request) which must use the submission kind.
//
// Returns error when either exceeds the fixed session profile.
func validateSessionSubmission(config Configuration, request Request) error {
	if config.Profile != SessionProfile || request.Kind != "submission" || request.Entrypoint != "" {
		return sandboxwire.ErrProtocol
	}
	return validateSubmission(Configuration{Profile: Profile, Imports: config.Imports},
		Request{Kind: "expression", Source: request.Source, Entrypoint: ""})
}

// earlierDeadline selects the phase bound without extending the total lifetime.
//
// Takes phase (time.Time) which is the phase deadline.
// Takes lifetime (time.Time) which is the session deadline.
//
// Returns time.Time which is the earlier of the two.
func earlierDeadline(phase, lifetime time.Time) time.Time {
	if phase.Before(lifetime) {
		return phase
	}
	return lifetime
}

// restrictedResponse serialises cooperative results without exposing interpreter objects.
//
// Takes result (app.RestrictedResult) which holds the evaluated output and cost.
// Takes err (error) which is the evaluation error, or nil.
//
// Returns Response with bounded output. Error text remains untrusted diagnostics.
func restrictedResponse(result app.RestrictedResult, err error) Response {
	response := Response{
		Value: result.Value, Output: result.Output, Error: "", Code: "ok",
		CostUsed: result.CostUsed, OutputTruncated: result.OutputTruncated,
	}
	if err != nil {
		response.Code = "evaluation_failed"
		response.Error = truncateError(err.Error())
	}
	return response
}

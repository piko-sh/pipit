//go:build linux && (amd64 || arm64)

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
	"errors"
	"slices"
	"time"

	"pipit.sh/pipit/internal/sandboxlinux"
	"pipit.sh/pipit/internal/sandboxwire"
)

// exchangeFilesystem refuses capability calls before successful compilation.
//
// Takes worker (Transport) which enforces independent host phase deadlines.
// Takes request (Request) which contains the previously validated source.
// Takes broker (*sandboxlinux.FilesystemBroker) which owns the independently confined
// filesystem broker.
//
// Returns Response for a compilation failure or completed execution.
// Returns error after any invalid phase transition.
func (session *connection) exchangeFilesystem(ctx context.Context, worker transport,
	request Request, broker *sandboxlinux.FilesystemBroker,
) (Response, error) {
	if err := worker.SetDeadline(time.Now().Add(compilationTimeout)); err != nil {
		return Response{}, err
	}
	if err := session.hostSend(sandboxwire.Run, 1, request); err != nil {
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
	execution, cancel := context.WithTimeout(ctx, executionTimeout)
	defer cancel()
	deadline, _ := execution.Deadline()
	if err := worker.SetDeadline(deadline); err != nil {
		return Response{}, err
	}
	if err := session.hostSend(sandboxwire.Run, 2, Request{Kind: "execute", Source: "", Entrypoint: ""}); err != nil {
		return Response{}, err
	}
	return session.relayFilesystem(execution, broker)
}

// relayFilesystem handles only ledger-validated calls and the matching final result.
//
// Takes broker (*sandboxlinux.FilesystemBroker) which independently validates each call.
//
// Returns Response for a validated execution result.
// Returns error after a capability denial, broker failure or malformed reply.
func (session *connection) relayFilesystem(ctx context.Context, broker *sandboxlinux.FilesystemBroker) (Response, error) {
	for {
		message, err := session.codec.Read()
		if err != nil {
			return Response{}, err
		}
		if err := session.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
			return Response{}, err
		}
		if message.Kind == sandboxwire.Call {
			if err := session.forwardFilesystemCall(ctx, broker, message); err != nil {
				return Response{}, err
			}
			continue
		}
		if message.Kind != sandboxwire.Result {
			return Response{}, sandboxwire.ErrProtocol
		}
		return decodeFilesystemResult(ctx, message)
	}
}

// forwardFilesystemCall authorises through the broker before emitting a correlated reply.
//
// Takes broker (*sandboxlinux.FilesystemBroker) which authorises the call.
// Takes message (sandboxwire.Message) which has passed ledger validation.
//
// Returns error without forwarding any partial or failed broker output.
func (session *connection) forwardFilesystemCall(ctx context.Context, broker *sandboxlinux.FilesystemBroker, message sandboxwire.Message) error {
	response, err := broker.Execute(ctx, message.Payload)
	if err != nil {
		return err
	}
	return session.hostSend(sandboxwire.Reply, message.ID, response)
}

// ExchangeFilesystem runs one source submission with an independently confined broker.
// Both process owners must be retained for cleanup retries after an error.
//
// Takes worker (*sandboxlinux.WorkerProcess) which owns the confined source process.
// Takes config (FilesystemConfiguration) which holds the immutable host-approved
// settings.
// Takes request (Request) which contains one source submission.
// Takes broker (*sandboxlinux.FilesystemBroker) which owns the independently confined
// filesystem broker.
//
// Returns Response only after both processes have been reaped and cleaned up.
// Returns error for invalid input, protocol failure or transport error.
func ExchangeFilesystem(ctx context.Context, worker *sandboxlinux.WorkerProcess, config FilesystemConfiguration,
	request Request, broker *sandboxlinux.FilesystemBroker,
) (response Response, err error) {
	if ctx == nil || worker == nil || broker == nil {
		var cleanupErr error
		if worker != nil {
			cleanupErr = worker.Close()
		}
		return Response{}, errors.Join(sandboxwire.ErrProtocol, cleanupErr, broker.Close())
	}
	done := make(chan struct{})
	var cancellationErr error
	stop := context.AfterFunc(ctx, func() {
		cancellationErr = errors.Join(worker.Close(), broker.Close())
		close(done)
	})
	defer func() {
		if !stop() {
			<-done
		}
		err = errors.Join(err, ctx.Err(), cancellationErr, worker.Close(), broker.Close())
		if err != nil {
			response = Response{Output: "", Error: "", Code: "", Value: nil, CostUsed: 0, OutputTruncated: false}
		}
	}()
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if config.Limits != nil {
		limits := *config.Limits
		config.Limits = &limits
	}
	config.Roots, config.Imports = slices.Clone(config.Roots), slices.Clone(config.Imports)
	if err := validateFilesystemSubmission(config, request); err != nil {
		return Response{}, err
	}
	session, err := filesystemHandshake(worker, config)
	if err != nil {
		return Response{}, err
	}
	response, err = session.exchangeFilesystem(ctx, worker, request, broker)
	if err != nil {
		return Response{}, err
	}
	if err := worker.ChargeOutput(len(response.Output)); err != nil {
		return Response{}, err
	}
	if err := worker.Wait(); err != nil {
		return Response{}, err
	}
	return response, nil
}

// filesystemHandshake negotiates the distinct profile without accepting new authority.
//
// Takes stream (Transport) which carries the confined channel.
// Takes config (FilesystemConfiguration) which holds the previously validated host
// configuration.
//
// Returns *connection for a fresh correlated connection.
// Returns error for a terminal startup failure.
func filesystemHandshake(stream transport, config FilesystemConfiguration) (*connection, error) {
	if err := stream.SetDeadline(time.Now().Add(startupTimeout)); err != nil {
		return nil, err
	}
	session, err := newConnection(stream)
	if err != nil {
		return nil, err
	}
	message, err := session.hostReceive(sandboxwire.Hello)
	if err != nil {
		return nil, err
	}
	var hello struct {
		Profile string `json:"profile"`
	}
	if err := message.DecodePayload(&hello); err != nil {
		return nil, err
	}
	if hello.Profile != Profile {
		return nil, sandboxwire.ErrProtocol
	}
	if err := session.hostSend(sandboxwire.Configure, 0, config); err != nil {
		return nil, err
	}
	message, err = session.hostReceive(sandboxwire.Ready)
	if err != nil {
		return nil, err
	}
	if err := message.DecodePayload(&struct{}{}); err != nil {
		return nil, err
	}
	return session, nil
}

// decodeFilesystemResult rejects compilation metadata during execution.
//
// Takes message (sandboxwire.Message) which is the correlated final response.
//
// Returns Response only before cancellation or deadline expiry.
// Returns error for compilation metadata during execution.
func decodeFilesystemResult(ctx context.Context, message sandboxwire.Message) (Response, error) {
	response, err := decodeResponse(message)
	if err != nil {
		return Response{}, err
	}
	if response.Code == "compiled" {
		return Response{}, sandboxwire.ErrProtocol
	}
	return response, ctx.Err()
}

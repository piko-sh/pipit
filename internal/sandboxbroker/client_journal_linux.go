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

package sandboxbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"pipit.sh/pipit/internal/sandboxwire"
)

// journalBudget verifies fresh matching owners before protocol admission.
//
// Takes journal (*LinuxRecoveryJournal) which is the independently locked host journal.
//
// Returns the original sealed admission ledger, never a replacement grant set.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) journalBudget(journal *LinuxRecoveryJournal) (*FilesystemBudget, error) {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.budget == nil {
		return nil, ErrClosed
	}
	owner.budget.mutex.Lock()
	defer owner.budget.mutex.Unlock()
	if owner.budget.closed || owner.budget.calls != 0 || journal.namespace != owner.namespace {
		return nil, ErrInvalidPolicy
	}
	records, err := journal.Records()
	if err != nil {
		return nil, err
	}
	if len(records) != 0 {
		return nil, ErrInvalidPolicy
	}
	return owner.budget, nil
}

// acknowledgeWrite validates prepared metadata and persists it before replying. It never
// extends the enclosing operation deadline or resolves a script path.
//
// Takes message (sandboxwire.Message) which is the sole preparation message from the
// broker.
// Takes call (*FilesystemCall) which is this operation's live reservation.
//
// Returns error without acknowledgement when validation or durability fails.
func (client *FilesystemClient) acknowledgeWrite(message sandboxwire.Message, call *FilesystemCall) error {
	if client.recovery == nil || client.journal == nil || call.Operation() != WriteFile ||
		message.Kind != sandboxwire.Call || message.ID != call.identity {
		return sandboxwire.ErrProtocol
	}
	record, err := decodeRecoveryRecord(message.Payload)
	if err != nil {
		return err
	}
	if err := client.recovery.record(client.journal, call, record); err != nil {
		return err
	}
	if client.closed.Load() {
		return ErrClosed
	}
	return client.send(sandboxwire.Message{Kind: sandboxwire.Reply, ID: message.ID, Payload: json.RawMessage("{}")})
}

// receiveFilesystemResult enforces the negotiated preparation requirement.
//
// Takes call (*FilesystemCall) which is this operation's host reservation while protocol
// admission is exclusive.
//
// Returns validated final data only after any required durable acknowledgement.
func (client *FilesystemClient) receiveFilesystemResult(call *FilesystemCall) (FilesystemResponse, error) {
	reply, err := client.receive()
	if err != nil {
		return FilesystemResponse{}, err
	}
	prepared := false
	if reply.Kind == sandboxwire.Call {
		if err := client.acknowledgeWrite(reply, call); err != nil {
			return FilesystemResponse{}, err
		}
		prepared = true
		reply, err = client.receive()
		if err != nil {
			return FilesystemResponse{}, err
		}
	}
	response, err := decodeFilesystemResponse(reply, call.identity, call)
	if err != nil {
		return FilesystemResponse{}, err
	}
	if response.Code != "" {
		return FilesystemResponse{}, fmt.Errorf("filesystem broker failed: %s", response.Code)
	}
	if client.recovery != nil && call.Operation() == WriteFile && !prepared {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	return response, nil
}

// OpenJournalledFilesystemClient requires durable consent for every successful write.
// Authority and journal remain caller-owned until process reaping and recovery finish.
//
// Takes stream (supervisedFilesystemTransport) which is the supervised broker transport.
// Takes authority (*LinuxRecoveryAuthority) which is a fresh owner derived from the
// sealed bootstrap.
// Takes journal (*LinuxRecoveryJournal) which is a fresh owner derived from the same
// sealed bootstrap.
//
// Returns a client using the authority's ledger, without a protocol fallback. Invalid
// startup closes the supplied process but retains caller-owned recovery state.
func OpenJournalledFilesystemClient(ctx context.Context, stream supervisedFilesystemTransport,
	authority *LinuxRecoveryAuthority, journal *LinuxRecoveryJournal,
) (*FilesystemClient, error) {
	if ctx == nil || stream == nil || authority == nil || journal == nil {
		if stream != nil {
			return nil, errors.Join(ErrInvalidPolicy, stream.Close())
		}
		return nil, ErrInvalidPolicy
	}
	if err := authority.ValidateJournal(journal); err != nil {
		return nil, errors.Join(err, stream.Close())
	}
	budget, err := authority.journalBudget(journal)
	if err != nil {
		return nil, errors.Join(err, stream.Close())
	}
	return openFilesystemClient(ctx, stream, budget, authority, journal)
}

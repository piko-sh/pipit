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

package sandboxwire

import (
	"fmt"
	"sync"
)

const (
	// HostToWorker identifies an outbound host message.
	HostToWorker Direction = "host-to-worker"

	// WorkerToHost identifies an inbound worker message.
	WorkerToHost Direction = "worker-to-host"

	// awaitHello permits only the initial worker hello.
	awaitHello State = "await-hello"

	// awaitConfigure permits only the host's configuration.
	awaitConfigure State = "await-configure"

	// awaitReady waits for the configured worker to acknowledge readiness.
	awaitReady State = "await-ready"

	// idle permits a new host submission.
	idle State = "idle"

	// running permits bounded broker exchanges and the matching result.
	running State = "running"

	// Closed is a terminal, host-requested shutdown.
	Closed State = "closed"

	// Failed is a terminal protocol violation.
	Failed State = "failed"

	// defaultCallsPerRun is the broker call limit when zero is requested.
	defaultCallsPerRun = 100

	// maximumCallsPerRun is the absolute ceiling for broker calls.
	maximumCallsPerRun = 65536

	// defaultOutstandingCalls is the concurrency limit when zero is requested.
	defaultOutstandingCalls = 4

	// maximumOutstandingCalls is the absolute ceiling for concurrent calls.
	maximumOutstandingCalls = 1024
)

// Direction identifies which endpoint sent a message.
type Direction string

// State identifies the connection's protocol phase.
type State string

// SessionLimits bounds the broker request ledger within each submission. These limits do
// not replace worker lifetime, resource or broker authority checks.
type SessionLimits struct {
	// MaxCallsPerRun bounds all broker requests made by one submission.
	MaxCallsPerRun int

	// MaxOutstandingCalls bounds simultaneous broker requests.
	MaxOutstandingCalls int
}

// Machine enforces handshake order, submission correlation and bounded broker IDs.
// Callers must observe each message exactly once in transport order, before acting on its
// payload.
type Machine struct {
	// pending tracks outstanding broker call IDs that need replies.
	pending map[uint64]struct{}

	// state holds the current protocol phase.
	state State

	// limits bounds broker requests within each submission.
	limits SessionLimits

	// lastRunID is the most recent submission correlation identifier.
	lastRunID uint64

	// lastCallID is the most recent broker call identifier.
	lastCallID uint64

	// calls counts broker requests in the current submission.
	calls int

	// mutex serialises all state transitions.
	mutex sync.Mutex
}

// NewMachine creates a lifecycle validator with finite broker ledger limits.
//
// Takes limits (SessionLimits) which select defaults when zero.
//
// Returns *Machine which waits for a worker hello.
// Returns error when a limit is negative, excessive or internally inconsistent.
func NewMachine(limits SessionLimits) (*Machine, error) {
	if limits.MaxCallsPerRun < 0 || limits.MaxCallsPerRun > maximumCallsPerRun {
		return nil, ErrInvalidLimits
	}
	if limits.MaxOutstandingCalls < 0 || limits.MaxOutstandingCalls > maximumOutstandingCalls {
		return nil, ErrInvalidLimits
	}
	if limits.MaxCallsPerRun == 0 {
		limits.MaxCallsPerRun = defaultCallsPerRun
	}
	if limits.MaxOutstandingCalls == 0 {
		limits.MaxOutstandingCalls = defaultOutstandingCalls
	}
	if limits.MaxOutstandingCalls > limits.MaxCallsPerRun {
		return nil, ErrInvalidLimits
	}
	return &Machine{
		pending:    make(map[uint64]struct{}),
		state:      awaitHello,
		limits:     limits,
		lastRunID:  0,
		lastCallID: 0,
		calls:      0,
		mutex:      sync.Mutex{},
	}, nil
}

// State reports the protocol phase.
//
// Returns State which is terminal after close or any rejected message.
//
// Concurrency: safe for concurrent use.
func (machine *Machine) State() State {
	machine.mutex.Lock()
	defer machine.mutex.Unlock()
	return machine.state
}

// Observe validates one previously decoded message before its payload is handled.
//
// Takes direction (Direction) which is determined by the host-owned transport.
// Takes message (Message) which must already have passed Codec validation.
//
// Returns error when a message violates lifecycle, direction or ledger limits.
//
// Concurrency: safe for concurrent use.
func (machine *Machine) Observe(direction Direction, message Message) error {
	machine.mutex.Lock()
	defer machine.mutex.Unlock()
	if machine.state == Closed || machine.state == Failed {
		return ErrClosed
	}
	if err := validateMessage(message); err != nil {
		machine.fail()
		return err
	}
	if err := machine.advance(direction, message); err != nil {
		machine.fail()
		return err
	}
	return nil
}

// advance applies a valid lifecycle transition without recovering from violations.
//
// Takes direction (Direction) which identifies the sending endpoint.
// Takes message (Message) which passed basic envelope validation.
//
// Returns error when the message is not valid in this phase.
func (machine *Machine) advance(direction Direction, message Message) error {
	if direction != HostToWorker && direction != WorkerToHost {
		return fmt.Errorf("%w: invalid message direction", ErrProtocol)
	}
	if message.Kind == Close && direction == HostToWorker {
		machine.state = Closed
		clear(machine.pending)
		return nil
	}
	switch machine.state {
	case awaitHello:
		return machine.handshake(direction, message.Kind, WorkerToHost, Hello, awaitConfigure)
	case awaitConfigure:
		return machine.handshake(direction, message.Kind, HostToWorker, Configure, awaitReady)
	case awaitReady:
		return machine.handshake(direction, message.Kind, WorkerToHost, Ready, idle)
	case idle:
		if direction == HostToWorker && message.Kind == Run && message.ID > machine.lastRunID {
			machine.lastRunID = message.ID
			machine.calls = 0
			machine.state = running
			return nil
		}
	case running:
		return machine.running(direction, message)
	case Closed, Failed:
		return ErrClosed
	}
	return fmt.Errorf("%w: unexpected %s in %s", ErrProtocol, message.Kind, machine.state)
}

// handshake advances one direction-specific handshake step.
//
// Takes direction (Direction) which identifies the actual sender.
// Takes kind (Kind) which identifies the actual message.
// Takes expectedDirection (Direction) which identifies the required sender.
// Takes expectedKind (Kind) which identifies the required message.
// Takes next (State) which is the phase after a successful transition.
//
// Returns error when the message is not the expected handshake step.
func (machine *Machine) handshake(direction Direction, kind Kind, expectedDirection Direction, expectedKind Kind, next State) error {
	if direction != expectedDirection || kind != expectedKind {
		return fmt.Errorf("%w: unexpected handshake message", ErrProtocol)
	}
	machine.state = next
	return nil
}

// running processes correlated broker messages and a submission's final result.
//
// Takes direction (Direction) which identifies the sending endpoint.
// Takes message (Message) which belongs to the active submission.
//
// Returns error when correlation, ordering or broker limits are violated.
func (machine *Machine) running(direction Direction, message Message) error {
	switch message.Kind {
	case Call:
		if direction != WorkerToHost || message.ID <= machine.lastCallID {
			return fmt.Errorf("%w: invalid broker call identifier or direction", ErrProtocol)
		}
		if machine.calls >= machine.limits.MaxCallsPerRun || len(machine.pending) >= machine.limits.MaxOutstandingCalls {
			return fmt.Errorf("%w: broker request limit", ErrProtocol)
		}
		machine.lastCallID = message.ID
		machine.calls++
		machine.pending[message.ID] = struct{}{}
		return nil
	case Reply:
		if direction != HostToWorker {
			return fmt.Errorf("%w: invalid broker reply direction", ErrProtocol)
		}
		if _, exists := machine.pending[message.ID]; !exists {
			return fmt.Errorf("%w: unsolicited broker reply", ErrProtocol)
		}
		delete(machine.pending, message.ID)
		return nil
	case Result:
		if direction != WorkerToHost || message.ID != machine.lastRunID || len(machine.pending) != 0 {
			return fmt.Errorf("%w: premature or mismatched result", ErrProtocol)
		}
		machine.state = idle
		return nil
	case Hello, Configure, Ready, Run, Close:
		return fmt.Errorf("%w: unexpected message during execution", ErrProtocol)
	default:
		return fmt.Errorf("%w: unknown execution message", ErrProtocol)
	}
}

// fail permanently poisons the state machine and discards its pending ledger.
func (machine *Machine) fail() {
	machine.state = Failed
	clear(machine.pending)
}

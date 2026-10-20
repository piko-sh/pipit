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

package sandboxwire_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

type transition struct {
	direction  sandboxwire.Direction
	kind       sandboxwire.Kind
	identifier uint64
}

func observe(machine *sandboxwire.Machine, step transition) error {
	return machine.Observe(step.direction, sandboxwire.Message{
		Kind:    step.kind,
		ID:      step.identifier,
		Payload: json.RawMessage("{}"),
	})
}

func idleMachine(t *testing.T, limits sandboxwire.SessionLimits) *sandboxwire.Machine {
	t.Helper()
	machine, err := sandboxwire.NewMachine(limits)
	require.NoError(t, err)
	for _, step := range []transition{
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Configure, identifier: 0},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Ready, identifier: 0},
	} {
		require.NoError(t, observe(machine, step))
	}
	return machine
}

func TestMachineValidSession(t *testing.T) {
	t.Parallel()
	machine := idleMachine(t, sandboxwire.SessionLimits{})
	for _, step := range []transition{
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 2},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Reply, identifier: 2},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Reply, identifier: 1},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 1},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 2},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 2},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Close, identifier: 0},
	} {
		require.NoError(t, observe(machine, step), "step %+v is valid", step)
	}
	require.Equal(t, sandboxwire.Closed, machine.State(), "the session ends closed")
	require.ErrorIs(t, observe(machine, transition{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 3}), sandboxwire.ErrClosed, "a closed session refuses new runs")
}

func TestMachineRejectsHandshakeViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []transition
	}{
		{name: "run before hello", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		}},
		{name: "hello from the host", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Hello, identifier: 0},
		}},
		{name: "ready before hello", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Ready, identifier: 0},
		}},
		{name: "hello with a non-zero id", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 1},
		}},
		{name: "unknown direction", steps: []transition{
			{direction: "unknown", kind: sandboxwire.Hello, identifier: 0},
		}},
		{name: "unknown kind", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: "unknown", identifier: 0},
		}},
		{name: "configure from the worker", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Configure, identifier: 0},
		}},
		{name: "run before ready", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Configure, identifier: 0},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{})
			require.NoError(t, err)
			for index, step := range tt.steps {
				err = observe(machine, step)
				if index < len(tt.steps)-1 {
					require.NoError(t, err, "every step before the last is valid")
				}
			}
			require.ErrorIs(t, err, sandboxwire.ErrProtocol, "an invalid handshake is a protocol error")
			require.Equal(t, sandboxwire.Failed, machine.State(), "an invalid handshake fails the session")
			hello := transition{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0}
			require.ErrorIs(t, observe(machine, hello), sandboxwire.ErrClosed, "a failed session refuses a new handshake")
		})
	}
}

func TestMachineRejectsExecutionViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []transition
	}{
		{name: "wrong run direction", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Run, identifier: 1},
		}},
		{name: "unsolicited result", steps: []transition{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 1},
		}},
		{name: "concurrent run", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 2},
		}},
		{name: "mismatched result", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 2},
		}},
		{name: "pending calls at result", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 1},
		}},
		{name: "unsolicited reply", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Reply, identifier: 1},
		}},
		{name: "worker reply", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Reply, identifier: 1},
		}},
		{name: "host call", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Call, identifier: 1},
		}},
		{name: "replayed run", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		}},
		{name: "replayed call across submissions", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Reply, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 2},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
		}},
		{name: "outstanding call limit", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 2},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 3},
		}},
		{name: "cumulative call limit", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Reply, identifier: 1},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 2},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Reply, identifier: 2},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 3},
		}},
		{name: "wrapped run identifier", steps: []transition{
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: ^uint64(0)},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Result, identifier: ^uint64(0)},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			machine := idleMachine(t, sandboxwire.SessionLimits{MaxCallsPerRun: 2, MaxOutstandingCalls: 2})
			for index, step := range tt.steps {
				err := observe(machine, step)
				if index < len(tt.steps)-1 {
					require.NoError(t, err)
					continue
				}
				require.ErrorIs(t, err, sandboxwire.ErrProtocol, "the violation is a protocol error")
				require.Equal(t, sandboxwire.Failed, machine.State(), "the violation fails the session")
			}
		})
	}
}

func TestMachineHostCanCloseDuringExecution(t *testing.T) {
	t.Parallel()
	machine := idleMachine(t, sandboxwire.SessionLimits{})
	for _, step := range []transition{
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Call, identifier: 1},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Close, identifier: 0},
	} {
		require.NoError(t, observe(machine, step))
	}
	require.Equal(t, sandboxwire.Closed, machine.State(), "a pending broker call does not block close")
}

func TestMachineRejectsInvalidLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		limits sandboxwire.SessionLimits
	}{
		{name: "negative calls per run", limits: sandboxwire.SessionLimits{MaxCallsPerRun: -1}},
		{name: "negative outstanding calls", limits: sandboxwire.SessionLimits{MaxOutstandingCalls: -1}},
		{name: "calls per run above the ceiling", limits: sandboxwire.SessionLimits{MaxCallsPerRun: 65537}},
		{name: "outstanding calls above the ceiling", limits: sandboxwire.SessionLimits{MaxOutstandingCalls: 1025}},
		{name: "more outstanding calls than calls per run", limits: sandboxwire.SessionLimits{MaxCallsPerRun: 1, MaxOutstandingCalls: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := sandboxwire.NewMachine(tt.limits)
			require.ErrorIs(t, err, sandboxwire.ErrInvalidLimits)
		})
	}
}

func FuzzMachineTransitions(fuzzer *testing.F) {
	fuzzer.Add([]byte{8, 1, 10, 19, 29, 22, 28, 7})
	fuzzer.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	fuzzer.Add([]byte{3, 3, 255, 7})
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1024 {
			return
		}
		machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{})
		require.NoError(t, err)
		kinds := []sandboxwire.Kind{sandboxwire.Hello, sandboxwire.Configure, sandboxwire.Ready, sandboxwire.Run, sandboxwire.Result, sandboxwire.Call, sandboxwire.Reply, sandboxwire.Close}
		terminal := false
		for _, value := range data {
			direction := sandboxwire.HostToWorker
			if value&8 != 0 {
				direction = sandboxwire.WorkerToHost
			}
			err := observe(machine, transition{direction: direction, kind: kinds[value&7], identifier: uint64(value >> 4)})
			if terminal {
				require.ErrorIs(t, err, sandboxwire.ErrClosed, "a terminal state stays closed")
			}
			state := machine.State()
			if err != nil {
				require.Contains(t, []sandboxwire.State{sandboxwire.Failed, sandboxwire.Closed}, state, "an error leaves the session failed or closed")
			}
			terminal = state == sandboxwire.Failed || state == sandboxwire.Closed
		}
	})
}

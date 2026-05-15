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
	"errors"
	"testing"

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
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []transition{
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0},
		{direction: sandboxwire.HostToWorker, kind: sandboxwire.Configure, identifier: 0},
		{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Ready, identifier: 0},
	} {
		if err := observe(machine, step); err != nil {
			t.Fatal(err)
		}
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
		if err := observe(machine, step); err != nil {
			t.Fatalf("step=%+v err=%v", step, err)
		}
	}
	if machine.State() != sandboxwire.Closed {
		t.Fatal("session did not close")
	}
	if err := observe(machine, transition{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 3}); !errors.Is(err, sandboxwire.ErrClosed) {
		t.Fatalf("closed session reopened: %v", err)
	}
}

func TestMachineRejectsHandshakeViolations(t *testing.T) {
	t.Parallel()
	for _, steps := range [][]transition{
		{{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1}},
		{{direction: sandboxwire.HostToWorker, kind: sandboxwire.Hello, identifier: 0}},
		{{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Ready, identifier: 0}},
		{{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 1}},
		{{direction: "unknown", kind: sandboxwire.Hello, identifier: 0}},
		{{direction: sandboxwire.WorkerToHost, kind: "unknown", identifier: 0}},
		{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0},
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Configure, identifier: 0},
		},
		{
			{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Configure, identifier: 0},
			{direction: sandboxwire.HostToWorker, kind: sandboxwire.Run, identifier: 1},
		},
	} {
		machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{})
		if err != nil {
			t.Fatal(err)
		}
		for index, step := range steps {
			err = observe(machine, step)
			if index < len(steps)-1 && err != nil {
				t.Fatalf("precondition failed: %v", err)
			}
		}
		if !errors.Is(err, sandboxwire.ErrProtocol) || machine.State() != sandboxwire.Failed {
			t.Fatalf("invalid handshake survived: state=%s err=%v", machine.State(), err)
		}
		if err := observe(machine, transition{direction: sandboxwire.WorkerToHost, kind: sandboxwire.Hello, identifier: 0}); !errors.Is(err, sandboxwire.ErrClosed) {
			t.Fatalf("failed handshake reusable: %v", err)
		}
	}
}

func TestMachineRejectsExecutionViolations(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
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
	} {
		t.Run(test.name, func(t *testing.T) {
			machine := idleMachine(t, sandboxwire.SessionLimits{MaxCallsPerRun: 2, MaxOutstandingCalls: 2})
			for index, step := range test.steps {
				err := observe(machine, step)
				if index < len(test.steps)-1 {
					if err != nil {
						t.Fatalf("precondition failed: %v", err)
					}
					continue
				}
				if !errors.Is(err, sandboxwire.ErrProtocol) || machine.State() != sandboxwire.Failed {
					t.Fatalf("violation accepted: state=%s err=%v", machine.State(), err)
				}
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
		if err := observe(machine, step); err != nil {
			t.Fatal(err)
		}
	}
	if machine.State() != sandboxwire.Closed {
		t.Fatal("close was blocked by a pending broker call")
	}
}

func TestMachineRejectsInvalidLimits(t *testing.T) {
	t.Parallel()
	for _, limits := range []sandboxwire.SessionLimits{
		{MaxCallsPerRun: -1}, {MaxOutstandingCalls: -1},
		{MaxCallsPerRun: 65537}, {MaxOutstandingCalls: 1025},
		{MaxCallsPerRun: 1, MaxOutstandingCalls: 2},
	} {
		if _, err := sandboxwire.NewMachine(limits); !errors.Is(err, sandboxwire.ErrInvalidLimits) {
			t.Fatalf("invalid limits accepted: %+v err=%v", limits, err)
		}
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
		if err != nil {
			t.Fatal(err)
		}
		kinds := []sandboxwire.Kind{sandboxwire.Hello, sandboxwire.Configure, sandboxwire.Ready, sandboxwire.Run, sandboxwire.Result, sandboxwire.Call, sandboxwire.Reply, sandboxwire.Close}
		terminal := false
		for _, value := range data {
			direction := sandboxwire.HostToWorker
			if value&8 != 0 {
				direction = sandboxwire.WorkerToHost
			}
			err := observe(machine, transition{direction: direction, kind: kinds[value&7], identifier: uint64(value >> 4)})
			if terminal && !errors.Is(err, sandboxwire.ErrClosed) {
				t.Fatalf("terminal state reopened: %v", err)
			}
			state := machine.State()
			if err != nil && state != sandboxwire.Failed && state != sandboxwire.Closed {
				t.Fatalf("error left a reusable state: %s", state)
			}
			terminal = state == sandboxwire.Failed || state == sandboxwire.Closed
		}
	})
}

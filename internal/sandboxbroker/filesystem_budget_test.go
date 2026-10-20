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
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

const readPayload = `{"operation":"fs.read","root":"data","path":"file","max_bytes":4}`

func testBudget(t *testing.T, limits FilesystemLimits) *FilesystemBudget {
	t.Helper()
	budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Read | Write | List}}, limits)
	require.NoError(t, err)
	return budget
}

func TestFilesystemPolicy(t *testing.T) {
	t.Parallel()
	grants := []RootGrant{{Name: "data", Rights: Read}}
	budget, err := NewFilesystemBudget(grants, FilesystemLimits{})
	require.NoError(t, err)
	grants[0] = RootGrant{Name: "other", Rights: Write}
	call, err := budget.Admit(brokerMessage(1, readPayload))
	require.NoError(t, err, "caller changed authority")
	require.NoError(t, call.Finish(0))
	for index, payload := range []string{
		`{"operation":"fs.write","root":"data","path":"file","data":""}`,
		`{"operation":"fs.list","root":"data","path":".","max_entries":1}`,
		`{"operation":"fs.read","root":"other","path":"file","max_bytes":1}`,
	} {
		_, err := budget.Admit(brokerMessage(uint64(index+2), payload))
		require.ErrorIs(t, err, ErrDenied, "rights inherited")
	}
	empty, err := NewFilesystemBudget(nil, FilesystemLimits{})
	require.NoError(t, err)
	_, err = empty.Admit(brokerMessage(1, readPayload))
	require.ErrorIs(t, err, ErrDenied, "empty grants allowed access")
	for _, invalid := range [][]RootGrant{
		{{Name: "data", Rights: 0}},
		{{Name: "data", Rights: 8}},
		{{Name: "/data", Rights: Read}},
		{{Name: "data", Rights: Read}, {Name: "data", Rights: Write}},
		make([]RootGrant, maximumRoots+1),
	} {
		_, err := NewFilesystemBudget(invalid, FilesystemLimits{})
		require.ErrorIs(t, err, ErrInvalidPolicy, "invalid grant accepted")
	}
}

func TestFilesystemLimits(t *testing.T) {
	t.Parallel()
	for _, limits := range []FilesystemLimits{
		{Calls: -1}, {Calls: defaultCalls + 1}, {Outstanding: -1}, {Outstanding: defaultOutstanding + 1},
		{ReadBytes: -1}, {ReadBytes: defaultReadBytes + 1}, {WriteBytes: -1}, {WriteBytes: defaultWriteBytes + 1},
		{ListEntries: -1}, {ListEntries: defaultEntries + 1}, {Calls: 1, Outstanding: 2},
	} {
		_, err := NewFilesystemBudget(nil, limits)
		require.ErrorIs(t, err, ErrInvalidPolicy, "accepted %+v", limits)
	}
}

func TestFilesystemReservations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, payload string
		limits        FilesystemLimits
	}{
		{name: "read", payload: readPayload, limits: FilesystemLimits{ReadBytes: 4}},
		{name: "write", payload: `{"operation":"fs.write","root":"data","path":"file","data":"YWJjZA=="}`, limits: FilesystemLimits{WriteBytes: 4}},
		{name: "list", payload: `{"operation":"fs.list","root":"data","path":".","max_entries":4}`, limits: FilesystemLimits{ListEntries: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			budget := testBudget(t, tt.limits)
			call, err := budget.Admit(brokerMessage(1, tt.payload))
			require.NoError(t, err)
			require.Equal(t, "data", call.root(), "reservation metadata lost")
			require.Equal(t, 4, call.limit())
			require.NotEmpty(t, call.path())
			require.NotEmpty(t, call.Operation())
			require.NoError(t, call.Finish(0))
			_, err = budget.Admit(brokerMessage(2, tt.payload))
			require.ErrorIs(t, err, errLimit, "short I/O refunded quota")
		})
	}
}

func TestFilesystemBusyAndDeniedConsumeCalls(t *testing.T) {
	t.Parallel()
	budget := testBudget(t, FilesystemLimits{Calls: 3, Outstanding: 1})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	require.NoError(t, err)
	_, err = budget.Admit(brokerMessage(2, readPayload))
	require.ErrorIs(t, err, errBusy)
	require.NoError(t, call.Finish(4))
	denied := `{"operation":"fs.read","root":"missing","path":"file","max_bytes":1}`
	_, err = budget.Admit(brokerMessage(3, denied))
	require.ErrorIs(t, err, ErrDenied)
	_, err = budget.Admit(brokerMessage(4, readPayload))
	require.ErrorIs(t, err, errLimit, "attempts not charged")
	_, err = budget.Admit(brokerMessage(5, readPayload))
	require.ErrorIs(t, err, ErrClosed, "exhaustion not terminal")
}

func TestFilesystemTerminalViolations(t *testing.T) {
	t.Parallel()
	for _, message := range []sandboxwire.Message{
		brokerMessage(1, readPayload), brokerMessage(0, readPayload), brokerMessage(2, "{}"),
		{Kind: sandboxwire.Result, ID: 2, Payload: []byte(readPayload)},
	} {
		budget := testBudget(t, FilesystemLimits{})
		call, err := budget.Admit(brokerMessage(1, readPayload))
		require.NoError(t, err)
		_, err = budget.Admit(message)
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "violation accepted")
		_, err = budget.Admit(brokerMessage(3, readPayload))
		require.ErrorIs(t, err, ErrClosed, "violation not terminal")
		require.NoError(t, call.Finish(0), "closed budget prevented cleanup")
	}
}

func TestFilesystemCallCompletion(t *testing.T) {
	t.Parallel()
	for _, used := range []int{-1, 5} {
		budget := testBudget(t, FilesystemLimits{})
		call, err := budget.Admit(brokerMessage(1, readPayload))
		require.NoError(t, err)
		err = call.Finish(used)
		require.ErrorIs(t, err, errLimit)
		_, err = budget.Admit(brokerMessage(2, readPayload))
		require.ErrorIs(t, err, ErrClosed)
	}
	budget := testBudget(t, FilesystemLimits{Outstanding: 1})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	require.NoError(t, err)
	require.NoError(t, call.Finish(4))
	next, err := budget.Admit(brokerMessage(2, readPayload))
	require.NoError(t, err, "slot not released")
	require.ErrorIs(t, call.Finish(0), sandboxwire.ErrProtocol, "duplicate completion accepted")
	require.NoError(t, next.Finish(0))
	require.Equal(t, 0, budget.outstanding, "outstanding counter corrupted")
}

func TestFilesystemWriteOwnership(t *testing.T) {
	t.Parallel()
	budget := testBudget(t, FilesystemLimits{})
	message := brokerMessage(1, `{"operation":"fs.write","root":"data","path":"file","data":"YWJjZA=="}`)
	call, err := budget.Admit(message)
	require.NoError(t, err)
	for index := range message.Payload {
		message.Payload[index] = 0
	}
	data := call.data()
	data[0] = 'z'
	require.Equal(t, "abcd", string(call.data()), "caller mutated reservation")
	require.Equal(t, WriteFile, call.Operation())
	require.Equal(t, "file", call.path())
	budget.Close()
	require.NoError(t, call.Finish(4))
	_, err = budget.Admit(brokerMessage(2, readPayload))
	require.ErrorIs(t, err, ErrClosed)
}

func TestFilesystemConcurrentCompletion(t *testing.T) {
	t.Parallel()
	budget := testBudget(t, FilesystemLimits{})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	require.NoError(t, err)
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() { results <- call.Finish(0) })
	}
	group.Wait()
	close(results)
	successes, duplicates := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, sandboxwire.ErrProtocol) {
			duplicates++
		} else {
			require.NoError(t, err, "only duplicates fail")
		}
	}
	require.Equal(t, 1, successes, "completion was not atomic")
	require.Equal(t, 1, duplicates)
	require.Equal(t, 0, budget.outstanding)
}

func TestFilesystemNilOwners(t *testing.T) {
	t.Parallel()
	var budget *FilesystemBudget
	budget.Close()
	_, err := budget.Admit(brokerMessage(1, readPayload))
	require.ErrorIs(t, err, ErrClosed)
	var call *FilesystemCall
	require.ErrorIs(t, call.Finish(0), ErrClosed)
	require.ErrorIs(t, new(FilesystemCall).Finish(0), ErrClosed)
}

func TestFilesystemCopiedReservation(t *testing.T) {
	t.Parallel()
	budget := testBudget(t, FilesystemLimits{})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	require.NoError(t, err)
	duplicate := *call
	require.NoError(t, call.Finish(0))
	require.ErrorIs(t, duplicate.Finish(0), sandboxwire.ErrProtocol, "copy completed the same reservation twice")
	require.Equal(t, 0, budget.outstanding, "copy corrupted the outstanding counter")
}

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

	"pipit.sh/pipit/internal/sandboxwire"
)

const readPayload = `{"operation":"fs.read","root":"data","path":"file","max_bytes":4}`

func testBudget(t *testing.T, limits FilesystemLimits) *FilesystemBudget {
	t.Helper()
	budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Read | Write | List}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func TestFilesystemPolicy(t *testing.T) {
	grants := []RootGrant{{Name: "data", Rights: Read}}
	budget, err := NewFilesystemBudget(grants, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	grants[0] = RootGrant{Name: "other", Rights: Write}
	call, err := budget.Admit(brokerMessage(1, readPayload))
	if err != nil {
		t.Fatal("caller changed authority:", err)
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	for index, payload := range []string{
		`{"operation":"fs.write","root":"data","path":"file","data":""}`,
		`{"operation":"fs.list","root":"data","path":".","max_entries":1}`,
		`{"operation":"fs.read","root":"other","path":"file","max_bytes":1}`,
	} {
		if _, err := budget.Admit(brokerMessage(uint64(index+2), payload)); !errors.Is(err, ErrDenied) {
			t.Fatal("rights inherited:", err)
		}
	}
	empty, err := NewFilesystemBudget(nil, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Admit(brokerMessage(1, readPayload)); !errors.Is(err, ErrDenied) {
		t.Fatal("empty grants allowed access:", err)
	}
	for _, invalid := range [][]RootGrant{
		{{Name: "data", Rights: 0}},
		{{Name: "data", Rights: 8}},
		{{Name: "/data", Rights: Read}},
		{{Name: "data", Rights: Read}, {Name: "data", Rights: Write}},
		make([]RootGrant, maximumRoots+1),
	} {
		if _, err := NewFilesystemBudget(invalid, FilesystemLimits{}); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatal("invalid grant accepted:", err)
		}
	}
}

func TestFilesystemLimits(t *testing.T) {
	for _, limits := range []FilesystemLimits{
		{Calls: -1}, {Calls: defaultCalls + 1}, {Outstanding: -1}, {Outstanding: defaultOutstanding + 1},
		{ReadBytes: -1}, {ReadBytes: defaultReadBytes + 1}, {WriteBytes: -1}, {WriteBytes: defaultWriteBytes + 1},
		{ListEntries: -1}, {ListEntries: defaultEntries + 1}, {Calls: 1, Outstanding: 2},
	} {
		if _, err := NewFilesystemBudget(nil, limits); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("accepted %+v: %v", limits, err)
		}
	}
}

func TestFilesystemReservations(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		limits        FilesystemLimits
	}{
		{"read", readPayload, FilesystemLimits{ReadBytes: 4}},
		{"write", `{"operation":"fs.write","root":"data","path":"file","data":"YWJjZA=="}`, FilesystemLimits{WriteBytes: 4}},
		{"list", `{"operation":"fs.list","root":"data","path":".","max_entries":4}`, FilesystemLimits{ListEntries: 4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := testBudget(t, test.limits)
			call, err := budget.Admit(brokerMessage(1, test.payload))
			if err != nil {
				t.Fatal(err)
			}
			if call.root() != "data" || call.limit() != 4 || call.path() == "" || call.Operation() == "" {
				t.Fatal("reservation metadata lost")
			}
			if err := call.Finish(0); err != nil {
				t.Fatal(err)
			}
			if _, err := budget.Admit(brokerMessage(2, test.payload)); !errors.Is(err, errLimit) {
				t.Fatal("short I/O refunded quota:", err)
			}
		})
	}
}

func TestFilesystemBusyAndDeniedConsumeCalls(t *testing.T) {
	budget := testBudget(t, FilesystemLimits{Calls: 3, Outstanding: 1})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Admit(brokerMessage(2, readPayload)); !errors.Is(err, errBusy) {
		t.Fatal(err)
	}
	if err := call.Finish(4); err != nil {
		t.Fatal(err)
	}
	denied := `{"operation":"fs.read","root":"missing","path":"file","max_bytes":1}`
	if _, err := budget.Admit(brokerMessage(3, denied)); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := budget.Admit(brokerMessage(4, readPayload)); !errors.Is(err, errLimit) {
		t.Fatal("attempts not charged:", err)
	}
	if _, err := budget.Admit(brokerMessage(5, readPayload)); !errors.Is(err, ErrClosed) {
		t.Fatal("exhaustion not terminal:", err)
	}
}

func TestFilesystemTerminalViolations(t *testing.T) {
	for _, message := range []sandboxwire.Message{
		brokerMessage(1, readPayload), brokerMessage(0, readPayload), brokerMessage(2, "{}"),
		{Kind: sandboxwire.Result, ID: 2, Payload: []byte(readPayload)},
	} {
		budget := testBudget(t, FilesystemLimits{})
		call, err := budget.Admit(brokerMessage(1, readPayload))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := budget.Admit(message); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatal("violation accepted:", err)
		}
		if _, err := budget.Admit(brokerMessage(3, readPayload)); !errors.Is(err, ErrClosed) {
			t.Fatal("violation not terminal:", err)
		}
		if err := call.Finish(0); err != nil {
			t.Fatal("closed budget prevented cleanup:", err)
		}
	}
}

func TestFilesystemCallCompletion(t *testing.T) {
	for _, used := range []int{-1, 5} {
		budget := testBudget(t, FilesystemLimits{})
		call, err := budget.Admit(brokerMessage(1, readPayload))
		if err != nil {
			t.Fatal(err)
		}
		if err := call.Finish(used); !errors.Is(err, errLimit) {
			t.Fatal(err)
		}
		if _, err := budget.Admit(brokerMessage(2, readPayload)); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
	budget := testBudget(t, FilesystemLimits{Outstanding: 1})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := call.Finish(4); err != nil {
		t.Fatal(err)
	}
	next, err := budget.Admit(brokerMessage(2, readPayload))
	if err != nil {
		t.Fatal("slot not released:", err)
	}
	if err := call.Finish(0); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal("duplicate completion accepted:", err)
	}
	if err := next.Finish(0); err != nil {
		t.Fatal(err)
	}
	if budget.outstanding != 0 {
		t.Fatal("outstanding counter corrupted")
	}
}

func TestFilesystemWriteOwnership(t *testing.T) {
	budget := testBudget(t, FilesystemLimits{})
	message := brokerMessage(1, `{"operation":"fs.write","root":"data","path":"file","data":"YWJjZA=="}`)
	call, err := budget.Admit(message)
	if err != nil {
		t.Fatal(err)
	}
	for index := range message.Payload {
		message.Payload[index] = 0
	}
	data := call.data()
	data[0] = 'z'
	if string(call.data()) != "abcd" || call.Operation() != WriteFile || call.path() != "file" {
		t.Fatal("caller mutated reservation")
	}
	budget.Close()
	if err := call.Finish(4); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Admit(brokerMessage(2, readPayload)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestFilesystemConcurrentCompletion(t *testing.T) {
	budget := testBudget(t, FilesystemLimits{})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	if err != nil {
		t.Fatal(err)
	}
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
			t.Fatal(err)
		}
	}
	if successes != 1 || duplicates != 1 || budget.outstanding != 0 {
		t.Fatal("completion was not atomic")
	}
}

func TestFilesystemNilOwners(t *testing.T) {
	var budget *FilesystemBudget
	budget.Close()
	if _, err := budget.Admit(brokerMessage(1, readPayload)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	var call *FilesystemCall
	if err := call.Finish(0); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := new(FilesystemCall).Finish(0); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestFilesystemCopiedReservation(t *testing.T) {
	budget := testBudget(t, FilesystemLimits{})
	call, err := budget.Admit(brokerMessage(1, readPayload))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := *call
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	if err := duplicate.Finish(0); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal("copy completed the same reservation twice:", err)
	}
	if budget.outstanding != 0 {
		t.Fatal("copy corrupted the outstanding counter")
	}
}

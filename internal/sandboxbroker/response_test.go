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
	"strings"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestDecodeFilesystemResponse(t *testing.T) {
	cases := []struct {
		name     string
		request  string
		response string
		valid    bool
	}{
		{"read", `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`, `{"code":"","data":"YWI=","entries":null,"written":0,"skipped":0}`, true},
		{"read-empty", `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`, `{"code":"","data":null,"entries":null,"written":0,"skipped":0}`, true},
		{"read-limit", `{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`, `{"code":"","data":"YWI=","entries":null,"written":0,"skipped":0}`, false},
		{"write", `{"operation":"fs.write","root":"data","path":"file","data":"YWI="}`, `{"code":"","data":null,"entries":null,"written":2,"skipped":0}`, true},
		{"short-write", `{"operation":"fs.write","root":"data","path":"file","data":"YWI="}`, `{"code":"","data":null,"entries":null,"written":1,"skipped":0}`, false},
		{"list", `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, `{"code":"","data":null,"entries":["file","other"],"written":0,"skipped":0}`, true},
		{"list-skipped", `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, `{"code":"","data":null,"entries":["file"],"written":0,"skipped":1}`, true},
		{"list-skipped-over-limit", `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, `{"code":"","data":null,"entries":["file"],"written":0,"skipped":2}`, false},
		{"list-skipped-negative", `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, `{"code":"","data":null,"entries":["file"],"written":0,"skipped":-1}`, false},
		{"read-skipped", `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`, `{"code":"","data":"YWI=","entries":null,"written":0,"skipped":1}`, false},
		{"denied-skipped", `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, `{"code":"denied","data":null,"entries":null,"written":0,"skipped":1}`, false},
		{"list-limit", `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, `{"code":"","data":null,"entries":["file","other"],"written":0,"skipped":0}`, false},
		{"list-path", `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, `{"code":"","data":null,"entries":["a/b"],"written":0,"skipped":0}`, false},
		{"list-parent", `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, `{"code":"","data":null,"entries":[".."],"written":0,"skipped":0}`, false},
		{"list-null-name", `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, `{"code":"","data":null,"entries":[null],"written":0,"skipped":0}`, false},
		{"list-device-name", `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, `{"code":"","data":null,"entries":["CON"],"written":0,"skipped":0}`, false},
		{"write-extra-data", `{"operation":"fs.write","root":"data","path":"file","data":"YWI="}`, `{"code":"","data":"","entries":null,"written":2,"skipped":0}`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Read | Write | List}}, FilesystemLimits{})
			if err != nil {
				t.Fatal(err)
			}
			call, err := budget.Admit(brokerMessage(1, test.request))
			if err != nil {
				t.Fatal(err)
			}
			message := brokerMessage(1, test.response)
			message.Kind = sandboxwire.Result
			response, err := decodeFilesystemResponse(message, 1, call)
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, sandboxwire.ErrProtocol) || response.Code != "" || response.Data != nil || response.Entries != nil || response.Written != 0 {
				t.Fatalf("invalid response escaped: %+v, %v", response, err)
			}
		})
	}
}

func TestDecodeFilesystemResponseRejectsMalformed(t *testing.T) {
	budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Read}}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	call, err := budget.Admit(brokerMessage(1, `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`))
	if err != nil {
		t.Fatal(err)
	}
	payloads := []string{
		strings.Repeat(" ", maximumBrokerFrameBytes+1),
		`{}`, `null`,
		`{"code":"","data":null,"entries":null}`,
		`{"code":"","data":null,"entries":null,"written":0}`,
		`{"code":"","data":null,"entries":null,"written":0,"skipped":null}`,
		`{"code":null,"data":null,"entries":null,"written":0,"skipped":0}`,
		`{"code":"","data":null,"entries":null,"written":null,"skipped":0}`,
		`{"code":"","data":[65],"entries":null,"written":0,"skipped":0}`,
		`{"code":"","data":null,"entries":[],"written":0,"skipped":0}`,
		`{"code":"","data":null,"entries":null,"written":1,"skipped":0}`,
		`{"code":"","data":null,"entries":null,"written":-1,"skipped":0}`,
		`{"code":"","data":null,"entries":null,"written":0.0,"skipped":0}`,
		`{"code":"","data":null,"entries":null,"written":0,"extra":true}`,
		`{"Code":"","data":null,"entries":null,"written":0,"skipped":0}`,
		`{"code":"","code":"","data":null,"entries":null,"written":0,"skipped":0}`,
		`{"code":"unknown","data":null,"entries":null,"written":0,"skipped":0}`,
		`{"code":"io","data":"","entries":null,"written":0,"skipped":0}`,
		`{"code":"denied","data":null,"entries":[],"written":0,"skipped":0}`,
		`{"code":"limit","data":null,"entries":null,"written":1,"skipped":0}`,
	}
	for _, payload := range payloads {
		message := brokerMessage(1, payload)
		message.Kind = sandboxwire.Result
		if _, err := decodeFilesystemResponse(message, 1, call); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Errorf("accepted %s: %v", payload, err)
		}
	}
	for _, code := range []string{"denied", "limit", "closed", "io"} {
		message := brokerMessage(1, `{"code":"`+code+`","data":null,"entries":null,"written":0,"skipped":0}`)
		message.Kind = sandboxwire.Result
		if _, err := decodeFilesystemResponse(message, 1, call); err != nil {
			t.Fatal(err)
		}
		if _, err := decodeFilesystemResponse(message, 2, call); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatal("accepted wrong identity")
		}
		if _, err := decodeFilesystemResponse(message, 1, nil); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatal("accepted absent reservation")
		}
		message.Kind = sandboxwire.Call
		if _, err := decodeFilesystemResponse(message, 1, call); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatal("accepted wrong kind")
		}
	}
}

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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestDecodeFilesystemResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		request  string
		response string
		valid    bool
	}{
		{name: "read", request: `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`, response: `{"code":"","data":"YWI=","entries":null,"written":0,"skipped":0}`, valid: true},
		{name: "read-empty", request: `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`, response: `{"code":"","data":null,"entries":null,"written":0,"skipped":0}`, valid: true},
		{name: "read-limit", request: `{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`, response: `{"code":"","data":"YWI=","entries":null,"written":0,"skipped":0}`, valid: false},
		{name: "write", request: `{"operation":"fs.write","root":"data","path":"file","data":"YWI="}`, response: `{"code":"","data":null,"entries":null,"written":2,"skipped":0}`, valid: true},
		{name: "short-write", request: `{"operation":"fs.write","root":"data","path":"file","data":"YWI="}`, response: `{"code":"","data":null,"entries":null,"written":1,"skipped":0}`, valid: false},
		{name: "list", request: `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, response: `{"code":"","data":null,"entries":["file","other"],"written":0,"skipped":0}`, valid: true},
		{name: "list-skipped", request: `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, response: `{"code":"","data":null,"entries":["file"],"written":0,"skipped":1}`, valid: true},
		{name: "list-skipped-over-limit", request: `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, response: `{"code":"","data":null,"entries":["file"],"written":0,"skipped":2}`, valid: false},
		{name: "list-skipped-negative", request: `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, response: `{"code":"","data":null,"entries":["file"],"written":0,"skipped":-1}`, valid: false},
		{name: "read-skipped", request: `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`, response: `{"code":"","data":"YWI=","entries":null,"written":0,"skipped":1}`, valid: false},
		{name: "denied-skipped", request: `{"operation":"fs.list","root":"data","path":".","max_entries":2}`, response: `{"code":"denied","data":null,"entries":null,"written":0,"skipped":1}`, valid: false},
		{name: "list-limit", request: `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, response: `{"code":"","data":null,"entries":["file","other"],"written":0,"skipped":0}`, valid: false},
		{name: "list-path", request: `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, response: `{"code":"","data":null,"entries":["a/b"],"written":0,"skipped":0}`, valid: false},
		{name: "list-parent", request: `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, response: `{"code":"","data":null,"entries":[".."],"written":0,"skipped":0}`, valid: false},
		{name: "list-null-name", request: `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, response: `{"code":"","data":null,"entries":[null],"written":0,"skipped":0}`, valid: false},
		{name: "list-device-name", request: `{"operation":"fs.list","root":"data","path":".","max_entries":1}`, response: `{"code":"","data":null,"entries":["CON"],"written":0,"skipped":0}`, valid: false},
		{name: "write-extra-data", request: `{"operation":"fs.write","root":"data","path":"file","data":"YWI="}`, response: `{"code":"","data":"","entries":null,"written":2,"skipped":0}`, valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Read | Write | List}}, FilesystemLimits{})
			require.NoError(t, err)
			call, err := budget.Admit(brokerMessage(1, tt.request))
			require.NoError(t, err)
			message := brokerMessage(1, tt.response)
			message.Kind = sandboxwire.Result
			response, err := decodeFilesystemResponse(message, 1, call)
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, sandboxwire.ErrProtocol, "an invalid response does not escape")
				require.Empty(t, response.Code)
				require.Nil(t, response.Data)
				require.Nil(t, response.Entries)
				require.Zero(t, response.Written)
			}
		})
	}
}

func TestDecodeFilesystemResponseRejectsMalformed(t *testing.T) {
	t.Parallel()
	budget, err := NewFilesystemBudget([]RootGrant{{Name: "data", Rights: Read}}, FilesystemLimits{})
	require.NoError(t, err)
	call, err := budget.Admit(brokerMessage(1, `{"operation":"fs.read","root":"data","path":"file","max_bytes":2}`))
	require.NoError(t, err)
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
		_, err := decodeFilesystemResponse(message, 1, call)
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "an invalid response is refused: %s", payload)
	}
	for _, code := range []string{"denied", "limit", "closed", "io"} {
		message := brokerMessage(1, `{"code":"`+code+`","data":null,"entries":null,"written":0,"skipped":0}`)
		message.Kind = sandboxwire.Result
		_, err := decodeFilesystemResponse(message, 1, call)
		require.NoError(t, err)
		_, err = decodeFilesystemResponse(message, 2, call)
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "accepted wrong identity")
		_, err = decodeFilesystemResponse(message, 1, nil)
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "accepted absent reservation")
		message.Kind = sandboxwire.Call
		_, err = decodeFilesystemResponse(message, 1, call)
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "accepted wrong kind")
	}
}

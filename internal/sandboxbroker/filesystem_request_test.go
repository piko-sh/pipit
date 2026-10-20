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
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

func brokerMessage(id uint64, payload string) sandboxwire.Message {
	return sandboxwire.Message{Kind: sandboxwire.Call, ID: id, Payload: json.RawMessage(payload)}
}

func TestFilesystemRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		payload   string
		operation Operation
		limit     int
		data      string
	}{
		{payload: `{"operation":"fs.read","root":"data","path":"file","max_bytes":65536}`, operation: ReadFile, limit: maximumFileBytes, data: ""},
		{payload: `{"operation":"fs.list","root":"data","path":".","max_entries":256}`, operation: ListDirectory, limit: maximumListEntries, data: ""},
		{payload: `{"operation":"fs.write","root":"data","path":"file","data":"aGVsbG8="}`, operation: WriteFile, limit: 5, data: "hello"},
		{payload: `{"operation":"fs.write","root":"data","path":"file","data":""}`, operation: WriteFile, limit: 0, data: ""},
	}
	for _, test := range cases {
		request, err := decodeFilesystemRequest(brokerMessage(1, test.payload))
		require.NoError(t, err, "%s", test.payload)
		require.Equal(t, test.operation, request.operation, "incorrect decoded request: %+v", request)
		require.Equal(t, test.limit, request.limit)
		require.Equal(t, test.data, string(request.data))
		require.Equal(t, "data", request.root)
	}
}

func TestFilesystemRequestRejectsMalformed(t *testing.T) {
	t.Parallel()
	prefix := `"root":"data","path":"file",`
	payloads := []string{
		`{}`, `null`, `[]`,
		`{` + prefix + `"operation":"fs.read"}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":null}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":0}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":-1}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":65537}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":1.0}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":"1"}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":1,"data":null}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":1,"max_entries":null}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":1,"max_bytes":2}`,
		`{` + prefix + `"Operation":"fs.read","max_bytes":1}`,
		`{` + prefix + `"operation":"fs.read","max_bytes":1,"extra":0}`,
		`{` + prefix + `"operation":"fs.write"}`,
		`{` + prefix + `"operation":"fs.write","data":null}`,
		`{` + prefix + `"operation":"fs.write","data":[65]}`,
		`{` + prefix + `"operation":"fs.write","data":"!"}`,
		`{` + prefix + `"operation":"fs.write","data":"","max_bytes":null}`,
		`{` + prefix + `"operation":"fs.write","data":"","max_entries":null}`,
		`{` + prefix + `"operation":"fs.list","max_entries":257}`,
		`{` + prefix + `"operation":"fs.list","max_entries":1,"data":null}`,
		`{` + prefix + `"operation":"fs.list","max_entries":1,"max_bytes":null}`,
		`{` + prefix + `"operation":"fs.remove"}`,
		`{"operation":"fs.read","root":"../data","path":"file","max_bytes":1}`,
		`{"operation":"fs.read","root":"data","path":".","max_bytes":1}`,
		`{` + prefix + `"operation":"fs.write","data":"` + base64.StdEncoding.EncodeToString(make([]byte, maximumFileBytes+1)) + `"}`,
		strings.Repeat(" ", maximumRequestBytes+1),
	}
	for _, payload := range payloads {
		_, err := decodeFilesystemRequest(brokerMessage(1, payload))
		require.ErrorIs(t, err, sandboxwire.ErrProtocol, "a malformed request is refused: %.120s", payload)
	}
	message := brokerMessage(0, `{}`)
	_, err := decodeFilesystemRequest(message)
	require.ErrorIs(t, err, sandboxwire.ErrProtocol)
	message.Kind, message.ID = sandboxwire.Result, 1
	_, err = decodeFilesystemRequest(message)
	require.ErrorIs(t, err, sandboxwire.ErrProtocol)
}

func FuzzFilesystemRequest(f *testing.F) {
	f.Add([]byte(`{"operation":"fs.read","root":"data","path":"file","max_bytes":1}`))
	f.Add([]byte(`{"operation":"fs.write","root":"data","path":"file","data":""}`))
	f.Add([]byte(`{"operation":"fs.list","root":"data","path":".","max_entries":1}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		request, err := decodeFilesystemRequest(sandboxwire.Message{Kind: sandboxwire.Call, ID: 1, Payload: payload})
		if err != nil {
			return
		}
		require.True(t, validRootName(request.root), "invalid authority")
		require.True(t, validRelativePath(request.path, request.operation == ListDirectory))
		switch request.operation {
		case ReadFile:
			require.GreaterOrEqual(t, request.limit, 1, "read limit escaped")
			require.LessOrEqual(t, request.limit, maximumFileBytes)
		case WriteFile:
			require.NotNil(t, request.data, "write limit escaped")
			require.Len(t, request.data, request.limit)
			require.LessOrEqual(t, request.limit, maximumFileBytes)
		case ListDirectory:
			require.GreaterOrEqual(t, request.limit, 1, "list limit escaped")
			require.LessOrEqual(t, request.limit, maximumListEntries)
		default:
			require.FailNow(t, "an unknown operation is refused")
		}
	})
}

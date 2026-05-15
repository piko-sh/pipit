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
	"errors"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

func brokerMessage(id uint64, payload string) sandboxwire.Message {
	return sandboxwire.Message{Kind: sandboxwire.Call, ID: id, Payload: json.RawMessage(payload)}
}

func TestFilesystemRequest(t *testing.T) {
	cases := []struct {
		payload   string
		operation Operation
		limit     int
		data      string
	}{
		{`{"operation":"fs.read","root":"data","path":"file","max_bytes":65536}`, ReadFile, maximumFileBytes, ""},
		{`{"operation":"fs.list","root":"data","path":".","max_entries":256}`, ListDirectory, maximumListEntries, ""},
		{`{"operation":"fs.write","root":"data","path":"file","data":"aGVsbG8="}`, WriteFile, 5, "hello"},
		{`{"operation":"fs.write","root":"data","path":"file","data":""}`, WriteFile, 0, ""},
	}
	for _, test := range cases {
		request, err := decodeFilesystemRequest(brokerMessage(1, test.payload))
		if err != nil {
			t.Fatalf("%s: %v", test.payload, err)
		}
		if request.operation != test.operation || request.limit != test.limit || string(request.data) != test.data || request.root != "data" {
			t.Fatalf("incorrect decoded request: %+v", request)
		}
	}
}

func TestFilesystemRequestRejectsMalformed(t *testing.T) {
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
		if _, err := decodeFilesystemRequest(brokerMessage(1, payload)); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Errorf("accepted malformed request %.120s: %v", payload, err)
		}
	}
	message := brokerMessage(0, `{}`)
	if _, err := decodeFilesystemRequest(message); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal(err)
	}
	message.Kind, message.ID = sandboxwire.Result, 1
	if _, err := decodeFilesystemRequest(message); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal(err)
	}
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
		if !validRootName(request.root) || !validRelativePath(request.path, request.operation == ListDirectory) {
			t.Fatal("invalid authority")
		}
		switch request.operation {
		case ReadFile:
			if request.limit < 1 || request.limit > maximumFileBytes {
				t.Fatal("read limit escaped")
			}
		case WriteFile:
			if request.data == nil || len(request.data) != request.limit || request.limit > maximumFileBytes {
				t.Fatal("write limit escaped")
			}
		case ListDirectory:
			if request.limit < 1 || request.limit > maximumListEntries {
				t.Fatal("list limit escaped")
			}
		default:
			t.Fatal("unknown operation accepted")
		}
	})
}

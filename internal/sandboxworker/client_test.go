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

package sandboxworker

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestExchangeWithSourceWorker(t *testing.T) {
	t.Parallel()
	host, worker := net.Pipe()
	defer host.Close()
	defer worker.Close()
	done := make(chan error, 1)
	go func() { done <- Serve(worker) }()
	response, err := Exchange(&accountingFixture{transport: host, charged: 0, err: nil},
		Configuration{Profile: Profile, Imports: []string{"math"}},
		Request{Kind: "expression", Source: "import \"math\"\nmath.Sqrt(49)", Entrypoint: ""},
	)
	if err != nil || response.Code != "ok" || string(response.Value) != "7" {
		t.Fatalf("response=%+v error=%v", response, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestExchangeRejectsWorkerReplies(t *testing.T) {
	t.Parallel()
	hello := fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": Profile})
	ready := fixtureMessage(t, sandboxwire.Ready, 0, struct{}{})
	valid := Response{
		Output: "", Error: "", Code: "ok", Value: json.RawMessage("7"),
		CostUsed: 0, OutputTruncated: false,
	}
	result := fixtureMessage(t, sandboxwire.Result, 1, valid)
	for _, messages := range [][]sandboxwire.Message{
		{ready},
		{fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": "trusted"})},
		{fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": "restricted-source-v1"})},
		{fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": Profile, "authority": "root"})},
		{hello, result},
		{hello, fixtureMessage(t, sandboxwire.Ready, 0, map[string]bool{"unconfined": true})},
		{hello, ready, fixtureMessage(t, sandboxwire.Result, 2, valid)},
		{hello, ready, fixtureMessage(t, sandboxwire.Call, 1, struct{}{})},
		{hello, ready, {Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage(`{"code":"ok","value":7,"extra":true}`)}},
		{hello, ready, {Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage(`{"code":"ok"}`)}},
		{hello, ready, {Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage(`{"code":"ok","value":7}`)}},
		{hello, ready, {Kind: sandboxwire.Result, ID: 1, Payload: json.RawMessage(`{"output":null,"error":"","code":"ok","value":7,"cost_used":0,"output_truncated":false}`)}},
	} {
		stream := sourceFixture(t, messages...)
		if _, err := Exchange(stream, Configuration{Profile: Profile, Imports: nil},
			Request{Kind: "expression", Source: "1", Entrypoint: ""}); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("malicious worker accepted: %v", err)
		}
	}
}

func TestValidateWorkerResponse(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"null", "true", "7", "-1.25", `"text"`} {
		response := Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage(value), CostUsed: 0, OutputTruncated: false}
		if err := validateResponse(response); err != nil {
			t.Fatalf("scalar %s rejected: %v", value, err)
		}
	}
	for _, mutate := range []func(*Response){
		func(response *Response) { response.Value = json.RawMessage("{}") },
		func(response *Response) { response.Value = json.RawMessage("[]") },
		func(response *Response) { response.Value = json.RawMessage("1e999") },
		func(response *Response) { response.Value = nil },
		func(response *Response) {
			response.Value = json.RawMessage("\"" + strings.Repeat("x", maximumScalarBytes) + "\"")
		},
		func(response *Response) { response.Output = strings.Repeat("x", maximumOutputBytes+1) },
		func(response *Response) { response.Error = "unexpected" },
		func(response *Response) { response.Code = "trusted" },
		func(response *Response) { response.Code = "evaluation_failed" },
		func(response *Response) { response.CostUsed = -1 },
		func(response *Response) {
			response.Code = "evaluation_failed"
			response.Error = strings.Repeat("x", maximumErrorBytes+1)
		},
	} {
		response := Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage("null"), CostUsed: 0, OutputTruncated: false}
		mutate(&response)
		if err := validateResponse(response); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("invalid result accepted: %v", err)
		}
	}
}

func TestExchangeValidatesBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, request := range []Request{
		{Kind: "bytecode"},
		{Kind: "expression", Entrypoint: "main"},
		{Kind: "file"},
		{Kind: "expression", Source: strings.Repeat("x", maximumSourceBytes+1)},
	} {
		stream := sourceFixture(t)
		_, err := Exchange(stream, Configuration{Profile: Profile, Imports: nil}, request)
		if !errors.Is(err, sandboxwire.ErrProtocol) || stream.output.Len() != 0 {
			t.Fatalf("invalid source reached worker: %v", err)
		}
	}
	oversized := make([]string, maximumSubmissionImports+1)
	for index := range oversized {
		oversized[index] = "math"
	}
	for _, config := range []Configuration{
		{Profile: "trusted"},
		{Profile: Profile, Imports: oversized},
	} {
		stream := sourceFixture(t)
		_, err := Exchange(stream, config, Request{Kind: "expression", Source: "1", Entrypoint: ""})
		if !errors.Is(err, sandboxwire.ErrProtocol) || stream.output.Len() != 0 {
			t.Fatalf("invalid policy reached worker: %v", err)
		}
	}
}

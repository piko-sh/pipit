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
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestWorkerRejectsExecutionGrantChanges(t *testing.T) {
	t.Parallel()
	config := fixtureMessage(t, sandboxwire.Configure, 0, Configuration{Profile: Profile, Imports: nil})
	source := fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "expression", Source: "println(\"executed\")\n7", Entrypoint: ""})
	for _, grant := range []sandboxwire.Message{
		fixtureMessage(t, sandboxwire.Run, 1, Request{Kind: "execute", Source: "", Entrypoint: ""}),
		fixtureMessage(t, sandboxwire.Run, 3, Request{Kind: "execute", Source: "", Entrypoint: ""}),
		fixtureMessage(t, sandboxwire.Run, 2, Request{Kind: "expression", Source: "8", Entrypoint: ""}),
		fixtureMessage(t, sandboxwire.Run, 2, Request{Kind: "execute", Source: "8", Entrypoint: ""}),
		fixtureMessage(t, sandboxwire.Run, 2, Request{Kind: "execute", Source: "", Entrypoint: "other"}),
		fixtureMessage(t, sandboxwire.Close, 0, struct{}{}),
	} {
		stream := sourceFixture(t, config, source, grant)
		if err := Serve(stream); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("invalid execution grant accepted: %v", err)
		}
		if bytes.Contains(stream.output.Bytes(), []byte("executed")) {
			t.Fatal("source ran before a valid execution grant")
		}
	}
}

func TestHostRejectsPhaseConfusion(t *testing.T) {
	t.Parallel()
	hello := fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": Profile})
	ready := fixtureMessage(t, sandboxwire.Ready, 0, struct{}{})
	compiled := Response{Output: "", Error: "", Code: "compiled", Value: json.RawMessage("null"), CostUsed: 0, OutputTruncated: false}
	compilation := fixtureMessage(t, sandboxwire.Result, 1, compiled)
	success := Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage("7"), CostUsed: 0, OutputTruncated: false}
	for _, replies := range [][]sandboxwire.Message{
		{fixtureMessage(t, sandboxwire.Result, 1, success)},
		{compilation, compilation},
		{compilation, fixtureMessage(t, sandboxwire.Result, 2, compiled)},
		{compilation, fixtureMessage(t, sandboxwire.Result, 3, success)},
	} {
		messages := append([]sandboxwire.Message{hello, ready}, replies...)
		stream := sourceFixture(t, messages...)
		_, err := Exchange(stream, Configuration{Profile: Profile, Imports: nil}, Request{Kind: "expression", Source: "7", Entrypoint: ""})
		if !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("phase confusion accepted: %v", err)
		}
	}
	for _, mutate := range []func(*Response){
		func(response *Response) { response.Output = "premature output" },
		func(response *Response) { response.Error = "premature error" },
		func(response *Response) { response.Value = json.RawMessage("7") },
		func(response *Response) { response.CostUsed = 1 },
		func(response *Response) { response.OutputTruncated = true },
	} {
		response := compiled
		mutate(&response)
		if err := validateResponse(response); !errors.Is(err, sandboxwire.ErrProtocol) {
			t.Fatalf("non-empty compilation acknowledgement accepted: %+v", response)
		}
	}
}

func TestHostPhaseDeadlinesAndCompileFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []bool{false, true} {
		hello := fixtureMessage(t, sandboxwire.Hello, 0, map[string]string{"profile": Profile})
		ready := fixtureMessage(t, sandboxwire.Ready, 0, struct{}{})
		first := Response{Output: "", Error: "", Code: "compiled", Value: json.RawMessage("null"), CostUsed: 0, OutputTruncated: false}
		if failure {
			first.Code = "evaluation_failed"
			first.Error = "compile failed"
		}
		messages := []sandboxwire.Message{hello, ready, fixtureMessage(t, sandboxwire.Result, 1, first)}
		if !failure {
			final := Response{Output: "", Error: "", Code: "ok", Value: json.RawMessage("7"), CostUsed: 0, OutputTruncated: false}
			messages = append(messages, fixtureMessage(t, sandboxwire.Result, 2, final))
		}
		stream := &phaseDeadlineFixture{fixtureTransport: sourceFixture(t, messages...), deadlines: nil}
		if _, err := Exchange(stream, Configuration{Profile: Profile, Imports: nil}, Request{Kind: "expression", Source: "7", Entrypoint: ""}); err != nil {
			t.Fatal(err)
		}
		want := []time.Duration{startupTimeout, compilationTimeout}
		if !failure {
			want = append(want, executionTimeout)
		}
		if len(stream.deadlines) != len(want) {
			t.Fatalf("unexpected phase count: %v", stream.deadlines)
		}
		for index, budget := range want {
			if stream.deadlines[index] > budget || stream.deadlines[index] < budget-time.Second {
				t.Fatalf("phase %d has wrong deadline: %v", index, stream.deadlines[index])
			}
		}
		if failure && bytes.Contains(stream.output.Bytes(), []byte("\"kind\":\"execute\"")) {
			t.Fatal("compile failure received execution permission")
		}
	}
}

type phaseDeadlineFixture struct {
	*fixtureTransport
	deadlines []time.Duration
}

func (stream *phaseDeadlineFixture) SetDeadline(deadline time.Time) error {
	stream.deadlines = append(stream.deadlines, time.Until(deadline))
	return stream.fixtureTransport.SetDeadline(deadline)
}

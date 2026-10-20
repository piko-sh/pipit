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
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountingFixture struct {
	transport
	err     error
	charged int
}

func (stream *accountingFixture) ChargeOutput(count int) error {
	stream.charged += count
	return stream.err
}

func TestExchangeChargesScriptOutput(t *testing.T) {
	t.Parallel()
	overflow := errors.New("combined output exhausted")
	for _, accountErr := range []error{nil, overflow} {
		host, worker := net.Pipe()
		done := make(chan error, 1)
		go func() {
			defer worker.Close()
			done <- Serve(worker)
		}()
		stream := &accountingFixture{transport: host, err: accountErr, charged: 0}
		response, err := Exchange(stream, Configuration{Profile: Profile, Imports: nil},
			Request{Kind: "expression", Source: "println(\"hello\")\n7", Entrypoint: ""})
		_ = host.Close()
		require.ErrorIs(t, err, accountErr)
		require.Equal(t, int(len("hello\n")+len("7")), stream.charged)
		if accountErr != nil {
			require.Empty(t, response.Code, "an over-budget result does not escape the host boundary")
			require.Empty(t, response.Output)
			require.Nil(t, response.Value)
		}
		require.NoError(t, <-done)
	}
}

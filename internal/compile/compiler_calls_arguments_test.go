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

package compile

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsBlockingStdlibMethod(t *testing.T) {
	t.Parallel()

	cases := []struct {
		receiver string
		method   string
		want     bool
	}{
		{receiver: "sync.WaitGroup", method: "Wait", want: true},
		{receiver: "sync.WaitGroup", method: "Add", want: false},
		{receiver: "sync.WaitGroup", method: "Done", want: false},
		{receiver: "sync.Mutex", method: "Lock", want: true},
		{receiver: "sync.Mutex", method: "Unlock", want: false},
		{receiver: "sync.RWMutex", method: "Lock", want: true},
		{receiver: "sync.RWMutex", method: "RLock", want: true},
		{receiver: "sync.RWMutex", method: "RUnlock", want: false},
		{receiver: "sync.Cond", method: "Wait", want: true},
		{receiver: "sync.Cond", method: "Signal", want: false},
		{receiver: "bytes.Buffer", method: "Write", want: false},
		{receiver: "", method: "Lock", want: false},
	}
	for _, c := range cases {
		require.Equalf(t, c.want, isBlockingStdlibMethod(c.receiver, c.method),
			"isBlockingStdlibMethod(%q, %q)", c.receiver, c.method)
	}
}

func TestTheResultsOfOneCallFillANativesParametersWhenTheirCountsFit(t *testing.T) {
	t.Parallel()

	two := types.NewTuple(types.NewParam(0, nil, "a", types.Typ[types.Int]), types.NewParam(0, nil, "b", types.Typ[types.Int]))
	fixed := types.NewSignatureType(nil, nil, nil, two, nil, false)
	require.True(t, multiReturnFillsParameters(fixed, 2), "math.Max(pair())")
	require.False(t, multiReturnFillsParameters(fixed, 3))

	variadicParameters := types.NewTuple(types.NewParam(0, nil, "w", types.Typ[types.Int]), types.NewParam(0, nil, "rest", types.NewSlice(types.Typ[types.Int])))
	variadic := types.NewSignatureType(nil, nil, nil, variadicParameters, nil, true)
	require.True(t, multiReturnFillsParameters(variadic, 1), "the variadic parameter may receive nothing")
	require.True(t, multiReturnFillsParameters(variadic, 3))
}

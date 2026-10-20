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

package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertBytecodeRejectsEmptyPayload(t *testing.T) {
	t.Parallel()
	inspection, err := ConvertBytecode(nil)
	require.Error(t, err)
	require.Nil(t, inspection)
}

func TestConvertBytecodeRecoversFromMalformedPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "root offset past the end", payload: []byte{0xff, 0xff, 0xff, 0xff}},
		{name: "root table at the end of the buffer", payload: []byte{0x08, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04}},
		{name: "truncated root table", payload: []byte{0x04, 0x00, 0x00, 0x00, 0xff, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, func() {
				inspection, err := ConvertBytecode(tt.payload)
				require.Error(t, err)
				require.Nil(t, inspection)
			}, "ConvertBytecode recovers instead of unwinding into the caller")
		})
	}
}

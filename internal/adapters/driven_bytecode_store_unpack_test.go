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

//go:build !js || !wasm

package adapters

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestBoundedCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		declared  int
		wantCount int
		wantErr   error
	}{
		{name: "longer than the payload", declared: math.MaxInt32, wantCount: 0, wantErr: errCorruptBytecodePayload},
		{name: "negative", declared: -1, wantCount: 0, wantErr: errCorruptBytecodePayload},
		{name: "empty", declared: 0, wantCount: 0, wantErr: nil},
		{name: "one element", declared: 1, wantCount: 1, wantErr: nil},
		{name: "half the payload", declared: 50, wantCount: 50, wantErr: nil},
		{name: "the whole payload", declared: 100, wantCount: 100, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			count, err := boundedCount(tt.declared, 100, "function body")
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCount, count)
		})
	}
}

func TestValidatedRegisterKind_RejectsOutOfRange(t *testing.T) {
	t.Parallel()

	for _, raw := range []int8{-1, int8(isa.NumRegisterKinds), 14, 100, 127} {
		t.Run(fmt.Sprintf("raw %d", raw), func(t *testing.T) {
			t.Parallel()
			kind, err := validatedRegisterKind(raw, "parameter")
			require.ErrorIs(t, err, errCorruptBytecodePayload)
			assert.Equal(t, uint8(0), kind)
		})
	}
}

func TestValidatedRegisterKind_AcceptsEveryRealKind(t *testing.T) {
	t.Parallel()

	for raw := range isa.NumRegisterKinds {
		t.Run(isa.RegisterKind(raw).String(), func(t *testing.T) {
			t.Parallel()
			kind, err := validatedRegisterKind(int8(raw), "result")
			require.NoError(t, err)
			assert.Equal(t, uint8(raw), kind)
		})
	}
}

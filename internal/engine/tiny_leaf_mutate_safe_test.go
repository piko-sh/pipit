//go:build safe || (js && wasm)

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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestTinyLeafSettableFieldNeedsAnAddressableReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		receiver func() reflect.Value
		name     string
		want     bool
	}{
		{
			name:     "a pointer receiver yields a settable field",
			receiver: func() reflect.Value { return reflect.ValueOf(&leafCounter{}) },
			want:     true,
		},
		{
			name:     "a value receiver is not settable",
			receiver: func() reflect.Value { return reflect.ValueOf(leafCounter{}) },
			want:     false,
		},
		{
			name:     "a nil pointer receiver is not settable",
			receiver: func() reflect.Value { return reflect.ValueOf((*leafCounter)(nil)) },
			want:     false,
		},
		{
			name:     "an invalid receiver is not settable",
			receiver: func() reflect.Value { return reflect.Value{} },
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := tinyLeafSettableField(tt.receiver(), leafLayout(0, reflect.Int64, isa.RegisterInt))

			require.Equal(t, tt.want, ok,
				"the pure-Go lane decides settability through reflect")
		})
	}
}

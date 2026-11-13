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
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEachSlogOperandVariantImplementsExactlyTheInterfacesItsScriptTypeDoes(t *testing.T) {
	t.Parallel()

	marshaler := reflect.TypeFor[json.Marshaler]()
	errorType := reflect.TypeFor[error]()
	textMarshaler := reflect.TypeFor[encoding.TextMarshaler]()
	logValuer := reflect.TypeFor[slog.LogValuer]()
	formatter := reflect.TypeFor[fmt.Formatter]()

	tests := []struct {
		variant any

		name string

		json bool

		isError bool

		text bool

		logValue bool

		isFormat bool
	}{
		{name: "no method", variant: slogOperandJSON{}, json: true, isFormat: true},
		{name: "MarshalText", variant: slogOperandJSONText{}, json: true, text: true, isFormat: true},
		{name: "Error without MarshalJSON", variant: slogOperandError{}, isError: true, isFormat: true},
		{name: "Error and MarshalText without MarshalJSON", variant: slogOperandErrorText{}, isError: true, text: true, isFormat: true},
		{name: "Error and MarshalJSON", variant: slogOperandJSONError{}, json: true, isError: true, isFormat: true},
		{name: "Error, MarshalJSON and MarshalText", variant: slogOperandJSONErrorText{}, json: true, isError: true, text: true, isFormat: true},
		{name: "LogValue", variant: slogOperandLogValuer{}, logValue: true, isFormat: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			variant := reflect.TypeOf(tt.variant)
			require.Equal(t, tt.json, variant.Implements(marshaler), "the JSON handler encodes an error without MarshalJSON by its Error")
			require.Equal(t, tt.isError, variant.Implements(errorType))
			require.Equal(t, tt.text, variant.Implements(textMarshaler), "the text handler prefers MarshalText")
			require.Equal(t, tt.logValue, variant.Implements(logValuer), "every handler resolves a LogValue first")
			require.Equal(t, tt.isFormat, variant.Implements(formatter))
			require.True(t, variant.Implements(adapterUnwrapperReflectType), "the script value stays reachable for refusal checks")
		})
	}
}

func TestASlogOperandWithoutScriptTypeIsPassedAsItIs(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	value := reflect.ValueOf(42)
	require.Equal(t, value, slogOperandFor(vm, value, argumentTypeContext{}))
	require.False(t, slogOperandFor(vm, reflect.Value{}, argumentTypeContext{}).IsValid())
	require.Nil(t, adapterHeldValueInterface(reflect.Value{}))
}

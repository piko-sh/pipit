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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

type equalityProbeStatus int

func probeStrictEquality(left any, right any) bool {
	return left == right
}

func init() {
	typemodel.RegisterIdentityTransparentFunction(probeStrictEquality)
}

func TestIdentityTransparentRegistrationTakesEffect(t *testing.T) {
	t.Parallel()

	assert.Positive(t, typemodel.IdentityTransparentFunctionCount(),
		"no identity-transparent functions registered; adapter unwrapping is silently disabled")
}

func TestEqualityHelperUnwrap_ComparesUnderlyingValues(t *testing.T) {
	t.Parallel()

	leftBox := equalityProbeStatus(1)
	rightBox := equalityProbeStatus(1)
	leftAdapter := &pipitStringerAdapter{underlying: reflect.ValueOf(&leftBox).Elem()}
	rightAdapter := &pipitStringerAdapter{underlying: reflect.ValueOf(&rightBox).Elem()}
	arguments := []reflect.Value{reflect.ValueOf(leftAdapter), reflect.ValueOf(rightAdapter)}

	require.False(t,
		probeStrictEquality(arguments[0].Interface(), arguments[1].Interface()),
		"comparing the adapter envelopes must not match before unwrapping")

	unwrapPipitAdapterArguments(reflect.ValueOf(probeStrictEquality), arguments)

	require.Equal(t, reflect.Int, arguments[0].Kind())
	require.Equal(t, reflect.Int, arguments[1].Kind())
	assert.True(t,
		probeStrictEquality(arguments[0].Interface(), arguments[1].Interface()),
		"after unwrapping the source-level values must compare equal")
}

func TestEqualityHelperUnwrap_KeepsAdaptersForRenderingFunctions(t *testing.T) {
	t.Parallel()

	adapter := &pipitStringerAdapter{underlying: reflect.ValueOf(equalityProbeStatus(1))}
	arguments := []reflect.Value{reflect.ValueOf(adapter)}

	unwrapPipitAdapterArguments(reflect.ValueOf(strings.ToUpper), arguments)

	assert.Equal(t, reflect.Pointer, arguments[0].Kind(),
		"a function that is not an identity or equality helper must still receive the Stringer adapter")
}

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

//go:build safe || (js && wasm)

package typemodel

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithoutForgingANamedCompositeTypeSharesItsUnderlyingType(t *testing.T) {
	t.Parallel()

	require.False(t, NamedCompositeTypesSupported())
	underlying := reflect.TypeFor[[]float64]()
	named, err := NamedCompositeTypeFor(probeIdentity("SafeCelsius"), underlying)
	require.NoError(t, err)
	require.Equal(t, underlying, named)
	require.False(t, IsNamedCompositeType(named))
	require.Zero(t, NamedCompositeID(named))
	_, err = NamedCompositeTypeFor(probeIdentity("SafeStruct"), reflect.TypeFor[struct{ X int }]())
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported, "an underlying type that cannot be named is still refused")
}

func TestWithoutForgingASelfReferentialGroupIsRefused(t *testing.T) {
	t.Parallel()

	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: placeholder, Identity: probeIdentity("SafeList"), Shape: "[]@SafeList"}}
	_, err = NamedCompositeCycleFor(members, func(map[string]reflect.Type) ([]reflect.Type, error) {
		require.Fail(t, "nothing is built without forging")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	require.ErrorIs(t, forgeLayoutError(), ErrNamedCompositeUnsupported)
	_, err = forgeNamedType(placeholder, "probe.SafeList", "example.com/probe")
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	require.ErrorIs(t, patchCycleMember(placeholder, placeholder), ErrNamedCompositeUnsupported)
}

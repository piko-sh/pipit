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

package engine

import (
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type safeRegistryProbe[T any] struct {
	Value T
	Count int
}

func safeRegistryProbeTypes() []reflect.Type {
	types := []reflect.Type{
		reflect.TypeFor[int](), reflect.TypeFor[string](), reflect.TypeFor[[]byte](), reflect.TypeFor[map[string]int](),
		reflect.TypeFor[safeRegistryProbe[int]](), reflect.TypeFor[safeRegistryProbe[string]](),
		reflect.TypeFor[*safeRegistryProbe[float64]](), reflect.TypeFor[[7]safeRegistryProbe[bool]](),
	}
	for length := range 3 * safeTypeFrontSlots / 2 {
		types = append(types, reflect.ArrayOf(length, reflect.TypeFor[uint16]()))
	}
	return types
}

func TestSafeTypeTokensRecoverTheirTypes(t *testing.T) {
	types := safeRegistryProbeTypes()
	tokens := make([]unsafe.Pointer, len(types))
	for index, registered := range types {
		tokens[index] = reflectValueABIType(registered)
		require.NotNil(t, tokens[index])
		require.Equal(t, tokens[index], reflectValueABIType(registered), "a type keeps its token")
	}
	for round := range 2 {
		for index, registered := range types {
			require.Equal(t, registered, safeLookupType(tokens[index]),
				"round %d: more types than front slots share slots, and each token still recovers its own type", round)
		}
	}
	require.Nil(t, reflectValueABIType(nil))
	require.Nil(t, safeLookupType(nil))
	var unregistered int
	require.Nil(t, safeLookupType(unsafe.Pointer(&unregistered)), "a pointer that is no token recovers nothing")
}

func TestSafeTypeRegistryServesConcurrentLookups(t *testing.T) {
	types := safeRegistryProbeTypes()
	var group sync.WaitGroup
	for worker := range 8 {
		group.Go(func() {
			for step := range 4 * len(types) {
				registered := types[(step*7+worker)%len(types)]
				if !assert.Equal(t, registered, safeLookupType(reflectValueABIType(registered)),
					"worker %d did not recover the type", worker) {
					return
				}
			}
		})
	}
	group.Wait()
}

func TestUnsafeNewAtBuildsAnAddressableValueFromAToken(t *testing.T) {
	probe := safeRegistryProbe[string]{Value: "held", Count: 3}
	value := unsafeNewAt(reflectValueABIType(reflect.TypeFor[safeRegistryProbe[string]]()), unsafe.Pointer(&probe), reflect.Struct)
	require.True(t, value.CanAddr())
	value.Field(1).SetInt(9)
	require.Equal(t, 9, probe.Count, "the value addresses the storage it was built over")
	require.False(t, unsafeNewAt(nil, unsafe.Pointer(&probe), reflect.Struct).IsValid())
}

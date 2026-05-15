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

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReflectSlicesUseGoEquality(t *testing.T) {
	a := new(int)
	b := new(int)

	require.True(t, reflectSlicesContains([]*int{a}, a))
	require.False(t, reflectSlicesContains([]*int{a}, b))
	require.Equal(t, 0, reflectSlicesIndex([]*int{a}, a))
	require.Equal(t, -1, reflectSlicesIndex([]*int{a}, b))
	require.False(t, reflectSlicesEqual([]*int{a}, []*int{b}))
	require.True(t, reflectSlicesEqual([]*int{a}, []*int{a}))

	require.Equal(t, []*int{a, b}, reflectSlicesCompact([]*int{a, b}),
		"distinct pointers with equal pointees must survive Compact; DeepEqual would collapse them")
	require.Equal(t, []*int{a}, reflectSlicesCompact([]*int{a, a}),
		"identical consecutive pointers must compact")
}

func TestReflectMapsUseGoEquality(t *testing.T) {
	a := new(int)
	b := new(int)

	require.True(t, reflectMapsEqual(map[int]*int{1: a}, map[int]*int{1: a}))
	require.False(t, reflectMapsEqual(map[int]*int{1: a}, map[int]*int{1: b}))
}

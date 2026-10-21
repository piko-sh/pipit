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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClosureChunkBaseIsTheLastChunksFirstElement(t *testing.T) {
	require.Zero(t, closureChunkBase[int](nil))
	require.Zero(t, closureChunkBase([][]int{{1}, {}}), "an empty last chunk")
	chunks := [][]int{{1}, {2, 3}}
	require.Equal(t, reflect.ValueOf(&chunks[1][0]).Pointer(), closureChunkBase(chunks))
}

func TestSafeBuildRegistersNoClosureWrappers(t *testing.T) {
	vm := newTestVM(t)
	noteSpawnedClosureObject(vm.Arena, &RuntimeClosure{})
	registerClosureWrapper(vm, reflect.ValueOf(func() {}), &RuntimeClosure{})
	registerBoundMethodWrapper(vm, reflect.ValueOf(func() {}), nil)
	require.Zero(t, closureChunkBase[byte](nil))
}

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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type reflectMethod struct {
	value    reflect.Value
	receiver reflect.Value
	name     string
}

func TestSyncMethodDecoderRecognisesTheBlockingMethods(t *testing.T) {
	var mutex sync.Mutex
	var rwMutex sync.RWMutex
	var group sync.WaitGroup
	cond := sync.NewCond(&mutex)
	cases := []struct {
		method reflectMethod
		kind   syncWaitKind
	}{
		{reflectMethodOf(&mutex, "Lock"), syncWaitMutexLock},
		{reflectMethodOf(&mutex, "Unlock"), syncWaitNone},
		{reflectMethodOf(&rwMutex, "Lock"), syncWaitRWMutexLock},
		{reflectMethodOf(&rwMutex, "RLock"), syncWaitRWMutexRLock},
		{reflectMethodOf(&rwMutex, "RUnlock"), syncWaitNone},
		{reflectMethodOf(&group, "Wait"), syncWaitGroupWait},
		{reflectMethodOf(&group, "Done"), syncWaitNone},
		{reflectMethodOf(cond, "Wait"), syncWaitCondWait},
		{reflectMethodOf(cond, "Signal"), syncWaitNone},
	}
	for _, testCase := range cases {
		got := decodeSyncWait(testCase.method.value, testCase.method.receiver)
		if !syncMethodDecoderAvailable() {
			continue
		}
		require.Equal(t, testCase.kind, got.kind, "%s", testCase.method.name)
	}
	group.Add(2)
	if syncMethodDecoderAvailable() {
		require.False(t, waitGroupIdle(&group))
		group.Add(-2)
		require.True(t, waitGroupIdle(&group))
	}
}

func reflectMethodOf(receiver any, name string) reflectMethod {
	value := reflect.ValueOf(receiver)
	return reflectMethod{value: value.MethodByName(name), receiver: value, name: reflect.TypeOf(receiver).String() + "." + name}
}

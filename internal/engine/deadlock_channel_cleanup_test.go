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
	"runtime"
	"testing"
	"weak"

	"github.com/stretchr/testify/require"
)

func TestChannelCleanupDoesNotRootTheStore(t *testing.T) {
	handle := noteChannelInGlobal()
	runtime.GC()
	runtime.GC()
	require.Nil(t, handle.Value(), "a store whose global holds a noted channel must still be collectable")
}

func noteChannelInGlobal() weak.Pointer[GlobalStore] {
	store := NewGlobalStore()
	channel := reflect.ValueOf(make(chan int))
	store.general = []reflect.Value{channel}
	store.noteInterpretedChannel(channel)
	return weak.Make(store)
}

func TestForgetInterpretedChannelRemovesTheKey(t *testing.T) {
	tests := []struct {
		name      string
		storeGone bool
	}{
		{name: "live store forgets the channel", storeGone: false},
		{name: "collected store is a no-op", storeGone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewGlobalStore()
			channel := reflect.ValueOf(make(chan int))
			store.noteInterpretedChannel(channel)
			key := uintptr(channel.UnsafePointer())
			require.True(t, store.isInterpretedChannel(channel))

			cleanup := channelCleanup{store: weak.Make(store), key: key}
			if tt.storeGone {
				cleanup.store = weak.Pointer[GlobalStore]{}
			}
			forgetInterpretedChannel(cleanup)

			require.Equal(t, tt.storeGone, store.isInterpretedChannel(channel))
			runtime.KeepAlive(channel)
		})
	}
}

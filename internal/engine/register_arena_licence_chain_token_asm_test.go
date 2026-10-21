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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoroutineTokenIdentifiesTheGoroutine(t *testing.T) {
	if !goroutineTokens {
		t.Skip("no goroutine identity in this build")
	}
	own := currentGoroutineToken()
	require.NotZero(t, own)
	require.Equal(t, own, currentGoroutineToken(), "the token is stable on one goroutine")
	const others = 16
	tokens := make([]uintptr, others)
	var started, release sync.WaitGroup
	started.Add(others)
	release.Add(1)
	var done sync.WaitGroup
	for index := range others {
		done.Go(func() {
			tokens[index] = currentGoroutineToken()
			started.Done()
			release.Wait()
		})
	}
	started.Wait()
	seen := map[uintptr]bool{own: true}
	for _, token := range tokens {
		require.NotZero(t, token)
		require.False(t, seen[token], "live goroutines have distinct tokens")
		seen[token] = true
	}
	release.Done()
	done.Wait()
}

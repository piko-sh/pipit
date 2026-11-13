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
	"testing"

	"github.com/stretchr/testify/require"
)

func collidingChainTokens(count int) []uintptr {
	start := func(token uintptr) uint64 { return uint64(token>>pointerAlignShift) * fibonacciHashMultiplier >> 52 }
	first := uintptr(1) << pointerAlignShift
	tokens := []uintptr{first}
	for candidate := first * 2; len(tokens) < count; candidate += 1 << pointerAlignShift {
		if start(candidate) == start(first) {
			tokens = append(tokens, candidate)
		}
	}
	return tokens
}

func TestServedChainRegistryFallsBackWhenTheSlotsAreTaken(t *testing.T) {
	tokens := collidingChainTokens(chainFastProbes + 1)
	vms := make([]*VM, len(tokens))
	for index := range vms {
		vms[index] = new(VM)
	}
	for index, token := range tokens {
		require.Nil(t, pushServedChain(token, vms[index]), "nothing ran on token %d before", index)
	}
	overflow := tokens[chainFastProbes]
	require.Nil(t, chainFastSlotOf(overflow, false), "every slot of the probe sequence is taken")
	require.Same(t, vms[chainFastProbes], servedChainTop(overflow), "the registry map holds the overflow")

	inner := new(VM)
	require.Same(t, vms[chainFastProbes], pushServedChain(overflow, inner))
	require.Same(t, inner, servedChainTop(overflow))
	popServedChain(overflow, inner, vms[chainFastProbes])
	require.Same(t, vms[chainFastProbes], servedChainTop(overflow), "the VM below is innermost again")
	popServedChain(overflow, inner, nil)
	require.Nil(t, servedChainTop(overflow), "a stack found out of order is emptied")

	popServedChain(tokens[0], inner, nil)
	require.Nil(t, servedChainTop(tokens[0]), "a slot found out of order is emptied too")
	for index, token := range tokens[1:chainFastProbes] {
		popServedChain(token, vms[index+1], nil)
		require.Nil(t, servedChainTop(token))
	}
}

func TestServedChainShardsSpreadTokens(t *testing.T) {
	shards := map[*servedChainRegistryShard]bool{}
	for token := uintptr(1) << 12; len(shards) < 2; token += 1 << servedChainShardShift {
		shards[servedChainShardOf(token)] = true
	}
	require.Len(t, shards, 2, "nearby goroutines use different shards")
}

func TestServedChainStaleReadsEachArenasCounter(t *testing.T) {
	quiet, read := NewRegisterArena(), NewRegisterArena()
	chain := []*RegisterArena{quiet, read}
	require.False(t, servedChainStale(chain, true))
	require.False(t, servedChainStale(chain, false))
	read.licenceState.chainBlind.bytes.word.Store(1)
	require.True(t, servedChainStale(chain, true), "a reading taken on one arena's byte counter")
	require.False(t, servedChainStale(chain, false))
	read.licenceState.chainBlind.all.word.Store(1)
	require.True(t, servedChainStale(chain, false))
}

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

func registeredExportChain(t *testing.T, length int) []*VM {
	t.Helper()
	chain := make([]*VM, length)
	for index := length - 1; index >= 0; index-- {
		vm := newTestVM(t)
		vm.reentrantInterpreterVM = index%3 != 0
		vm.servedChainRun.token.Store(1)
		if index+1 < length {
			vm.servedChainRun.below = chain[index+1]
		}
		chain[index] = vm
	}
	return chain
}

func TestExportingGuestCodeMarksEveryVMParkedBelow(t *testing.T) {
	chain := registeredExportChain(t, 3*servedChainMax)
	chain[0].markGuestCodeExported()
	for index, vm := range chain {
		require.True(t, vm.guestCodeExported, "VM %d below the exporter is marked", index)
		require.True(t, vm.exportShares, "VM %d shares its memory", index)
		if !vm.reentrantInterpreterVM {
			require.Contains(t, chain[0].exportedGates(), vm.exportGate, "a run of the exported code holds VM %d's gate", index)
		}
	}
}

func TestExportingAgainAboveAMarkedChainDoesNotWalkIt(t *testing.T) {
	chain := registeredExportChain(t, 3*servedChainMax)
	chain[1].markGuestCodeExported()
	deepest := chain[len(chain)-1]
	deepest.guestCodeExported = false

	for range 3 {
		chain[0].servedChainRun.exportSerial = 0
		chain[0].markGuestCodeExported()
	}

	require.False(t, deepest.guestCodeExported, "the VMs below were marked in their current runs, so the walk stops above them")
	require.True(t, chain[0].guestCodeExported, "the exporter itself is marked")
	for index, vm := range chain[1:] {
		if !vm.reentrantInterpreterVM {
			require.Contains(t, chain[0].exportedGates(), vm.exportGate, "the gates gathered below reach the new exporter (VM %d)", index+1)
		}
	}
}

func TestExportingAfterARunEndsWalksTheVMsNewChain(t *testing.T) {
	chain := registeredExportChain(t, 4)
	chain[0].markGuestCodeExported()
	below := chain[1]
	below.servedChainRun.exportSerial = 0
	below.guestCodeExported = false
	chain[0].servedChainRun.exportSerial = 0

	chain[0].markGuestCodeExported()

	require.True(t, below.guestCodeExported, "a VM whose run ended is marked again in its next run")
}

func TestAnUnregisteredVMIsMarkedOnEveryExport(t *testing.T) {
	vm := newTestVM(t)
	vm.markGuestCodeExported()
	vm.guestCodeExported = false
	vm.markGuestCodeExported()
	require.True(t, vm.guestCodeExported, "a VM that is not registered keeps no record of its run")
	require.Zero(t, vm.servedChainRun.exportSerial)
}

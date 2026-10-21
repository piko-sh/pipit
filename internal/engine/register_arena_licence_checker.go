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
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
)

var (
	// licenceAnyRejected is true once a program was rejected, so VMs skip the lookup until
	// then.
	licenceAnyRejected atomic.Bool

	// licenceCheckCounters counts the checker's verdicts.
	licenceCheckCounters struct{ programs, rejected, functions, recompiled atomic.Int64 }
)

// LicenceCheckStats counts the licence checker's verdicts since start-up.
type LicenceCheckStats struct {
	// Programs counts the programs checked.
	Programs int64

	// Rejected counts the programs rejected for a defect (run abandoned, without the minor
	// collection).
	Rejected int64

	// Recompiled counts the programs whose grants were rejected and which were compiled
	// again without arena promotion.
	Recompiled int64

	// Functions counts the functions checked.
	Functions int64
}

// licenceCheckStatistics returns the checker's verdict counts.
//
// Returns LicenceCheckStats which is a snapshot.
func licenceCheckStatistics() LicenceCheckStats {
	return LicenceCheckStats{
		Programs:   licenceCheckCounters.programs.Load(),
		Rejected:   licenceCheckCounters.rejected.Load(),
		Recompiled: licenceCheckCounters.recompiled.Load(),
		Functions:  licenceCheckCounters.functions.Load(),
	}
}

// NoteLicenceRecompile counts a program whose grants the checker rejected and which is
// compiled again without arena promotion.
func NoteLicenceRecompile() {
	if licenceCounting {
		licenceCheckCounters.recompiled.Add(1)
	}
}

// NoteLicenceCheck records the checked program's function count.
//
// When a defect causes rejection, it marks every function (MarkLicenceRejected) so its
// VMs abandon their arenas without running minor collection.
//
// Takes functions ([]*program.CompiledFunction) which are the program's functions.
// Takes rejected (bool) which is true when the program runs under the defect fallback.
func NoteLicenceCheck(functions []*program.CompiledFunction, rejected bool) {
	if licenceCounting {
		licenceCheckCounters.programs.Add(1)
		licenceCheckCounters.functions.Add(int64(len(functions)))
	}
	if !rejected {
		return
	}
	if licenceCounting {
		licenceCheckCounters.rejected.Add(1)
	}
	for _, f := range functions {
		f.MarkLicenceRejected()
	}
	licenceAnyRejected.Store(true)
}

// applyLicenceCheck switches the VM to the abandon fallback without the minor collection
// when function belongs to a program the licence checker rejected for a defect.
//
// Takes function (*program.CompiledFunction) which is the function the VM is about to
// run.
func (vm *VM) applyLicenceCheck(function *program.CompiledFunction) {
	if !licenceAnyRejected.Load() || function == nil {
		return
	}
	if function.LicenceRejected() {
		vm.Limits.AbandonArenasOnRelease = true
		vm.Limits.DisableMinorGC = true
	}
}

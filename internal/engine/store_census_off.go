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

//go:build !pipit_store_census || safe || (js && wasm)

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

// storeCensusBuilt is false outside the pipit_store_census build, and in the safe and
// js/wasm builds, which have none of the arena stores the census audits.
//
// The census hooks below sit behind constant-false branches the compiler removes.
const storeCensusBuilt = false

// censusVMState is the census's view of one VM; empty here.
type censusVMState struct{}

// censusDispatch is the census's dispatch wrapper; outside the census build it is
// flatDispatchSwitch itself, which the compiler inlines it to.
//
// Takes vm (*VM) which executes it.
// Takes frame (*CallFrame) which is the running frame.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction.
//
// Returns OpResult which is flatDispatchSwitch's result.
func censusDispatch(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return flatDispatchSwitch(vm, frame, registers, instruction)
}

// censusNoteHandOff records a hand-off for the census; a no-op here.
func censusNoteHandOff(_ *RegisterArena, _ HandOffCause, _ reflect.Value) {}

// censusEnterBoundary suspends instruction attribution for the census; a no-op here.
//
// Returns *censusVMState which is nil.
// Returns bool which is false.
func censusEnterBoundary(_ *RegisterArena) (*censusVMState, bool) { return nil, false }

// censusLeaveBoundary restores instruction attribution for the census; a no-op here.
func censusLeaveBoundary(_ *censusVMState, _ bool) {}

// censusNoteGlobalStore records a global store for the census; a no-op here.
func censusNoteGlobalStore(_ *GlobalStore, _ reflect.Value) {}

// censusNotePublished records values the host handed the guest; a no-op here.
func censusNotePublished(_ []reflect.Value) {}

// censusNoteMadeMap does nothing outside census builds.
func censusNoteMadeMap(_ reflect.Value) {}

// censusNoteLocalContainerStore does nothing outside census builds.
func censusNoteLocalContainerStore(_ *RegisterArena, _ reflect.Value) {}

// censusNoteCallbackReturn probes a callback's returned results for the census; a no-op
// here.
func censusNoteCallbackReturn(_ *RegisterArena, _ []any) {}

// censusNoteBuiltinCopy checks a copy builtin for the census; a no-op here.
func censusNoteBuiltinCopy(_ *RegisterArena, _ reflect.Value, _ int) {}

// StoreCensusViolations returns nil outside the census build.
//
// Returns map[string]int64 which is nil.
func StoreCensusViolations() map[string]int64 { return nil }

// StoreCensusGates reports whether a violation kind fails the census; outside the census
// build there are none.
//
// Returns bool which is false.
func StoreCensusGates(_ string) bool { return false }

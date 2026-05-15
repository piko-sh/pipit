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

package program

import "pipit.sh/pipit/internal/isa"

const (
	// GeneralAliasBankSize matches the general-bank register count tracked at every program
	// point. Kept equal to isa.GeneralRegisterBankSize so the per-PC environment matches the
	// bank's full addressing space.
	GeneralAliasBankSize = isa.GeneralRegisterBankSize
)

const (
	// AliasClassWild is the sentinel matching every other class. It is returned by the merge
	// widening step and by transfer functions that cannot determine a precise origin.
	AliasClassWild AliasClass = 0
)

// PointerAliasInfo is the per-function output of RunPointerAliasAnalysis. Unreached PCs
// carry an all-wild environment, a sound fallback where every may-alias query returns
// "yes".
type PointerAliasInfo struct {
	// PerPCEnv holds the post-execution alias environment for each PC in the function body.
	PerPCEnv []AliasEnvironment
}

// MayAlias reports whether two general-bank registers could refer to the same heap object
// at the program point immediately after pc executed. Returns the conservative "true"
// when the alias environment for pc is not populated.
//
// Takes pc (int) which is the PC whose post-execution environment is consulted.
// Takes regA (uint8) which is the first register slot being queried.
// Takes regB (uint8) which is the second register slot being queried.
//
// Returns true when regA == regB (always aliases), when either class is wild (unknown
// origin), or when both carry the same non-wild class (provably aliased).
// Returns false only when both classes are concrete and different; callers (CSE, LICM,
// GVN) use the false return to keep cached reads alive through writes via the other
// register.
func (info *PointerAliasInfo) MayAlias(pc int, regA, regB uint8) bool {
	if info == nil {
		return true
	}
	if regA == regB {
		return true
	}
	if pc < 0 || pc >= len(info.PerPCEnv) {
		return true
	}
	env := &info.PerPCEnv[pc]
	classA := env.Class[regA]
	classB := env.Class[regB]
	if classA == AliasClassWild || classB == AliasClassWild {
		return true
	}
	return classA == classB
}

// AliasEnvironment is the immutable snapshot of every general-bank register's alias class
// at one program point.
type AliasEnvironment struct {
	// Class holds the alias class for each general-bank register slot.
	Class [GeneralAliasBankSize]AliasClass
}

// AliasClass identifies a set of provably-equivalent pointer origins. Class zero is
// "wild" and matches every other class.
type AliasClass uint16

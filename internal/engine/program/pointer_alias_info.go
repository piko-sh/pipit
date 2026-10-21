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
	// classes holds the alias classes after each PC in turn, width of them for each PC.
	classes []AliasClass

	// width is the number of general-bank registers tracked, counted from register zero. A
	// register past it is wild at every PC.
	width int
}

// NewPointerAliasInfo returns alias information for a body of the given length, with
// every class wild.
//
// Takes words (int) which is the length of the function body.
// Takes width (int) which is the number of general-bank registers to track.
//
// Returns *PointerAliasInfo which answers "may alias" for every query until classes are
// stored in it.
func NewPointerAliasInfo(words, width int) *PointerAliasInfo {
	width = min(max(width, 0), GeneralAliasBankSize)
	return &PointerAliasInfo{classes: make([]AliasClass, words*width), width: width}
}

// ClassesAt returns the alias classes of the tracked registers after pc ran.
//
// Takes pc (int) which is the PC whose post-execution environment is wanted.
//
// Returns []AliasClass which the caller may write to, one class for each tracked
// register, or nil when pc lies outside the body.
func (info *PointerAliasInfo) ClassesAt(pc int) []AliasClass {
	start := pc * info.width
	if pc < 0 || start+info.width > len(info.classes) {
		return nil
	}
	return info.classes[start : start+info.width : start+info.width]
}

// MayAlias reports whether two general registers may refer to the same object after pc.
//
// Takes pc (int) which is the PC whose post-execution environment is consulted.
// Takes regA (uint8) which is the first register slot being queried.
// Takes regB (uint8) which is the second register slot being queried.
//
// Returns true when regA == regB (always aliases), when either class is wild (unknown
// origin), or when both carry the same non-wild class (provably aliased).
// Returns false only when both classes are concrete and different. Callers (CSE, LICM,
// GVN) use the false return to keep cached reads alive through writes via the other
// register.
func (info *PointerAliasInfo) MayAlias(pc int, regA, regB uint8) bool {
	if info == nil || regA == regB {
		return true
	}
	classes := info.ClassesAt(pc)
	if int(regA) >= len(classes) || int(regB) >= len(classes) {
		return true
	}
	classA, classB := classes[regA], classes[regB]
	if classA == AliasClassWild || classB == AliasClassWild {
		return true
	}
	return classA == classB
}

// AliasEnvironment snapshots every general register's alias class at one program point.
type AliasEnvironment struct {
	// Class holds the alias class for each general-bank register slot.
	Class [GeneralAliasBankSize]AliasClass
}

// AliasClass identifies a set of provably-equivalent pointer origins. Class zero is
// "wild" and matches every other class.
type AliasClass uint16

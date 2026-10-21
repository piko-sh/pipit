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

package isa

// HandOffCause names a point where a value may leave the arena's lifetime without a
// barrier that copies it out.
type HandOffCause uint8

const (
	// HandOffNativeArgUnknown is an argument to a native function the borrow registry does
	// not classify.
	HandOffNativeArgUnknown HandOffCause = iota

	// HandOffNativeArgRetains is an argument to a native function classed as retaining it.
	HandOffNativeArgRetains

	// HandOffNativeArgBorrows identifies an argument borrowed by a native function only for
	// the duration of the call.
	HandOffNativeArgBorrows

	// HandOffNativeArgResultAliases identifies an argument that a native function's result
	// may alias.
	HandOffNativeArgResultAliases

	// HandOffWalkExhausted is a host value the host-argument walk left unvisited because its
	// depth or leaf budget ran out.
	HandOffWalkExhausted

	// HandOffNotSettable is a host slot the host-argument walk could not overwrite.
	HandOffNotSettable

	// HandOffIdentityKept is a pointer, channel or function the escape walk passed through
	// by identity.
	HandOffIdentityKept

	// HandOffHeapPassthrough is a heap-backed slice the escape copy passed through without
	// copying its elements.
	HandOffHeapPassthrough

	// HandOffUntracedContainer is a store into a map, channel, global or heap slice that the
	// arena's mark does not trace.
	HandOffUntracedContainer

	// HandOffSpawn is a goroutine argument or captured cell handed to a new goroutine.
	HandOffSpawn

	// HandOffSharedStore is a value stored into a captured variable while the VM shares
	// memory with another goroutine, which can read the variable after this VM's arena is
	// released.
	HandOffSharedStore

	// NumHandOffCauses is the number of causes.
	NumHandOffCauses
)

// handOffCauseNames holds the stable names reported for each cause.
var handOffCauseNames = [NumHandOffCauses]string{
	HandOffNativeArgUnknown:       "nativeArgUnknown",
	HandOffNativeArgRetains:       "nativeArgRetains",
	HandOffNativeArgBorrows:       "nativeArgBorrows",
	HandOffNativeArgResultAliases: "nativeArgResultAliases",
	HandOffWalkExhausted:          "walkExhausted",
	HandOffNotSettable:            "notSettable",
	HandOffIdentityKept:           "identityKept",
	HandOffHeapPassthrough:        "heapPassthrough",
	HandOffUntracedContainer:      "untracedContainer",
	HandOffSpawn:                  "spawn",
	HandOffSharedStore:            "sharedStore",
}

// handOffCauseRevokes holds each cause's outcome, read once from the generated decision
// table (ModelCauseRevokes, licence_decisions_generated.go).
var handOffCauseRevokes = func() (revokes [NumHandOffCauses]bool) {
	for cause := range NumHandOffCauses {
		modelled, named := ModelCauseRevokes[handOffCauseNames[cause]]
		revokes[cause] = !named || modelled
	}
	return revokes
}()

// String returns the cause's stable name.
//
// Returns string which is the name, or "unknown" for an out-of-range cause.
func (c HandOffCause) String() string {
	if c >= NumHandOffCauses {
		return "unknown"
	}
	return handOffCauseNames[c]
}

// Revokes reports whether a hand-off of this cause that references arena storage would
// revoke that storage in a licensed arena. The answer comes from the generated decision
// table (licence_decisions_generated.go), which records the revocations licensed reuse's
// safety argument assumes, so the engine cannot drift from it.
//
// Returns bool which is false only for the informational native classes.
func (c HandOffCause) Revokes() bool {
	if c >= NumHandOffCauses {
		return true
	}
	return handOffCauseRevokes[c]
}

// handOffCauseByName returns the cause with the given stable name.
//
// Takes name (string) which is a name String returns.
//
// Returns HandOffCause and true when the name is known.
func handOffCauseByName(name string) (HandOffCause, bool) {
	for cause := range NumHandOffCauses {
		if handOffCauseNames[cause] == name {
			return cause, true
		}
	}
	return NumHandOffCauses, false
}

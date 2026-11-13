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

import "fmt"

// AuditEvent names the reuse event at which the reuse audit recorded a slab.
type AuditEvent uint8

const (
	// AuditEventRelease is an arena returned to the pool, whose slabs the next owner would
	// have reused.
	AuditEventRelease AuditEvent = iota + 1

	// AuditEventChunkRecycle is a dead chunk-sized slab that MinorGC would have zeroed and
	// reinstalled during the same execution.
	AuditEventChunkRecycle

	// AuditEventMarkDeadDrop is a retired slab that MinorGC found dead and dropped to the Go
	// collector. A reachable one does not corrupt memory, but it shows the mark phase missed
	// a reference.
	AuditEventMarkDeadDrop
)

// AuditHitClass classifies a recorded slab that the reuse audit found still reachable.
type AuditHitClass uint8

const (
	// AuditReleaseHit is a slab released with its arena that is still reachable.
	AuditReleaseHit AuditHitClass = iota + 1

	// AuditInRunUnrooted is a slab MinorGC found dead that was still reachable at a mid-run
	// checkpoint.
	AuditInRunUnrooted

	// AuditOutsideReach is a mid-run record still reachable at a release audit.
	AuditOutsideReach

	// AuditInRunOnly is a mid-run record that was reachable at a checkpoint but had become
	// unreachable by the release audit.
	AuditInRunOnly
)

// AuditOptions tunes one reuse audit pass.
type AuditOptions struct {
	// MaxPasses caps the forced collections run while waiting for two consecutive passes to
	// agree. Zero selects the default of six.
	MaxPasses int

	// KeepRecords leaves settled records in place so a later audit sees them again. The
	// default drops every record the audit examined.
	KeepRecords bool

	// IncludeActiveRuns also settles mid-run records whose run has not been released yet. A
	// final audit, after every run has ended or been abandoned, sets it.
	IncludeActiveRuns bool
}

// AuditHit describes one recorded slab the reuse audit found reachable, with the
// provenance captured when the slab was recorded. It holds no pointer into the slab.
type AuditHit struct {
	// SlabKind names the arena slab class, such as "byteSlab" or "sliceHeader".
	SlabKind string

	// VMKind names the kind of VM that owned the arena when it was acquired.
	VMKind string

	// Function is the qualified name of the function running when the slab was recorded,
	// when one was cheaply available.
	Function string

	// Opcode names the instruction at PC, when known.
	Opcode string

	// RecordID is the record's sequence number within the process.
	RecordID uint64

	// OwnerID is the arena's slab-registry owner identifier.
	OwnerID uint64

	// RunID identifies the arena acquisition the slab belonged to.
	RunID uint64

	// Address is the slab's start address when recorded. It is a plain number and keeps
	// nothing alive.
	Address uintptr

	// CapBytes is the slab's capacity in bytes.
	CapBytes int

	// UsedBytes is the slab's bump-allocated prefix in bytes when recorded.
	UsedBytes int

	// PC is the program counter of Function's frame when recorded, or -1.
	PC int

	// Passes is the number of forced collections the audit ran.
	Passes int

	// ArenaHeaderRefs counts, for a mid-run record, the arena's own slice-header slots whose
	// Data pointed into the slab when it was recorded.
	ArenaHeaderRefs int

	// GCCount is the arena's MinorGC count when the slab was recorded.
	GCCount uint32

	// Class is the hit's classification.
	Class AuditHitClass

	// Event is the reuse event at which the slab was recorded.
	Event AuditEvent

	// FromGrow is true when the slab had been retired by a grow, or was a free chunk, rather
	// than being the class's current slab.
	FromGrow bool

	// MidRunSeen is true when a mid-run checkpoint had already found the slab reachable.
	MidRunSeen bool

	// Stable is true when two consecutive collections agreed before MaxPasses ran out.
	Stable bool
}

// AuditCounters reports the reuse audit's running totals since the process started.
type AuditCounters struct {
	// Recorded counts slabs recorded.
	Recorded uint64

	// RecordedRelease counts slabs recorded at arena release, the cross-execution reuse
	// point.
	RecordedRelease uint64

	// RecordedChunkRecycle counts dead chunks MinorGC would have recycled mid-run.
	RecordedChunkRecycle uint64

	// RecordedMarkDeadDrop counts dead slabs MinorGC dropped to the Go collector, which is
	// not a reuse but tests the mark's soundness.
	RecordedMarkDeadDrop uint64

	// SkippedSmall counts slabs under the tiny-allocator guard, which a weak pointer cannot
	// track reliably.
	SkippedSmall uint64

	// SkippedEmpty counts zero-capacity slabs and zero-size sentinels.
	SkippedEmpty uint64

	// Pending counts records not yet settled by a release audit.
	Pending uint64

	// MidRunChecks counts mid-run checkpoint audits run.
	MidRunChecks uint64

	// ReleaseAudits counts release audits run.
	ReleaseAudits uint64

	// ReleaseHits counts release hits reported.
	ReleaseHits uint64

	// InRunUnrootedHits counts in-run-unrooted hits reported by mid-run checkpoints.
	InRunUnrootedHits uint64

	// OutsideReachHits counts outside-reach hits reported.
	OutsideReachHits uint64

	// InRunOnlyHits counts in-run-only hits reported.
	InRunOnlyHits uint64

	// RangeRecorded counts release records of runs of clean pages a licensed release kept as
	// slabs (page-granular revocation), which the audit tests by address range.
	RangeRecorded uint64

	// RangeCleared counts reachable range records a heap dump found no pointer into.
	RangeCleared uint64

	// RangeHeapDumps counts the heap dumps range records needed.
	RangeHeapDumps uint64
}

// DiagnosticBuilds reports which test-only diagnostic builds a binary was compiled with.
//
// Tests read it to scale their bounds or to choose what they assert; production code
// never branches on it.
type DiagnosticBuilds struct {
	// ReuseAudit is true in the reuse audit build (pipit_reuse_audit).
	ReuseAudit bool

	// StoreCensus is true in the store census build (pipit_store_census).
	StoreCensus bool

	// ArenaParanoid is true in the arena poisoning build (pipit_arena_paranoid).
	ArenaParanoid bool

	// TestHooks is true in the build with abort injection and the release-phase pauses
	// (pipit_testhooks).
	TestHooks bool
}

// String returns the event's lower-case name.
//
// Returns string which is the name, or "unknown" outside the enumeration.
func (event AuditEvent) String() string {
	switch event {
	case AuditEventRelease:
		return "release"
	case AuditEventChunkRecycle:
		return "chunkRecycle"
	case AuditEventMarkDeadDrop:
		return "markDeadDrop"
	default:
		return "unknown"
	}
}

// String returns the class's name.
//
// Returns string which is the name, or "unknown" outside the enumeration.
func (class AuditHitClass) String() string {
	switch class {
	case AuditReleaseHit:
		return "release-hit"
	case AuditInRunUnrooted:
		return "in-run-unrooted"
	case AuditOutsideReach:
		return "outside-reach"
	case AuditInRunOnly:
		return "in-run-only"
	default:
		return "unknown"
	}
}

// String renders the hit on one line for logs.
//
// Returns string which is the rendering.
func (hit AuditHit) String() string {
	return fmt.Sprintf("%s event=%s kind=%s cap=%d used=%d owner=%d run=%d vm=%q gc=%d fromGrow=%t midRunSeen=%t fn=%q pc=%d op=%q addr=%#x record=%d passes=%d stable=%t arenaHeaderRefs=%d",
		hit.Class, hit.Event, hit.SlabKind, hit.CapBytes, hit.UsedBytes, hit.OwnerID, hit.RunID, hit.VMKind,
		hit.GCCount, hit.FromGrow, hit.MidRunSeen, hit.Function, hit.PC, hit.Opcode, hit.Address, hit.RecordID,
		hit.Passes, hit.Stable, hit.ArenaHeaderRefs)
}

// ActiveDiagnosticBuilds returns the diagnostic builds this binary was compiled with.
//
// Returns DiagnosticBuilds which holds one flag per diagnostic build tag.
func ActiveDiagnosticBuilds() DiagnosticBuilds {
	return DiagnosticBuilds{
		ReuseAudit:    arenaAuditEnabled,
		StoreCensus:   storeCensusBuilt,
		ArenaParanoid: arenaParanoidEnabled,
		TestHooks:     abortInjectBuilt,
	}
}

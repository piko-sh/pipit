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

//go:build !pipit_reuse_audit

package engine

const (
	// arenaAuditEnabled gates the weak-reachability reuse audit; see the audit twin for what
	// the diagnostic build does. The default build compiles the hooks away entirely.
	arenaAuditEnabled = false

	// licencePageCheck is off outside the reuse audit build; see the audit twin.
	licencePageCheck = false
)

// arenaAudit is empty in the default build. It sits mid-struct in RegisterArena, so it
// adds no bytes there.
type arenaAudit struct{}

// AuditReuse returns nil in the default build, which records nothing.
//
// Takes _ (AuditOptions) which is unused in the default build.
//
// Returns nil.
func AuditReuse(AuditOptions) []AuditHit { return nil }

// AuditStats returns zero counters in the default build.
//
// Returns AuditCounters which is all zero.
func AuditStats() AuditCounters { return AuditCounters{} }

// AuditSetMidRunEvery is a no-op in the default build.
//
// Takes _ (uint64) which is unused in the default build.
//
// Returns zero.
func AuditSetMidRunEvery(uint64) uint64 { return 0 }

// auditRelease is a no-op in the default build; the audit twin records and replaces every
// data slab before the arena is reset.
func (*RegisterArena) auditRelease() {}

// auditAcquired is a no-op in the default build; the audit twin starts a new run.
//
// Takes _ (string) which is unused in the default build.
func (*arenaAudit) auditAcquired(string) {}

// auditRunEnded ends the arena's audit run; a no-op outside the audit build.
func (*arenaAudit) auditRunEnded() {}

// auditCompactPhase is a no-op in the default build; the audit twin records the slabs
// MinorGC is about to drop or recycle.
//
// Takes _ (*gcMarkState) which is unused in the default build.
//
// Returns func() which does nothing.
func (*RegisterArena) auditCompactPhase(*gcMarkState) func() { return func() {} }

// auditMidRunCheckpoint is a no-op in the default build; the audit twin runs a mid-run
// audit at the interval AuditSetMidRunEvery sets.
func auditMidRunCheckpoint() {}

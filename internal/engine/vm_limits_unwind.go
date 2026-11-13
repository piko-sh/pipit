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
	"cmp"
	"time"
)

// abortUnwindAllowance is the configured part of an unwinding's allowance, read from the
// VM's limits before the unwinding starts.
type abortUnwindAllowance struct {
	// cost is the total cost the pending defers may spend between them.
	cost int64

	// wall is the total wall time the pending defers may spend waiting.
	wall time.Duration

	// deferWallMax caps one deferred call's wall slice.
	deferWallMax time.Duration
}

// defaultAbortUnwindAllowance is the allowance of a VM whose limits leave the unwinding
// fields zero.
var defaultAbortUnwindAllowance = abortUnwindAllowance{
	cost:         defaultAbortUnwindCost,
	wall:         defaultAbortUnwindWall,
	deferWallMax: defaultAbortUnwindDeferWallMax,
}

// positiveOr returns value when it is positive and fallback otherwise.
//
// Takes value (T) which is the configured value, zero when unset.
// Takes fallback (T) which is the default.
//
// Returns T which is the value in force.
func positiveOr[T cmp.Ordered](value, fallback T) T {
	var zero T
	if value > zero {
		return value
	}
	return fallback
}

// abortUnwindAllowance returns the unwinding allowance these limits configure.
//
// Returns abortUnwindAllowance which has a default for every unset field.
func (l *VMLimits) abortUnwindAllowance() abortUnwindAllowance {
	return abortUnwindAllowance{
		cost:         positiveOr(l.AbortUnwindCost, defaultAbortUnwindCost),
		wall:         positiveOr(l.AbortUnwindWall, defaultAbortUnwindWall),
		deferWallMax: positiveOr(l.AbortUnwindDeferWallMax, defaultAbortUnwindDeferWallMax),
	}
}

// abortUnwindNativeGrace returns how long a deferred call may stay inside a native call
// past its wall deadline.
//
// Returns time.Duration which is the configured grace or the default.
func (l *VMLimits) abortUnwindNativeGrace() time.Duration {
	return positiveOr(l.AbortUnwindNativeGrace, defaultAbortUnwindNativeGrace)
}

// goroutineJoinUnwindCap returns how long the join keeps waiting while VMs unwind.
//
// Returns time.Duration which is the configured cap or the default.
func (l *VMLimits) goroutineJoinUnwindCap() time.Duration {
	return positiveOr(l.GoroutineJoinUnwindCap, defaultGoroutineJoinUnwindCap)
}

// leakedUnwinderCap returns how many leaked unwindings the service allows.
//
// Returns int64 which is the configured cap or the default.
func (l *VMLimits) leakedUnwinderCap() int64 {
	return positiveOr(l.LeakedUnwinderCap, defaultLeakedUnwinderCap)
}

// lockWalkMaxValues returns how many values the abandoned-lock check may visit.
//
// Returns int which is the configured budget or the default.
func (l *VMLimits) lockWalkMaxValues() int {
	return positiveOr(l.LockWalkMaxValues, defaultLockWalkMaxValues)
}

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

//go:build pipit_testhooks

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestArmAbortInjectionRejectsAnUnknownSite(t *testing.T) {
	err := ArmAbortInjection("no-such-site", 1)

	require.ErrorIs(t, err, errAbortInjectionUnknownSite)
	require.ErrorContains(t, err, `"no-such-site"`)
}

func TestAbortInjectionSitesNameTheSweptSites(t *testing.T) {
	sites := AbortInjectionSites()
	require.Len(t, sites, len(abortSweptSites))
	require.Contains(t, sites, abortSiteNames[abortSiteAllocBudget])
}

func TestAbortInjectionFiresOnItsTargetReach(t *testing.T) {
	require.NoError(t, ArmAbortInjection(abortSiteNames[abortSiteAllocBudget], 2))
	defer DisarmAbortInjection()
	arena := NewRegisterArena()
	injectAllocationAbort(arena)
	require.PanicsWithError(t, abortInjectedError(abortSiteAllocBudget).Error(), func() { injectAllocationAbort(arena) },
		"the second reach raises the arena-budget abort")
	report := DisarmAbortInjection()
	require.True(t, report.Fired)
	require.EqualValues(t, 2, report.Hits[abortSiteNames[abortSiteAllocBudget]])
	require.NotEmpty(t, report.FiredAt)
	injectAllocationAbort(arena)
}

func TestAbortInjectionRaisesTheHandOffAbort(t *testing.T) {
	require.NoError(t, ArmAbortInjection(abortSiteNames[abortSiteHandOffBudget], 1))
	defer DisarmAbortInjection()
	require.Panics(t, func() { injectHandOffAbort(NewRegisterArena()) })
}

func TestRecoverableAbortWaitsForAGuardedRun(t *testing.T) {
	require.NoError(t, ArmAbortInjection(abortSiteNames[abortSiteHandOffRecover], 1))
	defer DisarmAbortInjection()
	require.False(t, abortInjectInGuardedRun(), "a test is no guarded dispatch loop")
	injectHandOffAbort(NewRegisterArena())
	require.False(t, DisarmAbortInjection().Fired, "outside a guarded run the target moves on")
}

func TestAbortInjectedErrorMatchesTheRealLimit(t *testing.T) {
	require.ErrorIs(t, abortInjectedError(abortSiteAllocRecover), fault.ErrAllocationLimit)
	require.ErrorIs(t, abortInjectedError(abortSiteCost), fault.ErrCostBudgetExceeded)
	require.ErrorIs(t, abortInjectedError(abortSiteAllocBudget), fault.ErrArenaBudgetExceeded)
}

func TestInjectLockWindowStopCancelsTheExecution(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	vm := NewVM(ctx, NewGlobalStore(), nil)
	tracker := &ResourceTracker{}
	tracker.executionCancel.Store(&cancel)
	vm.Limits.Tracker = tracker
	require.Equal(t, opPanicError, vm.injectLockWindowStop())
	require.ErrorIs(t, context.Cause(ctx), errInjectedLockWindow)
	require.Error(t, vm.evalError)
}
